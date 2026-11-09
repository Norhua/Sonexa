package store

// ReleaseStructureTracks is for admission/editor impact only. Unlike library
// browsing it must retain every real member, including sync-only local tracks.
func (s *Store) ReleaseStructureTracks(releaseID int64) ([]TrackView, error) {
	rows, err := s.db.Query("SELECT "+trackSelectColumns+" FROM track_read t LEFT JOIN release_read r ON r.id=t.release_id WHERE t.release_id=? ORDER BY t.id", releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrackView{}
	for rows.Next() {
		track, err := scanTrackView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *track)
	}
	return out, rows.Err()
}
