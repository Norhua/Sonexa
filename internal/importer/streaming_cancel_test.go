package importer

import (
	"context"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type cancelledSearch struct {
	recordingSearcher
	started   chan struct{}
	cancelled chan struct{}
}

func (s *cancelledSearch) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	select {
	case s.cancelled <- struct{}{}:
	default:
	}
	return nil, ctx.Err()
}

type emptyPlaylist struct{ importPlaylist }

func (emptyPlaylist) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	return &playlists.Snapshot{Ref: ref, Name: "empty", FetchedAt: time.Now().UTC(), Completeness: playlists.Completeness{Complete: true, Pages: 1}}, nil
}
func TestRemovingMainDemandCancelsAndRequeuesRemainingTarget(t *testing.T) {
	st := openStore(t)
	svc, binding := importSyncFixture(t, st)
	p, err := svc.PrepareBinding(context.Background(), "other", "playlist-only", "other", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := svc.Commit(context.Background(), p.ID, p.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	searcher := &cancelledSearch{started: make(chan struct{}, 2), cancelled: make(chan struct{}, 2)}
	r := newRunner(st, searcher)
	done := make(chan struct{}, 1)
	r.emit = func(p Progress) {
		if p.Done {
			done <- struct{}{}
		}
	}
	if err := r.Start(context.Background(), binding.BatchID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-searcher.started:
	case <-time.After(5 * time.Second):
		t.Fatal("first search missing")
	}
	registry, err := playlists.NewRegistry(emptyPlaylist{})
	if err != nil {
		t.Fatal(err)
	}
	svc.SetRegistry(registry)
	p, err = svc.PrepareSync(context.Background(), "remove-main", binding.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-searcher.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("old representative not cancelled")
	}
	select {
	case <-searcher.started:
	case <-time.After(5 * time.Second):
		t.Fatal("remaining demand not requeued")
	}
	var id int64
	if err := st.DB().QueryRow(`SELECT id FROM import_item WHERE batch_id=?`, binding.BatchID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	destinations, err := st.ActiveImportDestinations(id)
	if err != nil || len(destinations) != 1 || *destinations[0].PlaylistID != other.PlaylistID {
		t.Fatal(destinations, err)
	}
	stats, err := st.Stats(binding.BatchID)
	if err != nil || stats.Discarded != 1 || stats.Total != 1 {
		t.Fatal(stats, err)
	}
	if err := st.MarkDiscarded(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner not stopped")
	}
}

func TestRemovingSyncDemandCancelsInFlightRunnerWithoutOrphan(t *testing.T) {
	st := openStore(t)
	_, binding := importSyncFixture(t, st)
	searcher := &cancelledSearch{started: make(chan struct{}, 1), cancelled: make(chan struct{}, 1)}
	r := newRunner(st, searcher)
	done := make(chan struct{}, 1)
	r.emit = func(p Progress) {
		if p.Done {
			done <- struct{}{}
		}
	}
	if err := r.Start(context.Background(), binding.BatchID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-searcher.started:
	case <-time.After(5 * time.Second):
		t.Fatal("search never started")
	}
	var id int64
	if err := st.DB().QueryRow(`SELECT id FROM import_item WHERE batch_id=?`, binding.BatchID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := st.MarkDiscarded(id); err != nil {
		t.Fatal(err)
	}
	select {
	case <-searcher.cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("removed work not cancelled")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runner not finished")
	}
	var tracks int
	if err := st.DB().QueryRow(`SELECT count(*) FROM track`).Scan(&tracks); err != nil || tracks != 0 {
		t.Fatal(tracks, err)
	}
	item, err := st.GetItem(id)
	if err != nil || item.Status != store.StatusDiscarded {
		t.Fatal(item, err)
	}
}
