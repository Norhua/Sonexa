package importer

import (
	"context"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// Runner fixtures exercise opaque source identities, not the MB UUID codec.
// Native MusicBrainz identity validation is covered by provider/App fixtures.
func (s *recordingSearcher) DisplayName() string { return "Runner fixture" }
func (s *recordingSearcher) InspectCandidate(c provider.Candidate) matching.CandidateIdentity {
	if c.Provider != s.ID() || c.MatchRef == "" || c.ReleaseRef == nil || *c.ReleaseRef == "" {
		return matching.CandidateIdentity{State: matching.IdentityInvalid}
	}
	return matching.CandidateIdentity{State: matching.IdentityExact,
		Track: domain.Identity{Provider: s.ID(), Ref: c.MatchRef}, Release: domain.Identity{Provider: s.ID(), Ref: *c.ReleaseRef}}
}
func (s *recordingSearcher) LoadDecisionFacts(ctx context.Context, request matching.DecisionRequest) ([]matching.TargetResolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]matching.TargetResolution, 0, len(request.Candidates))
	for _, c := range request.Candidates {
		snapshot, err := s.SnapshotFromCandidate(c)
		if err != nil {
			return nil, err
		}
		out = append(out, matching.ExistingCandidateFacts(c, s.InspectCandidate(c), request, snapshot))
	}
	return out, nil
}
