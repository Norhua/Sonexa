package app

import (
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestLibraryEventSchedulesCurrentProblemWithoutNetwork(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := rowFrom(matching.ScoredCandidate{Candidate: fullCandidate("recheck")})
	row.Title = strPtr("Song")
	itemID := enqueueOneItem(t, a, "Song", *row.Artist, *row.Album)
	body, err := store.EncodeReviewCandidates([]matching.ScoredCandidate{{Candidate: row.toCandidate()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, body, "[]"); err != nil {
		t.Fatal(err)
	}
	if err := a.startReviewWorker(a.ctx); err != nil {
		t.Fatal(err)
	}
	defer a.stopReviewWorker()
	track, err := a.AddToLibrary(row)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		item, err := a.store.GetItem(itemID)
		if err != nil {
			t.Fatal(err)
		}
		if item.Status == store.StatusSkipped {
			if item.TrackID == nil || *item.TrackID != track {
				t.Fatal(item)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatal("library event did not resolve current problem", item)
		case <-tick.C:
		}
	}
	if n, err := a.store.CountTracks(store.TrackFilter{}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
