package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/playlists"
)

func syncFixture(source, ref, title string) playlists.Snapshot {
	return playlists.Snapshot{Ref: playlists.Ref{Provider: domain.ProviderID(source), ID: ref}, Name: "上游歌单", FetchedAt: time.Now().UTC(),
		Entries:      []playlists.Entry{{EntryKey: "song:1", TrackRef: "song", Position: 1, Title: ptr(title), Artists: []string{"Artist"}, Album: ptr("Album")}},
		Completeness: playlists.Completeness{Complete: true, DeclaredTotal: 1, RawSeen: 1, TrackCount: 1, Pages: 1}}
}
func saveSyncFixture(t *testing.T, s *Store, snapshot playlists.Snapshot, enqueue bool) (playlistID, entryID, batchID int64) {
	t.Helper()
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		playlistID, err = insertSyncedPlaylistTx(tx, snapshot, "")
		if err != nil {
			return err
		}
		var entries []int64
		for _, entry := range snapshot.Entries {
			entryID, err = insertSyncedEntryTx(tx, playlistID, entry)
			if err != nil {
				return err
			}
			entries = append(entries, entryID)
		}
		if enqueue {
			batchID, err = enqueueSyncedInputsTx(tx, playlistID, entries)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return
}
func requireSQLFailure(t *testing.T, s *Store, query string, args ...any) {
	t.Helper()
	if _, err := s.db.Exec(query, args...); err == nil {
		t.Fatalf("accepted invalid state: %s", query)
	}
}

func TestLibraryMembershipCreationAndFirstTime(t *testing.T) {
	s := openTestStore(t)
	syncID, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "同步"})
	if err != nil {
		t.Fatal(err)
	}
	localID, err := s.CreateTrack(&Track{Title: "本地创建"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{syncID, localID} {
		track, err := s.GetTrack(id)
		if err != nil {
			t.Fatal(err)
		}
		view, err := s.GetTrackView(id)
		if err != nil {
			t.Fatal(err)
		}
		meta, err := s.GetMetadata(id)
		if err != nil {
			t.Fatal(err)
		}
		want := id == localID
		if track.InLibrary != want || view.InLibrary != want || meta.InLibrary != want || (track.LibraryAddedAt != nil) != want || (view.AddedAt != "") != want || meta.AddedAt != view.AddedAt || view.CreatedAt == "" {
			t.Fatalf("wrong membership projection: %+v %+v %+v", track, view, meta)
		}
	}
	first := time.Now().UTC().Add(7 * 24 * time.Hour)
	for n, member := range []bool{true, false, true, true} {
		if err := s.withTx(func(tx *sql.Tx) error {
			return setLibraryMembershipTx(tx, syncID, member, first.Add(time.Duration(n)*time.Hour))
		}); err != nil {
			t.Fatal(err)
		}
		got, err := s.GetTrack(syncID)
		if err != nil || got.InLibrary != member || got.LibraryAddedAt == nil || !got.LibraryAddedAt.Equal(first) {
			t.Fatalf("first membership reset: %+v %v", got, err)
		}
	}
	requireSQLFailure(t, s, `INSERT INTO track(created_at,updated_at) VALUES(datetime('now'),datetime('now'))`)
	requireSQLFailure(t, s, `INSERT INTO track(in_library,created_at,updated_at) VALUES(1,datetime('now'),datetime('now'))`)
	requireSQLFailure(t, s, `UPDATE track SET in_library=2 WHERE id=?`, syncID)
}

func TestAdmissionInsertionUsesPersistedDestinationMembership(t *testing.T) {
	for _, kind := range []string{"sync", "file", "mixed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			s := openTestStore(t)
			_, _, batch := saveSyncFixture(t, s, syncFixture("test", "one", "Song"), true)
			item, err := s.ClaimNextItem(batch)
			if err != nil || item == nil {
				t.Fatal(item, err)
			}
			if kind == "file" || kind == "mixed" {
				identityBatch(t, s, identityItem("Song", "Artist", "Album"))
			}
			if kind == "file" || kind == "cancelled" {
				if err := s.withTx(func(tx *sql.Tx) error { return cancelImportDestinationTx(tx, item.ID, "upstream_removed") }); err != nil {
					t.Fatal(err)
				}
			}
			var result *domain.AdmissionResult
			err = s.withTx(func(tx *sql.Tx) error {
				var err error
				result, err = insertAdmissionTx(tx, domain.AdmissionSeed{ImportItemID: item.ID, Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{Title: domain.Known("Song")}}}, domain.AdmissionBaseline{})
				return err
			})
			if kind == "cancelled" {
				if !errors.Is(err, ErrItemNotActive) {
					t.Fatal(err)
				}
				var count int
				if err := s.db.QueryRow(`SELECT count(*) FROM track`).Scan(&count); err != nil || count != 0 {
					t.Fatal(count, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			track, err := s.GetTrack(result.TrackID)
			if err != nil || track.InLibrary != (kind != "sync") {
				t.Fatal(track, err)
			}
		})
	}
}

func TestPreviousGenerationRejectedWithoutConversion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "previous.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE model_generation(id INTEGER PRIMARY KEY,generation TEXT); INSERT INTO model_generation VALUES(1,'upstream-snapshot-v1'); CREATE TABLE track(id INTEGER PRIMARY KEY); INSERT INTO track VALUES(42)`); err != nil {
		t.Fatal(err)
	}
	if opened, err := Open(path); !errors.Is(err, ErrIncompatibleModel) {
		if opened != nil {
			opened.Close()
		}
		t.Fatal(err)
	}
	var id int
	if err := db.QueryRow(`SELECT id FROM track`).Scan(&id); err != nil || id != 42 {
		t.Fatal(id, err)
	}
	var generation string
	if err := db.QueryRow(`SELECT generation FROM model_generation`).Scan(&generation); err != nil || generation != "upstream-snapshot-v1" {
		t.Fatal(generation, err)
	}
}

func TestSyncedPlaylistConstraintsAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "synced.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	p, e, _ := saveSyncFixture(t, s, syncFixture("test-a", "same", "Song"), false)
	saveSyncFixture(t, s, syncFixture("test-b", "same", "Song"), false)
	if err := s.withTx(func(tx *sql.Tx) error {
		_, err := insertSyncedPlaylistTx(tx, syncFixture("test-a", "same", "Song"), "")
		return err
	}); err == nil {
		t.Fatal("duplicate binding")
	}
	var repeated int64
	if err := s.withTx(func(tx *sql.Tx) error {
		var err error
		repeated, err = insertSyncedEntryTx(tx, p, playlists.Entry{EntryKey: "song:2", TrackRef: "song", Position: 2})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	requireSQLFailure(t, s, `UPDATE synced_playlist_entry SET entry_key='song:1' WHERE id=?`, repeated)
	requireSQLFailure(t, s, `UPDATE synced_playlist SET playlist_ref=' ' WHERE id=?`, p)
	requireSQLFailure(t, s, `UPDATE synced_playlist_entry SET local_state='imported' WHERE id=?`, e)
	requireSQLFailure(t, s, `UPDATE synced_playlist_entry SET local_state='imported',track_id=999 WHERE id=?`, e)
	track, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	requireSQLFailure(t, s, `UPDATE synced_playlist_entry SET track_id=? WHERE id=?`, track, e)
	requireSQLFailure(t, s, `UPDATE synced_playlist_entry SET local_state='ignored',track_id=? WHERE id=?`, track, e)
	requireSQLFailure(t, s, `UPDATE synced_playlist_entry SET upstream_present=0 WHERE id=?`, e)
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=?,upstream_present=0 WHERE id=?`, track, e); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET local_state='ignored' WHERE id=?`, repeated); err != nil {
		t.Fatal(err)
	}
	requireSQLFailure(t, s, `DELETE FROM track WHERE id=?`, track)
	before, err := s.SyncedPlaylistState(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.SyncedPlaylistState(p)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(before, after, err)
	}
	bindings, err := s.ListSyncedPlaylists()
	if err != nil || len(bindings) != 2 || bindings[0].SnapshotDigest == "" {
		t.Fatal(bindings, err)
	}
}

func TestSyncInputImmutableAndMainDestinationRevoked(t *testing.T) {
	s := openTestStore(t)
	p1, e1, b1 := saveSyncFixture(t, s, syncFixture("test", "a", "Song"), true)
	_, _, b2 := saveSyncFixture(t, s, syncFixture("test", "b", "Song (译名)"), true)
	item, err := s.ClaimNextItem(b1)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err := s.MarkReview(item.ID, ReviewNoMatch, `[]`, `[]`); err != nil {
		t.Fatal(err)
	}
	token, err := s.BeginReviewSearch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishReviewSearch(item.ID, token, "qq", "", `[]`, `["manual draft"]`); err != nil {
		t.Fatal(err)
	}
	seed := domain.AdmissionSeed{ImportItemID: item.ID, Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{}}}
	before, err := s.PreviewAdmission(seed)
	if err != nil {
		t.Fatal(err)
	}
	originals, err := s.OriginalInputs(item.ID)
	if err != nil || len(originals) != 2 {
		t.Fatal(originals, err)
	}
	var secondInput int64
	if err := s.db.QueryRow(`SELECT id FROM import_item WHERE batch_id=?`, b2).Scan(&secondInput); err != nil {
		t.Fatal(err)
	}
	requireSQLFailure(t, s, `INSERT INTO import_destination(input_id,kind,synced_entry_id,state,display_name) VALUES(999,'synced_playlist',?,'active','bad')`, e1)
	requireSQLFailure(t, s, `UPDATE import_destination SET synced_entry_id=? WHERE input_id=?`, e1, secondInput)
	requireSQLFailure(t, s, `UPDATE import_item SET raw_row='["changed"]' WHERE id=?`, item.ID)
	if _, err := s.db.Exec(`UPDATE synced_playlist SET upstream_name='新歌单名' WHERE id=?`, p1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET title='新快照名称',revision=revision+1 WHERE id=?`, e1); err != nil {
		t.Fatal(err)
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := cancelImportDestinationTx(tx, item.ID, "upstream_removed"); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM synced_playlist_entry WHERE id=?`, e1)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	after, err := s.PreviewAdmission(seed)
	if err != nil || before.ItemRevision == after.ItemRevision {
		t.Fatal(before, after, err)
	}
	unchanged, err := s.OriginalInputs(item.ID)
	if err != nil || !reflect.DeepEqual(originals, unchanged) {
		t.Fatal(unchanged, err)
	}
	effective, err := s.EffectiveImportItem(item.ID)
	if err != nil || effective.ID != item.ID || stringValue(effective.InTitle) != "Song (译名)" || effective.TriedVariants == nil || *effective.TriedVariants != `["manual draft"]` {
		t.Fatal(effective, err)
	}
	stored, err := s.GetItem(item.ID)
	if err != nil || stringValue(stored.InTitle) != "Song" || stored.Status != StatusReview {
		t.Fatal(stored, err)
	}
	if err := s.FinishReviewSearch(item.ID, token, "test", "", `[]`, `[]`); !errors.Is(err, ErrItemNotActive) {
		t.Fatal("old search accepted", err)
	}
	var inputs []ImportItem
	if err := s.withTx(func(tx *sql.Tx) error {
		var err error
		inputs, err = readImportInputsTx(tx, `id=? OR merged_item_id=?`, item.ID, item.ID)
		return err
	}); err != nil || len(inputs) != 1 || inputs[0].ID != secondInput {
		t.Fatal(inputs, err)
	}
	for _, batchID := range []int64{b1, b2} {
		batch, err := s.GetBatch(batchID)
		if err != nil || batch.Filename != "" || batch.SourceKind != "synced_playlist" || batch.DestinationName != "上游歌单" || batch.TotalRows != 1 {
			t.Fatal(batch, err)
		}
		stats, err := s.Stats(batchID)
		if err != nil || stats.Total != 1 {
			t.Fatal(stats, err)
		}
	}
	shellStats, err := s.Stats(b1)
	if err != nil || shellStats.Discarded != 1 || shellStats.Review != 0 || shellStats.Total != 1 {
		t.Fatal("cancelled shell input must count as removed", shellStats, err)
	}
	// A new matching input can still merge into the shell whose own demand is gone.
	b3 := identityBatch(t, s, identityItem("Song (译名)", "Artist", "Album"))
	stats, err := s.Stats(b3)
	if err != nil || stats.Merged != 1 {
		t.Fatal(stats, err)
	}
	var thirdInput int64
	if err := s.db.QueryRow(`SELECT id FROM import_item WHERE batch_id=?`, b3).Scan(&thirdInput); err != nil {
		t.Fatal(err)
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := cancelImportDestinationTx(tx, secondInput, "ignored"); err != nil {
			return err
		}
		return cancelImportDestinationTx(tx, thirdInput, "discarded")
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.RetryItem(item.ID); !errors.Is(err, ErrItemNotActive) {
		t.Fatal(err)
	}
	reviews, err := s.AllReviewItems()
	if err != nil || len(reviews) != 0 {
		t.Fatal(reviews, err)
	}
	stats, err = s.Stats(b2)
	if err != nil || stats.Merged != 1 || stats.Discarded != 0 || stats.Total != 1 {
		t.Fatal(stats, err)
	}
}

func TestSyncInputEnqueueAndCancellationRollback(t *testing.T) {
	s := openTestStore(t)
	_, _, batch := saveSyncFixture(t, s, syncFixture("only-playlist", "one", "Song"), true)
	item, err := s.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("rollback") }
	err = s.withTx(func(tx *sql.Tx) error { return cancelImportDestinationTx(tx, item.ID, "deleted") })
	s.beforeCommit = nil
	if err == nil {
		t.Fatal("expected rollback")
	}
	destinations, err := s.ActiveImportDestinations(item.ID)
	if err != nil || len(destinations) != 1 || destinations[0].Revision != 1 {
		t.Fatal(destinations, err)
	}
	before, _ := s.RecentBatches(100)
	err = s.withTx(func(tx *sql.Tx) error {
		_, err := enqueueSyncedInputsTx(tx, *destinations[0].PlaylistID, []int64{*destinations[0].SyncedEntryID})
		return err
	})
	if err == nil {
		t.Fatal("second active attempt accepted")
	}
	after, _ := s.RecentBatches(100)
	if len(before) != len(after) {
		t.Fatal("batch leaked")
	}
	if err := s.EnqueueItems(batch, []ImportItem{identityItem("bad", "bad", "bad")}); err == nil {
		t.Fatal("file bypass accepted")
	}
}
