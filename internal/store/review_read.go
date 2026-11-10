package store

import (
	"context"
	"database/sql"
	"encoding/json"
)

// Read snapshots must not inherit the write pool's BEGIN IMMEDIATE: merely
// opening a list must neither reserve the writer nor wait for a writer in WAL.
func (s *Store) beginRead() (*sql.Tx, error) {
	return s.db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
}

type prefixedScanner struct {
	row    interface{ Scan(...any) error }
	prefix []any
}

func (s prefixedScanner) Scan(dest ...any) error { return s.row.Scan(append(s.prefix, dest...)...) }

type ReviewReadItem struct {
	ImportItem
	OriginalInputs                [][]string
	Destinations                  []ImportDestination
	SearchProvider, SearchCountry string
	Searched                      bool
}

// ReadReviewItems projects every shell and its inputs in two queries under one
// read snapshot, rather than one write transaction and four queries per item.
func (s *Store) ReadReviewItems() ([]ReviewReadItem, error) {
	tx, err := s.beginRead()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`SELECT search_provider,search_country,search_manual,
 id,batch_id,row_index,raw_row,in_title,in_artist,in_album,in_year,in_track_no,in_disc_no,in_duration_ms,status,attempt_count,
 last_error,last_error_at,next_retry_at,candidates,tried_variants,review_reason,review_evidence,track_id,merged_into
 FROM import_item WHERE status IN ('review','failed') ORDER BY batch_id,row_index,id`)
	if err != nil {
		return nil, err
	}
	out := []ReviewReadItem{}
	positions := map[int64]int{}
	for rows.Next() {
		item := ReviewReadItem{OriginalInputs: [][]string{}, Destinations: []ImportDestination{}}
		read, e := scanItem(prefixedScanner{rows, []any{&item.SearchProvider, &item.SearchCountry, &item.Searched}})
		if e != nil {
			rows.Close()
			return nil, e
		}
		item.ImportItem = *read
		positions[item.ID] = len(out)
		out = append(out, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = tx.Query(`SELECT shell.id,i.id,i.raw_row,i.in_title,i.in_artist,i.in_album,
 d.kind,d.synced_entry_id,e.playlist_id,d.state,d.revision,
 CASE WHEN p.id IS NOT NULL THEN CASE WHEN p.alias<>'' THEN p.alias ELSE p.upstream_name END ELSE d.display_name END
 FROM import_item shell JOIN import_item i ON (i.id=shell.id OR i.merged_item_id=shell.id)
 JOIN import_destination d ON d.input_id=i.id
 LEFT JOIN synced_playlist_entry e ON e.id=d.synced_entry_id LEFT JOIN synced_playlist p ON p.id=e.playlist_id
 WHERE shell.status IN ('review','failed') ORDER BY shell.id,i.id=shell.id DESC,i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]map[string]bool{}
	representatives := map[int64]int64{}
	for rows.Next() {
		var shell int64
		var raw string
		var title, artist, album *string
		var d ImportDestination
		if err = rows.Scan(&shell, &d.InputID, &raw, &title, &artist, &album, &d.Kind, &d.SyncedEntryID, &d.PlaylistID, &d.State, &d.Revision, &d.DisplayName); err != nil {
			return nil, err
		}
		item := &out[positions[shell]]
		var cells []string
		if json.Unmarshal([]byte(raw), &cells) != nil {
			cells = []string{raw}
		}
		key, _ := json.Marshal(cells)
		if seen[shell] == nil {
			seen[shell] = map[string]bool{}
		}
		if !seen[shell][string(key)] {
			seen[shell][string(key)] = true
			item.OriginalInputs = append(item.OriginalInputs, cells)
		}
		if d.State == "active" {
			item.Destinations = append(item.Destinations, d)
			if representatives[shell] == 0 || d.InputID < representatives[shell] {
				representatives[shell] = d.InputID
				item.InTitle, item.InArtist, item.InAlbum = title, artist, album
			}
		}
	}
	return out, rows.Err()
}

func (s *Store) ReviewItemCount() (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM import_item WHERE status IN ('review','failed')`).Scan(&count)
	return count, err
}
