package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/matching"
)

// One fact set per view: never borrow the current title and snapshot album to
// assemble a match neither view actually supports. Snapshot identity is checked
// in SQL against BOTH current track and release bindings.
type ownedImportTrack struct {
	id                                     int64
	current, snapshot                      matching.Input
	snapshotValid                          bool
	source, ref, releaseSource, releaseRef string
	releaseID                              *int64
	bindingKey                             string
}

func ownedImportTracksTx(tx *sql.Tx, ids ...int64) ([]ownedImportTrack, error) {
	filter := ""
	var args []any
	if len(ids) > 0 {
		filter = " WHERE t.id=?"
		args = append(args, ids[0])
	}
	rows, err := tx.Query(`SELECT t.id,t.match_source,t.match_ref,t.release_id,r.provider,r.provider_ref,
 t.title,t.credit,r.title,t.duration_ms,t.disc_number,t.track_number,
 CASE WHEN json_extract(ts.body,'$.identity.provider')=t.match_source AND json_extract(ts.body,'$.identity.ref')=t.match_ref
 AND json_extract(ts.body,'$.release.provider')=r.provider AND json_extract(ts.body,'$.release.ref')=r.provider_ref
 AND json_extract(rs.body,'$.identity.provider')=r.provider AND json_extract(rs.body,'$.identity.ref')=r.provider_ref
 THEN json_object('title',json_extract(ts.body,'$.title.value'),'artist',json_extract(ts.body,'$.credit.value.text'),
 'album',json_extract(rs.body,'$.title.value'),'duration',json_extract(ts.body,'$.durationMS.value'),
 'disc',json_extract(ts.body,'$.discNumber.value'),'track',json_extract(ts.body,'$.trackNumber.value')) END
 FROM track_read t LEFT JOIN release_read r ON r.id=t.release_id
 LEFT JOIN track_snapshot ts ON ts.track_id=t.id LEFT JOIN release_snapshot rs ON rs.release_id=r.id`+filter+` ORDER BY t.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ownedImportTrack
	for rows.Next() {
		var t ownedImportTrack
		var source, ref, releaseSource, releaseRef, title, artist, album, snapshot *string
		if err = rows.Scan(&t.id, &source, &ref, &t.releaseID, &releaseSource, &releaseRef, &title, &artist, &album, &t.current.DurationMS, &t.current.DiscNo, &t.current.TrackNo, &snapshot); err != nil {
			return nil, err
		}
		t.source, t.ref, t.releaseSource, t.releaseRef = stringValue(source), stringValue(ref), stringValue(releaseSource), stringValue(releaseRef)
		t.current.Title, t.current.Artist, t.current.Album = stringValue(title), stringValue(artist), stringValue(album)
		if snapshot != nil {
			t.snapshotValid = true
			var fields struct {
				Title, Artist, Album  string
				Duration, Disc, Track *int
			}
			if err = json.Unmarshal([]byte(*snapshot), &fields); err != nil {
				return nil, err
			}
			t.snapshot = matching.Input{Title: fields.Title, Artist: fields.Artist, Album: fields.Album, DurationMS: fields.Duration, DiscNo: fields.Disc, TrackNo: fields.Track}
		}
		key, _ := json.Marshal([]any{t.source, t.ref, t.releaseID, t.releaseSource, t.releaseRef, t.current.DiscNo, t.current.TrackNo})
		t.bindingKey = string(key)
		out = append(out, t)
	}
	return out, rows.Err()
}

type confirmedImport struct {
	input      ImportItem
	trackID    int64
	bindingKey string
	context    *confirmationContext
}

func confirmedImportsTx(tx *sql.Tx) ([]confirmedImport, error) {
	rows, err := tx.Query(`SELECT i.id,i.raw_row,i.in_title,i.in_artist,i.in_album,i.in_duration_ms,i.in_disc_no,i.in_track_no,c.track_id,c.binding_key,c.target_context FROM import_confirmation c JOIN import_item i ON i.id=c.item_id ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []confirmedImport
	for rows.Next() {
		var c confirmedImport
		var raw *string
		if err = rows.Scan(&c.input.ID, &c.input.RawRow, &c.input.InTitle, &c.input.InArtist, &c.input.InAlbum, &c.input.InDurationMS, &c.input.InDiscNo, &c.input.InTrackNo, &c.trackID, &c.bindingKey, &raw); err != nil {
			return nil, err
		}
		if raw != nil {
			var context confirmationContext
			if err = json.Unmarshal([]byte(*raw), &context); err != nil {
				return nil, err
			}
			c.context = &context
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

type ownedImportDecision struct {
	TrackID int64                     `json:"trackId"`
	Rule    string                    `json:"rule"`
	Inputs  []matching.ImportEvidence `json:"inputs"`
}

// Every input must independently resolve to the SAME unique best-evidence
// collection. A weak ambiguous alias must not be disambiguated by a neighbour.
func decideOwnedImport(inputs []ImportItem, tracks []ownedImportTrack, confirmed []confirmedImport, exact map[int64]bool) ownedImportDecision {
	decision := ownedImportDecision{Rule: "owned-import-v3"}
	byTrack := map[int64][]confirmedImport{}
	for _, c := range confirmed {
		byTrack[c.trackID] = append(byTrack[c.trackID], c)
	}
	for _, input := range inputs {
		bestRank := 0
		var bestID int64
		var best matching.ImportEvidence
		ambiguous := false
		for _, track := range tracks {
			// Position conflict in current data remains a veto even when using
			// a previous snapshot/confirmation after user edits.
			if !matching.CompatibleImportPosition(importInput(input), track.current) {
				continue
			}
			evidence := matching.MatchOwnedInput(importInput(input), track.current, exact[track.id])
			if evidence.Rank > 0 {
				evidence.Facts = append(evidence.Facts, "current_values")
			}
			if other := matching.MatchOwnedInput(importInput(input), track.snapshot, exact[track.id]); other.Rank > evidence.Rank {
				other.Facts = append(other.Facts, "identity_checked_snapshot")
				evidence = other
			}
			for _, c := range byTrack[track.id] {
				if c.trackID == track.id && equivalentImport(input, c.input) && confirmedContextCompatible(c, track) &&
					(!track.snapshotValid || matching.CompatibleImportPosition(importInput(input), track.snapshot)) {
					evidence = matching.ImportEvidence{Kind: "confirmed_input", Rank: 3, Facts: []string{"previously_accepted_input", "unchanged_track_binding", "no_position_or_duration_conflict"}}
					if c.context != nil {
						evidence.Facts = append(evidence.Facts, "approved_target_context")
					}
					break
				}
			}
			if evidence.Rank > bestRank {
				bestRank, bestID, best, ambiguous = evidence.Rank, track.id, evidence, false
			} else if evidence.Rank > 0 && evidence.Rank == bestRank {
				ambiguous = true
			}
		}
		if bestID == 0 || ambiguous || (decision.TrackID != 0 && decision.TrackID != bestID) {
			return ownedImportDecision{}
		}
		decision.TrackID = bestID
		decision.Inputs = append(decision.Inputs, best)
	}
	return decision
}

func ownedImportDecisionTx(tx *sql.Tx, itemID int64, candidates []matching.ScoredCandidate) (ownedImportDecision, error) {
	inputs, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, itemID, itemID)
	if err != nil {
		return ownedImportDecision{}, err
	}
	tracks, err := ownedImportTracksTx(tx)
	if err != nil {
		return ownedImportDecision{}, err
	}
	confirmed, err := confirmedImportsTx(tx)
	if err != nil {
		return ownedImportDecision{}, err
	}
	return decideOwnedImport(inputs, tracks, confirmed, exactOwnedRefs(tracks, candidates)), nil
}

func exactOwnedRefs(tracks []ownedImportTrack, candidates []matching.ScoredCandidate) map[int64]bool {
	exact := map[int64]bool{}
	// Search corroboration must identify a unique compatible track, not merely
	// the only already-owned member of an ambiguous result pool.
	refs := map[string]bool{}
	for _, sc := range candidates {
		c := sc.Candidate
		if c.Provider == "" || c.MatchRef == "" {
			continue
		}
		key := string(c.Provider) + "\x00" + c.MatchRef
		refs[key] = true
	}
	if len(refs) == 1 {
		for _, track := range tracks {
			if track.source != "" && refs[track.source+"\x00"+track.ref] {
				exact[track.id] = true
			}
		}
	}
	return exact
}

func rememberImportConfirmationTx(tx *sql.Tx, itemID, trackID int64, evidence any) error {
	tracks, err := ownedImportTracksTx(tx, trackID)
	if err != nil {
		return err
	}
	key := ""
	var context confirmationContext
	for _, track := range tracks {
		if track.id == trackID {
			key = track.bindingKey
			context = trackConfirmationContext(track)
			break
		}
	}
	if key == "" {
		return sql.ErrNoRows
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	contextJSON, err := json.Marshal(context)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO import_confirmation(item_id,track_id,binding_key,evidence_json,created_at,target_context)
 SELECT id,?,?,?,?,? FROM import_item WHERE id=? OR merged_item_id=?
 ON CONFLICT(item_id) DO UPDATE SET track_id=excluded.track_id,binding_key=excluded.binding_key,evidence_json=excluded.evidence_json,created_at=excluded.created_at,target_context=excluded.target_context`, trackID, key, string(raw), time.Now().UTC(), string(contextJSON), itemID, itemID)
	return err
}

func skipOwnedImportTx(tx *sql.Tx, itemID int64, decision ownedImportDecision) error {
	raw, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE import_item SET status='skipped',track_id=?,review_reason=NULL,review_evidence=?,next_retry_at=NULL,last_error=NULL,search_revision=search_revision+1,updated_at=? WHERE id=? AND status IN ('queued','matching','review','failed')`, decision.TrackID, string(raw), time.Now().UTC(), itemID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrItemNotActive
	}
	return rememberImportConfirmationTx(tx, itemID, decision.TrackID, decision)
}

// ReconcileReviewDuplicates uses only original inputs and current local facts.
// Reading the review queue never launches searches or consumes manual drafts.
func (s *Store) ReconcileReviewDuplicates() error {
	return s.withTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT id,candidates FROM import_item WHERE status IN ('review','failed') AND search_manual=0 ORDER BY id`)
		if err != nil {
			return err
		}
		type pending struct {
			id  int64
			raw *string
		}
		var items []pending
		for rows.Next() {
			var p pending
			if err = rows.Scan(&p.id, &p.raw); err != nil {
				rows.Close()
				return err
			}
			items = append(items, p)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		if len(items) == 0 {
			return nil
		}
		tracks, err := ownedImportTracksTx(tx)
		if err != nil {
			return err
		}
		confirmed, err := confirmedImportsTx(tx)
		if err != nil {
			return err
		}
		for _, item := range items {
			var scored []matching.ScoredCandidate
			if item.raw != nil {
				if err := json.Unmarshal([]byte(*item.raw), &scored); err != nil {
					scored = nil
				}
			}
			inputs, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, item.id, item.id)
			if err != nil {
				return err
			}
			decision := decideOwnedImport(inputs, tracks, confirmed, exactOwnedRefs(tracks, scored))
			if decision.TrackID != 0 {
				if err = skipOwnedImportTx(tx, item.id, decision); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
