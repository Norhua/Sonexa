package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

func importTargetsTx(tx *sql.Tx, itemID int64) ([]domain.ImportTarget, error) {
	rows, err := tx.Query(`SELECT d.input_id,d.kind,COALESCE(e.id,0),COALESCE(p.id,0),d.revision,COALESCE(e.revision,0),COALESCE(p.revision,0),
 (d.kind='library' OR (p.id IS NOT NULL AND e.upstream_present=1 AND e.local_state='pending'))
 FROM import_item i JOIN import_destination d ON d.input_id=i.id
 LEFT JOIN synced_playlist_entry e ON e.id=d.synced_entry_id LEFT JOIN synced_playlist p ON p.id=e.playlist_id
 WHERE (i.id=? OR i.merged_item_id=?) AND d.state='active' ORDER BY d.input_id`, itemID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.ImportTarget{}
	for rows.Next() {
		var target domain.ImportTarget
		var valid bool
		if err := rows.Scan(&target.InputID, &target.Kind, &target.EntryID, &target.PlaylistID, &target.Revision, &target.EntryRevision, &target.PlaylistRevision, &valid); err != nil {
			return nil, err
		}
		if !valid {
			return nil, fmt.Errorf("%w: 导入目标状态不一致", ErrPlanStale)
		}
		out = append(out, target)
	}
	return out, rows.Err()
}
func sameImportTargets(a, b []domain.ImportTarget) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

// deliverImportTx is the only successful terminal write. Queue status,
// confirmations and every effective sync reference commit or roll back together.
// Reusing an existing track never changes its library membership.
func deliverImportTx(tx *sql.Tx, itemID, trackID int64, status ItemStatus, evidence any) ([]int64, error) {
	if status != StatusImported && status != StatusSkipped {
		return nil, ErrItemNotActive
	}
	targets, err := importTargetsTx(tx, itemID)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, ErrItemNotActive
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		return nil, err
	}
	row, err := tx.Exec(`UPDATE import_item SET status=?,track_id=?,review_reason=NULL,review_evidence=?,next_retry_at=NULL,last_error=NULL,
 revision=revision+1,search_revision=search_revision+1,updated_at=? WHERE id=? AND status IN ('queued','matching','review','failed')`, status, trackID, string(raw), time.Now().UTC(), itemID)
	if err != nil {
		return nil, err
	}
	if n, _ := row.RowsAffected(); n != 1 {
		return nil, ErrItemNotActive
	}
	if err := rememberImportConfirmationTx(tx, itemID, trackID, evidence); err != nil {
		return nil, err
	}
	playlists := map[int64]bool{}
	for _, target := range targets {
		row, err := tx.Exec(`UPDATE import_destination SET state='delivered',revision=revision+1 WHERE input_id=? AND state='active' AND revision=?`, target.InputID, target.Revision)
		if err != nil {
			return nil, err
		}
		if n, _ := row.RowsAffected(); n != 1 {
			return nil, ErrPlanStale
		}
		if target.Kind == "synced_playlist" {
			row, err := tx.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=?,revision=revision+1
 WHERE id=? AND revision=? AND local_state='pending' AND upstream_present=1`, trackID, target.EntryID, target.EntryRevision)
			if err != nil {
				return nil, err
			}
			if n, _ := row.RowsAffected(); n != 1 {
				return nil, ErrPlanStale
			}
			playlists[target.PlaylistID] = true
		}
	}
	ids := make([]int64, 0, len(playlists))
	for id := range playlists {
		if _, err := tx.Exec(`UPDATE synced_playlist SET revision=revision+1 WHERE id=?`, id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}

func discardImportTx(tx *sql.Tx, itemID int64) error {
	var status ItemStatus
	if err := tx.QueryRow(`SELECT status FROM import_item WHERE id=?`, itemID).Scan(&status); err != nil {
		return err
	}
	if status.IsTerminal() {
		return ErrItemNotActive
	}
	targets, err := importTargetsTx(tx, itemID)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return ErrItemNotActive
	}
	for _, target := range targets {
		if err := cancelImportDestinationTx(tx, target.InputID, "user_removed"); err != nil {
			return err
		}
		if target.Kind == "synced_playlist" {
			if _, err := tx.Exec(`UPDATE synced_playlist_entry SET local_state='ignored',revision=revision+1 WHERE id=? AND local_state='pending' AND upstream_present=1`, target.EntryID); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE synced_playlist SET revision=revision+1 WHERE id=?`, target.PlaylistID); err != nil {
				return err
			}
		}
	}
	return nil
}
