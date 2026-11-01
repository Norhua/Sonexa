package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func TestDecisionRequestSharesReleaseAndNeverEnumeratesGroup(t *testing.T) {
	var calls atomic.Int32
	body, err := json.Marshal(identityFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/ws/2/release/"+identityRelease || r.URL.Query().Get("inc") != detailInc {
			t.Errorf("unbounded/unexpected request %s", r.URL)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	client := provider.NewClient(nil, nopCache{}, "test")
	p := New(client, Config{Contact: "test", ServerURL: srv.URL}, nil)
	registry, err := matching.NewCandidateSources(p)
	if err != nil {
		t.Fatal(err)
	}
	seed := identitySeed()
	exact := seed
	exact.MatchRef = identityTrack
	out, err := registry.Load(context.Background(), provider.MusicBrainz, matching.DecisionRequest{
		Candidates: []provider.Candidate{seed, seed, exact}, NeedSummary: true, NeedTracklist: true,
		Position: identityPosition(1, 1),
	})
	if err != nil || len(out) != 3 || calls.Load() != 1 {
		t.Fatalf("out=%+v err=%v requests=%d", out, err, calls.Load())
	}
	for _, res := range out {
		if res.State != matching.TargetExact || res.Release == nil {
			t.Fatal(res)
		}
	}
}

func TestDecisionFetchErrorsGatesRetriesAndCancellation(t *testing.T) {
	body, err := json.Marshal(identityFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"failure", "retry", "gate", "cancel", "quota"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				n := calls.Add(1)
				if mode == "failure" || mode == "retry" && n == 1 {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write(body)
			}))
			defer srv.Close()
			limiters := provider.NewLimiters()
			limiters.Configure(provider.MusicBrainz, 100, time.Second)
			client := provider.NewClient(limiters, nopCache{}, "test")
			client.Retry = provider.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
			p := New(client, Config{Contact: "test", ServerURL: srv.URL}, nil)
			ctx := context.Background()
			if mode == "gate" {
				ctx = provider.WithRequestGate(ctx, func() error { return errors.New("disabled") })
			}
			if mode == "cancel" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "quota" {
				limiters.Configure(provider.MusicBrainz, 1, time.Hour)
				if err := limiters.Wait(ctx, provider.MusicBrainz); err != nil {
					t.Fatal(err)
				}
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer cancel()
			}
			out, err := p.LoadDecisionFacts(ctx, matching.DecisionRequest{Candidates: []provider.Candidate{identitySeed()}, Position: identityPosition(1, 1)})
			if mode == "retry" {
				if err != nil || len(out) != 1 || out[0].State != matching.TargetExact || calls.Load() != 2 {
					t.Fatal(out, err, calls.Load())
				}
			} else {
				if err == nil || out != nil {
					t.Fatal("failure fabricated successful target", out, err)
				}
				want := int32(0)
				if mode == "failure" {
					want = 2
				}
				if calls.Load() != want {
					t.Fatal("unexpected sends", calls.Load(), want)
				}
			}
		})
	}
}
