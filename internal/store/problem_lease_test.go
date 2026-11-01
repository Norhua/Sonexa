package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

func leaseProblem(t *testing.T, s *Store) (*ImportItem, ProblemScope) {
	t.Helper()
	item := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	scope, err := s.ItemProblemScope(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item, scope
}
func protected(t *testing.T, s *Store, id int64) bool {
	t.Helper()
	var held bool
	if err := s.withTx(func(tx *sql.Tx) error {
		var err error
		held, err = problemProtectedTx(tx, id, s.problemNow())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return held
}
func rechecks(t *testing.T, s *Store, id int64) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_recheck WHERE problem_id=?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
func TestProblemLeaseRenewReleaseAndPins(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	s.SetProblemClock(func() time.Time { return now })
	item, scope := leaseProblem(t, s)
	interaction, err := s.AcquireProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	if interaction.Scope.Revision != scope.Revision+1 || interaction.Scope.QueryRevision != scope.QueryRevision+1 || !protected(t, s, scope.ProblemID) {
		t.Fatal(interaction)
	}
	if _, err := s.AcquireProblemLease(interaction.Scope); !errors.Is(err, ErrProblemBusy) {
		t.Fatal(err)
	}
	before, _ := s.ReadProblemScope(scope.ProblemID)
	now = now.Add(30 * time.Second)
	forged := interaction.Lease
	forged.ExpiresAt = now.Add(100 * time.Hour).UnixMilli()
	renewed, err := s.RenewProblemLease(forged)
	if err != nil || renewed.ExpiresAt != now.Add(ProblemLeaseTTL).UnixMilli() {
		t.Fatal(renewed, err)
	}
	after, _ := s.ReadProblemScope(scope.ProblemID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("renew changed query/evidence", after)
	}
	token, err := s.BeginProblemQuery(interaction.Scope, ReviewQuery{Version: ReviewProtocolVersion, Title: "draft"})
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(ProblemLeaseTTL + time.Millisecond)
	if err := s.ReapProblemLeases(10); err != nil {
		t.Fatal(err)
	}
	if !protected(t, s, scope.ProblemID) {
		t.Fatal("in-flight pin lost on lease timeout")
	}
	if _, err := s.RenewProblemLease(*renewed); !errors.Is(err, ErrPlanStale) {
		t.Fatal("expired renewed", err)
	}
	if err := s.ReleaseProblemLease(interaction.Lease); err != nil {
		t.Fatal(err)
	}
	if !protected(t, s, scope.ProblemID) {
		t.Fatal("release dropped in-flight protection")
	}
	if err := s.CompleteProblemQuery(token, ReviewCandidateFacts{Version: ReviewProtocolVersion}); !errors.Is(err, ErrPlanStale) {
		t.Fatal("released response committed", err)
	}
	if protected(t, s, scope.ProblemID) || rechecks(t, s, scope.ProblemID) != 1 {
		t.Fatal("not released/coalesced")
	}
	got, _ := s.GetItem(item.ID)
	if got.Status != StatusReview || got.RawRow != item.RawRow {
		t.Fatal(got)
	}
	if err := s.ReapProblemLeases(10); err != nil {
		t.Fatal(err)
	}
	current, _ := s.ReadProblemScope(scope.ProblemID)
	next, err := s.AcquireProblemLease(current)
	if err != nil || next.Lease.Token == interaction.Lease.Token {
		t.Fatal(next, err)
	}
	if err := s.ReleaseProblemLease(interaction.Lease); !errors.Is(err, ErrPlanStale) {
		t.Fatal("old release affected new session", err)
	}
	if !protected(t, s, scope.ProblemID) {
		t.Fatal("new session lost")
	}
}

func TestProblemOperationSurvivesIdleExpiryButNotHardDeadline(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "idle expiry", true: "hard deadline"}[expire], func(t *testing.T) {
			s := openTestStore(t)
			now := time.Now()
			s.SetProblemClock(func() time.Time { return now })
			_, scope := leaseProblem(t, s)
			session, err := s.AcquireProblemLease(scope)
			if err != nil {
				t.Fatal(err)
			}
			token, err := s.BeginProblemQuery(session.Scope, ReviewQuery{Version: ReviewProtocolVersion, Title: "manual"})
			if err != nil {
				t.Fatal(err)
			}
			if expire {
				now = now.Add(ProblemOperationTTL)
			} else {
				now = now.Add(ProblemLeaseTTL)
			}
			if err := s.ReapProblemLeases(10); err != nil {
				t.Fatal(err)
			}
			err = s.CompleteProblemQuery(token, ReviewCandidateFacts{Version: ReviewProtocolVersion})
			if expire && !errors.Is(err, ErrPlanStale) || !expire && err != nil {
				t.Fatal(err)
			}
			if protected(t, s, scope.ProblemID) {
				t.Fatal("pin leaked")
			}
		})
	}
}

func TestProblemLeaseBackgroundGuardAndClaim(t *testing.T) {
	s := openTestStore(t)
	item, scope := leaseProblem(t, s)
	if err := s.RetryItem(item.ID); err != nil {
		t.Fatal(err)
	}
	scope, _ = s.ItemProblemScope(item.ID)
	session, err := s.AcquireProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClaimNextItem(item.BatchID); err != nil || got != nil {
		t.Fatal("claimed interactive row", got, err)
	}
	for _, call := range []func() error{
		func() error { return s.MarkSkipped(item.ID) },
		func() error { return s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]") },
		func() error { _, err := s.RecordFailure(item.ID, "background", time.Second, 1); return err },
	} {
		if err := call(); !errors.Is(err, ErrProblemBusy) {
			t.Fatal(err)
		}
	}
	if err := s.ReleaseProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	got, err := s.ClaimNextItem(item.BatchID)
	if err != nil || got == nil {
		t.Fatal(got, err)
	}
}

func TestProblemLeaseConcurrentAcquireAndRollback(t *testing.T) {
	s := openTestStore(t)
	_, scope := leaseProblem(t, s)
	forced := errors.New("commit failed")
	s.beforeCommit = func(*sql.Tx) error { return forced }
	if _, err := s.AcquireProblemLease(scope); !errors.Is(err, forced) {
		t.Fatal(err)
	}
	s.beforeCommit = nil
	current, _ := s.ReadProblemScope(scope.ProblemID)
	if !reflect.DeepEqual(current, scope) || protected(t, s, scope.ProblemID) {
		t.Fatal("partial acquire")
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.AcquireProblemLease(scope); results <- err }()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		} else if !errors.Is(err, ErrPlanStale) && !errors.Is(err, ErrProblemBusy) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
}

func TestProblemQueryOperationCannotBorrowNewerScope(t *testing.T) {
	s := openTestStore(t)
	_, scope := leaseProblem(t, s)
	first, err := s.BeginProblemQuery(scope, ReviewQuery{Version: ReviewProtocolVersion, Title: "old"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.BeginProblemQuery(first, ReviewQuery{Version: ReviewProtocolVersion, Title: "new"})
	if err != nil {
		t.Fatal(err)
	}
	forged := second
	forged.OperationToken = first.OperationToken
	if err := s.CompleteProblemQuery(forged, ReviewCandidateFacts{Version: ReviewProtocolVersion}); !errors.Is(err, ErrPlanStale) {
		t.Fatal("old operation adopted new query", err)
	}
	if err := s.CompleteProblemQuery(second, ReviewCandidateFacts{Version: ReviewProtocolVersion}); err != nil {
		t.Fatal(err)
	}
}

func TestProblemLeaseReaperHonorsLimitAndStopsWithContext(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	s.SetProblemClock(func() time.Time { return now })
	for _, title := range []string{"One", "Two", "Three"} {
		item := enqueueProblemInput(t, s, title, "Artist", "Album")
		if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
		scope, err := s.ItemProblemScope(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.AcquireProblemLease(scope); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(ProblemLeaseTTL)
	if err := s.ReapProblemLeases(1); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_lease").Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() { defer close(done); s.RunProblemLeaseReaper(ctx, func(err error) { t.Error(err) }) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("reaper ignored cancellation")
	}
}
func TestProblemLeaseRestartAndBoundedReaping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lease.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.SetProblemClock(func() time.Time { return now })
	_, scope := leaseProblem(t, s)
	session, err := s.AcquireProblemLease(scope)
	if err != nil {
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
	s.SetProblemClock(func() time.Time { return now })
	if !protected(t, s, scope.ProblemID) {
		t.Fatal("restart prematurely unprotected")
	}
	now = now.Add(ProblemLeaseTTL)
	if err := s.ReapProblemLeases(1); err != nil {
		t.Fatal(err)
	}
	if protected(t, s, scope.ProblemID) || rechecks(t, s, scope.ProblemID) != 1 {
		t.Fatal("expiry missing")
	}
	if _, err := s.RenewProblemLease(domain.ProblemLease{Token: session.Lease.Token, ProblemID: scope.ProblemID, ExpiresAt: now.Add(time.Hour).UnixMilli()}); !errors.Is(err, ErrPlanStale) {
		t.Fatal(err)
	}
}
