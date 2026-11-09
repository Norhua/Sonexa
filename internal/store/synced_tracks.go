package store

// SyncedTrackRow keys occurrences independently from local track identity.
// No generic TrackFilter can request all local entities.
type SyncedTrackRow struct {
	EntryID         int64     `json:"entryId"`
	TrackID         int64     `json:"trackId"`
	UpstreamPresent bool      `json:"upstreamPresent"`
	InLibrary       bool      `json:"inLibrary"`
	Track           TrackView `json:"track"`
}

// SyncedImportedCounts counts occurrences, not distinct local track IDs.
func (s *Store) SyncedImportedCounts() (map[int64]int, error) {
	rows, err := s.db.Query(`SELECT playlist_id,COUNT(*) FROM synced_playlist_entry WHERE local_state='imported' GROUP BY playlist_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var count int
		if err := rows.Scan(&id, &count); err != nil {
			return nil, err
		}
		out[id] = count
	}
	return out, rows.Err()
}

func (s *Store) ListSyncedTracks(playlistID int64, query string) ([]SyncedTrackRow, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	where := ""
	args := []any{playlistID}
	if q := ftsQuery(query); q != "" {
		where = " AND e.track_id IN (SELECT rowid FROM track_fts WHERE track_fts MATCH ?)"
		args = append(args, q)
	}
	rows, err := tx.Query(`SELECT e.id,e.track_id,e.upstream_present FROM synced_playlist_entry e WHERE e.playlist_id=? AND e.local_state='imported'`+where+` ORDER BY e.upstream_present DESC,e.position,e.id`, args...)
	if err != nil {
		return nil, err
	}
	out := []SyncedTrackRow{}
	for rows.Next() {
		var row SyncedTrackRow
		if err := rows.Scan(&row.EntryID, &row.TrackID, &row.UpstreamPresent); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// Every result is resolved and projected under one read transaction; cache
	// track projections for repeated occurrences without collapsing their rows.
	cache := map[int64]TrackView{}
	for i := range out {
		view, ok := cache[out[i].TrackID]
		if !ok {
			read, err := scanTrackView(tx.QueryRow("SELECT "+trackSelectColumns+" FROM track_read t LEFT JOIN release_read r ON r.id=t.release_id WHERE t.id=?", out[i].TrackID))
			if err != nil {
				return nil, err
			}
			view = *read
			cache[out[i].TrackID] = view
		}
		out[i].Track = view
		out[i].InLibrary = view.InLibrary
	}
	return out, nil
}
