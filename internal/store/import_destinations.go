package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
)

type ImportDestination struct {
	InputID       int64  `json:"inputId"`
	Kind          string `json:"kind"`
	SyncedEntryID *int64 `json:"syncedEntryId"`
	PlaylistID    *int64 `json:"playlistId"`
	State         string `json:"state"`
	Revision      int64  `json:"revision"`
	DisplayName   string `json:"displayName"`
}

func enqueueImportInputTx(tx *sql.Tx, batchID int64, ordinal int, it ImportItem, destination ImportDestination) (int64, error) {
	res, err := tx.Exec(`INSERT INTO import_item(batch_id,row_index,raw_row,in_title,in_artist,in_album,in_year,in_track_no,in_disc_no,in_duration_ms,status,updated_at)
 VALUES(?,?,?,?,?,?,?,?,?,?,'queued',?)`, batchID, ordinal, it.RawRow, it.InTitle, it.InArtist, it.InAlbum, it.InYear, it.InTrackNo, it.InDiscNo, it.InDurationMS, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	it.ID, err = res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`INSERT INTO import_destination(input_id,kind,synced_entry_id,state,display_name) VALUES(?,?,?,'active',?)`, it.ID, destination.Kind, destination.SyncedEntryID, destination.DisplayName); err != nil {
		return 0, err
	}
	if err := mergeTextInputTx(tx, it); err != nil {
		return 0, err
	}
	return it.ID, nil
}

// enqueueSyncedInputsTx receives entry IDs from a confirmed snapshot transaction.
// Snapshot updates never modify an existing import_item. Recovery calls this
// again to make a new attempt, not to revive a discarded or merged input.
func enqueueSyncedInputsTx(tx *sql.Tx, playlistID int64, entryIDs []int64) (int64, error) {
	if len(entryIDs) == 0 {
		return 0, nil
	}
	b, err := scanSyncedPlaylist(tx.QueryRow(`SELECT `+syncedPlaylistColumns+` FROM synced_playlist WHERE id=?`, playlistID))
	if err != nil {
		return 0, err
	}
	batchID, err := createImportBatchTx(tx, &ImportBatch{TotalRows: len(entryIDs)}, "synced_playlist", "synced_playlist", b.DisplayName())
	if err != nil {
		return 0, err
	}
	for ordinal, entryID := range entryIDs {
		var entry playlists.Entry
		var artists string
		if err := tx.QueryRow(`SELECT title,artists_json,album,duration_ms FROM synced_playlist_entry
 WHERE id=? AND playlist_id=? AND upstream_present=1 AND local_state='pending'`, entryID, playlistID).Scan(&entry.Title, &artists, &entry.Album, &entry.DurationMS); err != nil {
			return 0, err
		}
		if err := json.Unmarshal([]byte(artists), &entry.Artists); err != nil {
			return 0, err
		}
		var artist *string
		if entry.Artists != nil {
			value := strings.Join(entry.Artists, " / ")
			artist = &value
		}
		var duration *int
		durationText := ""
		if entry.DurationMS != nil {
			value := int(*entry.DurationMS)
			if int64(value) != *entry.DurationMS {
				return 0, fmt.Errorf("时长超出导入字段范围")
			}
			duration = &value
			durationText = strconv.FormatInt(*entry.DurationMS, 10)
		}
		raw, err := json.Marshal([]string{stringValue(entry.Title), stringValue(artist), stringValue(entry.Album), durationText})
		if err != nil {
			return 0, err
		}
		it := ImportItem{RawRow: string(raw), InTitle: entry.Title, InArtist: artist, InAlbum: entry.Album, InDurationMS: duration}
		if _, err := enqueueImportInputTx(tx, batchID, ordinal, it, ImportDestination{Kind: "synced_playlist", SyncedEntryID: &entryID, DisplayName: b.DisplayName()}); err != nil {
			return 0, err
		}
	}
	return batchID, nil
}

// ActiveImportDestinations includes every effective input, not just the shell.
// Display names follow live bindings; cancelled history keeps its saved summary.
func (s *Store) ActiveImportDestinations(itemID int64) ([]ImportDestination, error) {
	rows, err := s.db.Query(`SELECT d.input_id,d.kind,d.synced_entry_id,e.playlist_id,d.state,d.revision,
 CASE WHEN p.id IS NOT NULL THEN CASE WHEN p.alias<>'' THEN p.alias ELSE p.upstream_name END ELSE d.display_name END
 FROM import_destination d JOIN import_item i ON i.id=d.input_id
 LEFT JOIN synced_playlist_entry e ON e.id=d.synced_entry_id LEFT JOIN synced_playlist p ON p.id=e.playlist_id
 WHERE (i.id=? OR i.merged_item_id=?) AND d.state='active' ORDER BY d.input_id`, itemID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportDestination{}
	for rows.Next() {
		var d ImportDestination
		if err := rows.Scan(&d.InputID, &d.Kind, &d.SyncedEntryID, &d.PlaylistID, &d.State, &d.Revision, &d.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// cancelImportDestinationTx preserves the original input and batch denominator.
// The caller updates/removes the sync entry within this same transaction.
func cancelImportDestinationTx(tx *sql.Tx, inputID int64, reason string) error {
	var shellID int64
	if err := tx.QueryRow(`SELECT COALESCE(merged_item_id,id) FROM import_item WHERE id=?`, inputID).Scan(&shellID); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE import_destination SET state='cancelled',synced_entry_id=NULL,revision=revision+1,cancel_reason=? WHERE input_id=? AND state='active'`, reason, inputID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil || n == 0 {
		return err
	}
	_, err = tx.Exec(`UPDATE import_item SET revision=revision+1,search_revision=search_revision+1,
 resolved_source=NULL,resolved_ref=NULL,updated_at=?,
 status=CASE WHEN EXISTS(SELECT 1 FROM import_item i JOIN import_destination d ON d.input_id=i.id
 WHERE (i.id=? OR i.merged_item_id=?) AND d.state='active') THEN status ELSE 'discarded' END,
 next_retry_at=NULL WHERE id=? AND status IN ('queued','matching','review','failed')`, time.Now().UTC(), shellID, shellID, shellID)
	return err
}

// EffectiveImportItem overlays mapped fields only in the returned value. The
// shell's ID/status/search draft stay stable; no persisted raw input is rewritten.
func effectiveImportItemTx(tx *sql.Tx, shell *ImportItem) error {
	inputs, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, shell.ID, shell.ID)
	if err != nil {
		return err
	}
	if len(inputs) == 0 {
		return ErrItemNotActive
	}
	representative := inputs[0]
	shell.InTitle, shell.InArtist, shell.InAlbum = representative.InTitle, representative.InArtist, representative.InAlbum
	shell.InYear, shell.InDiscNo, shell.InTrackNo, shell.InDurationMS = representative.InYear, representative.InDiscNo, representative.InTrackNo, representative.InDurationMS
	return nil
}

func (s *Store) EffectiveImportItem(itemID int64) (*ImportItem, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	item, err := scanItem(tx.QueryRow(`SELECT id,batch_id,row_index,raw_row,in_title,in_artist,in_album,
 in_year,in_track_no,in_disc_no,in_duration_ms,status,attempt_count,last_error,last_error_at,next_retry_at,
 candidates,tried_variants,review_reason,review_evidence,track_id,merged_into FROM import_item WHERE id=?`, itemID))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if item.Status.IsTerminal() {
		return nil, ErrItemNotActive
	}
	if err := effectiveImportItemTx(tx, item); err != nil {
		return nil, err
	}
	return item, nil
}
