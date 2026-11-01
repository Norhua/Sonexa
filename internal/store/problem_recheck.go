package store

import (
	"context"
	"database/sql"
	"errors"
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

func (s *Store) RestoreProblemRechecks(ctx context.Context) error {
	return s.withTx(func(tx *sql.Tx) error {
		// A persisted cursor may have skipped members while they were retrying.
		// Rewind on startup; do not resurrect terminal rows or reset their errors.
		_, err := tx.ExecContext(ctx, `INSERT INTO review_problem_recheck(problem_id)
 SELECT id FROM review_problem WHERE status='active'
 ON CONFLICT(problem_id) DO UPDATE SET revision=revision+1,after_item=0,retry_at=0`)
		return err
	})
}

func (s *Store) NextRecheckDelay(ctx context.Context) (time.Duration, error) {
	var deadline sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT min(deadline) FROM (
 SELECT retry_at AS deadline FROM review_problem_recheck WHERE retry_at>?
 UNION ALL SELECT expires_at FROM review_problem_lease WHERE released=0 AND expires_at>?
 UNION ALL SELECT expires_at FROM review_problem_operation WHERE expires_at>?)`, s.problemNow(), s.problemNow(), s.problemNow()).Scan(&deadline)
	if err != nil {
		return 0, err
	}
	if !deadline.Valid {
		return 0, nil
	}
	delay := time.Duration(deadline.Int64-s.problemNow()) * time.Millisecond
	if delay < time.Millisecond {
		delay = time.Millisecond
	}
	return delay, nil
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

func (s *Store) QueueRecheckNetwork(work ProblemRecheck) error {
	return s.withTx(func(tx *sql.Tx) error {
		if err := checkInputScopeTx(tx, work.Scope); err != nil {
			return err
		}
		if err := s.checkBackgroundItemTx(tx, work.Item.ID); err != nil {
			return err
		}
		var revision int64
		if err := tx.QueryRow("SELECT revision FROM review_library_state WHERE id=1").Scan(&revision); err != nil {
			return err
		}
		if revision != work.LibraryRevision {
			return ErrPlanStale
		}
		// Failed rows retain their explicit manual-retry policy. A paused batch
		// also stays paused; this hint never restarts another batch implicitly.
		var batchState BatchStatus
		if err := tx.QueryRow("SELECT status FROM import_batch WHERE id=?", work.Item.BatchID).Scan(&batchState); err != nil {
			return err
		}
		if work.Item.Status != StatusReview || batchState == BatchPaused {
			return nil
		}
		if _, err := tx.Exec("UPDATE import_item SET status='queued',revision=revision+1,next_retry_at=NULL,updated_at=? WHERE id=?", time.Now().UTC(), work.Item.ID); err != nil {
			return err
		}
		if err := syncProblemItemTx(tx, work.Item.ID, nil); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO review_problem_network SELECT ? WHERE NOT EXISTS(SELECT 1 FROM review_problem_network WHERE batch_id=?)", work.Item.BatchID, work.Item.BatchID); err != nil {
			return err
		}
		_, err := tx.Exec("UPDATE import_batch SET status='running',finished_at=NULL WHERE id=?", work.Item.BatchID)
		return err
	})
}

func (s *Store) FinishProblemRecheck(work ProblemRecheck, failure error) error {
	return s.withTx(func(tx *sql.Tx) error {
		if failure == nil {
			var revision int64
			if err := tx.QueryRow("SELECT revision FROM review_library_state WHERE id=1").Scan(&revision); err != nil {
				return err
			}
			// Even a no-op decision is stale if membership, candidate evidence or
			// library competitors changed while evaluating it.
			var status ItemStatus
			if err := tx.QueryRow("SELECT status FROM import_item WHERE id=?", work.Item.ID).Scan(&status); err != nil {
				return err
			}
			if revision != work.LibraryRevision {
				return queueProblemRecheckTx(tx, work.ProblemID)
			}
			if status == StatusReview || status == StatusFailed {
				if err := checkInputScopeTx(tx, work.Scope); err != nil {
					if errors.Is(err, ErrPlanStale) {
						return queueProblemRecheckTx(tx, work.ProblemID)
					}
					return err
				}
			}
		}
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
