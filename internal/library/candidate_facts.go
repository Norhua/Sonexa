package library

import (
	"context"

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
	pool := matching.BuildCandidatePool(local[0], remote, sources, s.store)
	if err := ctx.Err(); err != nil {
		return matching.CandidatePool{}, err
	}
	return pool, nil
}
