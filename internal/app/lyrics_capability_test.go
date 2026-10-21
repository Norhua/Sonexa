package app

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type singleLyrics struct {
	searches, fetches int
	document          *lyricdoc.Document
}

func (*singleLyrics) ID() provider.ProviderID { return "lyrics-only" }
func (*singleLyrics) DisplayName() string     { return "独立歌词" }
func (p *singleLyrics) SearchLyrics(_ context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	p.searches++
	return []lyrics.Candidate{{Ref: "opaque", Title: q.Title}}, nil
}
func (p *singleLyrics) FetchLyrics(_ context.Context, _ lyrics.Candidate) (*lyricdoc.Document, error) {
	p.fetches++
	if p.document != nil {
		return p.document, nil
	}
	return lyricdoc.ParseText("[00:01]单能力歌词")
}

var _ lyrics.Provider = (*singleLyrics)(nil)

func TestSingleLyricsProviderThroughRealBindings(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	delivered, err := lyricdoc.Build(&lyricdoc.Input{
		Original:     lyricdoc.TextChannel("[00:10]ordinary fallback", nil),
		WordOriginal: lyricdoc.TextChannel("[offset:500]\n[00:01]<00:01>Hello <00:01.2><00:01.5>world\n[00:01]Hello world", nil),
		Translation:  lyricdoc.TextChannel("[offset:-500]\n[00:00]译文", nil),
		Romanization: lyricdoc.FailedChannel(lyricdoc.ErrFormat),
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &singleLyrics{document: delivered}
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
	finishLyricsPrefetch(t, a, session)
	preview, err := a.FetchLyrics(session, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	doc := preview.Document
	if doc != delivered {
		t.Fatal("binding replaced upstream document")
	}
	score := lyrics.ScoreContent(rows[0], doc, 0)
	if score.TotalScore == nil || score.Terms[3].Value != 15 {
		t.Fatal(score)
	}
	if err := lyricdoc.Validate(delivered); err != nil {
		t.Fatal("preview/score changed content", err)
	}
	if err := a.AcknowledgeLyricsPreview(session, rows[0].ID, preview.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev); err != nil {
		t.Fatal(err)
	}
	if err := lyricdoc.Validate(delivered); err != nil {
		t.Fatal("adoption changed content", err)
	}
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatal(err)
	}
	saved, err := a.GetTrackDetail(id)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != doc.Text {
		t.Fatal(saved, err)
	}
	// A changed but individually parseable text must not be repaired/reparsed
	// into a new success at adoption. Reject and preserve the saved file.
	originalText := delivered.Text
	delivered.Text = "[00:01]replacement"
	if _, err := a.AdoptLyrics(session, rows[0].ID, rev+1); !errors.Is(err, lyricdoc.ErrContract) {
		t.Fatal(err)
	}
	after, err := a.GetTrackDetail(id)
	if err != nil || after.Metadata.Lyrics == nil || *after.Metadata.Lyrics != originalText {
		t.Fatal(after, err)
	}
	delivered.Text = originalText
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
