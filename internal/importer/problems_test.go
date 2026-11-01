package importer

import (
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestRunnerEquivalentReviewRowsShareProblemWithoutHistory(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{}
	runner := newRunner(st, searcher)
	first := enqueueOne(t, st, "Song", "Artist", "Album")
	second := enqueueOne(t, st, "Song（译名：歌曲）", "Artist（原名：歌手）", "Album（译名：专辑）")
	runToCompletion(t, runner, first)
	runToCompletion(t, runner, second)
	var problems, members int
	if err := st.DB().QueryRow("SELECT count(*) FROM review_problem WHERE status='active'").Scan(&problems); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().QueryRow("SELECT count(*) FROM review_problem_item WHERE active=1").Scan(&members); err != nil {
		t.Fatal(err)
	}
	if problems != 1 || members != 2 {
		t.Fatal(problems, members)
	}
	for _, batch := range []int64{first, second} {
		stats, err := st.Stats(batch)
		if err != nil || stats.Total != 1 || stats.Review != 1 || stats.Imported != 0 || stats.Skipped != 0 {
			t.Fatal(stats, err)
		}
	}
	items, err := st.AllReviewItems()
	if err != nil || len(items) != 2 || items[0].RawRow == items[1].RawRow {
		t.Fatal(items, err)
	}
	// Single-row completion remains single-row until multi-input admission.
	if err := st.MarkDiscarded(items[0].ID); err != nil {
		t.Fatal(err)
	}
	other, err := st.GetItem(items[1].ID)
	if err != nil || other.Status != store.StatusReview {
		t.Fatal(other, err)
	}
	if err := st.DB().QueryRow("SELECT count(*) FROM review_problem_item WHERE active=1").Scan(&members); err != nil || members != 1 {
		t.Fatal(members, err)
	}
}
