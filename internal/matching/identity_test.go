package matching

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

type identityFake struct {
	id    provider.ProviderID
	state CandidateIdentityState
	load  func(DecisionRequest) ([]TargetResolution, error)
}

func (f identityFake) ID() provider.ProviderID { return f.id }
func (f identityFake) DisplayName() string     { return string(f.id) }
func (f identityFake) InspectCandidate(c provider.Candidate) CandidateIdentity {
	return CandidateIdentity{State: f.state, Track: domain.Identity{Provider: f.id, Ref: c.MatchRef}, Release: domain.Identity{Provider: f.id, Ref: deref(c.ReleaseRef)}}
}
func (f identityFake) LoadDecisionFacts(_ context.Context, r DecisionRequest) ([]TargetResolution, error) {
	return f.load(r)
}

type identityUnsupported struct{}

func (identityUnsupported) ID() provider.ProviderID { return "unsupported" }
func (identityUnsupported) DisplayName() string     { return "unsupported" }

func TestCandidateIdentityContract(t *testing.T) {
	for _, state := range []CandidateIdentityState{IdentityExact, IdentityUnresolved, IdentityInvalid, IdentityUnsupported} {
		for _, id := range []provider.ProviderID{"fake-one", "fake-two"} {
			r, err := NewCandidateSources(identityFake{id: id, state: state})
			if err != nil {
				t.Fatal(err)
			}
			release := "release"
			c := provider.Candidate{Provider: id, MatchRef: "track", ReleaseRef: &release}
			if got := r.Inspect(c); got.State != state {
				t.Fatal(got)
			}
			if state == IdentityExact {
				c.MatchRef = ""
				if got := r.Inspect(c); got.State != IdentityInvalid {
					t.Fatal(got)
				}
			}
		}
	}
	r, err := NewCandidateSources(identityUnsupported{})
	if err != nil {
		t.Fatal(err)
	}
	got := r.Inspect(provider.Candidate{Provider: "unsupported"})
	if got.State != IdentityUnsupported {
		t.Fatal(got)
	}
	out, err := r.Load(context.Background(), "unsupported", DecisionRequest{Candidates: []provider.Candidate{{Provider: "unsupported"}}})
	if err != nil || len(out) != 1 || out[0].State != TargetUnsupported {
		t.Fatal(out, err)
	}
	if _, err := NewCandidateSources(identityUnsupported{}, identityUnsupported{}); err == nil {
		t.Fatal("duplicate source accepted")
	}
}

func TestDecisionFactsContract(t *testing.T) {
	release := "release"
	c := provider.Candidate{Provider: "fake", MatchRef: "track", ReleaseRef: &release}
	request := DecisionRequest{Candidates: []provider.Candidate{c}}
	failure := errors.New("upstream failed")
	for _, tt := range []struct {
		name    string
		results []TargetResolution
		err     error
		bad     bool
	}{
		{"exact", []TargetResolution{{State: TargetExact, Candidates: []provider.Candidate{c}, Completeness: domain.Complete}}, nil, false},
		{"partial", []TargetResolution{{State: TargetInsufficient, Candidates: []provider.Candidate{c}, Completeness: domain.Partial}}, nil, false},
		{"missing", []TargetResolution{{State: TargetNotFound, Completeness: domain.Complete}}, nil, false},
		{"partial not found", []TargetResolution{{State: TargetNotFound, Completeness: domain.Partial}}, nil, true},
		{"empty exact", []TargetResolution{{State: TargetExact, Completeness: domain.Complete}}, nil, true},
		{"empty response", nil, nil, true},
		{"unknown state", []TargetResolution{{Completeness: domain.Complete}}, nil, true},
		{"wrong release", []TargetResolution{{State: TargetExact, Candidates: []provider.Candidate{c}, Release: &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: "fake", Ref: "other"}}, Completeness: domain.Complete}}, nil, true},
		{"source failure", nil, failure, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, err := NewCandidateSources(identityFake{id: "fake", state: IdentityExact, load: func(DecisionRequest) ([]TargetResolution, error) { return tt.results, tt.err }})
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.Load(context.Background(), "fake", request)
			if tt.bad && !errors.Is(err, ErrDecisionContract) {
				t.Fatalf("wanted contract error, got %v", err)
			}
			if !tt.bad && !errors.Is(err, tt.err) {
				t.Fatalf("source error changed: %v", err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var nilSources *CandidateSources
	if _, err := nilSources.Load(ctx, "fake", request); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
