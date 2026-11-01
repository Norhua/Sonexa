package store

import "database/sql"

// ProblemStats counts distinct active problems; SourceRows counts their active
// input members. Cross-batch totals cannot be obtained by summing batch stats.
type ProblemStats struct {
	ReviewProblems  int `json:"reviewProblems"`
	PendingProblems int `json:"pendingProblems"`
	FailedProblems  int `json:"failedProblems"`
	SourceRows      int `json:"sourceRows"`
}

func problemStatsTx(tx *sql.Tx, batchID int64) (ProblemStats, error) {
	var out ProblemStats
	err := tx.QueryRow(`WITH problems AS (
 SELECT p.id,sum(CASE WHEN i.status='failed' THEN 0 ELSE 1 END) AS pending
 FROM review_problem p JOIN review_problem_item m ON m.problem_id=p.id AND m.active=1
 JOIN import_item i ON i.id=m.item_id WHERE p.status='active' GROUP BY p.id
 ), scoped AS (
 SELECT p.id,p.pending,count(*) AS rows FROM problems p
 JOIN review_problem_item m ON m.problem_id=p.id AND m.active=1 JOIN import_item i ON i.id=m.item_id
 WHERE (?=0 OR i.batch_id=?) GROUP BY p.id,p.pending
 ) SELECT count(*),COALESCE(sum(pending>0),0),COALESCE(sum(pending=0),0),COALESCE(sum(rows),0) FROM scoped`, batchID, batchID).Scan(&out.ReviewProblems, &out.PendingProblems, &out.FailedProblems, &out.SourceRows)
	return out, err
}
func (s *Store) ReviewProblemStats(batchID int64) (ProblemStats, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return ProblemStats{}, err
	}
	defer tx.Rollback()
	return problemStatsTx(tx, batchID)
}
