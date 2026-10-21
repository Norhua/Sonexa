package app

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

func lyricTrack(t *testing.T, a *App) int64 {
	t.Helper()
	id, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "歌", Credit: strPtrOrNil("人")})
	if err != nil {
		t.Fatal(err)
	}
	return id
}
func TestLyricsPickerClearAndConflict(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	first, second := lyricTrack(t, a), lyricTrack(t, a)
	events := 0
	a.emit = func(name string, _ ...any) {
		if name == EventLibraryChanged {
			events++
		}
	}
	file := filepath.Join(t.TempDir(), "lyrics.lrc")
	text := "\ufeff[00:01.00]<00:01.00>逐<00:02.00>字  \r\n\r\n  1 < 2 (旁白) 😀　"
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	a.pickLyricsFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) { return file, nil }
	rev, _ := a.LyricsRevision(first)
	result, err := a.PickAndAdoptLyrics(first, rev)
	if err != nil || result.Cancelled || result.Revision != rev+1 {
		t.Fatalf("%+v %v", result, err)
	}
	d, _ := a.GetTrackDetail(first)
	if d.Metadata.Lyrics == nil || *d.Metadata.Lyrics != strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff") {
		t.Fatalf("lyrics lost: %+v", d.Metadata)
	}
	other, _ := a.GetTrackDetail(second)
	if other.Metadata.Lyrics != nil {
		t.Fatal("modified another song")
	}
	if _, err := a.ClearLyrics(first, rev); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatalf("stale clear: %v", err)
	}
	if _, err := a.ClearLyrics(first, result.Revision); err != nil {
		t.Fatal(err)
	}
	d, _ = a.GetTrackDetail(first)
	if d.Metadata.Lyrics != nil {
		t.Fatal("lyrics not cleared")
	}
	if events != 2 {
		t.Fatalf("notifications=%d", events)
	}
	a.pickLyricsFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) { return "", nil }
	rev, _ = a.LyricsRevision(first)
	result, err = a.PickAndAdoptLyrics(first, rev)
	if err != nil || !result.Cancelled || events != 2 {
		t.Fatalf("cancel: %+v %v", result, err)
	}
	if after, _ := a.LyricsRevision(first); after != rev {
		t.Fatal("cancel changed revision")
	}
}
func TestLyricsPickerRejectsBadFilesWithoutChangingCurrent(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	id := lyricTrack(t, a)
	file := filepath.Join(t.TempDir(), "bad.lrc")
	a.pickLyricsFile = func(context.Context, wruntime.OpenDialogOptions) (string, error) { return file, nil }
	for _, body := range [][]byte{[]byte("[ti:only metadata]"), {0xff, 0xfe, 0x00}, []byte(strings.Repeat("a", lyricdoc.MaxTextBytes+1)), []byte("krc1binary")} {
		if err := os.WriteFile(file, body, 0600); err != nil {
			t.Fatal(err)
		}
		rev, _ := a.LyricsRevision(id)
		if _, err := a.PickAndAdoptLyrics(id, rev); err == nil {
			t.Fatal("accepted bad file")
		}
		after, _ := a.LyricsRevision(id)
		if rev != after {
			t.Fatal("bad file wrote current")
		}
	}
}
func TestLyricsSearchSessionOwnershipGateAndCancellation(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	id := lyricTrack(t, a)
	a.limiters.Configure(provider.QQMusic, 1000, time.Second)
	calls := 0
	a.client.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"0039MnYb0qxYhV","name":"歌","interval":123}]}}}}}`
		requestBody, _ := io.ReadAll(r.Body)
		if strings.Contains(string(requestBody), "GetPlayLyricInfo") {
			body = `{"code":0,"req":{"code":0,"data":{"qrc":0,"lyric":"` + base64.StdEncoding.EncodeToString([]byte("[00:01.00]<00:01.00>网络<00:02.00>歌词")) + `","trans":"` + base64.StdEncoding.EncodeToString([]byte("[00:01.00]测试翻译")) + `"}},"plain":{"code":0,"data":{"lyric":""}}}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	session, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(session)
	cards, err := a.SearchLyrics(session, "qq", lyrics.Query{Title: "歌"})
	if err != nil || len(cards) != 1 {
		t.Fatalf("%+v %v", cards, err)
	}
	rev, _ := a.LyricsRevision(id)
	if _, err := a.AdoptLyrics(session, cards[0].ID, rev); err == nil {
		t.Fatal("adopt without preview")
	}
	if _, err := a.FetchLyrics(session, "forged"); err == nil {
		t.Fatal("forged candidate")
	}
	finishLyricsPrefetch(t, a, session)
	preview, err := a.FetchLyrics(session, cards[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	d := preview.Document
	if d.Format != "xlrc" {
		t.Fatalf("%+v %v", d, err)
	}
	other, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	defer a.CancelLyricsSearch(other)
	if _, err := a.AdoptLyrics(other, cards[0].ID, rev); err == nil {
		t.Fatal("adopt across sessions")
	}
	if err := a.AcknowledgeLyricsPreview(session, cards[0].ID, preview.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptLyrics(session, cards[0].ID, rev); err != nil {
		t.Fatal(err)
	}
	saved, err := a.GetTrackDetail(id)
	if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != d.Text || !strings.Contains(*saved.Metadata.Lyrics, "测试翻译") || !strings.Contains(*saved.Metadata.Lyrics, "<00:02.000>") {
		t.Fatalf("adoption lost translation or word times: %+v %v", saved, err)
	}
	if _, err := a.AdoptLyrics(session, cards[0].ID, rev); !errors.Is(err, store.ErrRevisionConflict) {
		t.Fatalf("stale adopt: %v", err)
	}
	a.CancelLyricsSearch(session)
	if _, err := a.FetchLyrics(session, cards[0].ID); err == nil {
		t.Fatal("cancelled session accepted")
	}
	cur := a.settings.Get()
	cur.MusicBrainz.Contact = ""
	if err := a.settings.Save(cur); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateLyricsSearch(id); err == nil {
		t.Fatal("gate bypassed")
	}
	if _, err := a.SearchLyrics(other, "qq", lyrics.Query{Title: "歌"}); err == nil {
		t.Fatal("gate not rechecked")
	}
	if calls != 2 {
		t.Fatalf("unexpected requests %d", calls)
	}
}
func TestCancelLyricsStopsInflightRequest(t *testing.T) {
	a, done := newTestApp(t)
	defer done()
	a.enricher.Pause()
	id := lyricTrack(t, a)
	started := make(chan struct{})
	a.client.HTTP.Transport = coverReplayTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	session, err := a.CreateLyricsSearch(id)
	if err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := a.SearchLyrics(session, "qq", lyrics.Query{Title: "歌"}); result <- err }()
	<-started
	a.CancelLyricsSearch(session)
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request was not cancelled")
	}
}
