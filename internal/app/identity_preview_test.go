package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

func TestDecisionPreviewReusesResponseCacheWithoutCreatingEntities(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	body, err := os.ReadFile("../provider/musicbrainz/testdata/identity_repeated_recording.json")
	if err != nil {
		t.Fatal(err)
	}
	var sends atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1); _, _ = w.Write(body) }))
	defer srv.Close()
	client := provider.NewClient(nil, a.store.AsProviderCache(), "test")
	p := musicbrainz.New(client, musicbrainz.Config{Contact: "test", ServerURL: srv.URL}, store.ParamsHash)
	const release = "11111111-1111-4111-8111-111111111111"
	const recording = "2283ceda-78b1-4d20-8f52-bdf73bd53686"
	const track = "f0a33d1a-628a-4a53-bc4e-6af077505956"
	// A standard detail read primes the exact cache key used by resolution.
	if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: release, TrackRef: track}); err != nil {
		t.Fatal(err)
	}
	r, err := matching.NewCandidateSources(p)
	if err != nil {
		t.Fatal(err)
	}
	request := matching.DecisionRequest{Candidates: []provider.Candidate{{Provider: provider.MusicBrainz, MatchRef: recording + "@" + release, ReleaseRef: strPtr(release), RecordingRef: strPtr(recording)}}}
	for i := 0; i < 2; i++ {
		out, err := r.Load(context.Background(), provider.MusicBrainz, request)
		if err != nil || len(out) != 1 || out[0].State != matching.TargetAmbiguous {
			t.Fatal(out, err)
		}
	}
	if sends.Load() != 1 {
		t.Fatalf("detail and decision did not share response cache: %d", sends.Load())
	}
	if got, err := a.store.FindByMatchRef("musicbrainz", track); err != nil || got != 0 {
		t.Fatal("preview created track", got, err)
	}
	if got, err := a.store.GetReleaseByRef("musicbrainz", release); err != nil || got != nil {
		t.Fatal("preview created release", got, err)
	}
}
