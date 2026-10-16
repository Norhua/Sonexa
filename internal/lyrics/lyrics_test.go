package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestTextFormatsAndInvalidInput(t *testing.T) {
	for _, tt := range []struct{ text, format string }{
		{"无时间轴\n第二行", "text"}, {"\ufeff[ti:歌曲]\r\n[00:01.20][00:02.300]正文", "lrc"}, {"[01:02.345]<01:02.345>逐<01:02.500>字", "enhanced_lrc"}, {"1 < 2 普通文字", "text"},
	} {
		d, err := ParseText(tt.text)
		if err != nil || d.Format != tt.format {
			t.Fatalf("%q: %+v %v", tt.text, d, err)
		}
	}
	for _, text := range []string{"", "[ti:歌名]\n[00:01.00]", "krc1binary", "a\x00b", string([]byte{0xff}), strings.Repeat("a", MaxTextBytes+1)} {
		if _, err := ParseText(text); err == nil {
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
		d, err := parseDocument(tt.source, []byte(tt.body))
		if err != nil || d.Text != tt.want {
			t.Fatalf("%s: %+v %v want %q", tt.source, d, err, tt.want)
		}
	}
	for _, tt := range []struct{ source, body string }{
		{"163", `{"code":200,"lrc":{"lyric":"[00:01.00]作曲 : 人\n[00:05.00]纯音乐，请欣赏"}}`},
		{"163", `{"code":301}`}, {"163", `{"code":200,"nolyric":true}`}, {"qq", `callback({"code":0,"lyric":""});alert(1)`}, {"qq", `{"code":1000}`}, {"qq", `{}`}, {"kuwo", `{"status":301,"msg":"音乐查询失败","data":null}`},
	} {
		if _, err := parseDocument(tt.source, []byte(tt.body)); err == nil {
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
		got, err := parseSearch(tt.source, []byte(tt.body), Query{Title: "歌"})
		if err != nil || len(got) != 1 || got[0].Ref != tt.ref || got[0].DurationMS != tt.ms || got[0].Source != tt.source {
			t.Fatalf("%s: %+v %v", tt.source, got, err)
		}
	}
	for _, source := range Sources() {
		if _, err := parseSearch(source.ID, []byte(`{}`), Query{}); err == nil {
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
	d, err := (Service{client}).Fetch(context.Background(), Candidate{Source: "kugou", Ref: "hash", DurationMS: 269000})
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
	s := Service{c}
	ctx := provider.WithRequestGate(context.Background(), func() error { return fmt.Errorf("disabled") })
	if _, err := s.Search(ctx, "163", Query{Title: "歌"}); err == nil || calls != 0 {
		t.Fatal("gate bypassed")
	}
	if _, err := s.Search(context.Background(), "163", Query{Title: "歌"}); err == nil || calls != 1 {
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
	_, err := (Service{c}).Search(context.Background(), "qq", Query{Title: "歌"})
	if err == nil || calls != 1 {
		t.Fatalf("redirect followed: calls=%d err=%v", calls, err)
	}
}

// Opt-in manual verification; only logs titles, type and byte counts, not lyrics.
func TestLiveSources(t *testing.T) {
	if os.Getenv("SONEXA_LYRICS_LIVE") != "1" {
		t.Skip("set SONEXA_LYRICS_LIVE=1 for upstream probes")
	}
	c := provider.NewClient(provider.NewLimiters(), nil, "Sonexa/lyrics-verification (https://github.com/norhua/sonexa)")
	s := Service{c}
	// Only probe a caller-selected source when troubleshooting unstable APIs.
	for _, source := range Sources() {
		if only := os.Getenv("SONEXA_LYRICS_SOURCE"); only != "" && source.ID != only {
			continue
		}
		t.Run(source.ID, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*60*1e9)
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
			candidates, err := s.Search(ctx, source.ID, Query{Title: title, Artist: artist, Album: os.Getenv("SONEXA_LYRICS_ALBUM"), DurationMS: duration})
			if err != nil {
				t.Fatal(err)
			}
			if len(candidates) == 0 {
				t.Fatal("no songs")
			}
			t.Logf("songs=%d first=%s ref=%s duration=%d", len(candidates), candidates[0].Title, candidates[0].Ref, candidates[0].DurationMS)
			for _, candidate := range candidates[:min(3, len(candidates))] {
				d, err := s.Fetch(ctx, candidate)
				if err != nil {
					t.Log(err)
					continue
				}
				t.Logf("lyrics candidate=%s ref=%s format=%s bytes=%d translation=%+v romanization=%s wordTiming=%s warnings=%v", candidate.Title, candidate.Ref, d.Format, len(d.Text), channelSummary(d.Translation), channelSummary(d.Romanization), d.WordTiming, d.Warnings)
				return
			}
			b, _ := json.Marshal(candidates[:min(3, len(candidates))])
			t.Fatalf("no lyrics in first three: %s", b)
		})
	}
}
