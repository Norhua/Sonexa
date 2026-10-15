package importer

import (
	"context"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestLibraryNotificationFollowsCommitBeforeOptionalEnrichment(t *testing.T) {
	for _, matched := range []bool{true, false} {
		t.Run(map[bool]string{true: "imported", false: "review"}[matched], func(t *testing.T) {
			st := openStore(t)
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{}}
			if matched {
				searcher.byTitle["歌曲"] = []provider.Candidate{mbCandidate("track", "歌曲", "艺术家", "专辑")}
			}
			batch := enqueueOne(t, st, "歌曲", "艺术家", "专辑")
			r := newRunner(st, searcher)
			notifications := 0
			r.SetLibraryChanged(func() {
				notifications++
				stats, err := st.Stats(batch)
				if err != nil || stats.Imported != 1 {
					t.Errorf("notified before import commit: %+v %v", stats, err)
				}
			})
			r.fetchOnMatch = true
			r.SetEnricher(func(int64, string, string) {
				if notifications != 1 {
					t.Error("optional network enrichment began before library was notified")
				}
			})
			events := runToCompletion(t, r, batch)
			want := 0
			if matched {
				want = 1
			}
			if notifications != want {
				t.Fatalf("notifications=%d want %d", notifications, want)
			}
			if !events[len(events)-1].Done || r.IsRunning(batch) {
				t.Fatal("completion event must observe stopped runner")
			}
		})
	}
}

func TestFinalProgressReadsStoppedBatch(t *testing.T) {
	st := openStore(t)
	r := newRunner(st, &recordingSearcher{byTitle: map[string][]provider.Candidate{}})
	batch := enqueueOne(t, st, "无结果", "艺术家", "专辑")
	done := make(chan bool, 1)
	r.emit = func(p Progress) {
		if p.Done {
			b, err := st.GetBatch(batch)
			done <- !r.IsRunning(batch) && err == nil && b.Status == store.BatchDone
		}
	}
	if err := r.Start(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	select {
	case stopped := <-done:
		if !stopped {
			t.Fatal("done event preceded durable final status")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing done event")
	}
}
