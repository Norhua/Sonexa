package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"time"
)

type LibraryAddition struct {
	WillAdd          []int64 `json:"willAdd"`
	AlreadyInLibrary []int64 `json:"alreadyInLibrary"`
}

func libraryAdditionTx(tx *sql.Tx, ids []int64) (*LibraryAddition, error) {
	out := &LibraryAddition{WillAdd: []int64{}, AlreadyInLibrary: []int64{}}
	seen := map[int64]bool{}
	for _, id := range ids {
		if id <= 0 {
			return nil, ErrPlanStale
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		var inLibrary bool
		if err := tx.QueryRow(`SELECT in_library FROM track WHERE id=?`, id).Scan(&inLibrary); err != nil {
			if err == sql.ErrNoRows {
				return nil, ErrPlanStale
			}
			return nil, err
		}
		if inLibrary {
			out.AlreadyInLibrary = append(out.AlreadyInLibrary, id)
		} else {
			out.WillAdd = append(out.WillAdd, id)
		}
	}
	sort.Slice(out.WillAdd, func(i, j int) bool { return out.WillAdd[i] < out.WillAdd[j] })
	sort.Slice(out.AlreadyInLibrary, func(i, j int) bool { return out.AlreadyInLibrary[i] < out.AlreadyInLibrary[j] })
	return out, nil
}
func (s *Store) PreviewLibraryAddition(ids []int64) (*LibraryAddition, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return libraryAdditionTx(tx, ids)
}
func (s *Store) CommitLibraryAddition(ctx context.Context, planID, hash string, ids []int64) (*LibraryAddition, error) {
	var result *LibraryAddition
	err := s.withTxContext(ctx, func(tx *sql.Tx) error {
		var storedHash, body string
		err := tx.QueryRow(`SELECT content_hash,result_json FROM operation_result WHERE plan_id=?`, planID).Scan(&storedHash, &body)
		if err == nil {
			if storedHash != hash {
				return ErrPlanStale
			}
			return json.Unmarshal([]byte(body), &result)
		}
		if err != sql.ErrNoRows {
			return err
		}
		result, err = libraryAdditionTx(tx, ids)
		if err != nil {
			return err
		}
		for _, id := range result.WillAdd {
			if err := setLibraryMembershipTx(tx, id, true, time.Now().UTC()); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO operation_result(plan_id,content_hash,result_json,committed_at) VALUES(?,?,?,?)`, planID, hash, string(raw), time.Now().UTC())
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
