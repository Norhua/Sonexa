package app

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestLyricsManualSingleFetchBudgetAndRetry(t *testing.T) {
	client := provider.NewClient(nil, nil, "test")
	sends := 0
	client.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		sends++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})
	attempt := 0
	p := &prefetchSource{id: "only", count: 5, fetch: func(ctx context.Context, c lyrics.Candidate) (*lyricdoc.Document, error) {
		if c.Ref != "03" {
			return lyricdoc.ParseText("plain")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 40*time.Second || time.Until(deadline) < 39*time.Second {
			t.Error("manual deadline did not reset", deadline)
		}
		attempt++
		count := 9
		if attempt > 1 {
			count = 1
		}
		for i := 0; i < count; i++ {
			_, err := client.Get(ctx, provider.GetRequest{Provider: "only", BaseURL: "https://test.example", CacheMode: provider.NoStore})
			if err != nil {
				return nil, err
			}
		}
		return lyricdoc.Build(&lyricdoc.Input{Text: "[00:01]manual original", Translation: lyricdoc.TextChannel("translation", nil)})
	}}
	a, _, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	cards, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.FetchLyrics(id, cards[3].ID); err == nil || len(p.calls()) != 0 {
		t.Fatal("manual fetch allowed before round finished", err)
	}
	finishLyricsPrefetch(t, a, id)
	if _, err := a.FetchLyrics(id, cards[3].ID); !errors.Is(err, provider.ErrSendBudget) || sends != 8 {
		t.Fatal("manual ninth send escaped", err, sends)
	}
	if _, err := a.FetchLyrics(id, cards[3].ID); err != nil || sends != 9 {
		t.Fatal("explicit retry did not receive fresh budget", err, sends)
	}
	snapshot, _ := a.LyricsSearchSnapshot(id)
	manual := snapshot.Candidates[3]
	if manual.Candidate.TotalScore == nil || *manual.Candidate.TotalScore <= *snapshot.Candidates[0].Candidate.TotalScore || manual.Translation.Status != "available" || manual.FetchState != "ready" {
		t.Fatal("manual result did not join content ranking", manual)
	}
	if len(p.calls()) != 5 || snapshot.Candidates[4].FetchState != "pending" || snapshot.Candidates[4].Candidate.TotalScore != nil {
		t.Fatal("manual fetch started neighboring candidate", p.calls())
	}
}

func TestLyricsManualCannotBypassRestrictionOrLeaveQueuedItemLoading(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	p := &prefetchSource{id: "only", count: 5, fetch: func(ctx context.Context, c lyrics.Candidate) (*lyricdoc.Document, error) {
		if c.Ref < "03" {
			return lyricdoc.ParseText("available")
		}
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return nil, &lyrics.FetchError{Kind: "rate_limit", StopSource: true, Cause: errors.New("limited")}
		}
	}}
	a, _, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	cards, _ := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	finishLyricsPrefetch(t, a, id)
	results := make(chan error, 2)
	go func() { _, err := a.FetchLyrics(id, cards[3].ID); results <- err }()
	<-started
	go func() { _, err := a.FetchLyrics(id, cards[4].ID); results <- err }()
	close(release)
	for i := 0; i < 2; i++ {
		if err := <-results; err == nil {
			t.Fatal("restriction bypassed")
		}
	}
	if len(p.calls()) != 4 {
		t.Fatal("queued manual item sent after restriction", p.calls())
	}
	if _, err := a.FetchLyrics(id, cards[3].ID); err == nil || len(p.calls()) != 4 {
		t.Fatal("manual retry bypassed restriction")
	}
	snapshot, _ := a.LyricsSearchSnapshot(id)
	for _, item := range snapshot.Candidates {
		if item.FetchState == "loading" || item.FetchState == "queued" {
			t.Fatal(item)
		}
	}
	if _, err := a.FetchLyrics(id, cards[0].ID); err != nil {
		t.Fatal("restriction invalidated cached document", err)
	}
}
