package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/norhua/sonexa/internal/matching"
)

type ProblemCandidate struct {
	ID       int64
	Revision int64
	Anchor   matching.ProblemAnchor
	Proof    matching.ProblemProof
}

type ProblemCandidates struct {
	Problems  []ProblemCandidate
	Truncated bool
}

// FindProblemCandidates is a read-only blocking query over ACTIVE anchors.
// Keys can over-recall; callers must re-prove membership in their write tx.
// Pagination, not the UI's browsing cap, allows inspection of every match.
func (s *Store) FindProblemCandidates(ctx context.Context, input matching.ProblemInput, after int64, limit int) (ProblemCandidates, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ProblemCandidates{}, err
	}
	defer tx.Rollback()
	out, err := findProblemCandidatesTx(ctx, tx, input, after, limit)
	if err != nil {
		return ProblemCandidates{}, err
	}
	return out, tx.Commit()
}

func findProblemCandidatesTx(ctx context.Context, tx *sql.Tx, input matching.ProblemInput, after int64, limit int) (ProblemCandidates, error) {
	out := ProblemCandidates{Problems: []ProblemCandidate{}}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	keys := matching.ProblemRecallKeys(input)
	if len(keys) == 0 {
		return out, nil
	}
	encoded, err := json.Marshal(keys)
	if err != nil {
		return out, err
	}
	// The anchor is the rebuildable source of these keys; no historical
	// decisions/candidate lists are searched. JSON avoids variable limits and
	// keeps proof versioning explicit without a separate mutable index yet.
	rows, err := tx.QueryContext(ctx, `SELECT id,revision,anchor_json,proof_json FROM review_problem p
 WHERE status='active' AND id>? AND rule_version=?
 AND json_extract(anchor_json,'$.version')=?
 AND EXISTS (
  SELECT 1 FROM json_each(p.anchor_json,'$.recallKeys') stored JOIN json_each(?) query
  ON json_extract(stored.value,'$.kind')=json_extract(query.value,'$.kind')
  AND json_extract(stored.value,'$.value')=json_extract(query.value,'$.value')
 ) ORDER BY id LIMIT ?`, after, matching.MetadataRuleVersion, matching.ProblemProofVersion, string(encoded), limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate ProblemCandidate
		var anchor, proof string
		if err := rows.Scan(&candidate.ID, &candidate.Revision, &anchor, &proof); err != nil {
			return out, err
		}
		if err := json.Unmarshal([]byte(anchor), &candidate.Anchor); err != nil {
			return out, fmt.Errorf("读取问题锚点失败: %w", err)
		}
		if err := json.Unmarshal([]byte(proof), &candidate.Proof); err != nil {
			return out, fmt.Errorf("读取问题证明失败: %w", err)
		}
		out.Problems = append(out.Problems, candidate)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	if len(out.Problems) > limit {
		out.Truncated = true
		out.Problems = out.Problems[:limit]
	}
	return out, nil
}
