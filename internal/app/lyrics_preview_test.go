package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/store"
)

func TestLyricsPreviewRequiresExactVersionAndRefreshClearsApproval(t *testing.T) {
	version := 0
	p := &prefetchSource{id: "only", count: 1, fetch: func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) {
		version++
		return lyricdoc.ParseText(fmt.Sprintf("version %d", version))
	}}
	a, _, done := prefetchApp(t, p)
	defer done()
	track := lyricTrack(t, a)
	id, _ := a.CreateLyricsSearch(track)
	cards, _ := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	key := cards[0].ID
	finishLyricsPrefetch(t, a, id)
	revision, _ := a.LyricsRevision(track)
	if _, err := a.AdoptLyrics(id, key, revision); err == nil {
		t.Fatal("prefetch approved adoption")
	}
	preview, err := a.FetchLyrics(id, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(id, key, revision); err == nil {
		t.Fatal("Fetch approved adoption before display")
	}
	for _, v := range []uint64{0, preview.Version + 1} {
		if err := a.AcknowledgeLyricsPreview(id, key, v); err == nil {
			t.Fatal("wrong version accepted", v)
		}
	}
	if err := a.AcknowledgeLyricsPreview(id, cards[0].SortKey, preview.Version); err == nil {
		t.Fatal("sort key granted permission")
	}
	other, _ := a.CreateLyricsSearch(track)
	defer a.CancelLyricsSearch(other)
	if err := a.AcknowledgeLyricsPreview(other, key, preview.Version); err == nil {
		t.Fatal("cross-session approval")
	}
	if err := a.AcknowledgeLyricsPreview(id, key, preview.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(id, key, revision); err != nil {
		t.Fatal(err)
	}
	if again, err := a.FetchLyrics(id, key); err != nil || again.Version != preview.Version || len(p.calls()) != 1 {
		t.Fatal("preview refetched", err)
	}
	next, err := a.RefreshLyrics(id, key)
	if err != nil || next.Version != preview.Version+1 || len(p.calls()) != 2 {
		t.Fatal("refresh did not replace version", next, err)
	}
	if err := a.AcknowledgeLyricsPreview(id, key, preview.Version); err == nil {
		t.Fatal("stale approval accepted")
	}
	if _, err := a.AdoptLyrics(id, key, revision+1); err == nil {
		t.Fatal("refresh kept old approval")
	}
	if err := a.AcknowledgeLyricsPreview(id, key, next.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(id, key, revision); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatal(err)
	}
	saved, err := a.GetTrackDetail(track)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != preview.Document.Text {
		t.Fatal("conflict replaced old file", err)
	}
	if _, err := a.AdoptLyrics(id, key, revision+1); err != nil {
		t.Fatal(err)
	}
	saved, _ = a.GetTrackDetail(track)
	if *saved.Metadata.Lyrics != next.Document.Text {
		t.Fatal("adoption did not use acknowledged version")
	}
}

func TestLyricsRefreshInFlightRejectsApprovalAndAdoption(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	calls := 0
	p := &prefetchSource{id: "only", count: 1, fetch: func(ctx context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
		calls++
		if calls > 1 {
			close(started)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
			}
		}
		return lyricdoc.ParseText("document")
	}}
	a, _, done := prefetchApp(t, p)
	defer done()
	track := lyricTrack(t, a)
	id, _ := a.CreateLyricsSearch(track)
	cards, _ := a.SearchLyrics(id, "only", lyrics.Query{Title: "Song"})
	finishLyricsPrefetch(t, a, id)
	key := cards[0].ID
	preview, _ := a.FetchLyrics(id, key)
	if err := a.AcknowledgeLyricsPreview(id, key, preview.Version); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { _, err := a.RefreshLyrics(id, key); finished <- err }()
	<-started
	if err := a.AcknowledgeLyricsPreview(id, key, preview.Version); err == nil {
		t.Error("approved during refresh")
	}
	revision, _ := a.LyricsRevision(track)
	if _, err := a.AdoptLyrics(id, key, revision); err == nil {
		t.Error("adopted during refresh")
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}
