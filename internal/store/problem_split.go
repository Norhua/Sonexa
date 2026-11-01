package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/matching"
)

// SplitProblemItem is an explicit single-row work-scope change. Both revisions
// come from the displayed scope; no row or candidate from another scope moves.
// The isolated problem cannot automatically absorb new equivalent inputs.
func (s *Store) SplitProblemItem(problemID, problemRevision, itemID, inputRevision int64) (int64, error) {
	return s.splitProblemItem(problemID, problemRevision, itemID, inputRevision, nil)
}
func (s *Store) SplitProblemMember(scope ProblemScope, itemID int64) (int64, error) {
	for _, member := range scope.Members {
		if member.ItemID == itemID {
			return s.splitProblemItem(scope.ProblemID, scope.Revision, itemID, member.InputRevision, &scope)
		}
	}
	return 0, ErrPlanStale
}
func (s *Store) splitProblemItem(problemID, problemRevision, itemID, inputRevision int64, scope *ProblemScope) (int64, error) {
	var created int64
	if problemID <= 0 || problemRevision <= 0 || itemID <= 0 || inputRevision <= 0 {
		return 0, ErrPlanStale
	}
	err := s.withTx(func(tx *sql.Tx) error {
		lease := ""
		if scope != nil {
			if err := checkProblemScopeTx(tx, *scope); err != nil {
				return err
			}
			lease = scope.LeaseToken
		}
		if err := s.checkProblemAccessTx(tx, problemID, lease); err != nil {
			return err
		}
		if err := checkActiveItemTx(tx, itemID, inputRevision); err != nil {
			return err
		}
		var body string
		var query, candidates *string
		err := tx.QueryRow(`SELECT m.proof_json,p.query_json,p.candidate_facts FROM review_problem_item m
 JOIN review_problem p ON p.id=m.problem_id WHERE p.id=? AND p.status='active' AND p.revision=?
 AND m.item_id=? AND m.active=1 AND m.input_revision=?`, problemID, problemRevision, itemID, inputRevision).Scan(&body, &query, &candidates)
		if err == sql.ErrNoRows {
			return ErrPlanStale
		}
		if err != nil {
			return err
		}
		var count int
		if err := tx.QueryRow("SELECT count(*) FROM review_problem_item WHERE problem_id=? AND active=1", problemID).Scan(&count); err != nil {
			return err
		}
		if count < 2 {
			return ErrPlanStale
		}
		var input matching.ProblemInput
		if err := json.Unmarshal([]byte(body), &input); err != nil {
			return err
		}
		now := time.Now().UTC()
		// Invalidate the detached row's prepared single-row plans too. Other
		// inputs' revisions/values are unchanged; problem revision changes.
		if _, err := tx.Exec("UPDATE review_problem_item SET active=0,revision=revision+1,updated_at=? WHERE problem_id=? AND item_id=?", now, problemID, itemID); err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE import_item SET revision=revision+1,updated_at=? WHERE id=?", now, itemID); err != nil {
			return err
		}
		var errCreate error
		created, errCreate = createProblemTx(tx, input, true)
		if errCreate != nil {
			return errCreate
		}
		if _, err := tx.Exec("UPDATE review_problem SET query_json=?,candidate_facts=? WHERE id=?", query, candidates, created); err != nil {
			return err
		}
		if _, err := tx.Exec("INSERT INTO review_problem_item(problem_id,item_id,input_revision,proof_json,created_at,updated_at) VALUES(?,?,?,?,?,?)", created, itemID, inputRevision+1, body, now, now); err != nil {
			return err
		}
		if err := rebuildProblemTx(tx, problemID); err != nil {
			return err
		}
		return rebuildProblemTx(tx, created)
	})
	if err != nil {
		return 0, err
	}
	return created, nil
}
