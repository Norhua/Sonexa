package library

import (
	"context"
	"errors"
	"strings"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
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
			if supported && matching.PositionUnique(input, target, fact.Release) {
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
	expected := ownerships[domain.Identity{Provider: selected.Candidate.Provider, Ref: selected.Candidate.MatchRef}]
	selected, err = s.prepareAutomaticTarget(ctx, selected, itemID, revision, expected)
	if err != nil {
		return nil, err
	}
	return &selected, nil
}
