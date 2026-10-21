package app

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestLyricsCandidateDeadlineWithFakeClock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		round, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		first, firstCancel := lyricCandidateContext(round)
		defer firstCancel()
		time.Sleep(25 * time.Second)
		second, secondCancel := lyricCandidateContext(round)
		defer secondCancel()
		if deadline, _ := first.Deadline(); !deadline.Equal(time.Now().Add(15 * time.Second)) {
			t.Fatal("first candidate exceeded 40 seconds")
		}
		if deadline, _ := second.Deadline(); !deadline.Equal(time.Now().Add(35 * time.Second)) {
			t.Fatal("later candidate exceeded round deadline")
		}
		time.Sleep(15 * time.Second)
		synctest.Wait()
		if !errors.Is(first.Err(), context.DeadlineExceeded) || second.Err() != nil {
			t.Fatal(first.Err(), second.Err())
		}
		time.Sleep(20 * time.Second)
		synctest.Wait()
		if !errors.Is(second.Err(), context.DeadlineExceeded) {
			t.Fatal(second.Err())
		}
	})
}

func TestLyricsRoundDeadlineAndCompletedDocumentsSurvive(t *testing.T) {
	deadlineSeen := make(chan time.Time, 3)
	p := &prefetchSource{id: "only", count: 3, fetch: func(ctx context.Context, c lyrics.Candidate) (*lyricdoc.Document, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Error("missing candidate deadline")
		}
		deadlineSeen <- deadline
		if c.Ref == "00" {
			return lyricdoc.ParseText("complete document")
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	a, events, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	before := time.Now()
	cards, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	s := a.lyricSessions[id]
	roundDeadline, ok := s.roundCtx.Deadline()
	if !ok || roundDeadline.Before(before.Add(60*time.Second)) || roundDeadline.After(time.Now().Add(60*time.Second)) {
		t.Fatal("round must start at search", roundDeadline)
	}
	// Inject a short remaining round without changing the live session.
	s.roundCancel()
	deadline := time.Now().Add(100 * time.Millisecond)
	s.roundCtx, s.roundCancel = context.WithDeadline(provider.WithSendBudget(s.ctx, s.roundBudget), deadline)
	if _, err := a.StartLyricsPrefetch(id); err != nil {
		t.Fatal(err)
	}
	last := waitPrefetch(t, events, id)
	if last.Phase != "stopped" || last.Success != 1 || last.Incomplete != 1 || len(p.calls()) != 2 {
		t.Fatal(last, p.calls())
	}
	for i := 0; i < 2; i++ {
		if got := <-deadlineSeen; !got.Equal(deadline) {
			t.Fatal("candidate exceeded earlier round deadline", got)
		}
	}
	doc, err := a.FetchLyrics(id, cards[0].ID)
	if err != nil || doc.Document.Text != "complete document" || len(p.calls()) != 2 {
		t.Fatal("round expiry invalidated completed document", doc, err)
	}
	if s.ctx.Err() != nil {
		t.Fatal("work deadline cancelled session")
	}
}

func TestLyricsCandidateDeadlineIncludesPoolWait(t *testing.T) {
	seen := make(chan time.Time, 1)
	p := &prefetchSource{id: "only", count: 1, fetch: func(ctx context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
		deadline, _ := ctx.Deadline()
		seen <- deadline
		return lyricdoc.ParseText("body")
	}}
	a, events, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	start := time.Now()
	a.StartLyricsPrefetch(id)
	waitPrefetch(t, events, id)
	if deadline := <-seen; deadline.Before(start.Add(40*time.Second)) || deadline.After(time.Now().Add(40*time.Second)) {
		t.Fatal("candidate deadline is not 40 seconds", deadline)
	}
}

func TestLyricsPartialRestrictionKeepsDocumentAndStopsRemaining(t *testing.T) {
	p := &prefetchSource{id: "only", count: 3, fetch: func(ctx context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
		provider.StopSendBudget(ctx, &lyrics.FetchError{Kind: "upstream", StopSource: true, Cause: errors.New("restricted sibling block")})
		return lyricdoc.ParseText("valid ordinary block")
	}}
	a, events, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	a.StartLyricsPrefetch(id)
	last := waitPrefetch(t, events, id)
	if last.Success != 1 || last.Phase != "stopped" || len(p.calls()) != 1 {
		t.Fatal(last, p.calls())
	}
	if last.Candidates[1].StopReason != "upstream" || last.Candidates[1].Translation.Status != "pending" {
		t.Fatal("unexecuted channel misreported", last.Candidates[1])
	}
}

func TestLyricsBudgetExhaustionStopsOnlyItsRound(t *testing.T) {
	p := &prefetchSource{id: "limited", count: 4, fetch: func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) {
		return nil, provider.ErrSendBudget
	}}
	other := &prefetchSource{id: "other", count: 1}
	a, events, done := prefetchApp(t, p, other)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(id, "limited", lyrics.Query{Title: "Song"})
	a.StartLyricsPrefetch(id)
	last := waitPrefetch(t, events, id)
	if last.Phase != "stopped" || len(p.calls()) != 1 {
		t.Fatal(last, p.calls())
	}
	for _, item := range last.Candidates {
		if item.FetchState == "loading" || item.FetchState == "queued" {
			t.Fatal(item)
		}
	}
	otherID, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(otherID, "other", lyrics.Query{Title: "Song"})
	a.StartLyricsPrefetch(otherID)
	if last := waitPrefetch(t, events, otherID); last.Success != 1 {
		t.Fatal("other source blocked", last)
	}
	if a.lyricSessions[id].restricted != nil && !errors.Is(a.lyricSessions[id].restricted, provider.ErrSendBudget) {
		t.Fatal("budget was treated as platform restriction")
	}
}
