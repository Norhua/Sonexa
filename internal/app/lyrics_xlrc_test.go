package app

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func TestXLRCFileInputRoundTripAndRevision(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	id := lyricTrack(t, a)
	file := filepath.Join(t.TempDir(), "lyrics.xlrc")
	text := "[lang:ja]\n[00:01][v:A]私[わたし]は\n[>en]I am\n[00:01][v:B]君[きみ]は\n[>en]You are"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	a.pickLyricsFile = func(_ context.Context, options wruntime.OpenDialogOptions) (string, error) {
		if !strings.Contains(options.Filters[0].Pattern, "*.xlrc") {
			t.Fatal(options)
		}
		return file, nil
	}
	rev, _ := a.LyricsRevision(id)
	result, err := a.PickAndAdoptLyrics(id, rev)
	if err != nil || result.Revision != rev+1 {
		t.Fatal(result, err)
	}
	saved, err := a.GetTrackDetail(id)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != text {
		t.Fatal(saved, err)
	}
	d, err := lyricdoc.ParseXLRC(*saved.Metadata.Lyrics)
	if err != nil || len(d.Rows) != 2 || d.Rows[1].Voice != "B" || d.Rows[1].Translation[0] != "You are" {
		t.Fatal(d, err)
	}
	if _, err = a.PickAndAdoptLyrics(id, rev); err == nil {
		t.Fatal("stale adoption accepted")
	}
}

func TestXLRCProviderPreviewScoreAdoptionAndReparse(t *testing.T) {
	d, err := lyricdoc.Build(&lyricdoc.Input{Text: "[xlrc:0.1]\n[lang:ja]\n[00:01][v:A]私[わたし]\n[>en]I\n[>ja-Latn]watashi\n[00:01][v:B]君[きみ]\n[>en]you"})
	if err != nil {
		t.Fatal(err)
	}
	source := &prefetchSource{id: "xlrc-test", count: 1, fetch: func(context.Context, lyrics.Candidate) (*lyricdoc.Document, error) { return d, nil }}
	a, _, done := prefetchApp(t, source)
	defer done()
	id := lyricTrack(t, a)
	session, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(session)
	cards, err := a.SearchLyrics(session, "xlrc-test", lyrics.Query{Title: "Song"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := finishLyricsPrefetch(t, a, session)
	if snapshot.Candidates[0].Candidate.ScoreTerms[3].Value != 15 || snapshot.Candidates[0].Candidate.ScoreTerms[4].Value != 5 {
		t.Fatal(snapshot)
	}
	rev, _ := a.LyricsRevision(id)
	if _, err = a.AdoptLyrics(session, cards[0].ID, rev); err == nil {
		t.Fatal("prefetch granted approval")
	}
	preview, err := a.FetchLyrics(session, cards[0].ID)
	if err != nil || preview.Document != d {
		t.Fatal(preview, err)
	}
	if err = a.AcknowledgeLyricsPreview(session, cards[0].ID, preview.Version); err != nil {
		t.Fatal(err)
	}
	if _, err = a.AdoptLyrics(session, cards[0].ID, rev); err != nil {
		t.Fatal(err)
	}
	saved, err := a.GetTrackDetail(id)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != d.Text {
		t.Fatal(saved, err)
	}
	round, err := lyricdoc.ParseXLRC(*saved.Metadata.Lyrics)
	if err != nil || !reflect.DeepEqual(round.Rows, d.Rows) {
		t.Fatal(round, err)
	}
	if err = lyricdoc.Validate(d); err != nil {
		t.Fatal(err)
	}
}
