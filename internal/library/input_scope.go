package library

import (
	"context"
	"encoding/json"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// Legacy single-row fields remain an adapter for Runner/editor callers. The
// sealed seed and baseline always carry the same explicit InputScope.
func (s *Service) prepareInputScope(seed *domain.AdmissionSeed) error {
	if seed.InputScope == nil {
		if seed.ImportItemID == 0 {
			if seed.ItemRevision != 0 {
				return store.ErrPlanStale
			}
			return nil
		}
		scope, err := s.store.ReadSingleInputScope(seed.ImportItemID, seed.ItemRevision)
		if err != nil {
			return err
		}
		seed.InputScope = scope
	} else if err := s.store.CheckInputScope(seed.InputScope); err != nil {
		return err
	}
	if seed.InputScope.Kind == domain.InputScopeSingle {
		member := seed.InputScope.Members[0]
		if seed.ImportItemID != 0 && seed.ImportItemID != member.ItemID || seed.ItemRevision != 0 && seed.ItemRevision != member.InputRevision {
			return store.ErrPlanStale
		}
		seed.ImportItemID, seed.ItemRevision = member.ItemID, member.InputRevision
	} else if seed.ImportItemID != 0 || seed.ItemRevision != 0 {
		return store.ErrPlanStale
	}
	return nil
}

func (s *Service) resolveScopedAdmissionCandidate(ctx context.Context, c provider.Candidate, scope *domain.InputScope) (provider.Candidate, error) {
	if scope == nil {
		return s.resolveAdmissionCandidate(ctx, c, 0)
	}
	r, err := s.identitySources()
	if err != nil {
		return c, err
	}
	if r.Inspect(c).State == matching.IdentityExact {
		return c, nil
	}
	var result provider.Candidate
	// Resolve each distinct position requirement once. Identical rows share
	// the same resolution; no row can lend its position to a different one.
	resolvedPositions := map[string]provider.Candidate{}
	for i, member := range scope.Members {
		item, err := s.store.GetItem(member.ItemID)
		if err != nil {
			return c, err
		}
		if item == nil || item.Revision != member.InputRevision {
			return c, store.ErrPlanStale
		}
		profile := store.ItemProfile(item)
		position := matching.PositionRequirement{Disc: profile.Disc, Track: profile.Track}
		// Parsing text/precision is not a distinct position demand.
		key, err := json.Marshal([]any{position.Disc.State, position.Disc.Value, position.Track.State, position.Track.Value})
		if err != nil {
			return c, err
		}
		resolved, ok := resolvedPositions[string(key)]
		if !ok {
			resolved, err = s.resolveCandidatePosition(ctx, c, position)
			if err != nil {
				return c, err
			}
			resolvedPositions[string(key)] = resolved
		}
		if i > 0 && (resolved.Provider != result.Provider || resolved.MatchRef != result.MatchRef ||
			resolved.ReleaseRef == nil || result.ReleaseRef == nil || *resolved.ReleaseRef != *result.ReleaseRef) {
			return c, &TargetResolutionError{Resolution: matching.TargetResolution{State: matching.TargetAmbiguous,
				Completeness: domain.Unloaded, Reason: "问题成员未解析到同一精确位置", Candidates: []provider.Candidate{result, resolved}}}
		}
		result = resolved
	}
	return result, nil
}
