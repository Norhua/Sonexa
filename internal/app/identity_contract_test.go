package app

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

func TestAlreadyExactProviderIdentityContractsWithoutNetwork(t *testing.T) {
	// Nil clients deliberately make any accidental network call fail.
	for _, tt := range []struct {
		source         matching.CandidateResolver
		track, release string
		invalid        []string
	}{
		{&itunes.Provider{}, "jp:123", "jp:456", []string{"US:123", "us:123", "123", "jp:no"}},
		{&qqmusic.Provider{}, "000W46Kt3BWQ0i", "001Frvoh4E456m", []string{"123", "invalid-mid!", ""}},
	} {
		t.Run(string(tt.source.ID()), func(t *testing.T) {
			registry, err := matching.NewCandidateSources(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			title, album := "Song", "Album"
			n := 10
			c := provider.Candidate{Provider: tt.source.ID(), MatchRef: tt.track, ReleaseRef: &tt.release, Title: &title, AlbumTitle: &album, ReleaseTrackTotal: &n}
			if got := registry.Inspect(c); got.State != matching.IdentityExact {
				t.Fatal(got)
			}
			for _, request := range []matching.DecisionRequest{{}, {NeedSummary: true}, {NeedTracklist: true}} {
				request.Candidates = []provider.Candidate{c}
				out, err := registry.Load(context.Background(), tt.source.ID(), request)
				if err != nil || len(out) != 1 {
					t.Fatal(out, err)
				}
				want := matching.TargetExact
				if request.NeedTracklist {
					want = matching.TargetUnsupported
				}
				if out[0].State != want || out[0].Completeness != domain.Unloaded || out[0].Candidates[0].MatchRef != tt.track {
					t.Fatal(out)
				}
			}
			for _, ref := range tt.invalid {
				bad := c
				bad.MatchRef = ref
				if got := registry.Inspect(bad); got.State != matching.IdentityInvalid {
					t.Fatal("invalid native identity accepted", got)
				}
			}
			request := matching.DecisionRequest{Candidates: []provider.Candidate{c}, Position: matching.PositionRequirement{Disc: matching.NumberInput{State: matching.ValueKnown, Value: 2}}}
			out, err := registry.Load(context.Background(), tt.source.ID(), request)
			if err != nil || out[0].State != matching.TargetInsufficient {
				t.Fatal(out, err)
			}
			c.ReleaseTrackTotal = nil
			out, err = registry.Load(context.Background(), tt.source.ID(), matching.DecisionRequest{Candidates: []provider.Candidate{c}, NeedSummary: true})
			if err != nil || out[0].State != matching.TargetInsufficient {
				t.Fatal(out, err)
			}
		})
	}
}
