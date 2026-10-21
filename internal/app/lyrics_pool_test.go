package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
)

func TestLyricsSelectingInflightPrefetchWaitsForSameWork(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	p := &prefetchSource{id: "only", count: 3, fetch: func(ctx context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
		once.Do(func() { close(started) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return lyricdoc.ParseText("body")
		}
	}}
	a, events, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	cards, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.StartLyricsPrefetch(id); err != nil {
		t.Fatal(err)
	}
	<-started
	results := make(chan error, 2)
	go func() { _, err := a.FetchLyrics(id, cards[0].ID); results <- err }()
	go func() { _, err := a.FetchLyrics(id, cards[1].ID); results <- err }()
	// Snapshot is readable while the provider blocks: no network under lock.
	snapshot, err := a.LyricsSearchSnapshot(id)
	if err != nil || snapshot.Candidates[0].FetchState != "loading" {
		t.Fatal(snapshot, err)
	}
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("preview waiter blocked")
		}
	}
	waitPrefetch(t, events, id)
	if got := p.calls(); len(got) != 3 {
		t.Fatal("duplicate fetch", got)
	}
}

func TestLyricsCancellationDrainsPoolAndQueuedWork(t *testing.T) {
	started := make(chan struct{}, 2)
	p := &prefetchSource{id: "only", count: 3, fetch: func(ctx context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, _, done := prefetchApp(t, p)
	defer done()
	ids := []string{}
	for i := 0; i < 2; i++ {
		id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
		ids = append(ids, id)
		if _, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.StartLyricsPrefetch(id); err != nil {
			t.Fatal(err)
		}
	}
	<-started
	for _, id := range ids {
		a.CancelLyricsSearch(id)
	}
	drained := make(chan struct{})
	go func() { a.lyricWorkers.Wait(); close(drained) }()
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("cancel did not drain workers")
	}
	if len(p.calls()) != 1 {
		t.Fatal("same source or cancelled queue started extra work", p.calls())
	}
	for _, id := range ids {
		if _, err := a.FetchLyrics(id, "old"); err == nil {
			t.Fatal("cancelled session revived")
		}
	}
}

func TestLyricsShutdownWaitsForProviderExit(t *testing.T) {
	started, cancelled, exit := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := &prefetchSource{id: "only", count: 1, fetch: func(ctx context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-exit
		return nil, ctx.Err()
	}}
	a, _, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	a.StartLyricsPrefetch(id)
	<-started
	stopped := make(chan struct{})
	go func() { a.shutdown(context.Background()); close(stopped) }()
	<-cancelled
	select {
	case <-stopped:
		t.Fatal("shutdown passed active provider")
	default:
	}
	close(exit)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("shutdown not drained")
	}
	if _, err := a.FetchLyrics(id, "old"); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
