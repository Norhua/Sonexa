package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
)

// These transaction primitives are intentionally not public mutation APIs.
// Binding and refresh must be admitted by the service's complete-snapshot plan.
func insertSyncedPlaylistTx(tx *sql.Tx, snapshot playlists.Snapshot, alias string) (int64, error) {
	if err := snapshot.Validate(); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return 0, err
	}
	sum := sha256.Sum256(raw)
	complete, err := json.Marshal(snapshot.Completeness)
	if err != nil {
		return 0, err
	}
	excluded, err := json.Marshal(snapshot.Excluded)
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(`INSERT INTO synced_playlist(provider,playlist_ref,upstream_name,alias,last_synced_at,fetched_at,snapshot_digest,completeness_json,excluded_json)
 VALUES(?,?,?,?,?,?,?,?,?)`, snapshot.Ref.Provider, snapshot.Ref.ID, snapshot.Name, strings.TrimSpace(alias), time.Now().UTC(), snapshot.FetchedAt.UTC(), hex.EncodeToString(sum[:]), string(complete), string(excluded))
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

func insertSyncedEntryTx(tx *sql.Tx, playlistID int64, entry playlists.Entry) (int64, error) {
	artists, err := json.Marshal(entry.Artists)
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(`INSERT INTO synced_playlist_entry(playlist_id,entry_key,track_ref,position,title,artists_json,album,duration_ms,upstream_present,local_state)
 VALUES(?,?,?,?,?,?,?,?,1,'pending')`, playlistID, entry.EntryKey, entry.TrackRef, entry.Position, entry.Title, string(artists), entry.Album, entry.DurationMS)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

const syncedPlaylistColumns = `id,provider,playlist_ref,upstream_name,alias,revision,last_synced_at,fetched_at,snapshot_digest,completeness_json,excluded_json`

func scanSyncedPlaylist(row interface{ Scan(...any) error }) (*playlists.Binding, error) {
	var b playlists.Binding
	var complete, excluded string
	err := row.Scan(&b.ID, &b.Ref.Provider, &b.Ref.ID, &b.UpstreamName, &b.Alias, &b.Revision, &b.LastSyncedAt, &b.FetchedAt, &b.SnapshotDigest, &complete, &excluded)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(complete), &b.Completeness); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(excluded), &b.Excluded); err != nil {
		return nil, err
	}
	return &b, nil
}

func (s *Store) ListSyncedPlaylists() ([]playlists.Binding, error) {
	rows, err := s.db.Query(`SELECT ` + syncedPlaylistColumns + ` FROM synced_playlist ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []playlists.Binding{}
	for rows.Next() {
		b, err := scanSyncedPlaylist(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *b)
	}
	return out, rows.Err()
}

func (s *Store) GetSyncedPlaylist(id int64) (*playlists.Binding, error) {
	b, err := scanSyncedPlaylist(s.db.QueryRow(`SELECT `+syncedPlaylistColumns+` FROM synced_playlist WHERE id=?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return b, err
}

// SyncedPlaylistState returns pending/ignored as well as imported occurrences.
// It is for planning; it is not the visible track list API.
func (s *Store) SyncedPlaylistState(id int64) ([]playlists.StoredEntry, error) {
	rows, err := s.db.Query(`SELECT id,playlist_id,entry_key,track_ref,position,title,artists_json,album,duration_ms,upstream_present,local_state,track_id,revision
 FROM synced_playlist_entry WHERE playlist_id=? ORDER BY upstream_present DESC,position,id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []playlists.StoredEntry{}
	for rows.Next() {
		var e playlists.StoredEntry
		var artists string
		if err := rows.Scan(&e.ID, &e.PlaylistID, &e.EntryKey, &e.TrackRef, &e.Position, &e.Title, &artists, &e.Album, &e.DurationMS, &e.UpstreamPresent, &e.LocalState, &e.TrackID, &e.Revision); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(artists), &e.Artists); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
