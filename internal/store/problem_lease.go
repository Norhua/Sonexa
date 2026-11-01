package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

const ProblemLeaseTTL = 90 * time.Second
const ProblemOperationTTL = 10 * time.Minute

var ErrProblemBusy = errors.New("problem_busy: 当前问题正在交互，请稍后重评")

type ProblemInteraction struct {
	Lease domain.ProblemLease `json:"lease"`
	Scope ProblemScope        `json:"scope"`
}

type ProblemOperation struct {
	Token     string
	ProblemID int64
}

// SetProblemClock is an assembly/test seam, not a client clock. Configure it
// before concurrent use. Lease renewal never reloads the queue or goes online.
func (s *Store) SetProblemClock(now func() time.Time) {
	s.leaseClockMu.Lock()
	defer s.leaseClockMu.Unlock()
	s.leaseClock = now
}
func (s *Store) problemNow() int64 {
	s.leaseClockMu.RLock()
	now := s.leaseClock
	s.leaseClockMu.RUnlock()
	if now != nil {
		return now().UnixMilli()
	}
	return time.Now().UnixMilli()
}
func problemToken() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
func queueProblemRecheckTx(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`INSERT INTO review_problem_recheck(problem_id)
 SELECT id FROM review_problem WHERE id=? AND status='active'
 ON CONFLICT(problem_id) DO UPDATE SET revision=revision+1,after_item=0,retry_at=0,last_error=NULL`, id)
	return err
}
func problemProtectedTx(tx *sql.Tx, id, now int64) (bool, error) {
	var protected bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM review_problem_lease WHERE problem_id=? AND released=0 AND expires_at>?)
 OR EXISTS(SELECT 1 FROM review_problem_operation WHERE problem_id=? AND expires_at>?)`, id, now, id, now).Scan(&protected)
	return protected, err
}
func checkProblemLeaseTx(tx *sql.Tx, id int64, token string, now int64) error {
	var valid bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM review_problem_lease l JOIN review_problem p ON p.id=l.problem_id
 WHERE l.problem_id=? AND l.token=? AND l.released=0 AND l.expires_at>? AND p.status='active')`, id, token, now).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrPlanStale
	}
	return nil
}
func (s *Store) checkProblemAccessTx(tx *sql.Tx, id int64, token string) error {
	if token != "" {
		return checkProblemLeaseTx(tx, id, token, s.problemNow())
	}
	var held bool
	if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM review_problem_lease WHERE problem_id=? AND released=0 AND expires_at>?)", id, s.problemNow()).Scan(&held); err != nil {
		return err
	}
	if held {
		return ErrProblemBusy
	}
	return nil
}

func (s *Store) checkProblemLeaseTxOptional(tx *sql.Tx, id int64, token string) error {
	if token == "" {
		return nil
	}
	return checkProblemLeaseTx(tx, id, token, s.problemNow())
}

func (s *Store) checkProblemOperationTx(tx *sql.Tx, op ProblemOperation) error {
	var valid bool
	err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM review_problem_operation o
 LEFT JOIN review_problem_lease l ON l.token=o.lease_token JOIN review_problem p ON p.id=o.problem_id
 WHERE o.token=? AND o.problem_id=? AND o.expires_at>? AND p.status='active'
 AND (o.lease_token IS NULL OR l.released=0))`, op.Token, op.ProblemID, s.problemNow()).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrPlanStale
	}
	return nil
}
func (s *Store) beginProblemOperationTx(tx *sql.Tx, id int64, lease string) (ProblemOperation, error) {
	op := ProblemOperation{ProblemID: id}
	now := s.problemNow()
	if lease != "" {
		if err := checkProblemLeaseTx(tx, id, lease, now); err != nil {
			return op, err
		}
	} else {
		var held bool
		if err := tx.QueryRow("SELECT EXISTS(SELECT 1 FROM review_problem_lease WHERE problem_id=? AND released=0 AND expires_at>?)", id, now).Scan(&held); err != nil {
			return op, err
		}
		if held {
			return op, ErrProblemBusy
		}
	}
	var err error
	op.Token, err = problemToken()
	if err != nil {
		return op, err
	}
	_, err = tx.Exec("INSERT INTO review_problem_operation(token,problem_id,lease_token,expires_at) VALUES(?,?,?,?)", op.Token, id, nonblank(lease), now+ProblemOperationTTL.Milliseconds())
	return op, err
}

func retireProblemClaimsTx(tx *sql.Tx, scope ProblemScope) error {
	for _, member := range scope.Members {
		row, err := tx.Exec("UPDATE import_item SET status='review',revision=revision+1 WHERE id=? AND status='matching'", member.ItemID)
		if err != nil {
			return err
		}
		n, err := row.RowsAffected()
		if err != nil {
			return err
		}
		if n > 0 {
			if err := syncProblemItemTx(tx, member.ItemID, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) AcquireProblemLease(scope ProblemScope) (*ProblemInteraction, error) {
	var out ProblemInteraction
	err := s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		if held, err := problemProtectedTx(tx, scope.ProblemID, s.problemNow()); err != nil {
			return err
		} else if held {
			return ErrProblemBusy
		}
		// No live operation remains; remove an expired/released lease and its
		// expired operation rows before replacing it with a fresh capability.
		if _, err := tx.Exec("DELETE FROM review_problem_lease WHERE problem_id=?", scope.ProblemID); err != nil {
			return err
		}
		token, err := problemToken()
		if err != nil {
			return err
		}
		out.Lease = domain.ProblemLease{Token: token, ProblemID: scope.ProblemID, ExpiresAt: s.problemNow() + ProblemLeaseTTL.Milliseconds()}
		if _, err := tx.Exec("INSERT INTO review_problem_lease(token,problem_id,expires_at) VALUES(?,?,?)", token, scope.ProblemID, out.Lease.ExpiresAt); err != nil {
			return err
		}
		// Acquisition invalidates decisions/queries started before interaction,
		// including an automatic plan submitted after this lease is released.
		if _, err := tx.Exec("UPDATE review_problem SET revision=revision+1,query_revision=query_revision+1 WHERE id=?", scope.ProblemID); err != nil {
			return err
		}
		// Retire a background claim before publishing the interactive scope.
		// Its late result keeps the old input revision and cannot overwrite
		// the draft. Candidate facts/original input/error are left untouched.
		if err := retireProblemClaimsTx(tx, scope); err != nil {
			return err
		}
		out.Scope, err = readProblemScopeTx(tx, scope.ProblemID)
		out.Scope.LeaseToken = token
		return err
	})
	return &out, err
}
func (s *Store) RenewProblemLease(lease domain.ProblemLease) (*domain.ProblemLease, error) {
	err := s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemLeaseTx(tx, lease.ProblemID, lease.Token, s.problemNow()); err != nil {
			return err
		}
		lease.ExpiresAt = s.problemNow() + ProblemLeaseTTL.Milliseconds()
		_, err := tx.Exec("UPDATE review_problem_lease SET expires_at=? WHERE token=?", lease.ExpiresAt, lease.Token)
		return err
	})
	return &lease, err
}
func (s *Store) ReleaseProblemLease(lease domain.ProblemLease) error {
	return s.withTx(func(tx *sql.Tx) error {
		var released bool
		err := tx.QueryRow("SELECT released FROM review_problem_lease WHERE token=? AND problem_id=?", lease.Token, lease.ProblemID).Scan(&released)
		if err == sql.ErrNoRows {
			return ErrPlanStale
		}
		if err != nil {
			return err
		}
		if released {
			return nil
		}
		row, err := tx.Exec("UPDATE review_problem_lease SET released=1 WHERE token=? AND problem_id=?", lease.Token, lease.ProblemID)
		if err != nil {
			return err
		}
		n, err := row.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrPlanStale
		}
		// Pins remain until operations return or their hard deadline. Their
		// results are already invalid: checkProblemOperationTx sees released.
		return queueProblemRecheckTx(tx, lease.ProblemID)
	})
}
func (s *Store) BeginInputOperation(scope *domain.InputScope) (ProblemOperation, error) {
	var op ProblemOperation
	if scope == nil || scope.Problem == nil {
		return op, nil
	}
	err := s.withTx(func(tx *sql.Tx) error {
		if err := checkInputScopeTx(tx, scope); err != nil {
			return err
		}
		var err error
		op, err = s.beginProblemOperationTx(tx, scope.Problem.ID, scope.LeaseToken)
		return err
	})
	return op, err
}
func (s *Store) CheckProblemOperation(op ProblemOperation) error {
	if op.Token == "" {
		return nil
	}
	return s.withTx(func(tx *sql.Tx) error { return s.checkProblemOperationTx(tx, op) })
}
func (s *Store) EndProblemOperation(op ProblemOperation) error {
	if op.Token == "" {
		return nil
	}
	return s.withTx(func(tx *sql.Tx) error {
		row, err := tx.Exec("DELETE FROM review_problem_operation WHERE token=? AND problem_id=?", op.Token, op.ProblemID)
		if err != nil {
			return err
		}
		n, err := row.RowsAffected()
		if err != nil || n == 0 {
			return err
		}
		return queueProblemRecheckTx(tx, op.ProblemID)
	})
}

// ReapProblemLeases is bounded and clock-injectable. Dead processes cannot
// retain pins forever; expiry enqueues a current-evidence recheck, not a target.
func (s *Store) ReapProblemLeases(limit int) error {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	return s.withTx(func(tx *sql.Tx) error {
		rows, err := tx.Query(`SELECT problem_id FROM review_problem_lease l
 WHERE (l.expires_at<=? OR l.released=1)
 AND NOT EXISTS(SELECT 1 FROM review_problem_operation o WHERE o.lease_token=l.token AND o.expires_at>?)
 UNION SELECT problem_id FROM review_problem_operation WHERE expires_at<=? LIMIT ?`, s.problemNow(), s.problemNow(), s.problemNow(), limit)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := queueProblemRecheckTx(tx, id); err != nil {
				return err
			}
			// An expired lease can remain as the parent of a live operation.
			// Don't mark it released: the bounded in-flight request may finish.
			if _, err := tx.Exec("DELETE FROM review_problem_operation WHERE problem_id=? AND expires_at<=?", id, s.problemNow()); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM review_problem_lease WHERE problem_id=? AND (expires_at<=? OR released=1)
 AND NOT EXISTS(SELECT 1 FROM review_problem_operation WHERE lease_token=review_problem_lease.token)`, id, s.problemNow()); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) checkAdmissionInteractionTx(tx *sql.Tx, seed domain.AdmissionSeed) error {
	scope := seed.InputScope
	if scope == nil || scope.Problem == nil {
		return nil
	}
	id := scope.Problem.ID
	if seed.Automatic {
		if scope.LeaseToken != "" {
			return ErrPlanStale
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
	if err := s.checkProblemAccessTx(tx, id, scope.LeaseToken); err != nil {
		return err
	}
	return nil
}
