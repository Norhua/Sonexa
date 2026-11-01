package app

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestAppExactSkipIsOfflineAndDoesNotNotifyLibrary(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := candidateRow()
	id, err := a.AddToLibrary(row)
	if err != nil {
		t.Fatal(err)
	}
	itemID := enqueueOneItem(t, a, "原曲名", "原署名", "原专辑")
	item, err := a.store.GetItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.GetTrack(id)
	if err != nil {
		t.Fatal(err)
	}
	var requests, libraryEvents atomic.Int32
	a.client.HTTP.Transport = countingTransport{&requests}
	a.emit = func(event string, _ ...any) {
		if event == EventLibraryChanged {
			libraryEvents.Add(1)
		}
	}
	settings := a.settings.Get()
	settings.MusicBrainz.Contact = ""
	if err := a.settings.Save(settings); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareSkipExisting(row, itemID, item.Revision)
	if err != nil || plan == nil || plan.Baseline.RequiresConfirmation {
		t.Fatalf("offline skip plan: %+v %v", plan, err)
	}
	result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil || !result.Existing || result.TrackID != id {
		t.Fatal(result, err)
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	got, err := a.store.GetItem(itemID)
	if err != nil || got.Status != store.StatusSkipped || got.ResolutionReason == nil || *got.ResolutionReason != store.ResolutionExistingTrack || got.Revision != item.Revision+1 {
		t.Fatal(got, err)
	}
	after, err := a.store.GetTrack(id)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("skip changed track", after, err)
	}
	if requests.Load() != 0 || libraryEvents.Load() != 0 {
		t.Fatalf("skip performed work: requests=%d libraryEvents=%d", requests.Load(), libraryEvents.Load())
	}
}

type delayedReviewSearcher struct {
	provider.MetadataProvider
	started chan struct{}
	release chan struct{}
	once    sync.Once
	hit     provider.Candidate
}

func (s *delayedReviewSearcher) SearchTracks(ctx context.Context, _ provider.Query) ([]provider.Candidate, error) {
	s.once.Do(func() { close(s.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.release:
		return []provider.Candidate{s.hit}, nil
	}
}

func TestLateReviewSearchCannotRestoreSkippedInput(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := candidateRow()
	if _, err := a.AddToLibrary(row); err != nil {
		t.Fatal(err)
	}
	itemID := enqueueOneItem(t, a, *row.Title, *row.Artist, *row.Album)
	item, err := a.store.GetItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareSkipExisting(row, itemID, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	base, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		t.Fatal(err)
	}
	searcher := &delayedReviewSearcher{MetadataProvider: base, started: make(chan struct{}), release: make(chan struct{}), hit: row.toCandidate()}
	a.registry.Register(searcher)
	var unblock sync.Once
	defer unblock.Do(func() { close(searcher.release) })
	result := make(chan error, 1)
	go func() {
		_, err := a.ResearchReviewItem(itemID, *row.Title, *row.Artist, *row.Album, "musicbrainz", "")
		result <- err
	}()
	select {
	case <-searcher.started:
	case err := <-result:
		t.Fatalf("search failed early: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("search not started")
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	terminal, err := a.store.GetItem(itemID)
	if err != nil {
		t.Fatal(err)
	}
	unblock.Do(func() { close(searcher.release) })
	select {
	case err := <-result:
		if !errors.Is(err, store.ErrPlanStale) {
			t.Fatalf("late response accepted: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("search did not finish")
	}
	after, err := a.store.GetItem(itemID)
	if err != nil || !reflect.DeepEqual(terminal, after) {
		t.Fatal("late search restored input", after, err)
	}
}
