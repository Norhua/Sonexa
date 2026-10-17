package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type singleLyrics struct{ searches, fetches int }

func (*singleLyrics) ID() provider.ProviderID { return "lyrics-only" }
func (*singleLyrics) DisplayName() string     { return "独立歌词" }
func (p *singleLyrics) SearchLyrics(_ context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	p.searches++
	return []lyrics.Candidate{{Ref: "opaque", Title: q.Title}}, nil
}
func (p *singleLyrics) FetchLyrics(_ context.Context, _ lyrics.Candidate) (*lyrics.Payload, error) {
	p.fetches++
	return &lyrics.Payload{Text: "[00:01]单能力歌词"}, nil
}

var _ lyrics.Provider = (*singleLyrics)(nil)

func TestSingleLyricsProviderThroughRealBindings(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	p := &singleLyrics{}
	r, err := lyrics.NewRegistry(lyrics.Entry{Provider: p})
	if err != nil {
		t.Fatal(err)
	}
	a.lyricSvc = &lyrics.Service{Sources: r}
	if !reflect.DeepEqual(a.ListLyricsSources(), []lyrics.Source{{ID: "lyrics-only", Name: "独立歌词"}}) {
		t.Fatal(a.ListLyricsSources())
	}
	if _, err := a.registry.Get(p.ID()); err == nil {
		t.Fatal("single capability entered metadata registry")
	}
	id := lyricTrack(t, a)
	session, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(session)
	if _, err := a.SearchLyrics(session, "cover-only", lyrics.Query{Title: "song"}); err == nil || p.searches != 0 {
		t.Fatal("unknown capability dispatched")
	}
	rows, err := a.SearchLyrics(session, "lyrics-only", lyrics.Query{Title: "song"})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	rev, _ := a.LyricsRevision(id)
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev); err == nil {
		t.Fatal("adopt without preview")
	}
	if _, err := a.FetchLyrics(session, "forged"); err == nil {
		t.Fatal("forged candidate accepted")
	}
	doc, err := a.FetchLyrics(session, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatal(err)
	}
	saved, err := a.GetTrackDetail(id)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != doc.Text {
		t.Fatal(saved, err)
	}
	// Replacing the current service does not replace an existing session snapshot.
	a.lyricSvc = nil
	if _, err := a.FetchLyrics(session, rows[0].ID); err != nil || p.fetches != 1 {
		t.Fatal("session lost cached service/document", err)
	}
	a.lyricSessions[session].created = time.Now().Add(-31 * time.Minute)
	if _, err := a.FetchLyrics(session, rows[0].ID); err == nil {
		t.Fatal("expired session accepted")
	}
	if len(a.ListLyricsSources()) != 0 {
		t.Fatal("uninitialized static fallback")
	}
}
