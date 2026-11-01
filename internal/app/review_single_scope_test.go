package app

import (
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestProblemNoMatchNeededUsesDisplayedSingleScopeAndLease(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	one, two, scope := appProblemRows(t, a)
	interaction, err := a.AcquireReviewProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.PrepareProblemNoMatchNeeded(scope, one); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("old displayed scope accepted", err)
	}
	withoutToken := interaction.Scope
	withoutToken.LeaseToken = ""
	if _, err := a.PrepareProblemNoMatchNeeded(withoutToken, one); !errors.Is(err, store.ErrProblemBusy) {
		t.Fatal("lease holder bypassed", err)
	}
	if _, err := a.PrepareProblemNoMatchNeeded(interaction.Scope, two+100); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("nonmember accepted", err)
	}
	current, err := a.UpdateReviewProblemDraft(interaction.Scope, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "not original"})
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareProblemNoMatchNeeded(current, two)
	if err != nil || plan.Baseline.InputScope.Kind != domain.InputScopeSingle || len(plan.Baseline.InputScope.Members) != 1 || plan.Baseline.InputScope.Members[0].ItemID != two || plan.Baseline.InputScope.LeaseToken != interaction.Lease.Token {
		t.Fatal(plan, err)
	}
	result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil || len(result.Inputs) != 1 || result.Inputs[0].ItemID != two {
		t.Fatal(result, err)
	}
	track, err := a.store.GetTrack(result.TrackID)
	if err != nil || track.Title != "Song（译名：歌曲）" {
		t.Fatal("draft replaced original", track, err)
	}
	remaining, _ := a.store.GetItem(one)
	if remaining.Status != store.StatusReview {
		t.Fatal("other source ended", remaining)
	}
	if _, err := a.PrepareProblemNoMatchNeeded(current, one); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("old membership accepted", err)
	}
}
