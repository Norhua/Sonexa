package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
)

func TestProblemSplitPreservesOthersAndPreventsImmediateRegroup(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song（译名：歌曲）", "Artist", "Album")
	for _, item := range []*ImportItem{a, b} {
		if err := s.MarkReview(item.ID, ReviewAmbiguous, `[{"manual":"candidate"}]`, "[]"); err != nil {
			t.Fatal(err)
		}
	}
	beforeA, _ := s.GetItem(a.ID)
	beforeB, _ := s.GetItem(b.ID)
	page, err := s.FindProblemCandidates(context.Background(), matching.ProblemInput{Profile: *a.Profile}, 0, 10)
	if err != nil || len(page.Problems) != 1 {
		t.Fatal(page, err)
	}
	problem := page.Problems[0]
	newID, err := s.SplitProblemItem(problem.ID, problem.Revision, b.ID, beforeB.Revision)
	if err != nil || newID == problem.ID {
		t.Fatal(newID, err)
	}
	activeProblemCounts(t, s, 2, 2)
	afterA, _ := s.GetItem(a.ID)
	afterB, _ := s.GetItem(b.ID)
	if !reflect.DeepEqual(beforeA, afterA) || afterB.RawRow != beforeB.RawRow || afterB.Candidates == nil || *afterB.Candidates != *beforeB.Candidates || afterB.Revision != beforeB.Revision+1 {
		t.Fatal("split changed source/candidate ownership", afterA, afterB)
	}
	if _, err := s.SplitProblemItem(problem.ID, problem.Revision, a.ID, afterA.Revision); !errors.Is(err, ErrPlanStale) {
		t.Fatal("old scope accepted", err)
	}
	if err := s.MarkReviewAtRevision(b.ID, beforeB.Revision, ReviewNoMatch, "[]", "[]"); !errors.Is(err, ErrPlanStale) {
		t.Fatal("late detached-row response accepted", err)
	}
	// Re-running the detached row stays in its isolated problem.
	if err := s.RetryItem(b.ID, afterB.Revision); err != nil {
		t.Fatal(err)
	}
	retry, err := s.ClaimNextItem(b.BatchID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReviewAtRevision(b.ID, retry.Revision, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	c := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	if err := s.MarkReview(c.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	var cProblem, bProblem int64
	if err := s.db.QueryRow("SELECT problem_id FROM review_problem_item WHERE item_id=? AND active=1", c.ID).Scan(&cProblem); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT problem_id FROM review_problem_item WHERE item_id=? AND active=1", b.ID).Scan(&bProblem); err != nil {
		t.Fatal(err)
	}
	if cProblem != problem.ID || bProblem != newID {
		t.Fatal("isolated work regrouped", cProblem, bProblem)
	}
	activeProblemCounts(t, s, 2, 3)
	if err := s.MarkDiscarded(b.ID); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 2)
}

func TestProblemSplitRollsBackAllScopeChanges(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	for _, item := range []*ImportItem{a, b} {
		if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
	}
	page, _ := s.FindProblemCandidates(context.Background(), matching.ProblemInput{Profile: *a.Profile}, 0, 10)
	before, _ := s.GetItem(b.ID)
	forced := errors.New("split commit failed")
	s.beforeCommit = func(*sql.Tx) error { return forced }
	if id, err := s.SplitProblemItem(page.Problems[0].ID, page.Problems[0].Revision, b.ID, before.Revision); id != 0 || !errors.Is(err, forced) {
		t.Fatal(id, err)
	}
	s.beforeCommit = nil
	activeProblemCounts(t, s, 1, 2)
	after, _ := s.GetItem(b.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("partial split", after)
	}
}
