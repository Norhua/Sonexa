package library

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestProblemLeaseProtectsApprovalAndReleasesForFreshAutomaticDecision(t *testing.T) {
	s, st, _ := newTestService(t)
	commit(t, s, prepare(t, s, seed("existing", "old", "group", "Album", "Artist")), false)
	item := problemInput(t, st, "Song")
	token, err := st.ItemProblemScope(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	target := queueSeed(item, seed("new", "new-track", "group", "Album", "Artist"))
	target.Automatic = true
	old := prepare(t, s, target)
	session, err := st.AcquireProblemLease(token)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := st.ReadProblemInputScope(session.Scope)
	if err != nil {
		t.Fatal(err)
	}
	manual := seed("new", "new-track", "group", "Album", "Artist")
	manual.InputScope = scope
	plan := prepare(t, s, manual)
	approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), old.ID, old.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrProblemBusy) {
		t.Fatal("auto ended editing problem", err)
	}
	if _, err := s.PrepareAdmission(context.Background(), target); !errors.Is(err, store.ErrProblemBusy) {
		t.Fatal("automatic plan during confirm", err)
	}
	if _, err := st.RenewProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash); err != nil {
		t.Fatal("renew invalidated approval", err)
	}
	if err := st.ReleaseProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, *approval); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("released approval committed", err)
	}
	if _, err := s.CommitAdmission(context.Background(), old.ID, old.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("pre-interaction automatic plan revived", err)
	}
	// A new current-evidence decision may act after release; another request
	// already collected the target, so this one only skips the input.
	created := commit(t, s, prepare(t, s, seed("new", "new-track", "group", "Album", "Artist")), true)
	current, err := st.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	skip, err := s.PrepareSkipExisting(automaticAdmissionContext(context.Background()), candidate("new", "new-track", nil), item.ID, current.Revision)
	if err != nil || !skip.Seed.Automatic {
		t.Fatal(skip, err)
	}
	result := commit(t, s, skip, false)
	if !result.Existing || result.TrackID != created.TrackID {
		t.Fatal(result)
	}
}

func TestProblemLeaseAllowsOwnCommitAndRejectsLateResolution(t *testing.T) {
	for _, release := range []bool{false, true} {
		t.Run(map[bool]string{false: "own confirmation", true: "release during resolution"}[release], func(t *testing.T) {
			s, st, _ := newTestService(t)
			item := problemInput(t, st, "Song")
			token, _ := st.ItemProblemScope(item.ID)
			session, err := st.AcquireProblemLease(token)
			if err != nil {
				t.Fatal(err)
			}
			scope, err := st.ReadProblemInputScope(session.Scope)
			if err != nil {
				t.Fatal(err)
			}
			p := &scopedResolutionProvider{}
			s.registry.Register(p)
			p.load = func(matching.DecisionRequest) ([]matching.TargetResolution, error) {
				if release {
					if err := st.ReleaseProblemLease(session.Lease); err != nil {
						t.Fatal(err)
					}
				}
				return []matching.TargetResolution{resolvedFact(t, candidate("release", "track", nil))}, nil
			}
			plan, err := s.PrepareScopedCandidate(context.Background(), candidate("release", "unresolved", nil), *scope)
			if release {
				if !errors.Is(err, store.ErrPlanStale) {
					t.Fatal(plan, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			commit(t, s, plan, false)
			if err := st.ReleaseProblemLease(session.Lease); err != nil {
				t.Fatal("cannot release completed problem", err)
			}
		})
	}
}

func TestExpiredProblemLeaseDoesNotAuthorizeApproval(t *testing.T) {
	s, st, clock := newTestService(t)
	st.SetProblemClock(clock.Now)
	item := problemInput(t, st, "Song")
	token, _ := st.ItemProblemScope(item.ID)
	session, err := st.AcquireProblemLease(token)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := st.ReadProblemInputScope(session.Scope)
	if err != nil {
		t.Fatal(err)
	}
	target := seed("release", "track", "group", "Album", "Artist")
	target.InputScope = scope
	plan := prepare(t, s, target)
	clock.now = clock.now.Add(store.ProblemLeaseTTL + time.Millisecond)
	if _, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
}
