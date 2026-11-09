package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
)

var _ playlists.Repository = (*Store)(nil)

func (s *Store) FindSyncedPlaylist(ref playlists.Ref) (*playlists.Binding, error) {
	b, err := scanSyncedPlaylist(s.db.QueryRow(`SELECT `+syncedPlaylistColumns+` FROM synced_playlist WHERE provider=? AND playlist_ref=?`, ref.Provider, ref.ID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}
func readPlaylistStateTx(tx *sql.Tx, id int64) (*playlists.State, error) {
	b, err := scanSyncedPlaylist(tx.QueryRow(`SELECT `+syncedPlaylistColumns+` FROM synced_playlist WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := readSyncedEntries(tx, id)
	if err != nil {
		return nil, err
	}
	return &playlists.State{Binding: *b, Entries: entries}, nil
}
func (s *Store) ReadPlaylistState(id int64) (*playlists.State, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return readPlaylistStateTx(tx, id)
}
func playlistOperationResultTx(tx *sql.Tx, id, hash string) (*playlists.CommitResult, error) {
	var storedHash, body string
	err := tx.QueryRow(`SELECT content_hash,result_json FROM operation_result WHERE plan_id=?`, id).Scan(&storedHash, &body)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedHash != hash {
		return nil, playlists.ErrPlanStale
	}
	var result playlists.CommitResult
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		return nil, err
	}
	if result.PlaylistID == 0 {
		return nil, playlists.ErrPlanStale
	}
	return &result, nil
}
func (s *Store) PlaylistOperationResult(id, hash string) (*playlists.CommitResult, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return playlistOperationResultTx(tx, id, hash)
}

// ApplyPlaylist is a server-only transaction boundary. The service retains the
// plan; frontend bindings accept only its token/hash and chosen ignored IDs.
func (s *Store) ApplyPlaylist(ctx context.Context, plan playlists.Plan, restoreIDs []int64) (*playlists.CommitResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	hash, err := playlists.PlanHash(plan)
	if err != nil {
		return nil, err
	}
	if plan.ID == "" || plan.Hash != hash {
		return nil, playlists.ErrPlanStale
	}
	if err := plan.Snapshot.Validate(); err != nil {
		return nil, err
	}
	var result *playlists.CommitResult
	err = s.withTxContext(ctx, func(tx *sql.Tx) error {
		previous, err := playlistOperationResultTx(tx, plan.ID, plan.Hash)
		if err != nil {
			return err
		}
		if previous != nil {
			result = previous
			return nil
		}
		state, err := readPlaylistStateTx(tx, plan.PlaylistID)
		if err != nil {
			return err
		}
		if plan.PlaylistID != 0 && (state == nil || state.Binding.Revision != plan.BaseRevision) {
			return playlists.ErrPlanStale
		}
		difference, err := playlists.Diff(state, plan.Snapshot)
		if err != nil {
			return err
		}
		allowed := map[int64]bool{}
		for _, ignored := range difference.Ignored {
			allowed[ignored.EntryID] = true
		}
		restore := map[int64]bool{}
		for _, id := range restoreIDs {
			if !allowed[id] || restore[id] {
				return playlists.ErrPlanStale
			}
			restore[id] = true
		}
		playlistID := plan.PlaylistID
		if state == nil {
			var existing int64
			err := tx.QueryRow(`SELECT id FROM synced_playlist WHERE provider=? AND playlist_ref=?`, plan.Snapshot.Ref.Provider, plan.Snapshot.Ref.ID).Scan(&existing)
			if err == nil {
				return &playlists.AlreadyBoundError{PlaylistID: existing}
			}
			if err != sql.ErrNoRows {
				return err
			}
			playlistID, err = insertSyncedPlaylistTx(tx, plan.Snapshot, plan.Alias)
			if err != nil {
				return err
			}
		}
		prior := map[string]playlists.StoredEntry{}
		if state != nil {
			for _, entry := range state.Entries {
				prior[entry.EntryKey] = entry
			}
		}
		for _, removed := range difference.Removed {
			if removed.Disposition == "retain_absent" {
				if _, err := tx.Exec(`UPDATE synced_playlist_entry SET upstream_present=0,revision=revision+1 WHERE id=?`, removed.EntryID); err != nil {
					return err
				}
				continue
			}
			if err := detachSyncedEntryDemandsTx(tx, removed.EntryID, "upstream_removed"); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM synced_playlist_entry WHERE id=?`, removed.EntryID); err != nil {
				return err
			}
		}
		var enqueue []int64
		for _, entry := range plan.Snapshot.Entries {
			old, exists := prior[entry.EntryKey]
			if !exists {
				id, err := insertSyncedEntryTx(tx, playlistID, entry)
				if err != nil {
					return err
				}
				enqueue = append(enqueue, id)
				continue
			}
			artists, err := json.Marshal(entry.Artists)
			if err != nil {
				return err
			}
			state := old.LocalState
			if restore[old.ID] {
				state = playlists.Pending
				enqueue = append(enqueue, old.ID)
			}
			if _, err := tx.Exec(`UPDATE synced_playlist_entry SET position=?,title=?,artists_json=?,album=?,duration_ms=?,upstream_present=1,local_state=?,revision=revision+1 WHERE id=?`, entry.Position, entry.Title, string(artists), entry.Album, entry.DurationMS, state, old.ID); err != nil {
				return err
			}
		}
		complete, err := json.Marshal(plan.Snapshot.Completeness)
		if err != nil {
			return err
		}
		excluded, err := json.Marshal(plan.Snapshot.Excluded)
		if err != nil {
			return err
		}
		snapshotHash, err := playlists.SnapshotDigest(plan.Snapshot)
		if err != nil {
			return err
		}
		if plan.PlaylistID != 0 {
			if _, err := tx.Exec(`UPDATE synced_playlist SET upstream_name=?,revision=revision+1,last_synced_at=?,fetched_at=?,snapshot_digest=?,completeness_json=?,excluded_json=? WHERE id=?`, plan.Snapshot.Name, time.Now().UTC(), plan.Snapshot.FetchedAt, snapshotHash, string(complete), string(excluded), playlistID); err != nil {
				return err
			}
		}
		batchID, err := enqueueSyncedInputsTx(tx, playlistID, enqueue)
		if err != nil {
			return err
		}
		result = &playlists.CommitResult{PlaylistID: playlistID, BatchID: batchID}
		if err := tx.QueryRow(`SELECT revision FROM synced_playlist WHERE id=?`, playlistID).Scan(&result.Revision); err != nil {
			return err
		}
		body, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO operation_result(plan_id,content_hash,result_json,committed_at) VALUES(?,?,?,?)`, plan.ID, plan.Hash, string(body), time.Now().UTC()); err != nil {
			return err
		}
		return ctx.Err()
	})
	if err != nil {
		return nil, err
	}
	s.notifyImportInvalidation()
	return result, nil
}
func detachSyncedEntryDemandsTx(tx *sql.Tx, entryID int64, reason string) error {
	rows, err := tx.Query(`SELECT input_id FROM import_destination WHERE synced_entry_id=? AND state='active'`, entryID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := cancelImportDestinationTx(tx, id, reason); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`UPDATE import_destination SET synced_entry_id=NULL WHERE synced_entry_id=?`, entryID)
	return err
}
