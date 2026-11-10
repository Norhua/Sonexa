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
	where := ""
	args := []any{playlistID}
	if q := ftsQuery(query); q != "" {
		where = " AND e.track_id IN (SELECT rowid FROM track_fts WHERE track_fts MATCH ?)"
		args = append(args, q)
	}
	rows, err := s.db.Query(`SELECT e.id,e.track_id,e.upstream_present,`+trackSelectColumns+`
 FROM synced_playlist_entry e JOIN track_read t ON t.id=e.track_id
 LEFT JOIN release_read r ON r.id=t.release_id
 WHERE e.playlist_id=? AND e.local_state='imported'`+where+` ORDER BY e.upstream_present DESC,e.position,e.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SyncedTrackRow{}
	for rows.Next() {
		var row SyncedTrackRow
		view, err := scanTrackView(prefixedScanner{rows, []any{&row.EntryID, &row.TrackID, &row.UpstreamPresent}})
		if err != nil {
			return nil, err
		}
		row.Track, row.InLibrary = *view, view.InLibrary
		out = append(out, row)
	}
	return out, rows.Err()
}
