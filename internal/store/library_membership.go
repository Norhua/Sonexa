package store

import (
	"database/sql"
	"time"
)

func initialLibraryTime(inLibrary bool, now time.Time) *time.Time {
	if !inLibrary {
		return nil
	}
	return &now
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

// setLibraryMembershipTx keeps the first admission time even after removal.
// User-facing preview and reference-aware removal are separate use cases.
func setLibraryMembershipTx(tx *sql.Tx, trackID int64, inLibrary bool, now time.Time) error {
	result, err := tx.Exec(`UPDATE track SET in_library=?,
 library_added_at=CASE WHEN ? THEN COALESCE(library_added_at,?) ELSE library_added_at END,
 revision=revision+CASE WHEN in_library<>? THEN 1 ELSE 0 END,
 updated_at=CASE WHEN in_library<>? THEN ? ELSE updated_at END WHERE id=?`, inLibrary, inLibrary, now, inLibrary, inLibrary, now, trackID)
	if err != nil {
		return err
	}
	if n, err := result.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrPlanStale
	}
	return nil
}

// Import creation uses persisted active destinations, never a client flag.
func newImportInLibraryTx(tx *sql.Tx, itemID int64) (bool, error) {
	if itemID == 0 {
		return true, nil // explicit local/search creation
	}
	var active, library int
	err := tx.QueryRow(`SELECT COUNT(*),COALESCE(SUM(d.kind='library'),0)
 FROM import_destination d JOIN import_item i ON i.id=d.input_id
 WHERE (i.id=? OR i.merged_item_id=?) AND d.state='active'`, itemID, itemID).Scan(&active, &library)
	if err != nil {
		return false, err
	}
	if active == 0 {
		return false, ErrItemNotActive
	}
	return library > 0, nil
}
