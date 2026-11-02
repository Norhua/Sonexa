package importer

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestRunnerCompleteEvidenceImportsStudioAndRepeatedImportSkips(t *testing.T) {
	st := openStore(t)
	studio := mbCandidate("studio", "3253212", "reche", "3253212")
	live := mbCandidate("live", "3253212", "reche", "reche GaLLERy on live")
	studio.RecordingRef = sptr("rec-studio")
	live.RecordingRef = sptr("rec-live")
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"3253212": {studio, live}}}
	runner := newRunner(st, searcher)
	batch := enqueueOne(t, st, "3253212", "reche", "3253212")
	runToCompletion(t, runner, batch)
	stats, _ := st.Stats(batch)
	if stats.Imported != 1 {
		t.Fatal(stats)
	}
	again := enqueueOne(t, st, "3253212（译名）", "reche（注音）", "3253212（译名）")
	runToCompletion(t, runner, again)
	stats, _ = st.Stats(again)
	if stats.Skipped != 1 || stats.Review != 0 {
		t.Fatal(stats)
	}
	var count int
	st.DB().QueryRow("SELECT COUNT(*) FROM track").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}

func TestRunnerLocalCompleteInputSkipsWithoutPretendingUpstreamBinding(t *testing.T) {
	st := openStore(t)
	_, err := st.CreateTrackWithProvenance(store.NewTrackInput{Title: "Song", Credit: sptr("Artist"), Album: sptr("Album")})
	if err != nil {
		t.Fatal(err)
	}
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{}}
	batch := enqueueOne(t, st, "Song", "Artist", "Album")
	runToCompletion(t, newRunner(st, searcher), batch)
	stats, _ := st.Stats(batch)
	if stats.Skipped != 1 || stats.Review != 0 {
		t.Fatal(stats)
	}
}

func TestRunnerOwnedOtherAlbumDoesNotSkipCurrentInput(t *testing.T) {
	st := openStore(t)
	c := mbCandidate("other", "Song", "Artist", "Other Album")
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {c}}}
	runner := newRunner(st, searcher)
	runToCompletion(t, runner, enqueueOne(t, st, "Song", "Artist", "Other Album"))
	batch := enqueueOne(t, st, "Song", "Artist", "Wanted Album")
	runToCompletion(t, runner, batch)
	stats, _ := st.Stats(batch)
	if stats.Skipped != 0 || stats.Review != 1 {
		t.Fatal(stats)
	}
}

func TestRunnerDistinctAliasesResolvedToSameTrackMergeInReview(t *testing.T) {
	st := openStore(t)
	c := mbCandidate("new-track", "Song", "Artist", "Album")
	c.ReleaseRef = sptr("new-release")
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {c}, "Other input": {c}}}
	runner := newRunner(st, searcher)
	owned := mbCandidate("owned", "Another song", "Artist", "Album")
	// Existing group makes this concrete release require confirmation.
	plan, err := runner.library.PrepareCandidate(context.Background(), owned, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runner.library.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	first := enqueueOne(t, st, "Song", "Artist", "Album")
	runToCompletion(t, runner, first)
	// Different artist credits are not text duplicates; exact title/album and
	// the same resolved upstream track provide the additional evidence.
	second := enqueueOne(t, st, "Song", "Other credit", "Album")
	runToCompletion(t, runner, second)
	stats, _ := st.Stats(second)
	if stats.Merged != 1 {
		t.Fatal(stats)
	}
	items, err := st.AllReviewItems()
	if err != nil || len(items) != 1 {
		t.Fatal(items, err)
	}
	originals, err := st.OriginalInputs(items[0].ID)
	if err != nil || len(originals) != 2 {
		t.Fatal(originals, err)
	}
}
