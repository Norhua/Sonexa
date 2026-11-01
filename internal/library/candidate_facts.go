package library

import (
	"context"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// CandidateFacts collects coherent current/local/remote views for one input.
// It does not select a version, perform upstream I/O or grant an admission plan.
// Runner's automatic decision pipeline consumes this in the following stage.
func (s *Service) CandidateFacts(ctx context.Context, input matching.InputProfile, remote []provider.Candidate) (matching.CandidatePool, error) {
	if err := ctx.Err(); err != nil {
		return matching.CandidatePool{}, err
	}
	sources, err := s.identitySources()
	if err != nil {
		return matching.CandidatePool{}, err
	}
	var recall matching.LocalRecall = s.store
	local, err := recall.RecallLocal(ctx, []matching.InputProfile{input}, 100)
	if err != nil {
		return matching.CandidatePool{}, err
	}
	identities := []domain.Identity{}
	for _, candidate := range remote {
		if identity := sources.Inspect(candidate); identity.State == matching.IdentityExact {
			identities = append(identities, identity.Track)
		}
	}
	var facts matching.LocalFactLookup = s.store
	exact, err := facts.ReadLocalFacts(ctx, identities)
	if err != nil {
		return matching.CandidatePool{}, err
	}
	positions := map[int64]int{}
	for i, record := range local[0].Records {
		positions[record.TrackID] = i
	}
	for _, record := range exact {
		if i, found := positions[record.TrackID]; found {
			local[0].Records[i] = record // freshest whole view, never field merging
		} else {
			positions[record.TrackID] = len(local[0].Records)
			local[0].Records = append(local[0].Records, record)
		}
	}
	pool := matching.BuildCandidatePool(local[0], remote, sources, s.store)
	if err := ctx.Err(); err != nil {
		return matching.CandidatePool{}, err
	}
	return pool, nil
}
