package app

import (
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestTaskEventsCountUnchangedAndSkipNoLibraryNotification(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	row := candidateRow()
	if _, err := a.AddToLibrary(row); err != nil {
		t.Fatal(err)
	}
	one, _, scope := appProblemRows(t, a)
	events := map[string]int{}
	var revisions []int64
	a.emit = func(name string, data ...any) {
		events[name]++
		if name == EventTasksChanged {
			revisions = append(revisions, data[0].(TaskChange).Revision)
		}
	}
	a.attachTaskNotifications()
	before, err := a.GetReviewProblemStats()
	if err != nil {
		t.Fatal(err)
	}
	if err := a.NoneMatchedProblem(scope); err != nil {
		t.Fatal(err)
	}
	after, err := a.GetReviewProblemStats()
	if err != nil || after != before {
		t.Fatal(after, before, err)
	}
	if events[EventReviewChanged] != 1 || events[EventTasksChanged] != 1 {
		t.Fatal("same-count mutation invisible", events)
	}
	scope, err = a.store.ItemProblemScope(one)
	if err != nil {
		t.Fatal(err)
	}
	input, err := a.GetReviewAdmissionScope(scope)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareProblemSkipExisting(row, *input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	if events[EventReviewChanged] != 2 || events[EventTasksChanged] != 2 || events[EventLibraryChanged] != 0 || events[EventEnrichProgress] != 0 {
		t.Fatal("skip emitted library/enrich or missed tasks", events)
	}
	if len(revisions) != 2 || revisions[1] <= revisions[0] {
		t.Fatal(revisions)
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	if events[EventTasksChanged] != 2 {
		t.Fatal("idempotent retry emitted false change", events)
	}
}
func TestTaskEventsIncludeBackgroundRecheckWithoutImport(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	c := fullCandidate("background-event")
	c.Title = strPtr("Song")
	itemID := enqueueOneItem(t, a, "Song", *c.ArtistName, *c.AlbumTitle)
	body, err := store.EncodeReviewCandidates([]matching.ScoredCandidate{{Candidate: c}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.MarkReview(itemID, store.ReviewAmbiguous, body, "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c})); err != nil {
		t.Fatal(err)
	}
	events := map[string]int{}
	a.emit = func(name string, _ ...any) { events[name]++ }
	a.attachTaskNotifications()
	if _, err := a.store.ScheduleLibraryRechecks(a.ctx, 32); err != nil {
		t.Fatal(err)
	}
	if _, err := a.library.RecheckPending(a.ctx, 16); err != nil {
		t.Fatal(err)
	}
	if events[EventReviewChanged] != 1 || events[EventTasksChanged] != 1 || events[EventLibraryChanged] != 0 || events[EventEnrichProgress] != 0 {
		t.Fatal(events)
	}
}

func TestTaskEventsArePostCommitAndRollbackSilent(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	one, _, scope := appProblemRows(t, a)
	calls := 0
	a.emit = func(name string, _ ...any) {
		if name != EventReviewChanged {
			return
		}
		calls++
		item, err := a.store.GetItem(one)
		if err != nil || item.Status != store.StatusDiscarded {
			t.Error("event preceded committed state", item, err)
		}
	}
	a.attachTaskNotifications()
	if _, err := a.store.DB().Exec(fmt.Sprintf(`CREATE TRIGGER reject_discard BEFORE UPDATE OF status ON import_item WHEN NEW.id=%d BEGIN SELECT RAISE(ABORT,'injected'); END`, one)); err != nil {
		t.Fatal(err)
	}
	if err := a.DiscardReviewProblemMembers(scope, []int64{one}); err == nil {
		t.Fatal("injected failure ignored")
	}
	if calls != 0 {
		t.Fatal("failed tx emitted", calls)
	}
	if _, err := a.store.DB().Exec("DROP TRIGGER reject_discard"); err != nil {
		t.Fatal(err)
	}
	if err := a.DiscardReviewProblemMembers(scope, []int64{one}); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
