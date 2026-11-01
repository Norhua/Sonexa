package store

import "context"

// RecheckNetworkBatches returns only batches explicitly queued by re-evaluation.
// It does not turn every paused/manual failed batch into background work.
func (s *Store) RecheckNetworkBatches(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT n.batch_id FROM review_problem_network n JOIN import_batch b ON b.id=n.batch_id
 WHERE b.status='running' ORDER BY n.batch_id LIMIT 32`)
	if err != nil {
		return nil, err
	}
	return readRecallIDs(rows)
}
func (s *Store) PruneRecheckNetwork() error {
	_, err := s.db.Exec(`DELETE FROM review_problem_network WHERE NOT EXISTS(
 SELECT 1 FROM import_item i WHERE i.batch_id=review_problem_network.batch_id AND i.status IN ('queued','matching'))`)
	return err
}
