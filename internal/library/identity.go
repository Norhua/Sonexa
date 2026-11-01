package library

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// TargetResolutionError carries explicit alternatives back to the caller. It
// never grants an admission plan; the user must select a real track first.
type TargetResolutionError struct{ Resolution matching.TargetResolution }

func (e *TargetResolutionError) Error() string {
	return fmt.Sprintf("候选目标尚未唯一解析（%s）：%s", e.Resolution.State, e.Resolution.Reason)
}

func (s *Service) identitySources() (*matching.CandidateSources, error) {
	s.mu.Lock()
	identities, registry := s.identities, s.registry
	s.mu.Unlock()
	if identities != nil {
		return identities, nil
	}
	// Non-App consumers may inject only a metadata registry. Capability
	// discovery is interface-based, never source-specific ID dispatch.
	inputs := []provider.Upstream{}
	for _, p := range registry.List() {
		inputs = append(inputs, p)
	}
	return matching.NewCandidateSources(inputs...)
}

func (s *Service) validateSeedIdentity(seed domain.AdmissionSeed) error {
	t := seed.Snapshot.Track
	if t == nil || (t.Identity.Provider == "" && t.Identity.Ref == "") {
		return nil
	}
	if t.Release == nil || seed.Snapshot.Release == nil || *t.Release != seed.Snapshot.Release.Identity {
		return fmt.Errorf("曲目缺少一致的精确发行关系")
	}
	r, err := s.identitySources()
	if err != nil {
		return err
	}
	c := provider.Candidate{Provider: t.Identity.Provider, MatchRef: t.Identity.Ref, ReleaseRef: &t.Release.Ref,
		DiscNumber: ptrField(t.DiscNumber), TrackNumber: ptrField(t.TrackNumber)}
	if t.Recording.State == domain.Value {
		c.RecordingRef = &t.Recording.Value.Ref
	}
	if got := r.Inspect(c); got.State != matching.IdentityExact {
		return &TargetResolutionError{Resolution: matching.TargetResolution{State: matching.TargetInvalid, Completeness: domain.Unloaded, Reason: string(got.State) + ":" + got.Reason}}
	}
	return nil
}

func (s *Service) resolveAdmissionCandidate(ctx context.Context, c provider.Candidate, itemID int64) (provider.Candidate, error) {
	r, err := s.identitySources()
	if err != nil {
		return c, err
	}
	if r.Inspect(c).State == matching.IdentityExact {
		return c, nil
	}
	request := matching.DecisionRequest{Candidates: []provider.Candidate{c}}
	if itemID != 0 {
		item, err := s.store.GetItem(itemID)
		if err != nil {
			return c, err
		}
		if item != nil && item.Profile != nil {
			request.Position = matching.PositionRequirement{Disc: item.Profile.Disc, Track: item.Profile.Track}
		}
	}
	resolved, err := r.Load(ctx, c.Provider, request)
	if err != nil {
		return c, err
	}
	if len(resolved) != 1 {
		return c, matching.ErrDecisionContract
	}
	if resolved[0].State != matching.TargetExact {
		return c, &TargetResolutionError{Resolution: resolved[0]}
	}
	return resolved[0].Candidates[0], nil
}

// resolveSelectionIdentities expands unresolved seeds in a single bounded
// request per source so sibling candidates share release responses. Any
// unresolved competitor keeps the whole automatic decision in review.
func (s *Service) resolveSelectionIdentities(ctx context.Context, scored []matching.ScoredCandidate, profile *matching.InputProfile) ([]matching.ScoredCandidate, bool, error) {
	r, err := s.identitySources()
	if err != nil {
		return nil, false, err
	}
	requests := map[provider.ProviderID]matching.DecisionRequest{}
	positions := map[provider.ProviderID][]int{}
	order := []provider.ProviderID{}
	for i, sc := range scored {
		if r.Inspect(sc.Candidate).State == matching.IdentityExact {
			continue
		}
		id := sc.Candidate.Provider
		if _, ok := requests[id]; !ok {
			order = append(order, id)
		}
		request := requests[id]
		request.Candidates = append(request.Candidates, sc.Candidate)
		if profile != nil {
			request.Position = matching.PositionRequirement{Disc: profile.Disc, Track: profile.Track}
		}
		requests[id] = request
		positions[id] = append(positions[id], i)
	}
	replacements := map[int]matching.TargetResolution{}
	unresolved := false
	for _, id := range order {
		results, err := r.Load(ctx, id, requests[id])
		if err != nil {
			return nil, false, err
		}
		for j, res := range results {
			replacements[positions[id][j]] = res
			if res.State != matching.TargetExact {
				unresolved = true
			}
		}
	}
	out := []matching.ScoredCandidate{}
	for i, sc := range scored {
		res, ok := replacements[i]
		if !ok {
			out = append(out, sc)
			continue
		}
		if len(res.Candidates) == 0 {
			out = append(out, sc)
			continue
		}
		for _, c := range res.Candidates {
			next := sc
			next.Candidate = c
			if profile != nil {
				// Query values are comparison evidence only, not source fields.
				evidence := matching.CheckMetadataEvidence(*profile, matching.MetadataFacts{Candidate: c, Source: string(c.Provider) + ":" + c.MatchRef,
					AlbumComplete: res.Release != nil && res.Release.Title.State == domain.Value, PositionVerified: res.State == matching.TargetExact})
				if evidence.Eligibility != matching.MetadataEligible {
					unresolved = true
				}
			}
			out = append(out, next)
		}
	}
	return out, unresolved, nil
}
