package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestKuwoInferenceStaysInsideAdapter(t *testing.T) {
	list := []map[string]any{}
	var lrc strings.Builder
	for i := 0; i < 8; i++ {
		for j, text := range []string{fmt.Sprintf("Original verse %d", i), fmt.Sprintf("译文%d", i)} {
			second := i*2 + j + 1
			list = append(list, map[string]any{"time": second, "lineLyric": text})
			fmt.Fprintf(&lrc, "[00:%02d.000]%s\n", second, text)
		}
	}
	for _, source := range []string{"kuwo", "163"} {
		t.Run(source, func(t *testing.T) {
			a, done := newTestApp(t)
			defer done()
			a.enricher.Pause()
			client := provider.NewClient(nil, nil, "test")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				var response any
				if source == "kuwo" {
					if r.URL.Path == "/r.s" {
						response = map[string]any{"TOTAL": 1, "abslist": []map[string]any{{"MUSICRID": "MUSIC_123", "NAME": "Song", "ARTIST": "Artist"}}}
					} else {
						response = map[string]any{"code": 200, "data": map[string]any{"lrclist": list}}
					}
				} else if r.URL.Path == "/api/song/lyric/v1" {
					response = map[string]any{"code": 200, "lrc": map[string]string{"lyric": strings.TrimSuffix(lrc.String(), "\n")}}
				} else {
					response = map[string]any{"code": 200, "result": map[string]any{"songs": []map[string]any{{"id": 123, "name": "Song", "ar": []map[string]string{{"name": "Artist"}}}}}}
				}
				body, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})
			s := sourceService(client)
			a.lyricSvc = &s
			id := lyricTrack(t, a)
			session, err := a.CreateLyricsSearch(id)
			if err != nil {
				t.Fatal(err)
			}
			defer a.CancelLyricsSearch(session)
			candidates, err := a.SearchLyrics(session, source, lyrics.Query{Title: "Song", Artist: "Artist"})
			if err != nil || len(candidates) != 1 {
				t.Fatal(candidates, err)
			}
			d, err := a.FetchLyrics(session, candidates[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			score := lyrics.ScoreContent(candidates[0], d, 0)
			if source == "kuwo" {
				if d.Translation.Status != "available" || len(d.Original.Track.Lines) != 8 || score.Terms[3].Value != 15 {
					t.Fatal(d, score)
				}
				summary := channelSummary(d.Translation)
				if !strings.Contains(summary, "inference=kuwo-embedded-v1 layer=1 count=8") || strings.Contains(summary, "译文") || strings.Contains(summary, "sourceTimeMs") {
					t.Fatal("unsafe or incomplete summary", summary)
				}
			} else if len(d.Original.Track.Lines) != 16 || score.Terms[3].Value != 0 || d.Translation.Status == "available" {
				t.Fatal("other source ran Kuwo rule", d)
			}
			revision, _ := a.LyricsRevision(id)
			if _, err := a.AdoptLyrics(session, candidates[0].ID, revision); err != nil {
				t.Fatal(err)
			}
			saved, err := a.GetTrackDetail(id)
			if err != nil || saved.Metadata.Lyrics == nil || *saved.Metadata.Lyrics != d.Text {
				t.Fatal("adoption rewrote lyrics", err)
			}
			if err := lyricdoc.Validate(d); err != nil {
				t.Fatal("business changed document", err)
			}
		})
	}
}

func TestKuwoLyricsOutcomesStayDistinct(t *testing.T) {
	for _, tt := range []struct{ name, body, kind, status string }{
		{"unknown", `{"code":200,"status":200,"data":{"lrclist":[{"time":1,"lineLyric":"original"}]}}`, "", "unknown"},
		{"no lyrics", `{"code":200,"status":200,"data":{"lrclist":[]}}`, "no_lyrics", ""},
		{"business", `{"code":301,"status":301}`, "upstream", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, err := replayService(tt.body).Fetch(context.Background(), lyrics.Candidate{Source: "kuwo", Ref: "123"})
			if tt.kind == "" {
				if err != nil || d.Translation.Status != tt.status {
					t.Fatal(d, err)
				}
				return
			}
			failure, ok := err.(*lyrics.FetchError)
			if !ok || failure.Kind != tt.kind || d != nil {
				t.Fatal(d, err)
			}
		})
	}
}
