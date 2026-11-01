package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestReviewProblemQueueSourcesScopeAndReadOnly(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	one, two, scope := appProblemRows(t, a)
	second, _ := a.store.GetItem(two)
	if _, err := a.store.RecordFailure(two, "fixture failed", time.Second, 1, second.Revision); err != nil {
		t.Fatal(err)
	}
	// A different explicit album must remain a separate problem.
	batch, err := a.store.CreateBatch(&store.ImportBatch{Filename: "other.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.store.EnqueueItems(batch, []store.ImportItem{{RawRow: `["Song","Artist","Other Album"]`, InTitle: strPtr("Song"), InArtist: strPtr("Artist"), InAlbum: strPtr("Other Album")}}); err != nil {
		t.Fatal(err)
	}
	other, err := a.store.ClaimNextItem(batch)
	if err != nil || other == nil {
		t.Fatal(other, err)
	}
	if err := a.store.MarkReview(other.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	base, _ := a.registry.Get(provider.MusicBrainz)
	searcher := &delayedReviewSearcher{MetadataProvider: base, started: make(chan struct{}, 1), release: make(chan struct{})}
	a.registry.Register(searcher)
	queue, err := a.GetReviewProblemQueue()
	if err != nil || len(queue.Items) != 2 || queue.Problems != (store.ProblemStats{ReviewProblems: 2, PendingProblems: 2, SourceRows: 3}) {
		t.Fatal(queue, err)
	}
	p := queue.Items[0]
	if p.ID != scope.ProblemID || p.Status != "pending" || len(p.Sources) != 2 || p.Scope.LeaseToken != "" {
		t.Fatal(p)
	}
	for i, id := range []int64{one, two} {
		item, _ := a.store.GetItem(id)
		source := p.Sources[i]
		if source.ID != id || source.Revision != item.Revision || source.BatchID != item.BatchID || source.BatchTag != "source.csv" || source.RawRow != item.RawRow || source.RowIndex != item.RowIndex || !reflect.DeepEqual(source.InTitle, item.InTitle) || !reflect.DeepEqual(source.Profile, store.ItemProfile(item)) {
			t.Fatal("lost source", source, item)
		}
		if source.InDurationMS == nil || *source.InDurationMS != 620000 || *source.InDiscNo != 2 || *source.InTrackNo != 3 || *source.InYear != 2020 {
			t.Fatal("lost numeric input", source)
		}
		if p.Scope.Members[i].ItemID != id || p.Scope.Members[i].InputRevision != item.Revision {
			t.Fatal("mixed scope snapshot", p.Scope)
		}
	}
	if p.Sources[1].LastError == nil || *p.Sources[1].LastError != "fixture failed" || p.Sources[1].AttemptCount != 1 || p.Sources[1].LastErrorAt == nil {
		t.Fatal(p.Sources[1])
	}
	again, err := a.GetReviewProblemQueue()
	if err != nil || !reflect.DeepEqual(queue, again) {
		t.Fatal("read changed state", again, err)
	}
	select {
	case <-searcher.started:
		t.Fatal("queue read searched upstream")
	default:
	}
	if err := a.DiscardReviewProblemMembers(p.Scope, []int64{one}); err != nil {
		t.Fatal(err)
	}
	next, err := a.GetReviewProblemQueue()
	if err != nil || next.Revision <= queue.Revision || next.Items[0].ID != p.ID || next.Items[0].Status != "failed" || len(next.Items[0].Sources) != 1 || next.Items[0].Sources[0].ID != two || next.Problems.FailedProblems != 1 {
		t.Fatal("stable problem lost on source removal", next, err)
	}
}

func TestReviewProblemQueueDraftRetryAndEmpty(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	one, two, scope := appProblemRows(t, a)
	interaction, err := a.AcquireReviewProblemLease(scope)
	if err != nil {
		t.Fatal(err)
	}
	query := store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "draft only", Provider: "itunes", Country: "JP"}
	_, err = a.UpdateReviewProblemDraft(interaction.Scope, query)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := a.GetReviewProblemQueue()
	if err != nil || !reflect.DeepEqual(queue.Items[0].Query, &query) || queue.Items[0].Sources[0].RawRow != `["Song"]` || queue.Items[0].Scope.LeaseToken != "" {
		t.Fatal(queue, err)
	}
	if err := a.ReleaseReviewProblemLease(interaction.Lease); err != nil {
		t.Fatal(err)
	}
	item, _ := a.store.GetItem(two)
	if _, err := a.store.RecordFailure(two, "failed", time.Second, 1, item.Revision); err != nil {
		t.Fatal(err)
	}
	if err := a.store.RetryItem(two); err != nil {
		t.Fatal(err)
	}
	queue, err = a.GetReviewProblemQueue()
	if err != nil || len(queue.Items[0].Sources) != 2 || queue.Items[0].Sources[1].Status != "queued" || queue.Problems.SourceRows != 2 {
		t.Fatal("retrying source disappeared", queue, err)
	}
	if err := a.DiscardReviewProblemMembers(queue.Items[0].Scope, []int64{one, two}); err != nil {
		t.Fatal(err)
	}
	queue, err = a.GetReviewProblemQueue()
	if err != nil || queue.Items == nil || len(queue.Items) != 0 || queue.Problems != (store.ProblemStats{}) {
		t.Fatal(queue, err)
	}
}
