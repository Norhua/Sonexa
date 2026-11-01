package library

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// selectionEvidence filters targets before any edition/group preference. The
// display pool keeps excluded candidates; a score never grants eligibility.
type selectionEvidence struct {
	eligible  []matching.ScoredCandidate
	display   []matching.ScoredCandidate
	summaries map[domain.Identity]domain.ReleaseSummary
	reasons   []matching.DecisionReason
	pending   bool
}

func (s *Service) constrainSelection(ctx context.Context, scored []matching.ScoredCandidate, input matching.InputProfile) (selectionEvidence, error) {
	out := selectionEvidence{display: scored, summaries: map[domain.Identity]domain.ReleaseSummary{}}
	sources, err := s.identitySources()
	if err != nil {
		return out, err
	}
	candidates := make([]provider.Candidate, len(scored))
	for i := range scored {
		candidates[i] = scored[i].Candidate
	}
	initial, err := s.CandidateFacts(ctx, input, candidates)
	if err != nil {
		return out, err
	}
	// Gather only facts that can change a non-excluded target. Batching by
	// source allows its adapter to share responses across the same release.
	requests := map[provider.ProviderID]matching.DecisionRequest{}
	positions := map[provider.ProviderID][]int{}
	order := []provider.ProviderID{}
	needed := map[int]bool{}
	for _, target := range initial.Targets {
		evidence := target.Check(input)
		if evidence.Eligibility != matching.MetadataUnresolved {
			continue
		}
		for _, fact := range target.Facts {
			if fact.Kind != "remote" {
				continue
			}
			for i, c := range candidates {
				if needed[i] || c.Provider != fact.Metadata.Candidate.Provider || c.MatchRef != fact.Metadata.Candidate.MatchRef || value(c.ReleaseRef) != value(fact.Metadata.Candidate.ReleaseRef) {
					continue
				}
				// An explicit input mismatch does not need identity/summary I/O.
				if matching.CheckMetadataEvidence(input, fact.Metadata).Eligibility == matching.MetadataExcluded {
					continue
				}
				needPosition := sources.Inspect(c).State != matching.IdentityExact || c.DiscNumber == nil || c.TrackNumber == nil
				needSummary := c.AlbumTitle == nil || *c.AlbumTitle == ""
				needDescription := c.Title == nil || (input.Artist.Text.Raw != "" && c.ArtistName == nil) || (input.Duration.State == matching.ValueKnown && c.DurationMS == nil)
				if !needPosition && !needSummary && !needDescription {
					continue // unexplained conflict is not fixed by speculative I/O
				}
				if _, ok := requests[c.Provider]; !ok {
					order = append(order, c.Provider)
				}
				request := requests[c.Provider]
				request.Candidates = append(request.Candidates, c)
				request.Position = matching.PositionRequirement{Disc: input.Disc, Track: input.Track}
				request.NeedTracklist = request.NeedTracklist || needPosition || needDescription
				request.NeedSummary = request.NeedSummary || needSummary
				requests[c.Provider] = request
				positions[c.Provider] = append(positions[c.Provider], i)
				needed[i] = true
			}
		}
	}
	resolved := map[int]matching.TargetResolution{}
	for _, source := range order {
		results, err := sources.Load(ctx, source, requests[source])
		if err != nil {
			return out, err
		}
		for i, result := range results {
			resolved[positions[source][i]] = result
		}
	}
	out.display = []matching.ScoredCandidate{}
	loaded := map[string]*domain.ReleaseSnapshot{}
	blocked := map[string]string{}
	absent := map[string]bool{}
	for i, original := range scored {
		result, ok := resolved[i]
		if !ok {
			out.display = append(out.display, original)
			continue
		}
		if result.Release != nil {
			out.summaries[result.Release.Identity] = result.Release.Summary()
		}
		if len(result.Candidates) == 0 {
			out.display = append(out.display, original)
			if result.State == matching.TargetNotFound && result.Completeness == domain.Complete {
				absent[candidateKey(original.Candidate)] = true
			} else {
				blocked[candidateKey(original.Candidate)] = result.Reason
			}
			continue
		}
		for _, candidate := range result.Candidates {
			next := original
			next.Candidate = candidate
			out.display = append(out.display, next)
			loaded[candidateKey(candidate)] = result.Release
			if result.State != matching.TargetExact && result.State != matching.TargetAmbiguous {
				blocked[candidateKey(candidate)] = result.Reason
			}
		}
	}
	candidates = nil
	for _, sc := range out.display {
		if !absent[candidateKey(sc.Candidate)] {
			candidates = append(candidates, sc.Candidate)
		}
	}
	for i, sc := range scored {
		if _, ok := resolved[i]; ok && sources.Inspect(sc.Candidate).State == matching.IdentityExact && !absent[candidateKey(sc.Candidate)] {
			// Keep the original exact source view too. Loading new details is
			// not permission to erase a conflicting search/snapshot assertion.
			candidates = append(candidates, sc.Candidate)
		}
	}
	pool, err := s.CandidateFacts(ctx, input, candidates)
	if err != nil {
		return out, err
	}
	out.pending = pool.Truncated
	if pool.Truncated {
		out.reasons = append(out.reasons, matching.DecisionReason{Code: "local_recall_truncated"})
	}
	for _, target := range pool.Targets {
		if absent[string(target.Identity.Track.Provider)+"\x00"+target.Identity.Release.Ref+"\x00"+target.Identity.Track.Ref] {
			target.Issues = append(target.Issues, matching.DecisionReason{Code: "local_target_missing_from_source"})
		}
		hasRemote := false
		for i := range target.Facts {
			fact := &target.Facts[i]
			key := candidateKey(fact.Metadata.Candidate)
			if fact.Kind == "remote" {
				hasRemote = true
				if release := loaded[key]; release != nil {
					fact.Release = release
				}
				if reason, ok := blocked[key]; ok {
					target.Issues = append(target.Issues, matching.DecisionReason{Code: "decision_facts_unavailable", Detail: reason})
				}
			}
		}
		// A remote miss does not turn local recall into a complete search.
		// Keep the local-only album/position threshold after rejoining facts.
		// Likewise a remote row cannot hide competing positions already known
		// from a complete release list (including positions not collected).
		positionUnresolved := false
		for _, fact := range target.Facts {
			if fact.Release != nil && fact.Release.TracklistCompleteness == domain.Complete && !matching.PositionUnique(input, target, fact.Release) {
				positionUnresolved = true
			}
		}
		evidence := target.Check(input)
		if evidence.Eligibility != matching.MetadataExcluded {
			if !hasRemote && input.Album.Raw == "" {
				evidence.Eligibility = matching.MetadataUnresolved
				evidence.Reasons = append(evidence.Reasons, matching.DecisionReason{Code: "local_target_album_required"})
			}
			if positionUnresolved {
				evidence.Eligibility = matching.MetadataUnresolved
				evidence.Reasons = append(evidence.Reasons, matching.DecisionReason{Code: "position_ambiguous"})
			}
		}
		out.reasons = append(out.reasons, evidence.Reasons...)
		for _, facet := range evidence.Facets {
			out.reasons = append(out.reasons, facet.Reasons...)
		}
		switch evidence.Eligibility {
		case matching.MetadataExcluded:
			continue
		case matching.MetadataUnresolved:
			out.pending = true
			continue
		}
		var chosen *matching.CandidateFact
		for i := range target.Facts {
			fact := &target.Facts[i]
			if fact.Kind == "current" || evidence.Facets[i].Eligibility != matching.MetadataEligible {
				continue
			}
			chosen = fact
			break
		}
		if chosen == nil {
			// A current-name view may establish correspondence to an already
			// verified local position; do not copy its names into source facts.
			for _, fact := range target.Facts {
				if fact.Kind != "current" && fact.Metadata.PositionVerified && fact.Release != nil {
					copy := fact
					chosen = &copy
					break
				}
			}
		}
		if chosen == nil {
			out.pending = true
			continue
		}
		c := chosen.Metadata.Candidate
		if chosen.Release != nil {
			out.summaries[chosen.Release.Identity] = chosen.Release.Summary()
		}
		sc := matching.ScoredCandidate{Candidate: c, MatchedVariant: "本地有效事实"}
		found := false
		for _, display := range out.display {
			if candidateKey(display.Candidate) == candidateKey(c) {
				sc.Score, sc.MatchedVariant = display.Score, display.MatchedVariant
				found = true
				break
			}
		}
		if !found {
			sc.Score = matching.DefaultWeights().ScoreCandidate(provider.Query{Title: input.Title.Raw, Artist: input.Artist.Text.Raw, Album: input.Album.Raw}, c)
			out.display = append(out.display, sc)
		}
		out.eligible = append(out.eligible, sc)
	}
	// Identity expansion can make several seeds name the same exact track.
	// Keep all source views in the evidence pool above, one row in the UI.
	seenDisplay := map[string]bool{}
	display := out.display[:0]
	for _, sc := range out.display {
		if sources.Inspect(sc.Candidate).State == matching.IdentityExact {
			key := candidateKey(sc.Candidate)
			if seenDisplay[key] {
				continue
			}
			seenDisplay[key] = true
		}
		display = append(display, sc)
	}
	out.display = display
	if out.pending {
		out.reasons = append(out.reasons, matching.DecisionReason{Code: "unresolved_competitor", Detail: fmt.Sprintf("eligible=%d", len(out.eligible))})
	}
	return out, nil
}
