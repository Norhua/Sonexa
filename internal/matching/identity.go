package matching

import (
	"context"
	"errors"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

type CandidateIdentityState string

const (
	IdentityExact       CandidateIdentityState = "exact"
	IdentityUnresolved  CandidateIdentityState = "unresolved"
	IdentityInvalid     CandidateIdentityState = "invalid"
	IdentityUnsupported CandidateIdentityState = "unsupported"
)

type CandidateIdentity struct {
	State   CandidateIdentityState `json:"state"`
	Track   domain.Identity        `json:"track"`
	Release domain.Identity        `json:"release"`
	Reason  string                 `json:"reason,omitempty"`
}

type PositionRequirement struct {
	Disc  NumberInput
	Track NumberInput
}

type DecisionRequest struct {
	Candidates    []provider.Candidate
	Position      PositionRequirement
	NeedTracklist bool
	NeedSummary   bool
}

type TargetState string

const (
	TargetExact        TargetState = "exact"
	TargetAmbiguous    TargetState = "ambiguous"
	TargetNotFound     TargetState = "not_found"
	TargetInsufficient TargetState = "insufficient"
	TargetInvalid      TargetState = "invalid"
	TargetUnsupported  TargetState = "unsupported"
)

type TargetResolution struct {
	State        TargetState             `json:"state"`
	Candidates   []provider.Candidate    `json:"candidates"`
	Release      *domain.ReleaseSnapshot `json:"release,omitempty"`
	Completeness domain.Completeness     `json:"completeness"`
	Reason       string                  `json:"reason,omitempty"`
}

// CandidateResolver is declared by the consumer. InspectCandidate MUST be
// network-free. LoadDecisionFacts returns one result per input, in order, and
// may share responses within the bounded request. Neither operation writes
// library entities, determines ownership, or incorporates query text as facts.
type CandidateResolver interface {
	provider.Upstream
	InspectCandidate(provider.Candidate) CandidateIdentity
	LoadDecisionFacts(context.Context, DecisionRequest) ([]TargetResolution, error)
}

var ErrDecisionContract = errors.New("候选身份/决策资料契约不一致")

// CandidateSources is immutable after assembly. Unsupported capabilities are
// explicit, not empty successful results and never inferred from source IDs.
type CandidateSources struct {
	sources map[provider.ProviderID]CandidateResolver
}

func NewCandidateSources(inputs ...provider.Upstream) (*CandidateSources, error) {
	r := &CandidateSources{sources: map[provider.ProviderID]CandidateResolver{}}
	seen := map[provider.ProviderID]bool{}
	for _, input := range inputs {
		if input == nil || input.ID() == "" {
			return nil, fmt.Errorf("候选身份来源缺少 ID")
		}
		id := input.ID()
		if seen[id] {
			return nil, fmt.Errorf("重复候选身份来源 %q", id)
		}
		seen[id] = true
		if resolver, ok := input.(CandidateResolver); ok {
			r.sources[id] = resolver
		}
	}
	return r, nil
}

func (r *CandidateSources) Inspect(c provider.Candidate) CandidateIdentity {
	if r == nil || r.sources[c.Provider] == nil {
		return CandidateIdentity{State: IdentityUnsupported, Reason: "identity_capability_unavailable"}
	}
	out := r.sources[c.Provider].InspectCandidate(c)
	switch out.State {
	case IdentityExact:
		if out.Track.Provider != c.Provider || out.Track.Ref == "" || out.Release.Provider != c.Provider || out.Release.Ref == "" ||
			out.Track.Ref != c.MatchRef || out.Release.Ref != deref(c.ReleaseRef) {
			return CandidateIdentity{State: IdentityInvalid, Reason: "identity_contract_mismatch"}
		}
	case IdentityUnresolved, IdentityInvalid, IdentityUnsupported:
	default:
		return CandidateIdentity{State: IdentityInvalid, Reason: "unknown_identity_state"}
	}
	return out
}

func (r *CandidateSources) Load(ctx context.Context, id provider.ProviderID, request DecisionRequest) ([]TargetResolution, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, c := range request.Candidates {
		if c.Provider != id {
			return nil, ErrDecisionContract
		}
	}
	if r == nil || r.sources[id] == nil {
		out := make([]TargetResolution, len(request.Candidates))
		for i := range out {
			out[i] = TargetResolution{State: TargetUnsupported, Completeness: domain.Unloaded, Reason: "identity_capability_unavailable"}
		}
		return out, nil
	}
	out, err := r.sources[id].LoadDecisionFacts(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(request.Candidates) {
		return nil, ErrDecisionContract
	}
	for i, res := range out {
		if !validTargetState(res.State) {
			return nil, ErrDecisionContract
		}
		switch res.Completeness {
		case domain.Complete, domain.Partial, domain.Unloaded:
		default:
			return nil, ErrDecisionContract
		}
		if (res.State == TargetExact && len(res.Candidates) != 1) || (res.State == TargetAmbiguous && len(res.Candidates) < 2) {
			return nil, ErrDecisionContract
		}
		if res.State == TargetNotFound && (res.Completeness != domain.Complete || len(res.Candidates) != 0) {
			return nil, ErrDecisionContract
		}
		release := domain.Identity{Provider: id, Ref: deref(request.Candidates[i].ReleaseRef)}
		if res.Release != nil && res.Release.Identity != release {
			return nil, ErrDecisionContract
		}
		for _, c := range res.Candidates {
			check := r.Inspect(c)
			if check.State != IdentityExact || check.Release != release {
				return nil, ErrDecisionContract
			}
		}
	}
	return out, nil
}

func validTargetState(state TargetState) bool {
	switch state {
	case TargetExact, TargetAmbiguous, TargetNotFound, TargetInsufficient, TargetInvalid, TargetUnsupported:
		return true
	default:
		return false
	}
}
