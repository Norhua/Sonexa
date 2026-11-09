package app

import (
	"context"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type reviewPlaylist struct{}

func (reviewPlaylist) ID() provider.ProviderID { return "playlist-only" }
func (reviewPlaylist) DisplayName() string     { return "目标" }
func (reviewPlaylist) PlaylistAvailability() playlists.Availability {
	return playlists.Availability{Available: true}
}
func (reviewPlaylist) ParsePlaylist(input string) (playlists.Ref, error) {
	return playlists.Ref{Provider: "playlist-only", ID: input}, nil
}
func (reviewPlaylist) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	title, album := "Song", "Album"
	return &playlists.Snapshot{Ref: ref, Name: "长歌单名", FetchedAt: time.Now().UTC(), Entries: []playlists.Entry{{EntryKey: "one:1", TrackRef: "one", Position: 1, Title: &title, Artists: []string{"Artist"}, Album: &album}}, Completeness: playlists.Completeness{Complete: true, DeclaredTotal: 1, RawSeen: 1, TrackCount: 1, Pages: 1}}, nil
}
func TestReviewDestinationDTOAndImmediateDiscardNotification(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	registry, err := playlists.NewRegistry(reviewPlaylist{})
	if err != nil {
		t.Fatal(err)
	}
	fetcher := playlists.NewFetcher()
	defer fetcher.Close()
	svc, err := playlists.NewService(registry, a.store, fetcher, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer svc.Close()
	var batch int64
	for _, ref := range []string{"a", "b"} {
		p, err := svc.PrepareBinding(context.Background(), ref, "playlist-only", ref, "别名 "+ref)
		if err != nil {
			t.Fatal(err)
		}
		result, err := svc.Commit(context.Background(), p.ID, p.Hash, nil)
		if err != nil {
			t.Fatal(err)
		}
		if batch == 0 {
			batch = result.BatchID
		}
	}
	item, err := a.store.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err := a.store.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	queue, err := a.GetReviewQueue()
	if err != nil || queue.Total != 1 || len(queue.Items[0].ImportDestinations) != 2 {
		t.Fatal(queue, err)
	}
	batches, err := a.GetImportQueue()
	if err != nil || len(batches) != 2 || batches[0].SourceKind != "synced_playlist" || batches[0].DestinationName != "别名 b" {
		t.Fatal(batches, err)
	}
	notified := 0
	a.emit = func(name string, args ...any) {
		if name == EventLibraryChanged {
			notified++
			items, err := a.store.AllReviewItems()
			if err != nil || len(items) != 0 {
				t.Error("notification before discard", items, err)
			}
		}
	}
	if err := a.DiscardReviewItem(item.ID); err != nil {
		t.Fatal(err)
	}
	if notified != 1 {
		t.Fatal("missing immediate success event", notified)
	}
	if err := a.DiscardReviewItem(item.ID); err == nil {
		t.Fatal("repeat discard should reject")
	}
	if notified != 1 {
		t.Fatal("failure emitted success")
	}
	summary, err := a.GetTaskSummary()
	if err != nil || summary.ReviewPending != 0 {
		t.Fatal(summary, err)
	}
}
