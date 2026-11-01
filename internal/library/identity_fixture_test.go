package library

import (
	"context"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// Unit-test source IDs are opaque to the business. Explicitly implement the
// same capability contract instead of bypassing validation in production.
func (p *summaryProvider) DisplayName() string { return "Test source" }
func (p *summaryProvider) InspectCandidate(c provider.Candidate) matching.CandidateIdentity {
	if c.Provider != p.ID() || c.MatchRef == "" || c.ReleaseRef == nil || *c.ReleaseRef == "" {
		return matching.CandidateIdentity{State: matching.IdentityInvalid}
	}
	return matching.CandidateIdentity{State: matching.IdentityExact,
		Track: domain.Identity{Provider: p.ID(), Ref: c.MatchRef}, Release: domain.Identity{Provider: p.ID(), Ref: *c.ReleaseRef}}
}
func (p *summaryProvider) LoadDecisionFacts(ctx context.Context, request matching.DecisionRequest) ([]matching.TargetResolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]matching.TargetResolution, 0, len(request.Candidates))
	for _, c := range request.Candidates {
		snapshot, err := p.SnapshotFromCandidate(c)
		if err != nil {
			return nil, err
		}
		out = append(out, matching.ExistingCandidateFacts(c, p.InspectCandidate(c), request, snapshot))
	}
	return out, nil
}
