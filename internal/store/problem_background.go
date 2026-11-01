package store

import "database/sql"

func (s *Store) checkBackgroundItemTx(tx *sql.Tx, itemID int64) error {
	var id int64
	if err := tx.QueryRow("SELECT problem_id FROM review_problem_item WHERE item_id=? AND active=1", itemID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	held, err := problemProtectedTx(tx, id, s.problemNow())
	if err != nil {
		return err
	}
	if held {
		return ErrProblemBusy
	}
	return nil
}

// DeferProtectedInput returns an in-flight background row to review without
// publishing its candidates over an interactive draft. Claim avoids protected
// rows; this handles a lease acquired after an earlier claim.
func (s *Store) DeferProtectedInput(itemID, revision int64) (bool, error) {
	var deferred bool
	err := s.withTx(func(tx *sql.Tx) error {
		var id int64
		err := tx.QueryRow("SELECT problem_id FROM review_problem_item WHERE item_id=? AND active=1", itemID).Scan(&id)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		deferred, err = problemProtectedTx(tx, id, s.problemNow())
		if err != nil || !deferred {
			return err
		}
		if err := queueProblemRecheckTx(tx, id); err != nil {
			return err
		}
		// A newer interactive result owns its row now. Do not undo it.
		row, err := tx.Exec("UPDATE import_item SET status='review',revision=revision+1 WHERE id=? AND revision=? AND status='matching'", itemID, revision)
		if err != nil {
			return err
		}
		n, err := row.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		return syncProblemItemTx(tx, itemID, nil)
	})
	return deferred, err
}
