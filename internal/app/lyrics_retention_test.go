package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
)

func TestLyricsRetentionCloseBeforeReservedWorkerStarts(t *testing.T) {
	p := &prefetchSource{id: "only", count: 3}
	a, _, done := prefetchApp(t, p)
	defer done()
	a.lyricRetention = lyrics.NewRetentionBudget(4096, 4096)
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	cards, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	s := a.lyricSessions[id]
	// Reproduce the exact boundary: Start reserved a flight, but close runs
	// before the goroutine can begin it. No timing-dependent sleep is needed.
	s.mu.Lock()
	s.phase, s.prefetchStarted = "prefetching", true
	for _, key := range s.top {
		s.inflight[key] = &lyricFlight{done: make(chan struct{})}
		item := s.summaries[key]
		item.FetchState = "queued"
		s.summaries[key] = item
	}
	flight := s.inflight[cards[0].ID]
	s.mu.Unlock()
	a.CancelLyricsSearch(id)
	a.runLyricsPrefetch(id, s)
	if _, err := waitLyricFlight(context.Background(), flight); !errors.Is(err, context.Canceled) {
		t.Fatal("reservation not cancelled", err)
	}
	if len(p.calls()) != 0 || len(s.inflight) != 0 || len(s.documents) != 0 || len(s.previewed) != 0 || a.lyricRetention.Used() != 0 {
		t.Fatal("closed session retained or restarted work")
	}
	for _, item := range s.summaries {
		if item.FetchState == "loading" || item.FetchState == "queued" {
			t.Fatal("closed reservation not terminal", item.FetchState)
		}
	}
	s.close() // repeated release is harmless
	if a.lyricRetention.Used() != 0 {
		t.Fatal("double release")
	}
}

func TestLyricsRetentionRejectsNewDocumentsWithoutEviction(t *testing.T) {
	doc, err := lyricdoc.Build(&lyricdoc.Input{Text: "[00:01]private original", Translation: lyricdoc.TextChannel("private translation", nil)})
	if err != nil {
		t.Fatal(err)
	}
	bytes := lyricdoc.RetainedBytes(doc)
	if bytes <= len(doc.Text) {
		t.Fatal("retention ignored projections")
	}
	p := &prefetchSource{id: "only", count: 3, fetch: func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) { return doc, nil }}
	a, _, done := prefetchApp(t, p)
	defer done()
	a.lyricRetention = lyrics.NewRetentionBudget(bytes, 2*bytes)
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	cards, _ := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	snapshot := finishLyricsPrefetch(t, a, id)
	if snapshot.Success != 1 || snapshot.Failed != 2 || a.lyricRetention.Used() != bytes {
		t.Fatal(snapshot, a.lyricRetention.Used())
	}
	if snapshot.Candidates[1].ErrorKind != "memory_limit" {
		t.Fatal("memory rejection not explicit", snapshot.Candidates[1])
	}
	preview, err := a.FetchLyrics(id, cards[0].ID)
	if err != nil || preview.Document != doc {
		t.Fatal("retained document evicted", err)
	}
	if err := a.AcknowledgeLyricsPreview(id, cards[0].ID, preview.Version); err != nil {
		t.Fatal(err)
	}
	// Refresh attempts a larger document while preserving the retained text.
	large, _ := lyricdoc.ParseText(strings.Repeat("large", bytes))
	p.fetch = func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) { return large, nil }
	if _, err := a.RefreshLyrics(id, cards[0].ID); !errors.Is(err, lyrics.ErrRetentionLimit) {
		t.Fatal(err)
	}
	again, err := a.FetchLyrics(id, cards[0].ID)
	if err != nil || again.Document != doc || again.Version != preview.Version || a.lyricRetention.Used() != bytes {
		t.Fatal("rejection replaced or truncated preview", err)
	}
	body, err := json.Marshal(snapshot)
	if err != nil || strings.Contains(string(body), "private") || strings.Contains(string(body), "associations") {
		t.Fatal("summary contains full document")
	}
	a.CancelLyricsSearch(id)
	if a.lyricRetention.Used() != 0 {
		t.Fatal("close retained memory", a.lyricRetention.Used())
	}
}

func TestLyricsProcessRetentionAndExpiryRelease(t *testing.T) {
	doc, _ := lyricdoc.ParseText("body")
	bytes := lyricdoc.RetainedBytes(doc)
	budget := lyrics.NewRetentionBudget(2*bytes, bytes)
	p := &prefetchSource{id: "only", count: 1, fetch: func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) { return doc, nil }}
	a, _, done := prefetchApp(t, p)
	defer done()
	b, _, doneB := prefetchApp(t, p)
	defer doneB()
	a.lyricRetention, b.lyricRetention = budget, budget
	first, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(first, "only", lyrics.Query{Title: "Song"})
	finishLyricsPrefetch(t, a, first)
	second, _ := b.CreateLyricsSearch(lyricTrack(t, b))
	cards, _ := b.SearchLyrics(second, "only", lyrics.Query{Title: "Song"})
	if last := finishLyricsPrefetch(t, b, second); last.Success != 0 || last.Candidates[0].ErrorKind != "memory_limit" {
		t.Fatal("process limit bypassed", last)
	}
	a.lyricSessions[first].created = time.Now().Add(-31 * time.Minute)
	if _, err := a.LyricsSearchSnapshot(first); err == nil || budget.Used() != 0 {
		t.Fatal("expiry did not release memory", err)
	}
	if _, err := b.FetchLyrics(second, cards[0].ID); err != nil || budget.Used() != bytes {
		t.Fatal("released memory not reusable", err)
	}
	b.shutdown(context.Background())
	if budget.Used() != 0 {
		t.Fatal("shutdown retained memory")
	}
}
