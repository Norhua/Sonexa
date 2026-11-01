package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRecheckFailureAndNoopCASPreserveWork(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	s.SetProblemClock(func() time.Time { return now })
	item, scope := leaseProblem(t, s)
	if err := s.QueueProblemRecheck(scope.ProblemID); err != nil {
		t.Fatal(err)
	}
	work, err := s.NextProblemRecheck(context.Background())
	if err != nil || work == nil {
		t.Fatal(work, err)
	}
	if err := s.FinishProblemRecheck(*work, errors.New("fixture failure")); err != nil {
		t.Fatal(err)
	}
	if next, err := s.NextProblemRecheck(context.Background()); err != nil || next != nil {
		t.Fatal(next, err)
	}
	var failure string
	if err := s.db.QueryRow("SELECT last_error FROM review_problem_recheck WHERE problem_id=?", scope.ProblemID).Scan(&failure); err != nil || failure != "fixture failure" {
		t.Fatal(failure, err)
	}
	now = now.Add(time.Minute)
	work, err = s.NextProblemRecheck(context.Background())
	if err != nil || work == nil {
		t.Fatal(work, err)
	}
	if err := s.NoneMatchedProblem(scope); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishProblemRecheck(*work, nil); err != nil {
		t.Fatal(err)
	}
	if rechecks(t, s, scope.ProblemID) != 1 {
		t.Fatal("stale no-op consumed new evidence")
	}
	got, _ := s.GetItem(item.ID)
	if got.LastError != nil || got.Status != StatusReview {
		t.Fatal(got)
	}
}
func TestRecheckRestartRetainsPendingAndNeverRestoresTerminal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recheck.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, scope := leaseProblem(t, s)
	if err := s.QueueProblemRecheck(scope.ProblemID); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.RestoreProblemRechecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	work, err := s.NextProblemRecheck(context.Background())
	if err != nil || work == nil {
		t.Fatal(work, err)
	}
	if err := s.DiscardProblemMembers(scope, []int64{work.Item.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishProblemRecheck(*work, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreProblemRechecks(context.Background()); err != nil {
		t.Fatal(err)
	}
	if next, err := s.NextProblemRecheck(context.Background()); err != nil || next != nil {
		t.Fatal(next, err)
	}
}
func TestRecheckNetworkRespectsFailedAndPausedRows(t *testing.T) {
	for _, state := range []string{"review", "failed", "paused"} {
		t.Run(state, func(t *testing.T) {
			s := openTestStore(t)
			item, scope := leaseProblem(t, s)
			if state == "failed" {
				if _, err := s.RecordFailure(item.ID, "network failed", time.Second, 1); err != nil {
					t.Fatal(err)
				}
			}
			if state == "paused" {
				if err := s.SetBatchStatus(item.BatchID, BatchPaused); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.QueueProblemRecheck(scope.ProblemID); err != nil {
				t.Fatal(err)
			}
			work, err := s.NextProblemRecheck(context.Background())
			if err != nil || work == nil {
				t.Fatal(work, err)
			}
			if err := s.QueueRecheckNetwork(*work); err != nil {
				t.Fatal(err)
			}
			batches, err := s.RecheckNetworkBatches(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got, _ := s.GetItem(item.ID)
			if state == "review" {
				if len(batches) != 1 || got.Status != StatusQueued {
					t.Fatal(batches, got)
				}
			} else if len(batches) != 0 || got.Status != work.Item.Status {
				t.Fatal(batches, got)
			}
		})
	}
}
