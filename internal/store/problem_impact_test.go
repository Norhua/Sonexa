package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func TestProblemImpactTracksOldAndNewIdentityAndNames(t *testing.T) {
	s := openTestStore(t)
	item, scope := leaseProblem(t, s)
	unrelated := enqueueProblemInput(t, s, "Different", "Another", "Unrelated")
	if err := s.MarkReview(unrelated.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	c := provider.Candidate{Provider: "fixture", MatchRef: "original-track", ReleaseRef: ptr("release"), Title: ptr("Song"), ArtistName: ptr("Artist"), AlbumTitle: ptr("Album")}
	id, err := s.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.ScheduleLibraryRechecks(context.Background(), 1); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if rechecks(t, s, scope.ProblemID) != 1 {
		t.Fatal("new track failed to schedule")
	}
	unrelatedScope, _ := s.ItemProblemScope(unrelated.ID)
	if rechecks(t, s, unrelatedScope.ProblemID) != 0 {
		t.Fatal("unrelated problem scheduled")
	}
	if _, err := s.db.Exec("DELETE FROM review_problem_recheck"); err != nil {
		t.Fatal(err)
	}
	// Rebinding and renaming still match the previous identity/name index even
	// if the changed track no longer resembles the pending original input.
	if err := s.SetProvenance(id, "fixture", "new-track"); err != nil {
		t.Fatal(err)
	}
	name := "Renamed"
	if err := s.UpdateTrackFields(id, &name, nil, true); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteTrack(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleLibraryRechecks(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if rechecks(t, s, scope.ProblemID) != 1 {
		t.Fatal("delete lost old facts", item)
	}
	var dirty int
	if err := s.db.QueryRow("SELECT count(*) FROM review_library_dirty").Scan(&dirty); err != nil || dirty != 0 {
		t.Fatal(dirty, err)
	}
}
func TestProblemImpactIdentityWorksWithoutCommonTextAndPages(t *testing.T) {
	s := openTestStore(t)
	c := provider.Candidate{Provider: "fixture", MatchRef: "track", ReleaseRef: ptr("release"), Title: ptr("Remote title"), ArtistName: ptr("Remote artist"), AlbumTitle: ptr("Remote album")}
	body, err := EncodeReviewCandidates([]matching.ScoredCandidate{{Candidate: c}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 105; i++ {
		item := enqueueProblemInput(t, s, fmt.Sprintf("Local %d", i), "Input artist", "Input album")
		if err := s.MarkReview(item.ID, ReviewAmbiguous, body, "[]"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AdmitCandidate(c); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleLibraryRechecks(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	var queued, dirty int
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_recheck").Scan(&queued); err != nil || queued != 100 {
		t.Fatal(queued, err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM review_library_dirty").Scan(&dirty); err != nil || dirty != 1 {
		t.Fatal(dirty, err)
	}
	if _, err := s.ScheduleLibraryRechecks(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_recheck").Scan(&queued); err != nil || queued != 105 {
		t.Fatal(queued, err)
	}
}
func TestProblemImpactFailureRollsBackEventConsumption(t *testing.T) {
	s := openTestStore(t)
	_, scope := leaseProblem(t, s)
	if _, err := s.AdmitCandidate(provider.Candidate{Provider: "fixture", MatchRef: "track", ReleaseRef: ptr("release"), Title: ptr("Song"), ArtistName: ptr("Artist"), AlbumTitle: ptr("Album")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`CREATE TRIGGER reject_recheck BEFORE INSERT ON review_problem_recheck BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleLibraryRechecks(context.Background(), 1); err == nil {
		t.Fatal("injected failure ignored")
	}
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM review_library_dirty").Scan(&count); err != nil || count != 1 {
		t.Fatal("event lost", count, err)
	}
	if _, err := s.db.Exec("DROP TRIGGER reject_recheck"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ScheduleLibraryRechecks(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if rechecks(t, s, scope.ProblemID) != 1 {
		t.Fatal(scope)
	}
}
