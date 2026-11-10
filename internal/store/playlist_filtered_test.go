package store

import (
	"context"
	"database/sql"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/playlists"
)

func TestFilteredImportedEntriesKeepPresenceAndTrackReferences(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("saved", "absent")}
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
		for i, entry := range entries {
			if err := detachSyncedEntryDemandsTx(tx, entry.ID, "test_delivered"); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=?,upstream_present=? WHERE id=?`, trackID, i == 0, entry.ID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	source.snapshot = syncTracks()
	source.snapshot.Excluded = []playlists.Excluded{{Kind: "unavailable_track", TrackRef: "saved"}, {Kind: "unavailable_track", TrackRef: "absent"}}
	source.snapshot.Completeness.RawSeen = 2
	source.snapshot.Completeness.DeclaredTotal = 2
	source.snapshot.Completeness.ExcludedCount = 2
	p, err := svc.PrepareSync(context.Background(), "filtered-imported", bound.PlaylistID)
	if err != nil || len(p.Difference.Removed)+len(p.Difference.Restored)+len(p.Difference.Added) != 0 {
		t.Fatal(p, err)
	}
	result, err := svc.Commit(context.Background(), p.ID, p.Hash, nil)
	if err != nil || result.BatchID != 0 {
		t.Fatal(result, err)
	}
	after, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(before, after, err)
	}
}

func TestFilteredPlaylistCommitPreservesExistingEntriesAndDemands(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("filtered", "ignored", "removed")}
	svc := newSyncService(t, s, source)
	bound := bindSync(t, svc)
	entries, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	ignoreStoredEntry(t, s, entries[1].ID)
	before, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	var inputID int64
	if err := s.db.QueryRow(`SELECT input_id FROM import_destination WHERE synced_entry_id=? AND state='active'`, entries[0].ID).Scan(&inputID); err != nil {
		t.Fatal(err)
	}
	source.snapshot = syncTracks("new")
	source.snapshot.Excluded = []playlists.Excluded{{Kind: "unavailable_track", TrackRef: "filtered", Reason: "filtered"}, {Kind: "unavailable_track", TrackRef: "ignored"}}
	source.snapshot.Completeness.RawSeen = 3
	source.snapshot.Completeness.DeclaredTotal = 3
	source.snapshot.Completeness.ExcludedCount = 2
	p, err := svc.PrepareSync(context.Background(), "filtered", bound.PlaylistID)
	if err != nil || len(p.Difference.Removed) != 1 || len(p.Difference.Added) != 1 {
		t.Fatal(p, err)
	}
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); err != nil {
		t.Fatal(err)
	}
	after, err := s.SyncedPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	for _, original := range before[:2] {
		found := false
		for _, entry := range after {
			if entry.ID == original.ID {
				found = true
				if !reflect.DeepEqual(entry, original) {
					t.Fatal("filtered entry changed", entry, original)
				}
			}
		}
		if !found {
			t.Fatal("filtered entry removed")
		}
	}
	var state string
	if err := s.db.QueryRow(`SELECT state FROM import_destination WHERE input_id=? AND synced_entry_id=?`, inputID, entries[0].ID).Scan(&state); err != nil || state != "active" {
		t.Fatal(state, err)
	}
	binding, err := s.GetSyncedPlaylist(bound.PlaylistID)
	if err != nil || len(binding.Excluded) != 2 {
		t.Fatal(binding, err)
	}
	// Once readable again it is the same occurrence, not a new import.
	source.snapshot = syncTracks("filtered", "ignored", "new")
	p, err = svc.PrepareSync(context.Background(), "readable", bound.PlaylistID)
	if err != nil || len(p.Difference.Added) != 0 || len(p.Difference.Removed) != 0 || len(p.Difference.Ignored) != 1 {
		t.Fatal(p, err)
	}
}
