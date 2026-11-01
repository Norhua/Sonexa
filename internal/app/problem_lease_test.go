package app

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestReviewLeaseReleaseRejectsInFlightResearch(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	one, _, scope := appProblemRows(t, a)
	session, err := a.AcquireReviewProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	row := candidateRow()
	base, _ := a.registry.Get(provider.MusicBrainz)
	searcher := &delayedReviewSearcher{MetadataProvider: base, started: make(chan struct{}), release: make(chan struct{}), hit: row.toCandidate()}
	a.registry.Register(searcher)
	result := make(chan error, 1)
	go func() {
		_, err := a.ResearchReviewProblem(session.Scope, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "Song", Provider: "musicbrainz"})
		result <- err
	}()
	select {
	case <-searcher.started:
	case err := <-result:
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		t.Fatal("search not started")
	}
	before, err := a.store.GetItem(one)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.ReleaseReviewProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	close(searcher.release)
	select {
	case err := <-result:
		if !errors.Is(err, store.ErrPlanStale) {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late query did not finish")
	}
	after, err := a.store.GetItem(one)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(after, err)
	}
	var pins int
	if err := a.store.DB().QueryRow("SELECT count(*) FROM review_problem_operation").Scan(&pins); err != nil || pins != 0 {
		t.Fatal(pins, err)
	}
	current, err := a.GetReviewProblemScope(scope.ProblemID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AcquireReviewProblemLease(current); err != nil {
		t.Fatal("cannot reopen", err)
	}
}

func TestReviewDraftRevisionInvalidatesOlderQueryWithoutChangingInput(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	one, _, scope := appProblemRows(t, a)
	session, err := a.AcquireReviewProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	before, err := a.store.GetItem(one)
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.store.BeginProblemQuery(session.Scope, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "old"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := a.UpdateReviewProblemDraft(token, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "new draft", Provider: "musicbrainz"})
	if err != nil {
		t.Fatal(err)
	}
	if current.QueryRevision != token.QueryRevision+1 || current.LeaseToken != session.Lease.Token {
		t.Fatal(current)
	}
	if err := a.store.CompleteProblemQuery(token, store.ReviewCandidateFacts{Version: store.ReviewProtocolVersion}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
	after, err := a.store.GetItem(one)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(after, err)
	}
	if _, err := a.RenewReviewProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
}
