package store

import (
	"encoding/json"
	"fmt"
)

// ReviewProblemSnapshot is a read model, not an authorization to widen an
// operation. Scope and every source row come from the same database snapshot.
type ReviewProblemSnapshot struct {
	Scope   ProblemScope
	Query   *ReviewQuery
	Sources []ReviewProblemSource
}

type ReviewProblemSource struct {
	Item     ImportItem
	BatchTag string
}

type ReviewProblemQueue struct {
	Revision int64
	Problems ProblemStats
	Items    []ReviewProblemSnapshot
}

// ReadReviewProblemQueue includes all active members, including rows retrying
// in queued/matching. Reading never acquires a lease or schedules network work.
func (s *Store) ReadReviewProblemQueue() (*ReviewProblemQueue, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &ReviewProblemQueue{Items: []ReviewProblemSnapshot{}}
	if err := tx.QueryRow("SELECT revision FROM task_change_state WHERE id=1").Scan(&out.Revision); err != nil {
		return nil, err
	}
	rows, err := tx.Query("SELECT id,query_json FROM review_problem WHERE status='active' ORDER BY id")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var raw *string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		p := ReviewProblemSnapshot{Scope: ProblemScope{ProblemID: id}, Sources: []ReviewProblemSource{}}
		if raw != nil {
			p.Query = &ReviewQuery{}
			if err := json.Unmarshal([]byte(*raw), p.Query); err != nil || p.Query.Version != ReviewProtocolVersion {
				rows.Close()
				return nil, fmt.Errorf("问题 %d 的查询版本或内容无效", id)
			}
		}
		out.Items = append(out.Items, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()

	// Close each cursor before the next query; never mix a later membership
	// snapshot with an earlier count or silently omit a source on read failure.
	filenames := map[int64]string{}
	for i := range out.Items {
		p := &out.Items[i]
		p.Scope, err = readProblemScopeTx(tx, p.Scope.ProblemID)
		if err != nil {
			return nil, err
		}
		items, err := tx.Query("SELECT "+itemColumns+` FROM import_item
 WHERE id IN (SELECT item_id FROM review_problem_item WHERE problem_id=? AND active=1) ORDER BY id`, p.Scope.ProblemID)
		if err != nil {
			return nil, err
		}
		sources, err := collectItems(items)
		items.Close()
		if err != nil {
			return nil, err
		}
		for _, item := range sources {
			filename, known := filenames[item.BatchID]
			if !known {
				if err := tx.QueryRow("SELECT filename FROM import_batch WHERE id=?", item.BatchID).Scan(&filename); err != nil {
					return nil, err
				}
				filenames[item.BatchID] = filename
			}
			p.Sources = append(p.Sources, ReviewProblemSource{Item: item, BatchTag: filename})
		}
	}
	out.Problems, err = problemStatsTx(tx, 0)
	if err != nil {
		return nil, err
	}
	return out, nil
}
