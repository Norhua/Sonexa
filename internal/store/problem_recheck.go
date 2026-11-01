package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

type ProblemRecheck struct {
	ProblemID       int64
	Revision        int64
	LibraryRevision int64
	Scope           *domain.InputScope
	Proof           string
	Item            *ImportItem
}

// NextProblemRecheck reads one member at a time to bound even very large
// problems. No claims or rows are changed here; revision/CAS protects finish.
func (s *Store) NextProblemRecheck(ctx context.Context) (*ProblemRecheck, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var out ProblemRecheck
	var itemID int64
	err = tx.QueryRowContext(ctx, `SELECT q.problem_id,q.revision,m.item_id FROM review_problem_recheck q
 JOIN review_problem p ON p.id=q.problem_id AND p.status='active'
 JOIN review_problem_item m ON m.problem_id=p.id AND m.active=1
 JOIN import_item i ON i.id=m.item_id AND i.status IN ('review','failed')
 WHERE q.retry_at<=? AND m.item_id>q.after_item
 AND NOT EXISTS(SELECT 1 FROM review_problem_lease l WHERE l.problem_id=p.id AND l.released=0 AND l.expires_at>?)
 AND NOT EXISTS(SELECT 1 FROM review_problem_operation o WHERE o.problem_id=p.id AND o.expires_at>?)
 ORDER BY q.problem_id,m.item_id LIMIT 1`, s.problemNow(), s.problemNow(), s.problemNow()).Scan(&out.ProblemID, &out.Revision, &itemID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out.Scope, err = readSingleInputScopeTx(tx, itemID, 0)
	if err != nil {
		return nil, err
	}
	out.Item, err = scanItem(tx.QueryRowContext(ctx, "SELECT "+itemColumns+" FROM import_item WHERE id=?", itemID))
	if err != nil {
		return nil, err
	}
	if err := tx.QueryRow("SELECT proof_json FROM review_problem_item WHERE problem_id=? AND item_id=? AND active=1", out.ProblemID, itemID).Scan(&out.Proof); err != nil {
		return nil, err
	}
	if err := tx.QueryRow("SELECT revision FROM review_library_state WHERE id=1").Scan(&out.LibraryRevision); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Store) FinishProblemRecheck(work ProblemRecheck, failure error) error {
	return s.withTx(func(tx *sql.Tx) error {
		if failure != nil {
			_, err := tx.Exec("UPDATE review_problem_recheck SET last_error=?,retry_at=? WHERE problem_id=? AND revision=?", failure.Error(), s.problemNow()+time.Minute.Milliseconds(), work.ProblemID, work.Revision)
			return err
		}
		// Changes during evaluation must not consume the new hint. A successful
		// skip may have removed this member, but the hint is not musical evidence.
		if _, err := tx.Exec("UPDATE review_problem_recheck SET after_item=? WHERE problem_id=? AND revision=?", work.Item.ID, work.ProblemID, work.Revision); err != nil {
			return err
		}
		_, err := tx.Exec(`DELETE FROM review_problem_recheck WHERE problem_id=? AND revision=?
 AND NOT EXISTS(SELECT 1 FROM review_problem_item m JOIN import_item i ON i.id=m.item_id
 WHERE m.problem_id=review_problem_recheck.problem_id AND m.active=1 AND m.item_id>after_item AND i.status IN ('review','failed'))`, work.ProblemID, work.Revision)
		return err
	})
}
func (s *Store) CheckRecheckRevision(revision int64) error {
	var current int64
	if err := s.db.QueryRow("SELECT revision FROM review_library_state WHERE id=1").Scan(&current); err != nil {
		return err
	}
	if current != revision {
		return ErrPlanStale
	}
	return nil
}

// QueueProblemRecheck is also used on current-work membership changes/recovery.
func (s *Store) QueueProblemRecheck(id int64) error {
	return s.withTx(func(tx *sql.Tx) error { return queueProblemRecheckTx(tx, id) })
}
