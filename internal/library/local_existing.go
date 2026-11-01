package library

import (
	"context"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
)

// PrepareLocalExisting uses the same metadata evidence as the full pipeline,
// but succeeds without upstream I/O only when the input names its target album
// and complete local facts establish the position. A nil selection means more
// facts are needed, not "not owned". No historical input/selection is read.
func (s *Service) PrepareLocalExisting(ctx context.Context, input matching.InputProfile, itemID, revision int64) (*Selection, error) {
	pool, err := s.CandidateFacts(ctx, input, nil)
	if err != nil {
		return nil, err
	}
	if pool.Truncated || len(pool.Targets) == 0 || input.Album.Raw == "" {
		return nil, nil
	}
	prepared := []selectionCandidate{}
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
			if localPositionUnique(input, target, fact.Release) {
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
		groups[resolution.Groups[0].ID] = true
		prepared = append(prepared, selectionCandidate{candidate: chosen.Metadata.Candidate, summary: chosen.Release.Summary(), groups: groups, existing: true})
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
	for _, entry := range release.Tracklist.Value {
		if entry.Track.State != domain.Value || entry.Track.Value.Provider != release.Identity.Provider || entry.Track.Value.Ref == "" || entry.DiscNumber <= 0 || entry.TrackNumber <= 0 {
			return false
		}
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
		evidence := matching.CheckMetadataEvidence(input, matching.MetadataFacts{Candidate: candidate, Source: "local:competing_position", AlbumComplete: release.Title.State == domain.Value, PositionVerified: true})
		if evidence.Eligibility != matching.MetadataExcluded {
			return false
		}
	}
	return found
}
