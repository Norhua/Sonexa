package store

import "encoding/json"

type CandidateLocalKey struct{ Provider, Ref, ReleaseRef string }
type CandidateLocalState struct {
	TrackID, ReleaseID, CoverRevision int64
	InLibrary                         bool
}

// CandidateLocalStates performs exact indexed lookups in one read statement.
// It does not load full track/release snapshots or register candidate artwork.
func (s *Store) CandidateLocalStates(keys []CandidateLocalKey) ([]CandidateLocalState, error) {
	if len(keys) == 0 {
		return []CandidateLocalState{}, nil
	}
	raw, err := json.Marshal(keys)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT COALESCE(t.id,0),COALESCE(t.in_library,0),COALESCE(r.id,0),COALESCE(c.revision,0)
 FROM json_each(?) k
 LEFT JOIN track t ON json_extract(k.value,'$.Provider')<>'' AND json_extract(k.value,'$.Ref')<>''
 AND t.match_source=json_extract(k.value,'$.Provider') AND t.match_ref=json_extract(k.value,'$.Ref')
 LEFT JOIN release r ON json_extract(k.value,'$.ReleaseRef')<>'' AND r.provider=json_extract(k.value,'$.Provider') AND r.provider_ref=json_extract(k.value,'$.ReleaseRef')
 LEFT JOIN release_cover_selection c ON c.release_id=r.id ORDER BY CAST(k.key AS INTEGER)`, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CandidateLocalState{}
	for rows.Next() {
		var state CandidateLocalState
		if err := rows.Scan(&state.TrackID, &state.InLibrary, &state.ReleaseID, &state.CoverRevision); err != nil {
			return nil, err
		}
		out = append(out, state)
	}
	return out, rows.Err()
}
