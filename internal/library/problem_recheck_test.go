package library

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func pendingCandidates(t *testing.T, st *store.Store, title, artist, album string, candidates ...provider.Candidate) *store.ImportItem {
	t.Helper()
	batch, err := st.CreateBatch(&store.ImportBatch{Filename: "recheck.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	profile := matching.NewInputProfile(title, artist, album)
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `["` + title + `"]`, Profile: &profile}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	var scored []matching.ScoredCandidate
	for _, c := range candidates {
		scored = append(scored, matching.ScoredCandidate{Candidate: c})
	}
	body, err := store.EncodeReviewCandidates(scored, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReview(item.ID, store.ReviewAmbiguous, body, "[]"); err != nil {
		t.Fatal(err)
	}
	item, err = st.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func runRechecks(t *testing.T, s *Service, st *store.Store) {
	t.Helper()
	for range 50 {
		n, err := st.ScheduleLibraryRechecks(context.Background(), 32)
		if err != nil {
			t.Fatal(err)
		}
		m, err := s.RecheckPending(context.Background(), 16)
		if err != nil {
			t.Fatal(err)
		}
		if n < 32 && m < 16 {
			return
		}
	}
	t.Fatal("recheck did not drain")
}
func collectCandidate(t *testing.T, s *Service, c provider.Candidate) *domain.AdmissionResult {
	t.Helper()
	plan, err := s.PrepareCandidate(context.Background(), c, 0)
	if err != nil {
		t.Fatal(err)
	}
	return commit(t, s, plan, plan.Baseline.RequiresConfirmation)
}
func TestRecheckCurrentEvidenceNeverCopiesManualChoice(t *testing.T) {
	s, st, _ := newTestService(t)
	a := candidate("edition-a", "track-a", nil)
	b := candidate("edition-b", "track-b", nil)
	b.AlbumTitle = ptr("Other Album")
	b.ReleaseGroupRef = ptr("other-group")
	a.RecordingRef = ptr("same-recording")
	b.RecordingRef = ptr("same-recording")
	exact := pendingCandidates(t, st, "Song", "Artist", "Album", a, b)
	ambiguous := pendingCandidates(t, st, "Song", "Artist", "", a, b)
	explicit := pendingCandidates(t, st, "Song", "Artist", "Other Album", a, b)
	unknown := b
	unknown.AlbumTitle = nil
	unknown.ReleaseGroupRef = nil
	incomplete := pendingCandidates(t, st, "Song", "Artist", "Album", a, unknown)
	saved := collectCandidate(t, s, a)
	runRechecks(t, s, st)
	got, err := st.GetItem(exact.ID)
	if err != nil || got.Status != store.StatusSkipped || got.TrackID == nil || *got.TrackID != saved.TrackID {
		t.Fatal("exact input not skipped", got, err)
	}
	for _, item := range []*store.ImportItem{ambiguous, explicit, incomplete} {
		got, err := st.GetItem(item.ID)
		want := store.StatusReview
		if item.ID == incomplete.ID {
			want = store.StatusQueued
		} // missing facts go through Runner, never guessed locally
		if err != nil || got.Status != want || got.RawRow != item.RawRow {
			t.Fatal("manual choice propagated", got, err)
		}
	}
	if n, err := st.CountTracks(store.TrackFilter{}); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}
func TestRecheckNoResultUsesSufficientCurrentFactsOnly(t *testing.T) {
	s, st, _ := newTestService(t)
	item := pendingCandidates(t, st, "Song（译名：歌曲）", "Artist", "Album")
	c := candidate("release", "track", nil)
	saved := collectCandidate(t, s, c)
	runRechecks(t, s, st)
	got, err := st.GetItem(item.ID)
	if err != nil || got.Status != store.StatusReview {
		t.Fatal("sparse local unique skipped", got, err)
	}
	snapshot, err := provider.CandidateSnapshot(c.Provider, c)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Release.TracklistCompleteness = domain.Complete
	snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{{Track: domain.Known(snapshot.Track.Identity), Title: snapshot.Track.Title, Credit: snapshot.Track.Credit, Recording: snapshot.Track.Recording, DiscNumber: *c.DiscNumber, TrackNumber: *c.TrackNumber}})
	rev, err := st.TrackRevision(saved.TrackID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ApplySnapshot(saved.TrackID, snapshot, rev); err != nil {
		t.Fatal(err)
	}
	runRechecks(t, s, st)
	got, err = st.GetItem(item.ID)
	if err != nil || got.Status != store.StatusSkipped {
		t.Fatal(got, err)
	}
}
func TestRecheckRetainsDecisionCompletenessAndPositionFacts(t *testing.T) {
	for _, reason := range []string{"partial", "duplicate-position"} {
		t.Run(reason, func(t *testing.T) {
			s, st, _ := newTestService(t)
			c := candidate("release", "track", nil)
			item := pendingCandidates(t, st, "Song", "Artist", "Album", c)
			sources, err := s.identitySources()
			if err != nil {
				t.Fatal(err)
			}
			pool := matching.BuildCandidatePool(matching.LocalRecallResult{}, []provider.Candidate{c}, sources, st)
			snapshot, err := provider.CandidateSnapshot(c.Provider, c)
			if err != nil {
				t.Fatal(err)
			}
			snapshot.Release.TracklistCompleteness = domain.Complete
			snapshot.Release.Tracklist = domain.Known([]domain.TracklistEntry{
				{Track: domain.Known(snapshot.Track.Identity), Title: snapshot.Track.Title, Credit: snapshot.Track.Credit, Recording: snapshot.Track.Recording, DiscNumber: 1, TrackNumber: 1},
				{Track: domain.Known(domain.Identity{Provider: c.Provider, Ref: "other-position"}), Title: snapshot.Track.Title, Credit: snapshot.Track.Credit, Recording: snapshot.Track.Recording, DiscNumber: 1, TrackNumber: 2},
			})
			if reason == "partial" {
				pool.Truncated = true
			} else {
				pool.Targets[0].Facts[0].Release = snapshot.Release
			}
			if err := st.MarkReviewWithProblem(item.ID, item.Revision, store.ReviewAmbiguous, *item.Candidates, "[]", "", matching.ProblemInput{Profile: store.ItemProfile(item), Pool: &pool}); err != nil {
				t.Fatal(err)
			}
			collectCandidate(t, s, c)
			runRechecks(t, s, st)
			got, err := st.GetItem(item.ID)
			if err != nil || got.Status != store.StatusReview {
				t.Fatal("saved display hid incomplete decision", got, err)
			}
		})
	}
}

func TestRecheckCommitRejectsChangedCompetitorNotOnlyTarget(t *testing.T) {
	s, st, _ := newTestService(t)
	c := candidate("release", "track", nil)
	item := pendingCandidates(t, st, "Song", "Artist", "Album", c)
	collectCandidate(t, s, c)
	if _, err := st.ScheduleLibraryRechecks(context.Background(), 32); err != nil {
		t.Fatal(err)
	}
	work, err := st.NextProblemRecheck(context.Background())
	if err != nil || work == nil {
		t.Fatal(work, err)
	}
	ctx := context.WithValue(context.Background(), recheckContextKey{}, &recheckContext{revision: work.LibraryRevision})
	ctx = context.WithValue(ctx, automaticScopeKey{}, work.Scope)
	profile := store.ItemProfile(item)
	selected, err := s.SelectCandidate(ctx, []matching.ScoredCandidate{{Candidate: c}}, item.ID, &profile)
	if err != nil || selected.Plan == nil {
		t.Fatal(selected, err)
	}
	competitor := candidate("other-release", "other-track", nil)
	competitor.ReleaseGroupRef = ptr("different-group")
	collectCandidate(t, s, competitor)
	if _, err := s.CommitAdmission(ctx, selected.Plan.ID, selected.Plan.Hash, domain.AdmissionApproval{}); err != store.ErrPlanStale {
		t.Fatal("changed competitor ignored", err)
	}
	got, _ := st.GetItem(item.ID)
	if got.Status != store.StatusReview {
		t.Fatal(got)
	}
}

func TestRecheckDoesNotConsumeLeaseOrConcurrentFactChange(t *testing.T) {
	s, st, _ := newTestService(t)
	c := candidate("release", "track", nil)
	item := pendingCandidates(t, st, "Song", "Artist", "Album", c)
	token, _ := st.ItemProblemScope(item.ID)
	session, err := st.AcquireProblemLease(token)
	if err != nil {
		t.Fatal(err)
	}
	saved := collectCandidate(t, s, c)
	runRechecks(t, s, st)
	got, _ := st.GetItem(item.ID)
	if got.Status != store.StatusReview {
		t.Fatal(got)
	}
	if err := st.ReleaseProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	work, err := st.NextProblemRecheck(context.Background())
	if err != nil || work == nil {
		t.Fatal(work, err)
	}
	if err := st.DeleteTrack(saved.TrackID); err != nil {
		t.Fatal(err)
	}
	if err := s.recheckInput(context.Background(), *work); err != nil && err != store.ErrPlanStale {
		t.Fatal(err)
	}
	got, _ = st.GetItem(item.ID)
	if got.Status != store.StatusReview {
		t.Fatal("deleted target skipped", got)
	}
}
