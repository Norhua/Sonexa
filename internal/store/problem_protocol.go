package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/norhua/sonexa/internal/matching"
)

const ReviewProtocolVersion = 1

type ProblemMemberRevision struct {
	ItemID        int64 `json:"itemId"`
	Revision      int64 `json:"revision"`
	InputRevision int64 `json:"inputRevision"`
}
type ProblemScope struct {
	LeaseToken        string                  `json:"leaseToken,omitempty"`
	OperationToken    string                  `json:"operationToken,omitempty"`
	Version           int                     `json:"version"`
	ProblemID         int64                   `json:"problemId"`
	Revision          int64                   `json:"revision"`
	QueryRevision     int64                   `json:"queryRevision"`
	CandidateRevision int64                   `json:"candidateRevision"`
	Members           []ProblemMemberRevision `json:"members"`
}
type ReviewQuery struct {
	Version  int    `json:"version"`
	Title    string `json:"title"`
	Artist   string `json:"artist"`
	Album    string `json:"album"`
	Provider string `json:"provider"`
	Country  string `json:"country"`
}

// ReviewCandidateFacts contains source facts only; never UI ownership/actions.
type ReviewCandidateFacts struct {
	Version       int                        `json:"version"`
	Candidates    []matching.ScoredCandidate `json:"candidates"`
	TriedVariants []string                   `json:"triedVariants"`
}
type ProblemMemberFacts struct {
	ItemID        int64                `json:"itemId"`
	InputRevision int64                `json:"inputRevision"`
	Facts         ReviewCandidateFacts `json:"facts"`
}
type ProblemCandidateFacts struct {
	Version int                  `json:"version"`
	Members []ProblemMemberFacts `json:"members"`
}

func EncodeReviewCandidates(candidates []matching.ScoredCandidate, variants []string) (string, error) {
	if candidates == nil {
		candidates = []matching.ScoredCandidate{}
	}
	if variants == nil {
		variants = []string{}
	}
	body, err := json.Marshal(ReviewCandidateFacts{Version: ReviewProtocolVersion, Candidates: candidates, TriedVariants: variants})
	return string(body), err
}
func DecodeReviewCandidates(raw string) (ReviewCandidateFacts, error) {
	out := ReviewCandidateFacts{Version: ReviewProtocolVersion, Candidates: []matching.ScoredCandidate{}, TriedVariants: []string{}}
	if raw == "" {
		return out, nil
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return out, err
	}
	var envelope struct {
		Version *int `json:"version"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil || envelope.Version == nil {
		return out, fmt.Errorf("候选事实缺少协议版本")
	}
	if out.Version != ReviewProtocolVersion {
		return out, fmt.Errorf("候选事实版本已失效")
	}
	return out, nil
}

func readProblemScopeTx(tx *sql.Tx, id int64) (ProblemScope, error) {
	out := ProblemScope{Version: ReviewProtocolVersion, ProblemID: id, Members: []ProblemMemberRevision{}}
	err := tx.QueryRow("SELECT revision,query_revision,candidate_revision FROM review_problem WHERE id=? AND status='active'", id).Scan(&out.Revision, &out.QueryRevision, &out.CandidateRevision)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrPlanStale
	}
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(`SELECT m.item_id,m.revision,i.revision,m.input_revision,i.status FROM review_problem_item m
 JOIN import_item i ON i.id=m.item_id WHERE m.problem_id=? AND m.active=1 ORDER BY m.item_id`, id)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var m ProblemMemberRevision
		var expected int64
		var status ItemStatus
		if err := rows.Scan(&m.ItemID, &m.Revision, &m.InputRevision, &expected, &status); err != nil {
			return out, err
		}
		if expected != m.InputRevision || !activeItemStatus(status) {
			return out, ErrPlanStale
		}
		out.Members = append(out.Members, m)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Members) == 0 {
		return out, ErrPlanStale
	}
	return out, nil
}
func checkProblemScopeTx(tx *sql.Tx, scope ProblemScope) error {
	current, err := readProblemScopeTx(tx, scope.ProblemID)
	if err != nil {
		return err
	}
	scope.LeaseToken, scope.OperationToken = "", ""
	if !reflect.DeepEqual(current, scope) {
		return ErrPlanStale
	}
	return nil
}
func (s *Store) ReadProblemScope(id int64) (ProblemScope, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProblemScope{}, err
	}
	defer tx.Rollback()
	return readProblemScopeTx(tx, id)
}
func (s *Store) ItemProblemScope(itemID int64) (ProblemScope, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProblemScope{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow("SELECT problem_id FROM review_problem_item WHERE item_id=? AND active=1", itemID).Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return ProblemScope{}, ErrPlanStale
		}
		return ProblemScope{}, err
	}
	return readProblemScopeTx(tx, id)
}

// UpdateProblemDraft invalidates old searches/plans without publishing new
// candidates or modifying source rows. Only the active lease holder may edit.
func (s *Store) UpdateProblemDraft(scope ProblemScope, query ReviewQuery) (ProblemScope, error) {
	var out ProblemScope
	if query.Version != ReviewProtocolVersion || scope.LeaseToken == "" {
		return out, ErrPlanStale
	}
	err := s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		if err := checkProblemLeaseTx(tx, scope.ProblemID, scope.LeaseToken, s.problemNow()); err != nil {
			return err
		}
		body, err := json.Marshal(query)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE review_problem SET query_json=?,query_revision=query_revision+1,revision=revision+1,updated_at=? WHERE id=?", string(body), time.Now().UTC(), scope.ProblemID); err != nil {
			return err
		}
		out, err = readProblemScopeTx(tx, scope.ProblemID)
		out.LeaseToken = scope.LeaseToken
		return err
	})
	return out, err
}

func problemQueryScopeHash(scope ProblemScope) (string, error) {
	scope.OperationToken = ""
	return inputEvidenceHash(scope)
}

// BeginProblemQuery reserves a query revision before I/O. A later query,
// membership change or terminal row makes the returned token unusable.
func (s *Store) BeginProblemQuery(scope ProblemScope, query ReviewQuery) (ProblemScope, error) {
	if query.Version != ReviewProtocolVersion {
		return ProblemScope{}, ErrPlanStale
	}
	var out ProblemScope
	err := s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		body, err := json.Marshal(query)
		if err != nil {
			return err
		}
		if _, err := tx.Exec("UPDATE review_problem SET query_json=?,query_revision=query_revision+1,revision=revision+1,updated_at=? WHERE id=?", string(body), time.Now().UTC(), scope.ProblemID); err != nil {
			return err
		}
		op, err := s.beginProblemOperationTx(tx, scope.ProblemID, scope.LeaseToken)
		if err != nil {
			return err
		}
		if err := retireProblemClaimsTx(tx, scope); err != nil {
			return err
		}
		out, err = readProblemScopeTx(tx, scope.ProblemID)
		out.LeaseToken, out.OperationToken = scope.LeaseToken, op.Token
		hash, err := problemQueryScopeHash(out)
		if err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE review_problem_operation SET scope_hash=? WHERE token=?", hash, op.Token)
		return err
	})
	return out, err
}
func (s *Store) CompleteProblemQuery(scope ProblemScope, facts ReviewCandidateFacts) error {
	op := ProblemOperation{Token: scope.OperationToken, ProblemID: scope.ProblemID}
	defer s.EndProblemOperation(op)
	if facts.Version != ReviewProtocolVersion {
		return ErrPlanStale
	}
	return s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		if err := s.checkProblemOperationTx(tx, op); err != nil {
			return err
		}
		hash, err := problemQueryScopeHash(scope)
		if err != nil {
			return err
		}
		var saved string
		if err := tx.QueryRow("SELECT scope_hash FROM review_problem_operation WHERE token=?", op.Token).Scan(&saved); err != nil {
			return err
		}
		if saved != hash {
			return ErrPlanStale
		}
		body, err := EncodeReviewCandidates(facts.Candidates, facts.TriedVariants)
		if err != nil {
			return err
		}
		variants, err := json.Marshal(facts.TriedVariants)
		if err != nil {
			return err
		}
		for _, m := range scope.Members {
			if _, err := tx.Exec(`UPDATE import_item SET status='review',candidates=?,tried_variants=?,revision=revision+1,updated_at=? WHERE id=?`, body, string(variants), time.Now().UTC(), m.ItemID); err != nil {
				return err
			}
			if err := syncProblemItemTx(tx, m.ItemID, nil); err != nil {
				return err
			}
		}
		return nil
	})
}

// NoneMatchedProblem invalidates in-flight queries and clears only the explicit
// current problem scope; all source rows remain active and unchanged in origin.
func (s *Store) NoneMatchedProblem(scope ProblemScope) error {
	return s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		if err := s.checkProblemAccessTx(tx, scope.ProblemID, scope.LeaseToken); err != nil {
			return err
		}
		body, err := EncodeReviewCandidates(nil, nil)
		if err != nil {
			return err
		}
		for _, m := range scope.Members {
			if _, err := tx.Exec("UPDATE import_item SET candidates=?,revision=revision+1,updated_at=? WHERE id=?", body, time.Now().UTC(), m.ItemID); err != nil {
				return err
			}
			if err := syncProblemItemTx(tx, m.ItemID, nil); err != nil {
				return err
			}
		}
		_, err = tx.Exec("UPDATE review_problem SET query_revision=query_revision+1 WHERE id=?", scope.ProblemID)
		return err
	})
}
func selectedProblemMembers(scope ProblemScope, ids []int64) ([]ProblemMemberRevision, error) {
	if len(ids) == 0 {
		return nil, ErrPlanStale
	}
	selected := []ProblemMemberRevision{}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] {
			return nil, ErrPlanStale
		}
		seen[id] = true
		found := false
		for _, member := range scope.Members {
			if member.ItemID == id {
				selected = append(selected, member)
				found = true
				break
			}
		}
		if !found {
			return nil, ErrPlanStale
		}
	}
	return selected, nil
}
func (s *Store) RetryProblemMembers(scope ProblemScope, ids []int64) ([]int64, error) {
	members, err := selectedProblemMembers(scope, ids)
	if err != nil {
		return nil, err
	}
	batches := []int64{}
	err = s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		if err := s.checkProblemAccessTx(tx, scope.ProblemID, scope.LeaseToken); err != nil {
			return err
		}
		seen := map[int64]bool{}
		for _, m := range members {
			var batch int64
			var status ItemStatus
			if err := tx.QueryRow("SELECT batch_id,status FROM import_item WHERE id=?", m.ItemID).Scan(&batch, &status); err != nil {
				return err
			}
			if status != StatusFailed {
				return ErrPlanStale
			}
			if _, err := tx.Exec("UPDATE import_item SET status='queued',next_retry_at=NULL,revision=revision+1,updated_at=? WHERE id=?", time.Now().UTC(), m.ItemID); err != nil {
				return err
			}
			if err := syncProblemItemTx(tx, m.ItemID, nil); err != nil {
				return err
			}
			if !seen[batch] {
				seen[batch] = true
				batches = append(batches, batch)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return batches, nil
}
func (s *Store) DiscardProblemMembers(scope ProblemScope, ids []int64) error {
	members, err := selectedProblemMembers(scope, ids)
	if err != nil {
		return err
	}
	return s.withTx(func(tx *sql.Tx) error {
		if err := checkProblemScopeTx(tx, scope); err != nil {
			return err
		}
		if err := s.checkProblemAccessTx(tx, scope.ProblemID, scope.LeaseToken); err != nil {
			return err
		}
		for _, m := range members {
			if err := finishProblemItemTx(tx, m.ItemID); err != nil {
				return err
			}
			if _, err := tx.Exec("UPDATE import_item SET status='discarded',resolution_reason=?,revision=revision+1,updated_at=? WHERE id=?", ResolutionUserDiscarded, time.Now().UTC(), m.ItemID); err != nil {
				return err
			}
		}
		return nil
	})
}

// rebuildProblemCandidatesTx keeps one source-only shape for all current rows.
// Legacy scored arrays from internal old row writers are projected into the
// same source-only envelope; production Runner and research write it directly.
func rebuildProblemCandidatesTx(tx *sql.Tx, id int64) error {
	out := ProblemCandidateFacts{Version: ReviewProtocolVersion, Members: []ProblemMemberFacts{}}
	rows, err := tx.Query(`SELECT i.id,i.revision,i.candidates FROM review_problem_item m JOIN import_item i ON i.id=m.item_id WHERE m.problem_id=? AND m.active=1 ORDER BY i.id`, id)
	if err != nil {
		return err
	}
	for rows.Next() {
		var m ProblemMemberFacts
		var body *string
		if err := rows.Scan(&m.ItemID, &m.InputRevision, &body); err != nil {
			rows.Close()
			return err
		}
		raw := ""
		if body != nil {
			raw = *body
		}
		facts, err := DecodeReviewCandidates(raw)
		if err != nil {
			var legacy []matching.ScoredCandidate
			if json.Unmarshal([]byte(raw), &legacy) != nil {
				rows.Close()
				return err
			}
			// Internal older scored fixtures are source facts too. Never
			// discard a valid source candidate merely due to its old wrapper.
			facts, _ = DecodeReviewCandidates("")
			for _, candidate := range legacy {
				if candidate.Candidate.MatchRef != "" {
					facts.Candidates = append(facts.Candidates, candidate)
				}
			}
		}
		m.Facts = facts
		out.Members = append(out.Members, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	body, err := json.Marshal(out)
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE review_problem SET candidate_facts=?,candidate_revision=candidate_revision+1 WHERE id=?", string(body), id)
	return err
}
