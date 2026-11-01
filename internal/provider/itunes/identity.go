package itunes

import (
	"context"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

var _ matching.CandidateResolver = (*Provider)(nil)

func (p *Provider) InspectCandidate(c provider.Candidate) matching.CandidateIdentity {
	if c.Provider != provider.ITunes || c.ReleaseRef == nil || c.MatchRef == "" {
		return matching.CandidateIdentity{State: matching.IdentityInvalid, Reason: "missing_itunes_identity"}
	}
	target, err := p.Refs().Target(*c.ReleaseRef, c.MatchRef)
	if err != nil || target.Track == nil || target.Track.Ref != c.MatchRef || target.Release.Ref != *c.ReleaseRef {
		return matching.CandidateIdentity{State: matching.IdentityInvalid, Reason: "invalid_itunes_track_release"}
	}
	return matching.CandidateIdentity{State: matching.IdentityExact, Track: *target.Track, Release: target.Release}
}

func (p *Provider) LoadDecisionFacts(ctx context.Context, request matching.DecisionRequest) ([]matching.TargetResolution, error) {
	out := make([]matching.TargetResolution, 0, len(request.Candidates))
	for _, c := range request.Candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		identity := p.InspectCandidate(c)
		if identity.State != matching.IdentityExact {
			out = append(out, matching.ExistingCandidateFacts(c, identity, request, nil))
			continue
		}
		snapshot, err := p.SnapshotFromCandidate(c)
		if err != nil {
			return nil, err
		}
		out = append(out, matching.ExistingCandidateFacts(c, identity, request, snapshot))
	}
	return out, nil
}
