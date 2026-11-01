package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func enqueueProblemInput(t *testing.T, s *Store, title, artist, album string) *ImportItem {
	t.Helper()
	batch, err := s.CreateBatch(&ImportBatch{Filename: "rows.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	profile := matching.NewInputProfile(title, artist, album)
	if err := s.EnqueueItems(batch, []ImportItem{{RawRow: "[\"" + title + "\"]", InTitle: &title, InArtist: &artist, InAlbum: &album, Profile: &profile}}); err != nil {
		t.Fatal(err)
	}
	item, err := s.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func activeProblemCounts(t *testing.T, s *Store, problems, members int) {
	t.Helper()
	var p, m int
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem WHERE status='active'").Scan(&p); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem_item WHERE active=1").Scan(&m); err != nil {
		t.Fatal(err)
	}
	if p != problems || m != members {
		t.Fatalf("problems/members=%d/%d want %d/%d", p, m, problems, members)
	}
}

func TestProblemLifecycleConcurrentAcrossBatchesAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "problems.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song（译名：歌曲）", "Artist", "Album")
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() { defer wg.Done(); errs <- s.MarkReviewAtRevision(a.ID, a.Revision, ReviewNoMatch, "[]", "[]") }()
	go func() {
		defer wg.Done()
		_, err := s.RecordFailure(b.ID, "fixture network failure", time.Second, 1, b.Revision)
		errs <- err
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	activeProblemCounts(t, s, 1, 2)
	if err := s.SetBatchStatus(a.BatchID, BatchPaused); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 2)
	gotA, _ := s.GetItem(a.ID)
	gotB, _ := s.GetItem(b.ID)
	if gotA.RawRow != a.RawRow || gotB.RawRow != b.RawRow || gotA.Status != StatusReview || gotB.Status != StatusFailed || gotB.LastError == nil || *gotB.LastError != "fixture network failure" {
		t.Fatal(gotA, gotB)
	}
	if err := s.RetryItem(b.ID, gotB.Revision); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.ClaimNextItem(b.BatchID)
	if err != nil || claimed == nil {
		t.Fatal(claimed, err)
	}
	if err := s.ResetStaleMatching(); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 2)
	if err := s.MarkSkipped(a.ID); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 1)
	if err := s.MarkReviewAtRevision(a.ID, a.Revision, ReviewNoMatch, "[]", "[]"); !errors.Is(err, ErrPlanStale) {
		t.Fatal("terminal input restored", err)
	}
	if err := s.MarkDiscarded(b.ID); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 0, 0)
	// A later equivalent input is a new active problem, not a remembered choice.
	c := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	if err := s.MarkReview(c.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 1, 1)
	var total int
	if err := s.db.QueryRow("SELECT count(*) FROM review_problem").Scan(&total); err != nil || total != 2 {
		t.Fatal(total, err)
	}
	rows, err := s.FindProblemCandidates(context.Background(), matching.ProblemInput{Profile: *c.Profile}, 0, 10)
	if err != nil || len(rows.Problems) != 1 || len(rows.Problems[0].Anchor.Inputs) != 1 {
		t.Fatal(rows, err)
	}
}

func TestProblemLifecycleRollbackAndDistinctInputs(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	forced := errors.New("forced commit failure")
	s.beforeCommit = func(*sql.Tx) error { return forced }
	if err := s.MarkReview(a.ID, ReviewNoMatch, "[]", "[]"); !errors.Is(err, forced) {
		t.Fatal(err)
	}
	s.beforeCommit = nil
	activeProblemCounts(t, s, 0, 0)
	got, _ := s.GetItem(a.ID)
	if got.Status != StatusMatching || got.Revision != a.Revision {
		t.Fatal("partial row transition", got)
	}
	for _, item := range []*ImportItem{a, enqueueProblemInput(t, s, "Song", "Artist", "Other Album"), enqueueProblemInput(t, s, "Song", "Artist", ""), enqueueProblemInput(t, s, "Song", "Artist", "")} {
		if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
	}
	activeProblemCounts(t, s, 4, 4)
	// A finish failure rolls back membership removal and row terminal state.
	s.beforeCommit = func(*sql.Tx) error { return forced }
	if err := s.MarkSkipped(a.ID); !errors.Is(err, forced) {
		t.Fatal(err)
	}
	s.beforeCommit = nil
	activeProblemCounts(t, s, 4, 4)
	got, _ = s.GetItem(a.ID)
	if got.Status != StatusReview {
		t.Fatal(got)
	}
}

func TestProblemLifecycleExactFactsAreRequiredForDifferentMasks(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song", "Artist", "")
	c := enqueueProblemInput(t, s, "Song", "Artist", "")
	makeInput := func(item *ImportItem, ref string) matching.ProblemInput {
		title, artist, album, release := "Song", "Artist", "Album", "release"
		disc, track := 1, 1
		identity := matching.CandidateIdentity{State: matching.IdentityExact, Track: domain.Identity{Provider: "fixture", Ref: ref}, Release: domain.Identity{Provider: "fixture", Ref: release}}
		return matching.ProblemInput{Profile: *item.Profile, Pool: &matching.CandidatePool{Targets: []matching.CandidateTarget{{Identity: identity, Facts: []matching.CandidateFact{{Kind: "remote", Metadata: matching.MetadataFacts{Source: "fixture", AlbumComplete: true, PositionVerified: true,
			Candidate: provider.Candidate{Provider: "fixture", MatchRef: ref, ReleaseRef: &release, Title: &title, ArtistName: &artist, AlbumTitle: &album, DiscNumber: &disc, TrackNumber: &track}}}}}}}}
	}
	for i, item := range []*ImportItem{a, b, c} {
		ref := "same-track"
		if i == 2 {
			ref = "different-track"
		}
		if err := s.MarkReviewWithProblem(item.ID, item.Revision, ReviewAmbiguous, "[]", "[]", "", makeInput(item, ref)); err != nil {
			t.Fatal(err)
		}
	}
	activeProblemCounts(t, s, 2, 3)
	if err := s.MarkImported(a.ID, 123); err != nil {
		t.Fatal(err)
	}
	activeProblemCounts(t, s, 2, 2)
	got, err := s.GetItem(a.ID)
	if err != nil || got.ResolutionProblemID == nil || got.ResolutionProblemRevision == nil {
		t.Fatal("terminal diagnostic scope missing", got, err)
	}
}

func TestProblemLifecycleDoesNotChainPairwiseAliases(t *testing.T) {
	s := openTestStore(t)
	for _, title := range []string{"Alpha（原名：Beta）", "Alpha", "Beta"} {
		item := enqueueProblemInput(t, s, title, "Artist", "Album")
		if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
	}
	activeProblemCounts(t, s, 2, 3)
}
