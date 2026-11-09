package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

var ErrItemNotActive = errors.New("条目已处理或状态已变化，请刷新复核队列")

func importInput(it ImportItem) matching.Input {
	return matching.Input{Title: stringValue(it.InTitle), Artist: stringValue(it.InArtist), Album: stringValue(it.InAlbum), DurationMS: it.InDurationMS, DiscNo: it.InDiscNo, TrackNo: it.InTrackNo}
}
func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// readImportInputsTx closes its cursor before any write, including on the
// single-connection SQLite configuration. Source row numbers are not consulted.
func readImportInputsTx(tx *sql.Tx, where string, args ...any) ([]ImportItem, error) {
	rows, err := tx.Query(`SELECT id,raw_row,in_title,in_artist,in_album,in_duration_ms,in_disc_no,in_track_no,in_year FROM import_item WHERE (`+where+`) AND EXISTS(SELECT 1 FROM import_destination d WHERE d.input_id=import_item.id AND d.state='active') ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []ImportItem
	for rows.Next() {
		var it ImportItem
		if err := rows.Scan(&it.ID, &it.RawRow, &it.InTitle, &it.InArtist, &it.InAlbum, &it.InDurationMS, &it.InDiscNo, &it.InTrackNo, &it.InYear); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// exactInput treats missing metadata as identical only when the mapped input
// itself is identical and contains a searchable value. Different column maps
// cannot accidentally equate identical raw arrays.
func exactInput(a, b ImportItem) bool {
	ai, bi := importInput(a), importInput(b)
	if ai.Title == "" && ai.Artist == "" && ai.Album == "" {
		return false
	}
	return a.RawRow == b.RawRow && ai.Title == bi.Title && ai.Artist == bi.Artist && ai.Album == bi.Album && matching.CompatibleImportPosition(ai, bi)
}
func equivalentImport(a, b ImportItem) bool {
	return exactInput(a, b) || matching.SameImportText(importInput(a), importInput(b))
}

func mergeImportTx(tx *sql.Tx, itemID, targetID int64) error {
	if _, err := tx.Exec(`UPDATE import_item SET merged_item_id=? WHERE merged_item_id=?`, targetID, itemID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE import_item SET revision=revision+1,search_revision=search_revision+1,updated_at=? WHERE id=?`, time.Now().UTC(), targetID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE import_item SET status='merged',merged_item_id=?,next_retry_at=NULL,updated_at=? WHERE id=? AND status IN ('queued','matching','review','failed')`, targetID, time.Now().UTC(), itemID)
	return err
}

// mergeTextInputTx only coalesces an unambiguous active target. All retained
// aliases must agree, preventing a missing position from bridging tracks 1/2.
func mergeTextInputTx(tx *sql.Tx, item ImportItem) error {
	rows, err := tx.Query(`SELECT shell.id,i.raw_row,i.in_title,i.in_artist,i.in_album,i.in_duration_ms,i.in_disc_no,i.in_track_no
 FROM import_item shell JOIN import_item i ON (i.id=shell.id OR i.merged_item_id=shell.id)
 JOIN import_destination d ON d.input_id=i.id AND d.state='active'
 WHERE shell.id<>? AND shell.status IN ('queued','matching','review','failed') ORDER BY shell.id,i.id`, item.ID)
	if err != nil {
		return err
	}
	compatible := map[int64]bool{}
	for rows.Next() {
		var shellID int64
		var input ImportItem
		if err := rows.Scan(&shellID, &input.RawRow, &input.InTitle, &input.InArtist, &input.InAlbum, &input.InDurationMS, &input.InDiscNo, &input.InTrackNo); err != nil {
			rows.Close()
			return err
		}
		if _, seen := compatible[shellID]; !seen {
			compatible[shellID] = true
		}
		compatible[shellID] = compatible[shellID] && equivalentImport(item, input)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	var targets []int64
	for shell, match := range compatible {
		if match {
			targets = append(targets, shell)
		}
	}
	if len(targets) == 1 {
		return mergeImportTx(tx, item.ID, targets[0])
	}
	return nil
}

// OriginalInputs returns immutable distinct source contents across all absorbed
// batches. The canonical input is first; neither path nor source row is stored.
func (s *Store) OriginalInputs(itemID int64) ([][]string, error) {
	rows, err := s.db.Query(`SELECT raw_row FROM import_item WHERE id=? OR merged_item_id=? ORDER BY id=? DESC,id`, itemID, itemID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][]string{}
	seen := map[string]bool{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cells []string
		if json.Unmarshal([]byte(raw), &cells) != nil {
			cells = []string{raw}
		}
		key, _ := json.Marshal(cells)
		if !seen[string(key)] {
			seen[string(key)] = true
			out = append(out, cells)
		}
	}
	return out, rows.Err()
}

// ResolveImport checks current bindings inside the same write transaction as
// the terminal transition, then registers an unambiguous selected track for
// cross-input coalescing. An ISRC/recording or overlapping search pool is never
// a binding. A nil selected candidate still allows safe library duplicate checks.
func (s *Store) ResolveImport(itemID int64, candidates []matching.ScoredCandidate, selected *provider.Candidate) (bool, error) {
	handled := false
	err := s.withTx(func(tx *sql.Tx) error {
		var status ItemStatus
		if err := tx.QueryRow(`SELECT status FROM import_item WHERE id=?`, itemID).Scan(&status); err != nil {
			return err
		}
		if status.IsTerminal() {
			handled = true
			return nil
		}
		decision, err := ownedImportDecisionTx(tx, itemID, candidates)
		if err != nil {
			return err
		}
		if decision.TrackID != 0 {
			handled = true
			return skipOwnedImportTx(tx, itemID, decision)
		}
		inputs, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, itemID, itemID)
		if err != nil {
			return err
		}
		if len(inputs) == 0 {
			return ErrItemNotActive
		}
		if selected == nil || selected.MatchRef == "" {
			return nil
		}
		// A single search hit can still be the wrong album; only register a
		// resolved identity when every absorbed input agrees with its context.
		selectedInput := matching.Input{Title: stringValue(selected.Title), Album: stringValue(selected.AlbumTitle), DurationMS: selected.DurationMS, DiscNo: selected.DiscNumber, TrackNo: selected.TrackNumber}
		for _, input := range inputs {
			if !matching.SameTrackContext(importInput(input), selectedInput) {
				return nil
			}
		}
		if _, err := tx.Exec(`UPDATE import_item SET resolved_source=?,resolved_ref=? WHERE id=?`, selected.Provider, selected.MatchRef, itemID); err != nil {
			return err
		}
		var target int64
		err = tx.QueryRow(`SELECT id FROM import_item WHERE id<>? AND resolved_source=? AND resolved_ref=? AND status IN ('queued','matching','review','failed') ORDER BY id LIMIT 1`, itemID, selected.Provider, selected.MatchRef).Scan(&target)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		others, err := readImportInputsTx(tx, `id=? OR merged_item_id=?`, target, target)
		if err != nil {
			return err
		}
		for _, input := range inputs {
			for _, other := range others {
				if !matching.CompatibleImportPosition(importInput(input), importInput(other)) {
					return nil
				}
			}
		}
		handled = true
		return mergeImportTx(tx, itemID, target)
	})
	return handled, err
}

// SkipReviewCandidate rechecks the exact binding, not a client-supplied local ID.
func (s *Store) SkipReviewCandidate(itemID int64, source, ref string) error {
	return s.withTx(func(tx *sql.Tx) error {
		var id int64
		if source == "" || ref == "" {
			return fmt.Errorf("缺少精确曲目身份")
		}
		if err := tx.QueryRow(`SELECT id FROM track WHERE match_source=? AND match_ref=?`, source, ref).Scan(&id); err != nil {
			return err
		}
		res, err := tx.Exec(`UPDATE import_item SET status='skipped',track_id=?,review_reason=NULL,next_retry_at=NULL,updated_at=? WHERE id=? AND status IN ('review','failed')`, id, time.Now().UTC(), itemID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrItemNotActive
		}
		return rememberImportConfirmationTx(tx, itemID, id, map[string]string{"kind": "manual_skip"})
	})
}

// Search tokens prevent both out-of-order writes and resurrection after an action.
func (s *Store) BeginReviewSearch(itemID int64) (int64, error) {
	var revision int64
	err := s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE import_item SET search_revision=search_revision+1,search_manual=1,resolved_source=NULL,resolved_ref=NULL WHERE id=? AND status IN ('review','failed')`, itemID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrItemNotActive
		}
		return tx.QueryRow(`SELECT search_revision FROM import_item WHERE id=?`, itemID).Scan(&revision)
	})
	return revision, err
}
func (s *Store) FinishReviewSearch(itemID, revision int64, source, country, candidates, variants string) error {
	return s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(`UPDATE import_item SET status='review',search_manual=1,search_provider=?,search_country=?,candidates=?,tried_variants=?,updated_at=? WHERE id=? AND search_revision=? AND status IN ('review','failed')`, source, country, candidates, variants, time.Now().UTC(), itemID, revision)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrItemNotActive
		}
		return nil
	})
}
func (s *Store) SetImportSearchSource(itemID int64, source string) error {
	_, err := s.db.Exec(`UPDATE import_item SET search_provider=?,search_country='',search_manual=0 WHERE id=? AND status='matching'`, source, itemID)
	return err
}

func (s *Store) ReviewSearchSource(itemID int64) (string, string, bool, error) {
	var source, country string
	var manual bool
	err := s.db.QueryRow(`SELECT search_provider,search_country,search_manual FROM import_item WHERE id=?`, itemID).Scan(&source, &country, &manual)
	return source, country, manual, err
}
