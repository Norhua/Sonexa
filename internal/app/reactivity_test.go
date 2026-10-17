package app

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestRefreshSearchCandidatesUsesOnlyCurrentLocalState(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	var requests atomic.Int32
	a.client.HTTP.Transport = countingTransport{&requests}
	row := candidateRow()
	row.Confidence, row.MatchedVariant = 87, "原搜索"
	rows, err := a.RefreshSearchCandidates([]CandidateRow{row})
	if err != nil || len(rows) != 1 || rows[0].Action != "加入曲库" {
		t.Fatalf("initial: %+v %v", rows, err)
	}
	id, err := a.AddToLibrary(row)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = a.RefreshSearchCandidates([]CandidateRow{row, row})
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range rows {
		if got.LocalTrackID != id || got.Action != "已在库中" || got.ReleaseID == 0 || got.Confidence != 87 || got.MatchedVariant != "原搜索" || !reflect.DeepEqual(got.toCandidate(), row.toCandidate()) {
			t.Fatalf("local refresh lost facts or identity: %+v", got)
		}
	}
	if err := a.DeleteTracks([]int64{id}); err != nil {
		t.Fatal(err)
	}
	rows, err = a.RefreshSearchCandidates(rows)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range rows {
		if got.LocalTrackID != 0 || got.ReleaseID != 0 || got.CoverRevision != 0 || got.Action != "加入曲库" || !reflect.DeepEqual(got.Badges, []string{"musicbrainz"}) {
			t.Fatalf("deleted identity retained: %+v", got)
		}
	}
	if requests.Load() != 0 {
		t.Fatal("local refresh must not access network")
	}
}

func TestEnrichmentNotifiesCommittedTrackBeforeNextTrackFinishes(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	createPendingTrack(t, a, "第一首")
	createPendingTrack(t, a, "第二首")
	notified := make(chan int, 4)
	var completed atomic.Int32
	a.emit = func(event string, _ ...any) {
		if event == EventLibraryChanged {
			stats, err := a.store.EnrichStats()
			if err == nil {
				notified <- stats.Pending
			}
		}
	}
	a.enricher.enrich = func(ctx context.Context, _ store.EnrichTarget) error {
		if completed.Add(1) == 1 {
			return nil
		}
		<-ctx.Done()
		return ctx.Err()
	}
	if err := a.StartEnrich(); err != nil {
		t.Fatal(err)
	}
	select {
	case pending := <-notified:
		if pending != 1 {
			t.Fatalf("notification before committed state: pending=%d", pending)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first track completion did not notify while task is still running")
	}
	a.enricher.Stop()
	select {
	case <-notified:
		t.Fatal("cancelled track emitted a library notification")
	default:
	}
}

func TestFailedEnrichmentDoesNotNotifyLibraryChange(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	id := createPendingTrack(t, a, "失败")
	var notifications int
	a.enricher.onLibraryChanged = func() { notifications++ }
	a.enricher.enrich = func(context.Context, store.EnrichTarget) error { return fmt.Errorf("upstream failed") }
	failures := 0
	a.enricher.step(context.Background(), store.EnrichTarget{TrackID: id}, &failures)
	if notifications != 0 {
		t.Fatal("failed enrichment notified library change")
	}
}

func TestImportProductionWiringNotifiesLibraryAfterCommit(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	base, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		t.Fatal(err)
	}
	row := candidateRow()
	row.AlbumCredit = row.Artist
	searcher := &recordingSearcher{MetadataProvider: base, hit: []provider.Candidate{row.toCandidate()}}
	a.registry = provider.NewRegistry()
	a.registry.Register(searcher)
	a.rebuildRunner(a.settings.Get())
	itemID := enqueueOneItem(t, a, *row.Title, *row.Artist, *row.Album)
	item, err := a.store.GetItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.RetryItem(itemID); err != nil {
		t.Fatal(err)
	}
	changed := make(chan int64, 4)
	done := make(chan bool, 1)
	a.emit = func(event string, data ...any) {
		if event == EventLibraryChanged {
			id, _ := a.store.FindByMatchRef(row.Provider, row.MatchRef)
			changed <- id
		}
		if event == EventImportProgress && data[0].(importer.Progress).Done {
			done <- !a.runner.IsRunning(item.BatchID)
		}
	}
	if err := a.runner.Start(context.Background(), item.BatchID); err != nil {
		t.Fatal(err)
	}
	select {
	case stopped := <-done:
		if !stopped {
			t.Fatal("final event observed running batch")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing final progress")
	}
	select {
	case id := <-changed:
		if id == 0 {
			t.Fatal("library notification preceded commit")
		}
	default:
		t.Fatal("production runner did not emit library change")
	}
}

func TestTaskSummaryIncludesFailedItemsAcrossBatches(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	one := enqueueOneItem(t, a, "待选", "艺术家", "专辑")
	two := enqueueOneItem(t, a, "失败", "艺术家", "专辑")
	if err := a.store.MarkReview(one, store.ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.RecordFailure(two, "error", time.Second, 1); err != nil {
		t.Fatal(err)
	}
	summary, err := a.GetTaskSummary()
	if err != nil || summary.ReviewPending != 2 {
		t.Fatalf("summary=%+v error=%v", summary, err)
	}
}
