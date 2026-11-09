package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

type syncSource struct {
	snapshot playlists.Snapshot
	err      error
}

func (p *syncSource) ID() provider.ProviderID { return p.snapshot.Ref.Provider }
func (p *syncSource) DisplayName() string     { return "仅歌单替身" }
func (p *syncSource) PlaylistAvailability() playlists.Availability {
	return playlists.Availability{Available: true}
}
func (p *syncSource) ParsePlaylist(input string) (playlists.Ref, error) {
	ref := p.snapshot.Ref
	ref.ID = input
	return ref, ref.Validate()
}
func (p *syncSource) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	if p.err != nil {
		return nil, p.err
	}
	copy := p.snapshot
	copy.Ref = ref
	return &copy, nil
}
func newSyncService(t *testing.T, s *Store, p *syncSource) *playlists.Service {
	t.Helper()
	registry, err := playlists.NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := playlists.NewFetcher()
	service, err := playlists.NewService(registry, s, fetcher, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Close(); fetcher.Close() })
	return service
}
func syncTracks(refs ...string) playlists.Snapshot {
	s := syncFixture("playlist-only", "list", "Song")
	s.Entries = nil
	s.Completeness = playlists.Completeness{Complete: true, DeclaredTotal: len(refs), RawSeen: len(refs), TrackCount: len(refs), Pages: 1}
	count := map[string]int{}
	for i, ref := range refs {
		count[ref]++
		s.Entries = append(s.Entries, playlists.Entry{EntryKey: playlists.OccurrenceKey(ref, count[ref]), TrackRef: ref, Position: i + 1, Title: ptr(ref), Artists: []string{"Artist"}, Album: ptr("Album")})
	}
	return s
}
func bindSync(t *testing.T, svc *playlists.Service) *playlists.CommitResult {
	t.Helper()
	preview, err := svc.PrepareBinding(context.Background(), "bind", "playlist-only", "list", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func ignoreStoredEntry(t *testing.T, s *Store, entryID int64) {
	t.Helper()
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := detachSyncedEntryDemandsTx(tx, entryID, "ignored"); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE synced_playlist_entry SET local_state='ignored',revision=revision+1 WHERE id=?`, entryID); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE synced_playlist SET revision=revision+1 WHERE id=(SELECT playlist_id FROM synced_playlist_entry WHERE id=?)`, entryID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

func TestPlaylistBindingSyncAtomicAndImmutableInputs(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("a", "b", "b")}
	svc := newSyncService(t, s, source)
	preview, err := svc.PrepareBinding(context.Background(), "first", "playlist-only", "list", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"synced_playlist", "import_batch", "import_item"} {
		var n int
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatal(table, n, err)
		}
	}
	first, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatal(first, again, err)
	}
	entries, err := s.SyncedPlaylistState(first.PlaylistID)
	if err != nil || len(entries) != 3 {
		t.Fatal(entries, err)
	}
	stats, err := s.Stats(first.BatchID)
	if err != nil || stats.Total != 3 || stats.Merged != 1 {
		t.Fatal(stats, err)
	}
	var raw string
	var itemID int64
	if err := s.db.QueryRow(`SELECT input_id FROM import_destination WHERE synced_entry_id=?`, entries[0].ID).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT raw_row FROM import_item WHERE id=?`, itemID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	source.snapshot = syncTracks("b", "a", "b", "new")
	source.snapshot.Name = "renamed playlist"
	source.snapshot.Entries[1].Title = ptr("renamed song")
	preview, err = svc.PrepareSync(context.Background(), "refresh", first.PlaylistID)
	if err != nil || len(preview.Difference.Added) != 1 || len(preview.Difference.Removed) != 0 {
		t.Fatal(preview, err)
	}
	result, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil || result.Revision != 2 || result.BatchID == first.BatchID {
		t.Fatal(result, err)
	}
	var unchanged string
	if err := s.db.QueryRow(`SELECT raw_row FROM import_item WHERE id=?`, itemID).Scan(&unchanged); err != nil || raw != unchanged {
		t.Fatal(raw, unchanged, err)
	}
	updated, err := s.SyncedPlaylistState(first.PlaylistID)
	if err != nil || len(updated) != 4 || updated[1].ID != entries[0].ID || stringValue(updated[1].Title) != "renamed song" {
		t.Fatal(updated, err)
	}
	binding, err := s.GetSyncedPlaylist(first.PlaylistID)
	if err != nil || binding.DisplayName() != "renamed playlist" {
		t.Fatal(binding, err)
	}
	// Failure cannot yield a trusted empty/delete plan or advance the snapshot.
	source.snapshot.Completeness.Complete = false
	if _, err := svc.PrepareSync(context.Background(), "incomplete", first.PlaylistID); playlists.Code(err) != playlists.Incomplete {
		t.Fatal(err)
	}
	still, err := s.GetSyncedPlaylist(first.PlaylistID)
	if err != nil || still.Revision != binding.Revision {
		t.Fatal(still, err)
	}
}

func TestPlaylistSyncRollbackAtEachWriteStage(t *testing.T) {
	for _, table := range []string{"synced_playlist_entry", "import_destination", "import_batch", "import_item", "synced_playlist", "operation_result", "before_commit"} {
		t.Run(table, func(t *testing.T) {
			s := openTestStore(t)
			source := &syncSource{snapshot: syncTracks("old")}
			svc := newSyncService(t, s, source)
			bound := bindSync(t, svc)
			before, err := s.ReadPlaylistState(bound.PlaylistID)
			if err != nil {
				t.Fatal(err)
			}
			source.snapshot = syncTracks("new")
			preview, err := svc.PrepareSync(context.Background(), "change", bound.PlaylistID)
			if err != nil {
				t.Fatal(err)
			}
			if table == "before_commit" {
				s.beforeCommit = func(*sql.Tx) error { return errors.New("injected") }
			} else {
				op := "INSERT"
				if table == "import_destination" || table == "synced_playlist" {
					op = "UPDATE"
				}
				if _, err := s.db.Exec(fmt.Sprintf(`CREATE TRIGGER fail_sync BEFORE %s ON %s BEGIN SELECT RAISE(ABORT,'injected'); END`, op, table)); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil); err == nil {
				t.Fatal("expected rollback")
			}
			s.beforeCommit = nil
			if table != "before_commit" {
				if _, err := s.db.Exec(`DROP TRIGGER fail_sync`); err != nil {
					t.Fatal(err)
				}
			}
			after, err := s.ReadPlaylistState(bound.PlaylistID)
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal(before, after, err)
			}
			stats, err := s.Stats(bound.BatchID)
			if err != nil || stats.Total != 1 || stats.Queued != 1 {
				t.Fatal(stats, err)
			}
			var batches int
			if err := s.db.QueryRow(`SELECT count(*) FROM import_batch`).Scan(&batches); err != nil || batches != 1 {
				t.Fatal(batches, err)
			}
			// 将取消注入到最后的事务提交前，不能只靠提交入口检查。
			if table == "before_commit" {
				ctx, cancel := context.WithCancel(context.Background())
				s.beforeCommit = func(*sql.Tx) error { cancel(); return nil }
				if _, err := svc.Commit(ctx, preview.ID, preview.Hash, nil); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				s.beforeCommit = nil
			}
			// Same retained preview can be retried after a transient storage failure.
			if _, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPlaylistIgnoredSelectiveRecoveryAndObservedReaddition(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("a", "b")}
	svc := newSyncService(t, s, source)
	bound := bindSync(t, svc)
	entries, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		ignoreStoredEntry(t, s, entry.ID)
	}
	for attempt := 0; attempt < 2; attempt++ {
		p, err := svc.PrepareSync(context.Background(), fmt.Sprintf("nochange-%d", attempt), bound.PlaylistID)
		if err != nil || len(p.Difference.Ignored) != 2 || len(p.Difference.Added) != 0 {
			t.Fatal(p, err)
		}
		if attempt == 0 {
			svc.Cancel(p.ID)
		} else if result, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); err != nil || result.BatchID != 0 {
			t.Fatal(result, err)
		}
	}
	// Restore with latest confirmed metadata, not the previous discarded input.
	source.snapshot.Entries[0].Title = ptr("new a")
	p, err := svc.PrepareSync(context.Background(), "restore", bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, []int64{999}); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal(err)
	}
	result, err := svc.Commit(context.Background(), p.ID, p.Hash, []int64{entries[0].ID})
	if err != nil || result.BatchID == 0 {
		t.Fatal(result, err)
	}
	after, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil || after[0].LocalState != playlists.Pending || after[1].LocalState != playlists.Ignored {
		t.Fatal(after, err)
	}
	stats, err := s.Stats(bound.BatchID)
	if err != nil || stats.Discarded != 2 || stats.Total != 2 {
		t.Fatal(stats, err)
	}
	item, err := s.ClaimNextItem(result.BatchID)
	if err != nil || stringValue(item.InTitle) != "new a" {
		t.Fatal(item, err)
	}
	// Observe b's removal, then its re-addition: old ignore is not a permanent ban.
	source.snapshot = syncTracks("a")
	p, err = svc.PrepareSync(context.Background(), "remove-b", bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); err != nil {
		t.Fatal(err)
	}
	source.snapshot = syncTracks("a", "b")
	p, err = svc.PrepareSync(context.Background(), "readd-b", bound.PlaylistID)
	if err != nil || len(p.Difference.Added) != 1 || len(p.Difference.Ignored) != 0 {
		t.Fatal(p, err)
	}
	result, err = svc.Commit(context.Background(), p.ID, p.Hash, nil)
	if err != nil || result.BatchID == 0 {
		t.Fatal(result, err)
	}
}

func TestPlaylistImportedAbsentRetentionAndSharedPendingCancellation(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("saved", "pending")}
	svc := newSyncService(t, s, source)
	bound := bindSync(t, svc)
	entries, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	trackID, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "User title"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := detachSyncedEntryDemandsTx(tx, entries[0].ID, "test_delivered"); err != nil {
			return err
		}
		_, err := tx.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=? WHERE id=?`, trackID, entries[0].ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	other := syncTracks("pending")
	other.Ref.ID = "other"
	_, _, otherBatch := saveSyncFixture(t, s, other, true)
	source.snapshot = syncTracks() // verified empty removes only pending demands
	p, err := svc.PrepareSync(context.Background(), "empty", bound.PlaylistID)
	if err != nil || len(p.Difference.Removed) != 2 {
		t.Fatal(p, err)
	}
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); err != nil {
		t.Fatal(err)
	}
	after, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil || len(after) != 1 || after[0].UpstreamPresent || *after[0].TrackID != trackID {
		t.Fatal(after, err)
	}
	var shellID int64
	if err := s.db.QueryRow(`SELECT merged_item_id FROM import_item WHERE batch_id=?`, otherBatch).Scan(&shellID); err != nil {
		t.Fatal(err)
	}
	item, err := s.EffectiveImportItem(shellID)
	if err != nil || stringValue(item.InTitle) != "pending" {
		t.Fatal(item, err)
	}
	destinations, err := s.ActiveImportDestinations(shellID)
	if err != nil || len(destinations) != 1 {
		t.Fatal(destinations, err)
	}
	source.snapshot = syncTracks("saved")
	p, err = svc.PrepareSync(context.Background(), "restore-saved", bound.PlaylistID)
	if err != nil || len(p.Difference.Restored) != 1 {
		t.Fatal(p, err)
	}
	result, err := svc.Commit(context.Background(), p.ID, p.Hash, nil)
	if err != nil || result.BatchID != 0 {
		t.Fatal(result, err)
	}
	track, err := s.GetTrack(trackID)
	if err != nil || track.InLibrary || track.Title != "User title" {
		t.Fatal(track, err)
	}
}

func TestPlaylistStaleAndCancelledPlansCannotDelete(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("a")}
	svc := newSyncService(t, s, source)
	bound := bindSync(t, svc)
	source.snapshot = syncTracks()
	p, err := svc.PrepareSync(context.Background(), "stale", bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE synced_playlist SET revision=revision+1 WHERE id=?`, bound.PlaylistID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal(err)
	}
	svc.Cancel(p.ID)
	p, err = svc.PrepareSync(context.Background(), "cancel", bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.Commit(ctx, p.ID, p.Hash, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	entries, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil || len(entries) != 1 {
		t.Fatal(entries, err)
	}
	before, err := s.GetSyncedPlaylist(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	if before.LastSyncedAt.After(time.Now().UTC()) {
		t.Fatal(before)
	}
}
