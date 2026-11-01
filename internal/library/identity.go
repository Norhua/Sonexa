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
	position := matching.PositionRequirement{}
	if itemID != 0 {
		item, err := s.store.GetItem(itemID)
		if err != nil {
			return c, err
		}
		if item != nil && item.Profile != nil {
			position = matching.PositionRequirement{Disc: item.Profile.Disc, Track: item.Profile.Track}
		}
	}
	return s.resolveCandidatePosition(ctx, c, position)
}

func (s *Service) resolveCandidatePosition(ctx context.Context, c provider.Candidate, position matching.PositionRequirement) (provider.Candidate, error) {
	r, err := s.identitySources()
	if err != nil {
		return c, err
	}
	if r.Inspect(c).State == matching.IdentityExact {
		return c, nil
	}
	request := matching.DecisionRequest{Candidates: []provider.Candidate{c}, Position: position}
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
