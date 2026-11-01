package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

func TestAdmissionResolvesSyntheticTargetAndRejectsSeedBypass(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	defer a.coverSvc.Close()
	a.enricher.Pause()
	body, err := os.ReadFile("../provider/musicbrainz/testdata/identity_repeated_recording.json")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); _, _ = w.Write(body) }))
	defer srv.Close()
	p := musicbrainz.New(provider.NewClient(nil, a.store.AsProviderCache(), "test"), musicbrainz.Config{Contact: "test", ServerURL: srv.URL}, store.ParamsHash)
	a.registry.Register(p)
	a.identitySources, err = matching.NewCandidateSources(p)
	if err != nil {
		t.Fatal(err)
	}
	const release = "11111111-1111-4111-8111-111111111111"
	const recording = "2283ceda-78b1-4d20-8f52-bdf73bd53686"
	const track = "f0a33d1a-628a-4a53-bc4e-6af077505956"
	candidate := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: recording + "@" + release, ReleaseRef: strPtr(release), RecordingRef: strPtr(recording), Title: strPtr("query must not become title"), AlbumTitle: strPtr("query must not become album")}
	row := rowFrom(matching.ScoredCandidate{Candidate: candidate})
	settings := a.settings.Get()
	blocked := settings
	blocked.MusicBrainz.Contact = ""
	if err := a.settings.Save(blocked); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddToLibrary(row); err == nil || requests.Load() != 0 {
		t.Fatal("legacy add bypassed outbound gate", err, requests.Load())
	}
	if err := a.settings.Save(settings); err != nil {
		t.Fatal(err)
	}
	view, err := a.PrepareAdmission(row, 0)
	if err != nil || view.Resolution == nil || len(view.Resolution.Candidates) != 2 || view.ID != "" {
		t.Fatal(view, err)
	}
	if count, err := a.store.CountTracks(store.TrackFilter{}); err != nil || count != 0 {
		t.Fatal("resolution created entity", count, err)
	}
	for _, resolved := range view.Resolution.Candidates {
		if resolved.Title == nil || *resolved.Title != "you" || resolved.Album == nil || *resolved.Album != "特典CD" {
			t.Fatal("query patched source snapshot", resolved)
		}
	}
	if view.Resolution.Candidates[0].Note != "第 1 碟 · 第 1 轨" || view.Resolution.Candidates[1].Note != "第 2 碟 · 第 1 轨" {
		t.Fatal("position alternatives cannot be distinguished", view.Resolution.Candidates)
	}
	// Choose one actual position and create it through the real App entry.
	selected := view.Resolution.Candidates[0]
	plan, err := a.PrepareAdmission(selected, 0)
	if err != nil || plan.Resolution != nil {
		t.Fatal(plan, err)
	}
	created, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil {
		t.Fatal(err)
	}
	profile := matching.NewInputProfile("you", "歌手", "特典CD")
	profile.Disc = matching.NumberInput{State: matching.ValueKnown, Value: 1, Unit: "number", Semantic: "disc"}
	profile.Track = matching.NumberInput{State: matching.ValueKnown, Value: 1, Unit: "number", Semantic: "track"}
	batch, err := a.store.CreateBatch(&store.ImportBatch{Filename: "identity.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.EnqueueItems(batch, []store.ImportItem{{RawRow: `["query"]`, Profile: &profile}}); err != nil {
		t.Fatal(err)
	}
	item, err := a.store.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	plan, err = a.PrepareAdmission(row, item.ID)
	if err != nil || plan.Resolution != nil || plan.Baseline.ExistingTrackID != created.TrackID {
		t.Fatal("resolved identity missed existing track", plan, err)
	}
	service, err := a.libraryService()
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{candidate.MatchRef, "not-a-track"} {
		seed := domain.AdmissionSeed{Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{Identity: domain.Identity{Provider: provider.MusicBrainz, Ref: ref}, Release: &domain.Identity{Provider: provider.MusicBrainz, Ref: release}, Recording: domain.Known(domain.Identity{Provider: provider.MusicBrainz, Ref: recording})}, Release: &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: provider.MusicBrainz, Ref: release}}}}
		_, err := service.PrepareAdmission(context.Background(), seed)
		var unresolved *library.TargetResolutionError
		if !errors.As(err, &unresolved) {
			t.Fatalf("direct unresolved seed bypassed identity: %v", err)
		}
	}
	if _, err := service.CandidateSeed(candidate, 0); err == nil {
		t.Fatal("unresolved CandidateSeed accepted")
	}
	// The deliberately wrong query-like fields above exercise manual source
	// resolution only. Automatic selection must exclude that explicit mismatch.
	selection, err := service.SelectCandidate(context.Background(), []matching.ScoredCandidate{{Candidate: candidate}}, item.ID, &profile)
	if err != nil || selection.Candidate != nil {
		t.Fatal("automatic selection ignored explicit source mismatch", selection, err)
	}
	candidate.Title, candidate.ArtistName, candidate.AlbumTitle = strPtr("you"), strPtr("歌手"), strPtr("特典CD")
	selection, err = service.SelectCandidate(context.Background(), []matching.ScoredCandidate{{Candidate: candidate, Score: matching.Score{HighConf: true}}}, item.ID, &profile)
	if err != nil || selection.Candidate == nil || selection.Candidate.MatchRef != track || selection.Plan.Baseline.ExistingTrackID != created.TrackID {
		t.Fatal("automatic resolution not wired", selection, err)
	}
	profile.Disc = matching.MissingNumber("number", "disc")
	selection, err = service.SelectCandidate(context.Background(), []matching.ScoredCandidate{{Candidate: candidate, Score: matching.Score{HighConf: true}}}, item.ID, &profile)
	if err != nil || selection.Candidate != nil || len(selection.ResolvedCandidates) != 2 || selection.Reason != library.ReasonAmbiguous {
		t.Fatal("automatic repeated position did not review", selection, err)
	}
}
