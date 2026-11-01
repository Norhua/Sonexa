package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestProblemSplitInvalidatesPreparedSingleRowApproval(t *testing.T) {
	s, st, _ := newTestService(t)
	profile := matching.NewInputProfile("Song", "Artist", "Album")
	batch, err := st.CreateBatch(&store.ImportBatch{Filename: "split.csv", TotalRows: 2})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: "[1]", Profile: &profile}, {RowIndex: 1, RawRow: "[2]", Profile: &profile}}); err != nil {
		t.Fatal(err)
	}
	var items []*store.ImportItem
	for range 2 {
		item, err := st.ClaimNextItem(batch)
		if err != nil {
			t.Fatal(err)
		}
		if err := st.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
		item, err = st.GetItem(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, item)
	}
	// Existing group + new release requires an explicit user approval.
	commit(t, s, prepare(t, s, seed("existing", "other", "group", "Album", "Artist")), false)
	plan, err := s.PrepareCandidate(context.Background(), candidate("new-release", "new-track", nil), items[1].ID)
	if err != nil || !plan.Baseline.RequiresConfirmation {
		t.Fatal(plan, err)
	}
	approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	page, err := st.FindProblemCandidates(context.Background(), matching.ProblemInput{Profile: profile}, 0, 10)
	if err != nil || len(page.Problems) != 1 {
		t.Fatal(page, err)
	}
	if _, err := st.SplitProblemItem(page.Problems[0].ID, page.Problems[0].Revision, items[1].ID, items[1].Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, *approval); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("old approval survived split", err)
	}
	if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	for _, item := range items {
		got, err := st.GetItem(item.ID)
		if err != nil || got.Status != store.StatusReview {
			t.Fatal(got, err)
		}
	}
}
