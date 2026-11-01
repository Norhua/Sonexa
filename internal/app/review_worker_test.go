package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestReviewWorkerLeaseReleaseResumesWithoutLibraryEvent(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := rowFrom(matching.ScoredCandidate{Candidate: fullCandidate("lease-release")})
	row.Title = strPtr("Song")
	itemID := enqueueOneItem(t, a, "Song", *row.Artist, *row.Album)
	body, err := store.EncodeReviewCandidates([]matching.ScoredCandidate{{Candidate: row.toCandidate()}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, body, "[]"); err != nil {
		t.Fatal(err)
	}
	scope, _ := a.store.ItemProblemScope(itemID)
	session, err := a.AcquireReviewProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.startReviewWorker(a.ctx); err != nil {
		t.Fatal(err)
	}
	defer a.stopReviewWorker()
	if _, err := a.AddToLibrary(row); err != nil {
		t.Fatal(err)
	}
	item, _ := a.store.GetItem(itemID)
	if item.Status != store.StatusReview {
		t.Fatal(item)
	}
	if err := a.ReleaseReviewProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		item, _ = a.store.GetItem(itemID)
		if item.Status == store.StatusSkipped {
			break
		}
		select {
		case <-deadline:
			t.Fatal("release not resumed", item)
		case <-ticker.C:
		}
	}
	var revision int64
	if err := a.store.DB().QueryRow("SELECT revision FROM review_library_state WHERE id=1").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	a.wakeReviewWorker()
	// Pure skip changes neither the library generation nor its dirty queue.
	var after int64
	if err := a.store.DB().QueryRow("SELECT revision FROM review_library_state WHERE id=1").Scan(&after); err != nil || after != revision {
		t.Fatal(after, revision, err)
	}
}

func TestRecheckNetworkUsesRunnerAndOutboundGate(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	full := fullCandidate("network-recheck")
	full.Title = strPtr("Song")
	sparse := full
	sparse.DiscNumber = nil
	sparse.TrackNumber = nil
	itemID := enqueueOneItem(t, a, "Song", *full.ArtistName, *full.AlbumTitle)
	body, err := store.EncodeReviewCandidates([]matching.ScoredCandidate{{Candidate: sparse}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, body, "[]"); err != nil {
		t.Fatal(err)
	}
	scope, _ := a.store.ItemProblemScope(itemID)
	if err := a.store.QueueProblemRecheck(scope.ProblemID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.library.RecheckPending(a.ctx, 16); err != nil {
		t.Fatal(err)
	}
	item, err := a.store.GetItem(itemID)
	if err != nil || item.Status != store.StatusQueued {
		t.Fatal(item, err)
	}
	base, _ := a.registry.Get(provider.MusicBrainz)
	searcher := &countRecheckSearcher{MetadataProvider: base, hit: full}
	a.runner = importer.NewRunner(a.store, matching.NewEngine(a.settings.Get().Matching), searcher, provider.DefaultRetryPolicy(), false, nil, a.library)
	settings := a.settings.Get()
	settings.MusicBrainz.Contact = ""
	if err := a.settings.Save(settings); err != nil {
		t.Fatal(err)
	}
	if err := a.resumeRecheckNetwork(a.ctx); err != nil {
		t.Fatal(err)
	}
	if searcher.calls.Load() != 0 || a.runner.AnyRunning() {
		t.Fatal("disabled outbound started Runner")
	}
	if _, err := a.GetReviewQueue(); err != nil {
		t.Fatal(err)
	}
	if searcher.calls.Load() != 0 {
		t.Fatal("read triggered network")
	}
	settings.MusicBrainz.Contact = "test@example.com"
	if err := a.settings.Save(settings); err != nil {
		t.Fatal(err)
	}
	if err := a.resumeRecheckNetwork(a.ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for a.runner.AnyRunning() {
		select {
		case <-deadline:
			t.Fatal("Runner did not finish")
		case <-ticker.C:
		}
	}
	item, err = a.store.GetItem(itemID)
	if err != nil || item.Status != store.StatusImported || searcher.calls.Load() == 0 {
		t.Fatal(item, err, searcher.calls.Load())
	}
}

type countRecheckSearcher struct {
	provider.MetadataProvider
	calls atomic.Int32
	hit   provider.Candidate
}

func (s *countRecheckSearcher) SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error) {
	s.calls.Add(1)
	return []provider.Candidate{s.hit}, nil
}

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
