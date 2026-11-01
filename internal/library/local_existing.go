package library

import (
	"context"
	"errors"
	"strings"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

// PrepareLocalExisting uses the same metadata evidence as the full pipeline,
// but succeeds without upstream I/O only when the input names its target album
// and complete local facts establish the position. A nil selection means more
// facts are needed, not "not owned". No historical input/selection is read.
func (s *Service) PrepareLocalExisting(ctx context.Context, input matching.InputProfile, itemID, revision int64) (*Selection, error) {
	if input.RuleVersion != matching.MetadataRuleVersion {
		return nil, errors.New("输入证据版本已失效，需要重新解析")
	}
	pool, err := s.CandidateFacts(ctx, input, nil)
	if err != nil {
		return nil, err
	}
	if pool.Truncated || len(pool.Targets) == 0 || strings.TrimSpace(input.Album.Raw) == "" {
		return nil, nil
	}
	prepared := []selectionCandidate{}
	ownerships := map[domain.Identity]matching.LocalMatch{}
	var commonGroup int64
	for _, target := range pool.Targets {
		evidence := target.Check(input)
		if evidence.Eligibility == matching.MetadataExcluded {
			continue
		}
		if evidence.Eligibility != matching.MetadataEligible || target.Ownership.State != matching.OwnershipOwned {
			return nil, nil
		}
		var chosen *matching.CandidateFact
		for i := range target.Facts {
			fact := &target.Facts[i]
			if fact.Kind == "current" || fact.Release == nil || !fact.Metadata.PositionVerified {
				continue
			}
			// Names can be established by the whole current view, while its
			// exact position is independently verified against this snapshot.
			// This is a link between fact views, never copying their fields.
			supported := evidence.Facets[i].Eligibility == matching.MetadataEligible
			for j, current := range target.Facts {
				if current.Kind == "current" && current.Metadata.PositionVerified && evidence.Facets[j].Eligibility == matching.MetadataEligible {
					supported = true
				}
			}
			if supported && localPositionUnique(input, target, fact.Release) {
				chosen = fact
				break
			}
		}
		if chosen == nil {
			return nil, nil
		}
		groups := map[int64]bool{}
		// Resolve only existing grouping facts; no member creation or requests.
		resolution, err := s.store.ResolveGroup(domain.Snapshot{Release: chosen.Release})
		if err != nil {
			return nil, err
		}
		if resolution.Insufficient || len(resolution.Groups) != 1 {
			return nil, nil
		}
		groupID := resolution.Groups[0].ID
		if commonGroup != 0 && commonGroup != groupID {
			return nil, nil // no transitive name bridge between different local groups
		}
		commonGroup = groupID
		groups[groupID] = true
		ownerships[target.Identity.Track] = target.Ownership
		prepared = append(prepared, selectionCandidate{candidate: chosen.Metadata.Candidate, summary: chosen.Release.Summary(), groups: groups, existing: true, owned: true})
	}
	if len(prepared) == 0 {
		return nil, nil
	}
	selected := selectCandidate(prepared)
	if selected.Candidate == nil || selected.Reason != "" {
		return nil, nil
	}
	plan, err := s.PrepareSkipExisting(ctx, *selected.Candidate, itemID, revision)
	if err != nil {
		return nil, err
	}
	// Skip preparation re-reads current bindings. A newer binding revision is
	// not permission to use the older automatic evidence we just checked.
	expected := ownerships[domain.Identity{Provider: selected.Candidate.Provider, Ref: selected.Candidate.MatchRef}]
	if plan.Baseline.ExistingTrackID != expected.TrackID || plan.Baseline.ExistingTrackRevision != expected.Revision || plan.Baseline.ExistingReleaseRevision != expected.ReleaseRevision {
		return nil, store.ErrPlanStale
	}
	selected.Plan = plan
	return &selected, nil
}

// Inspect every position that might be the input, not merely the collected
// position. The same recording elsewhere remains ambiguous even if unowned.
func localPositionUnique(input matching.InputProfile, target matching.CandidateTarget, release *domain.ReleaseSnapshot) bool {
	if release.TracklistCompleteness != domain.Complete || release.Tracklist.State != domain.Value || len(release.Tracklist.Value) == 0 {
		return false
	}
	found := false
	positions := map[[2]int]bool{}
	identities := map[domain.Identity]bool{}
	for _, entry := range release.Tracklist.Value {
		if entry.Track.State != domain.Value || entry.Track.Value.Provider != release.Identity.Provider || entry.Track.Value.Ref == "" || entry.DiscNumber <= 0 || entry.TrackNumber <= 0 {
			return false
		}
		position := [2]int{entry.DiscNumber, entry.TrackNumber}
		if positions[position] || identities[entry.Track.Value] {
			return false
		}
		positions[position], identities[entry.Track.Value] = true, true
		if entry.Track.Value == target.Identity.Track {
			if found {
				return false
			}
			found = true
			continue
		}
		track := &domain.TrackSnapshot{Identity: entry.Track.Value, Release: &release.Identity, Title: entry.Title, Credit: entry.Credit,
			DiscNumber: domain.Known(entry.DiscNumber), TrackNumber: domain.Known(entry.TrackNumber), DurationMS: entry.DurationMS, Recording: entry.Recording}
		candidate := matching.SnapshotCandidate(track, release)
		// Album correspondence was established for this exact release, possibly
		// by its current user name. Reusing the old source album name here could
		// incorrectly exclude every other position in the same release.
		positionInput := input
		positionInput.Album = matching.ParseMetadataText("", matching.MetadataAlbum)
		// Equal recording is not track identity, but it is enough to keep a
		// repeated position in contention even if one source title is an alias.
		sameRecording := false
		for _, fact := range target.Facts {
			ref := fact.Metadata.Candidate.RecordingRef
			if ref != nil && *ref != "" && candidate.RecordingRef != nil && *ref == *candidate.RecordingRef {
				sameRecording = true
			}
		}
		if sameRecording {
			if input.Disc.State == matching.ValueKnown && input.Disc.Value != int64(entry.DiscNumber) ||
				input.Track.State == matching.ValueKnown && input.Track.Value != int64(entry.TrackNumber) {
				continue
			}
			return false
		}
		evidence := matching.CheckMetadataEvidence(positionInput, matching.MetadataFacts{Candidate: candidate, Source: "local:competing_position", AlbumComplete: release.Title.State == domain.Value, PositionVerified: true})
		if evidence.Eligibility != matching.MetadataExcluded {
			return false
		}
	}
	return found
}
