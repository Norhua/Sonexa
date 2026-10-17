package app

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

func sourceService(c *provider.Client) lyrics.Service {
	s, err := buildLyricsService(c, qqmusic.New(c, qqmusic.Config{}, nil))
	if err != nil {
		panic(err)
	}
	return *s
}
func replayService(body string) lyrics.Service {
	c := provider.NewClient(nil, nil, "test")
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		response := body
		if r.URL.Host == "u.y.qq.com" && !strings.Contains(body, "\"body\"") {
			response = `{"code":1}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	return sourceService(c)
}
func channelSummary(c *lyrics.Channel) string {
	if c == nil {
		return "unknown"
	}
	return fmt.Sprintf("%s bytes=%d", c.Status, len(c.Text))
}

func TestTextFormatsAndInvalidInput(t *testing.T) {
	for _, tt := range []struct{ text, format string }{
		{"无时间轴\n第二行", "text"}, {"\ufeff[ti:歌曲]\r\n[00:01.20][00:02.300]正文", "lrc"}, {"[01:02.345]<01:02.345>逐<01:02.500>字", "enhanced_lrc"}, {"1 < 2 普通文字", "text"},
	} {
		d, err := lyrics.ParseText(tt.text)
		if err != nil || d.Format != tt.format {
			t.Fatalf("%q: %+v %v", tt.text, d, err)
		}
	}
	for _, text := range []string{"", "[ti:歌名]\n[00:01.00]", "krc1binary", "a\x00b", string([]byte{0xff}), strings.Repeat("a", lyrics.MaxTextBytes+1)} {
		if _, err := lyrics.ParseText(text); err == nil {
			t.Fatalf("accepted invalid lyrics %q", text[:min(50, len(text))])
		}
	}
}
func TestDocumentParsers(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("[00:02.50]You &amp; me"))
	for _, tt := range []struct{ source, body, want string }{
		{"qq", `callback({"code":0,"retcode":0,"lyric":"` + encoded + `"});`, "[00:02.50]You & me"},
		{"qq", `{"code":0,"lyric":"[00:01.00]原文","trans":"[00:01.00]翻译"}`, "[00:01.000]原文\n[00:01.000]翻译"},
		{"163", `{"code":200,"lrc":{"lyric":"[00:03.00]三\n[00:01.20][00:02.300]一"},"tlyric":{"lyric":"[00:02.30]译\n[00:03.0]三"}}`, "[00:01.200]一\n[00:02.300]一\n[00:02.300]译\n[00:03.000]三"},
		{"kuwo", `{"status":200,"data":{"lrclist":[{"time":"12.50","lineLyric":"后"},{"time":"2.3","lineLyric":"前"}]}}`, "[00:02.300]前\n[00:12.500]后"},
		{"kuwo", `{"status":200,"data":{"lrclist":[{"time":"bad","lineLyric":"无时间"},{"time":"1","lineLyric":"有时间"}]}}`, "无时间\n有时间"},
	} {
		d, err := replayService(tt.body).Fetch(context.Background(), lyrics.Candidate{Source: tt.source, Ref: "native"})
		if err != nil || d.Text != tt.want {
			t.Fatalf("%s: %+v %v want %q", tt.source, d, err, tt.want)
		}
	}
	for _, tt := range []struct{ source, body string }{
		{"163", `{"code":200,"lrc":{"lyric":"[00:01.00]作曲 : 人\n[00:05.00]纯音乐，请欣赏"}}`},
		{"163", `{"code":301}`}, {"163", `{"code":200,"nolyric":true}`}, {"qq", `callback({"code":0,"lyric":""});alert(1)`}, {"qq", `{"code":1000}`}, {"qq", `{}`}, {"kuwo", `{"status":301,"msg":"音乐查询失败","data":null}`},
	} {
		if _, err := replayService(tt.body).Fetch(context.Background(), lyrics.Candidate{Source: tt.source, Ref: "native"}); err == nil {
			t.Fatalf("accepted %s", tt.body)
		}
	}
}
func TestSearchParsers(t *testing.T) {
	for _, tt := range []struct {
		source, body, ref string
		ms                int
	}{
		{"163", `{"code":200,"result":{"songs":[{"id":123,"name":"歌","ar":[{"name":"人"}],"al":{"name":"专辑"},"dt":123456}]}}`, "123", 123456},
		{"qq", `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[{"mid":"0039MnYb0qxYhV","name":"歌","singer":[{"name":"人"}],"album":{"name":"专辑"},"interval":123}]}}}}}`, "0039MnYb0qxYhV", 123000},
		{"kugou", `{"status":1,"errcode":0,"data":{"info":[{"hash":"abcd","songname":"歌","singername":"人","album_name":"专辑","duration":123}]}}`, "abcd", 123000},
		{"kuwo", `{"TOTAL":"1","abslist":[{"MUSICRID":"MUSIC_123","NAME":"歌","ARTIST":"人","ALBUM":"专辑","DURATION":"123"}]}`, "123", 123000},
	} {
		got, err := replayService(tt.body).Search(context.Background(), tt.source, lyrics.Query{Title: "歌"})
		if err != nil || len(got) != 1 || got[0].Ref != tt.ref || got[0].DurationMS != tt.ms || got[0].Source != tt.source {
			t.Fatalf("%s: %+v %v", tt.source, got, err)
		}
	}
	for _, source := range sourceService(nil).Sources.List() {
		if _, err := replayService(`{}`).Search(context.Background(), source.ID, lyrics.Query{Title: "song"}); err == nil {
			t.Fatalf("%s accepted malformed response", source.ID)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestKugouUsesHTTPSHashDurationAndLRC(t *testing.T) {
	client := provider.NewClient(nil, nil, "test")
	count := 0
	client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		count++
		if r.URL.Scheme != "https" || r.URL.Host != "lyrics.kugou.com" {
			t.Fatalf("unsafe URL %s", r.URL)
		}
		body := ""
		switch r.URL.Path {
		case "/search":
			if r.URL.Query().Get("duration") != "269000" || r.URL.Query().Get("hash") != "hash" {
				t.Fatal(r.URL)
			}
			body = `{"status":200,"candidates":[{"id":"short","accesskey":"a","duration":1000},{"id":"full","accesskey":"b","duration":269000}]}`
		case "/download":
			if r.URL.Query().Get("id") != "full" || r.URL.Query().Get("fmt") != "lrc" {
				t.Fatal(r.URL)
			}
			body = `{"status":200,"error_code":0,"fmt":"lrc","content":"` + base64.StdEncoding.EncodeToString([]byte("[00:01.00]歌词")) + `"}`
		default:
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	d, err := sourceService(client).Fetch(context.Background(), lyrics.Candidate{Source: "kugou", Ref: "hash", DurationMS: 269000})
	if err != nil || count != 2 || d.Format != "lrc" {
		t.Fatalf("%+v %v %d", d, err, count)
	}
}
func TestLyricsRequestGateAndResponseLimit(t *testing.T) {
	c := provider.NewClient(nil, nil, "test")
	calls := 0
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", (4<<20)+1))), Header: http.Header{}}, nil
	})
	s := sourceService(c)
	ctx := provider.WithRequestGate(context.Background(), func() error { return fmt.Errorf("disabled") })
	if _, err := s.Search(ctx, "163", lyrics.Query{Title: "歌"}); err == nil || calls != 0 {
		t.Fatal("gate bypassed")
	}
	if _, err := s.Search(context.Background(), "163", lyrics.Query{Title: "歌"}); err == nil || calls != 1 {
		t.Fatal("unbounded response")
	}
}

func TestLyricsRejectsCrossHostRedirect(t *testing.T) {
	c := provider.NewClient(nil, nil, "test")
	calls := 0
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://untrusted.example/lyric"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	_, err := sourceService(c).Search(context.Background(), "qq", lyrics.Query{Title: "歌"})
	if err == nil || calls != 1 {
		t.Fatalf("redirect followed: calls=%d err=%v", calls, err)
	}
}

// Opt-in manual verification; only logs titles, type and byte counts, not lyrics.
func TestLiveSources(t *testing.T) {
	requireLive(t, "SONEXA_LYRICS_LIVE", os.Getenv("SONEXA_LYRICS_LIVE"))
	a, audit := newLiveApp(t, 40)
	matched := false
	for _, source := range a.ListLyricsSources() {
		if only := os.Getenv("SONEXA_LYRICS_SOURCE"); only != "" && source.ID != only {
			continue
		}
		matched = true
		if !t.Run(source.ID, func(t *testing.T) {
			ctx, cancel := liveContext(a, audit, 2*time.Minute)
			defer cancel()
			title := os.Getenv("SONEXA_LYRICS_TITLE")
			if title == "" {
				title = "青花瓷"
			}
			artist := os.Getenv("SONEXA_LYRICS_ARTIST")
			if artist == "" {
				artist = "周杰伦"
			}
			duration, _ := strconv.Atoi(os.Getenv("SONEXA_LYRICS_DURATION_MS"))
			candidates, err := a.lyricSvc.Search(ctx, source.ID, lyrics.Query{Title: title, Artist: artist, Album: os.Getenv("SONEXA_LYRICS_ALBUM"), DurationMS: duration})
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) == 0 {
				t.Fatal("no songs")
			}
			t.Logf("songs=%d first_ref=%s duration=%d", len(candidates), candidates[0].Ref, candidates[0].DurationMS)
			for _, candidate := range candidates[:min(3, len(candidates))] {
				d, err := a.lyricSvc.Fetch(ctx, candidate)
				if err != nil {
					if audit.gate() != nil {
						t.Fatal(errLiveStopped)
					}
					t.Logf("candidate=%s unavailable error_type=%T", candidate.Ref, err)
					continue
				}
				if audit.gate() != nil {
					t.Fatal(errLiveStopped)
				}
				t.Logf("ref=%s format=%s bytes=%d original=%s translation=%s romanization=%s wordTiming=%s warnings=%d", candidate.Ref, d.Format, len(d.Text), channelSummary(d.Original), channelSummary(d.Translation), channelSummary(d.Romanization), d.WordTiming, len(d.Warnings))
				return
			}
			t.Fatal("no lyrics in first three candidates")
		}) {
			return
		}
	}
	if !matched {
		t.Fatal("unknown SONEXA_LYRICS_SOURCE")
	}
}
