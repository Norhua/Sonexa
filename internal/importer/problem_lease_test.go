package importer

import (
	"context"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestRunnerDefersInteractingProblemWithoutNetworkOrFailure(t *testing.T) {
	st := openStore(t)
	track, _ := seedLocalRunnerTrack(t, st, true, false)
	batch := enqueueOne(t, st, "Song", "Artist", "Album")
	item, err := st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err := st.RetryItem(item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	scope, err := st.ItemProblemScope(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.AcquireProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	searcher := &recordingSearcher{}
	runner := newRunner(st, searcher)
	line := runner.processItem(context.Background(), item)
	if !strings.Contains(line, "暂缓") || searcher.queryCount() != 0 {
		t.Fatal(line, searcher.queryCount())
	}
	got, err := st.GetItem(item.ID)
	if err != nil || got.Status != store.StatusReview || got.AttemptCount != item.AttemptCount || got.LastError != item.LastError {
		t.Fatal(got, err)
	}
	if err := st.ReleaseProblemLease(session.Lease); err != nil {
		t.Fatal(err)
	}
	if err := st.RetryItem(item.ID); err != nil {
		t.Fatal(err)
	}
	item, err = st.ClaimNextItem(batch)
	if err != nil {
		t.Fatal(err)
	}
	line = runner.processItem(context.Background(), item)
	got, err = st.GetItem(item.ID)
	if err != nil || got.Status != store.StatusSkipped || got.TrackID == nil || *got.TrackID != track || !strings.Contains(line, "已存在跳过") || searcher.queryCount() != 0 {
		t.Fatal(got, line, err)
	}
}
