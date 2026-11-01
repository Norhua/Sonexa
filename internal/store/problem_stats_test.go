package store

import (
	"reflect"
	"testing"
	"time"
)

func TestProblemStatsCrossBatchMixedStatesAndMembership(t *testing.T) {
	s := openTestStore(t)
	a := enqueueProblemInput(t, s, "Song", "Artist", "Album")
	b := enqueueProblemInput(t, s, "Song（译名：歌曲）", "Artist", "Album")
	c := enqueueProblemInput(t, s, "Other", "Different", "Other album")
	if err := s.MarkReview(a.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []*ImportItem{b, c} {
		if _, err := s.RecordFailure(item.ID, "fixture", time.Second, 1); err != nil {
			t.Fatal(err)
		}
	}
	assert := func(batch int64, want ProblemStats) {
		t.Helper()
		got, err := s.ReviewProblemStats(batch)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatal(got, want, err)
		}
	}
	assert(0, ProblemStats{ReviewProblems: 2, PendingProblems: 1, FailedProblems: 1, SourceRows: 3})
	assert(a.BatchID, ProblemStats{ReviewProblems: 1, PendingProblems: 1, SourceRows: 1})
	assert(b.BatchID, ProblemStats{ReviewProblems: 1, PendingProblems: 1, SourceRows: 1}) // same global problem classification
	assert(c.BatchID, ProblemStats{ReviewProblems: 1, FailedProblems: 1, SourceRows: 1})
	stats, err := s.Stats(a.BatchID)
	if err != nil || stats.Total != 1 || stats.Review != 1 || stats.Unmatched != 1 || stats.Problems.PendingProblems != 1 {
		t.Fatal(stats, err)
	}
	scope, _ := s.ItemProblemScope(a.ID)
	if err := s.DiscardProblemMembers(scope, []int64{a.ID}); err != nil {
		t.Fatal(err)
	}
	assert(0, ProblemStats{ReviewProblems: 2, FailedProblems: 2, SourceRows: 2})
	assert(a.BatchID, ProblemStats{})
	if err := s.RetryItem(b.ID); err != nil {
		t.Fatal(err)
	}
	assert(0, ProblemStats{ReviewProblems: 2, PendingProblems: 1, FailedProblems: 1, SourceRows: 2})
}
func TestBatchStatsExistingSkipIsSubsetNotExtraRow(t *testing.T) {
	s := openTestStore(t)
	batch, err := s.CreateBatch(&ImportBatch{Filename: "stats", TotalRows: 8})
	if err != nil {
		t.Fatal(err)
	}
	items := make([]ImportItem, 8)
	for i := range items {
		items[i].RowIndex = i
		items[i].RawRow = "[]"
	}
	if err := s.EnqueueItems(batch, items); err != nil {
		t.Fatal(err)
	}
	// Direct fixture of mutually exclusive row states; distinct problem counts
	// are verified through production lifecycle above.
	statuses := []string{"queued", "matching", "imported", "review", "failed", "skipped", "skipped", "discarded"}
	for i, status := range statuses {
		reason := ""
		if i == 5 {
			reason = string(ResolutionExistingTrack)
		}
		if _, err := s.db.Exec("UPDATE import_item SET status=?,resolution_reason=? WHERE batch_id=? AND row_index=?", status, reason, batch, i); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := s.Stats(batch)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 8 || stats.Queued+stats.Matching+stats.Imported+stats.Review+stats.Failed+stats.Skipped+stats.Discarded != 8 || stats.Skipped != 2 || stats.ExistingSkipped != 1 || stats.Processed() != 6 {
		t.Fatal(stats)
	}
}
