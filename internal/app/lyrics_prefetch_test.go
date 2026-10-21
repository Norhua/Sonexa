package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

type prefetchSource struct {
	id    provider.ProviderID
	count int
	mu    sync.Mutex
	refs  []string
	fetch func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error)
}

func (p *prefetchSource) ID() provider.ProviderID { return p.id }
func (p *prefetchSource) DisplayName() string     { return string(p.id) }
func (p *prefetchSource) SearchLyrics(_ context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	rows := []lyrics.Candidate{}
	for i := p.count - 1; i >= 0; i-- {
		rows = append(rows, lyrics.Candidate{Ref: fmt.Sprintf("%02d", i), Title: q.Title, Artist: q.Artist, DurationMS: q.DurationMS})
	}
	return rows, nil
}
func (p *prefetchSource) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyricdoc.Document, error) {
	p.mu.Lock()
	p.refs = append(p.refs, c.Ref)
	p.mu.Unlock()
	if p.fetch != nil {
		return p.fetch(ctx, c)
	}
	return lyricdoc.Build(&lyricdoc.Input{Text: "[00:01]private lyric body", Translation: lyricdoc.TextChannel("private translation", nil)})
}
func (p *prefetchSource) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.refs...)
}

func prefetchApp(t *testing.T, providers ...*prefetchSource) (*App, <-chan LyricsProgress, func()) {
	t.Helper()
	a, cleanup := newTestApp(t)
	a.enricher.Pause()
	entries := []lyrics.Entry{}
	for _, p := range providers {
		entries = append(entries, lyrics.Entry{Provider: p})
	}
	r, err := lyrics.NewRegistry(entries...)
	if err != nil {
		t.Fatal(err)
	}
	a.lyricSvc = &lyrics.Service{Sources: r}
	events := make(chan LyricsProgress, 256)
	a.emit = func(name string, args ...any) {
		if name == EventLyricsProgress {
			events <- args[0].(LyricsProgress)
		}
	}
	return a, events, func() {
		a.mu.Lock()
		for id, s := range a.lyricSessions {
			s.close()
			delete(a.lyricSessions, id)
		}
		a.mu.Unlock()
		a.lyricWorkers.Wait()
		cleanup()
	}
}

func finishLyricsPrefetch(t *testing.T, a *App, id string) *LyricsProgress {
	t.Helper()
	if _, err := a.StartLyricsPrefetch(id); err != nil {
		t.Fatal(err)
	}
	a.lyricWorkers.Wait()
	snapshot, err := a.LyricsSearchSnapshot(id)
	if err != nil || !lyricRoundFinished(snapshot.Phase) {
		t.Fatal("prefetch did not terminate", snapshot, err)
	}
	return snapshot
}

func waitPrefetch(t *testing.T, events <-chan LyricsProgress, session string) LyricsProgress {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.SessionID == session && (event.Phase == "done" || event.Phase == "partial" || event.Phase == "stopped") {
				return event
			}
		case <-timer.C:
			t.Fatal("prefetch did not finish")
			return LyricsProgress{}
		}
	}
}

func TestLyricsPrefetchFrozenTopThreeAndIdempotence(t *testing.T) {
	p := &prefetchSource{id: "only", count: 20}
	p.fetch = func(_ context.Context, c lyrics.Candidate) (*lyricdoc.Document, error) {
		if c.Ref == "00" {
			return nil, lyricdoc.ErrNoLyrics
		}
		return lyricdoc.Build(&lyricdoc.Input{Text: "[00:01]private lyric body", Translation: lyricdoc.TextChannel("private translation", nil)})
	}
	a, events, done := prefetchApp(t, p)
	defer done()
	track := lyricTrack(t, a)
	id, err := a.CreateLyricsSearch(track)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.StartLyricsPrefetch(id); err == nil {
		t.Fatal("start before search")
	}
	cards, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song", Artist: "Artist", DurationMS: 3000})
	if err != nil || len(cards) != 20 {
		t.Fatal(cards, err)
	}
	cards[0].Ref = "forged" // returned slice cannot alter the frozen server list
	first, err := a.StartLyricsPrefetch(id)
	if err != nil {
		t.Fatal(err)
	}
	if first.Selected != 3 || first.Searched != 20 {
		t.Fatal(first)
	}
	for i := 0; i < 4; i++ {
		if _, err := a.StartLyricsPrefetch(id); err != nil {
			t.Fatal(err)
		}
	}
	last := waitPrefetch(t, events, id)
	if last.Success != 2 || last.NoLyrics != 1 || last.Incomplete != 0 || last.Phase != "partial" {
		t.Fatal(last)
	}
	if got := strings.Join(p.calls(), ","); got != "00,01,02" {
		t.Fatal("expanded or reordered frozen top", got)
	}
	for _, item := range last.Candidates {
		if item.Candidate.Ref > "02" && (item.FetchState != "pending" || item.Candidate.TotalScore != nil) {
			t.Fatal("scored unrequested item", item)
		}
	}
	body, err := json.Marshal(last)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "private lyric") || strings.Contains(string(body), "private translation") || strings.Contains(string(body), "associations") {
		t.Fatal("progress leaked full text")
	}
	snapshot, err := a.LyricsSearchSnapshot(id)
	if err != nil || snapshot.Sequence != last.Sequence {
		t.Fatal(snapshot, err)
	}
	if snapshot.Sequence <= first.Sequence {
		t.Fatal("sequence did not advance")
	}
	*snapshot.Candidates[1].Candidate.TotalScore = 999
	again, _ := a.LyricsSearchSnapshot(id)
	if *again.Candidates[1].Candidate.TotalScore == 999 {
		t.Fatal("snapshot aliased session")
	}
	if _, err := a.AdoptLyrics(id, last.Candidates[1].Candidate.ID, 1); err == nil {
		t.Fatal("prefetch granted adoption permission")
	}
	if _, err := a.FetchLyrics(id, last.Candidates[1].Candidate.ID); err != nil {
		t.Fatal(err)
	}
	if len(p.calls()) != 3 {
		t.Fatal("preview refetched")
	}
}

func TestLyricsPrefetchFourSourcesAtMostTwelve(t *testing.T) {
	ps := []*prefetchSource{{id: "163", count: 20}, {id: "qq", count: 20}, {id: "kugou", count: 20}, {id: "kuwo", count: 20}}
	a, events, done := prefetchApp(t, ps...)
	defer done()
	for _, p := range ps {
		id, err := a.CreateLyricsSearch(lyricTrack(t, a))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.SearchLyrics(id, string(p.id), lyrics.Query{Title: "Song"}); err != nil {
			t.Fatal(err)
		}
		if _, err := a.StartLyricsPrefetch(id); err != nil {
			t.Fatal(err)
		}
		if last := waitPrefetch(t, events, id); last.Success != 3 {
			t.Fatal(last)
		}
	}
	total := 0
	for _, p := range ps {
		total += len(p.calls())
	}
	if total != 12 {
		t.Fatal(total)
	}
}

func TestLyricsPrefetchEmptyAndShortResults(t *testing.T) {
	for _, count := range []int{0, 1, 2} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			p := &prefetchSource{id: "only", count: count}
			a, events, done := prefetchApp(t, p)
			defer done()
			id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
			if _, err := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"}); err != nil {
				t.Fatal(err)
			}
			snapshot, err := a.StartLyricsPrefetch(id)
			if err != nil {
				t.Fatal(err)
			}
			if count > 0 {
				last := waitPrefetch(t, events, id)
				snapshot = &last
			}
			if len(p.calls()) != count || snapshot.Selected != count || snapshot.Phase != "done" {
				t.Fatal(snapshot, p.calls())
			}
		})
	}
}

func TestLyricsPrefetchRestrictedFailureClosesQueuedItems(t *testing.T) {
	p := &prefetchSource{id: "only", count: 4, fetch: func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) {
		return nil, &lyrics.FetchError{Kind: "rate_limit", StopSource: true, Cause: errors.New("limited")}
	}}
	a, events, done := prefetchApp(t, p)
	defer done()
	id, _ := a.CreateLyricsSearch(lyricTrack(t, a))
	a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	if _, err := a.StartLyricsPrefetch(id); err != nil {
		t.Fatal(err)
	}
	last := waitPrefetch(t, events, id)
	if len(p.calls()) != 1 || last.Phase != "stopped" || last.Failed != 1 || last.Incomplete != 2 {
		t.Fatal(last, p.calls())
	}
	for _, item := range last.Candidates[1:3] {
		if item.FetchState != "skipped" || item.Translation.Status != "pending" {
			t.Fatal(item)
		}
	}
}
