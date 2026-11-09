package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// Exercise real App bindings and a real isolated database. The only fake is a
// capability provider; no test-side implementation substitutes transactions.
func TestSyncedPlaylistEndToEndSharedLifecycle(t *testing.T) {
	for _, mode := range []string{"automatic", "review", "no-match", "duplicate-skip"} {
		t.Run(mode, func(t *testing.T) {
			a, p := playlistApp(t)
			candidate := fullCandidate("end-to-end")
			p.fetch = func(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
				snapshot, err := p.reviewPlaylist.FetchPlaylist(ctx, ref)
				snapshot.Entries[0].Title = candidate.Title
				snapshot.Entries[0].Album = candidate.AlbumTitle
				snapshot.Entries[0].Artists = []string{*candidate.ArtistName}
				return snapshot, err
			}
			var libraryEvents, targetEvents, queueEvents atomic.Int32
			a.emit = func(name string, _ ...any) {
				switch name {
				case EventLibraryChanged:
					libraryEvents.Add(1)
				case EventSyncedPlaylistsChanged:
					targetEvents.Add(1)
				case EventImportQueueChanged:
					queueEvents.Add(1)
				}
			}
			first, second := bindPlaylist(t, a, "a"), bindPlaylist(t, a, "b")
			var id int64
			var err error
			if mode == "automatic" {
				base, err := a.registry.Get(provider.MusicBrainz)
				if err != nil {
					t.Fatal(err)
				}
				a.registry = provider.NewRegistry()
				a.registry.Register(&recordingSearcher{MetadataProvider: base, hit: []provider.Candidate{candidate}})
				a.rebuildRunner(a.settings.Get())
				if err := a.runner.Start(context.Background(), first.BatchID); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "automatic shared import", func() bool { return !a.runner.IsRunning(first.BatchID) })
				a.runner.Close()
				id, err = a.store.FindByMatchRef(string(candidate.Provider), candidate.MatchRef)
			} else {
				item := reviewSyncedItem(t, a, first.BatchID)
				queue, err := a.GetReviewQueue()
				if err != nil || queue.Total != 1 || len(queue.Items[0].ImportDestinations) != 2 {
					t.Fatal(queue, err)
				}
				// Delivery changes the revision and must invalidate a preview that
				// classified this occurrence as pending.
				old, err := a.PreparePlaylistSync("before-delivery", first.PlaylistID)
				if err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "review":
					id, err = a.AcceptCandidate(item, rowFrom(matching.ScoredCandidate{Candidate: candidate}))
				case "no-match":
					id, err = a.NoMatchNeeded(item)
				case "duplicate-skip":
					id, err = a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Shared exact local", MatchSource: strPtr(string(candidate.Provider)), MatchRef: &candidate.MatchRef})
					if err != nil {
						t.Fatal(err)
					}
					err = a.SkipReviewCandidate(item, rowFrom(matching.ScoredCandidate{Candidate: candidate}))
				}
				if err != nil {
					t.Fatal(err)
				}
				if _, err := a.CommitPlaylistSync(old.ID, old.Hash, nil); !errors.Is(err, playlists.ErrPlanStale) {
					t.Fatal("old pending preview survived delivery", err)
				}
				if err := a.CancelPlaylistOperation(old.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err != nil || id == 0 {
				t.Fatal(id, err)
			}
			assertRows := func(playlistID int64, present, member bool) {
				t.Helper()
				rows, err := a.GetSyncedPlaylistEntries(playlistID, "")
				if err != nil || len(rows) != 1 || rows[0].TrackID != id || rows[0].UpstreamPresent != present || rows[0].InLibrary != member || rows[0].Track.InLibrary != member {
					t.Fatal(rows, err)
				}
			}
			assertRows(first.PlaylistID, true, false)
			assertRows(second.PlaylistID, true, false)
			page, err := a.ListTracks(store.TrackFilter{})
			if err != nil || page.Total != 0 {
				t.Fatal(page, err)
			}
			collection, err := a.CreateCollection("普通歌单", "playlist", nil)
			if err != nil {
				t.Fatal(err)
			}
			if err := a.AddToCollection(collection, []int64{id}); err == nil {
				t.Fatal("uncollected organization accepted")
			}
			addition, err := a.PrepareLibraryAddition([]int64{id, id})
			if err != nil || len(addition.WillAdd) != 1 {
				t.Fatal(addition, err)
			}
			if _, err := a.CommitLibraryAddition(addition.ID, addition.Hash); err != nil {
				t.Fatal(err)
			}
			if err := a.AddToCollection(collection, []int64{id}); err != nil {
				t.Fatal(err)
			}
			assertRows(first.PlaylistID, true, true)
			assertRows(second.PlaylistID, true, true)
			page, err = a.ListTracks(store.TrackFilter{CollectionID: collection})
			if err != nil || page.Total != 1 || page.Tracks[0].ID != id {
				t.Fatal(page, err)
			}
			if err := a.DeleteTracks([]int64{id}); err != nil {
				t.Fatal(err)
			}
			assertRows(first.PlaylistID, true, false)
			page, err = a.ListTracks(store.TrackFilter{CollectionID: collection})
			if err != nil || page.Total != 0 {
				t.Fatal(page, err)
			}
			// An absent imported reference in another playlist still protects
			// the track when the first binding is deleted.
			p.fetch = func(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
				s, err := p.reviewPlaylist.FetchPlaylist(ctx, ref)
				s.Entries = nil
				s.Completeness = playlists.Completeness{Complete: true, Pages: 1}
				return s, err
			}
			preview, err := a.PreparePlaylistSync("remove-upstream", second.PlaylistID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.CommitPlaylistSync(preview.ID, preview.Hash, nil); err != nil {
				t.Fatal(err)
			}
			assertRows(second.PlaylistID, false, false)
			if err := a.DeleteSyncedPlaylist(first.PlaylistID); err != nil {
				t.Fatal(err)
			}
			assertRows(second.PlaylistID, false, false)
			if err := a.DeleteSyncedPlaylist(second.PlaylistID); err != nil {
				t.Fatal(err)
			}
			if track, err := a.store.GetTrack(id); err != nil || track != nil {
				t.Fatal(track, err)
			}
			if list, err := a.ListSyncedPlaylists(); err != nil || len(list) != 0 {
				t.Fatal(list, err)
			}
			if libraryEvents.Load() == 0 || libraryEvents.Load() != targetEvents.Load() || libraryEvents.Load() != queueEvents.Load() {
				t.Fatal("inconsistent event fanout", libraryEvents.Load(), targetEvents.Load(), queueEvents.Load())
			}
			fk, err := a.store.DB().Query(`PRAGMA foreign_key_check`)
			if err != nil {
				t.Fatal(err)
			}
			defer fk.Close()
			if fk.Next() {
				t.Fatal("orphan foreign key after cleanup")
			}
		})
	}
}
