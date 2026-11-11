package app

import (
	"encoding/json"
	"reflect"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestReviewReleaseMarkersRefreshExactStructureWithoutChangingTrackAction(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	var requests atomic.Int32
	a.client.HTTP.Transport = countingTransport{&requests}
	base := candidateRow()
	base.AlbumCredit = base.Artist
	id, err := a.store.AdmitCandidate(base.toCandidate())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec("UPDATE track SET in_library=0 WHERE id=?", id); err != nil {
		t.Fatal(err)
	}
	itemID := enqueueOneItem(t, a, "Unrelated input", "Input artist", "Input album")
	rows := []CandidateRow{base, base, base, base, base}
	rows[1].MatchRef = "new-track-in-existing-release"
	rows[2].MatchRef, rows[2].ReleaseRef = "new-release-track", strPtr("new-release")
	rows[3].Provider = "qq" // identical ref strings must not cross sources
	rows[4].MatchRef, rows[4].ReleaseGroupRef = "unknown-group-track", strPtr("unknown-group")
	for i := range rows {
		rows[i].ReleaseExists, rows[i].ReleaseGroupExists = true, true // persisted flags are never trusted
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, string(raw), "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.BeginReviewSearch(itemID); err != nil {
		t.Fatal(err)
	}
	check := func(wantRelease, wantGroup []bool) {
		t.Helper()
		queue, err := a.GetReviewQueue()
		if err != nil || len(queue.Items) != 1 || len(queue.Items[0].Candidates) != len(rows) {
			t.Fatal(queue, err)
		}
		for i, got := range queue.Items[0].Candidates {
			if got.ReleaseExists != wantRelease[i] || got.ReleaseGroupExists != wantGroup[i] || got.InLibrary {
				t.Fatalf("row %d: %+v", i, got)
			}
			wantLocal := i == 0 && wantRelease[0]
			if (got.LocalTrackID != 0) != wantLocal || (got.Action == "跳过") != wantLocal {
				t.Fatal("structure marker changed track action", got)
			}
			badges := []string{got.Provider}
			if wantLocal {
				badges = []string{"本地", got.Provider}
			}
			if !reflect.DeepEqual(got.Badges, badges) || !reflect.DeepEqual(got.toCandidate(), rows[i].toCandidate()) {
				t.Fatal("facts or track badges changed", got)
			}
		}
	}
	check([]bool{true, true, false, false, true}, []bool{true, true, true, false, false})
	if err := a.store.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	release, err := a.store.GetReleaseByRef(base.Provider, *base.ReleaseRef)
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		if err := a.store.DeleteOrphanRelease(release.ID); err != nil {
			t.Fatal(err)
		}
	}
	check(make([]bool, len(rows)), make([]bool, len(rows)))
	if requests.Load() != 0 {
		t.Fatal("queue marker read accessed network")
	}
}

func TestResearchReturnsReleaseMarkersAndKeepsManualCandidate(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	base := candidateRow()
	base.AlbumCredit = base.Artist
	if _, err := a.store.AdmitCandidate(base.toCandidate()); err != nil {
		t.Fatal(err)
	}
	candidate := base.toCandidate()
	candidate.MatchRef = "new-position"
	p, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		t.Fatal(err)
	}
	a.registry = provider.NewRegistry()
	a.registry.Register(&recordingSearcher{MetadataProvider: p, hit: []provider.Candidate{candidate}})
	itemID := enqueueOneItem(t, a, "Original", "Original artist", "Original album")
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	rows, err := a.ResearchReviewItem(itemID, *base.Title, *base.Artist, *base.Album, base.Provider, "")
	if err != nil || len(rows) != 1 || !rows[0].ReleaseExists || !rows[0].ReleaseGroupExists || rows[0].Action != "接受" || rows[0].LocalTrackID != 0 {
		t.Fatal(rows, err)
	}
	queue, err := a.GetReviewQueue()
	if err != nil || len(queue.Items) != 1 || !queue.Items[0].Searched || !queue.Items[0].Candidates[0].ReleaseExists {
		t.Fatal("manual search was auto-accepted or markers lost", queue, err)
	}
	// Automatic candidate snapshots use the same projection as manual rows.
	scored, err := json.Marshal([]matching.ScoredCandidate{{Candidate: candidate, Score: matching.Score{HighConf: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, string(scored), "[]"); err != nil {
		t.Fatal(err)
	}
	queue, err = a.GetReviewQueue()
	if err != nil || !queue.Items[0].Candidates[0].ReleaseExists || !queue.Items[0].Candidates[0].ReleaseGroupExists {
		t.Fatal(queue, err)
	}
}
