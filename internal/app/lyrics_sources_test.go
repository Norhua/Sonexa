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

	"github.com/norhua/sonexa/internal/lyricdoc"
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
			response = `{}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	return sourceService(c)
}
func channelSummary(c *lyricdoc.Channel) string {
	if c == nil {
		return "unknown"
	}
	summary := fmt.Sprintf("%s origin=%s sync=%s bytes=%d", c.Status, c.Origin, c.SyncStatus, len(c.Text))
	for _, diagnostic := range c.Diagnostics {
		if basis := diagnostic.Inference; basis != nil {
			summary += fmt.Sprintf(" inference=%s layer=%d count=%d", basis.RuleVersion, basis.Layer, basis.Count)
		}
	}
	return summary
}

func TestTextFormatsAndInvalidInput(t *testing.T) {
	for _, tt := range []struct{ text, format string }{
		{"无时间轴\n第二行", "text"}, {"\ufeff[ti:歌曲]\r\n[00:01.20][00:02.300]正文", "lrc"}, {"[01:02.345]<01:02.345>逐<01:02.500>字", "enhanced_lrc"}, {"1 < 2 普通文字", "text"},
	} {
		d, err := lyricdoc.ParseText(tt.text)
		if err != nil || d.Format != tt.format {
			t.Fatalf("%q: %+v %v", tt.text, d, err)
		}
	}
	for _, text := range []string{"", "[ti:歌名]\n[00:01.00]", "krc1binary", "a\x00b", string([]byte{0xff}), strings.Repeat("a", lyricdoc.MaxTextBytes+1)} {
		if _, err := lyricdoc.ParseText(text); err == nil {
			t.Fatalf("accepted invalid lyrics %q", text[:min(50, len(text))])
		}
	}
}
func TestDocumentParsers(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("[00:02.50]You &amp; me"))
	for _, tt := range []struct{ source, body, want string }{
		{"qq", `callback({"code":0,"retcode":0,"lyric":"` + encoded + `"});`, "[xlrc:0.1]\n[x-sonexa:1]\n[00:02.500]You & me"},
		{"qq", `{"code":0,"lyric":"[00:01.00]原文","trans":"[00:01.00]翻译"}`, "[xlrc:0.1]\n[x-sonexa:1]\n[00:01.000]原文\n[>und]翻译"},
		{"163", `{"code":200,"lrc":{"lyric":"[00:03.00]三\n[00:01.20][00:02.300]一"},"tlyric":{"lyric":"[00:02.30]译\n[00:03.0]三"}}`, "[xlrc:0.1]\n[x-sonexa:1]\n[00:01.200]一\n[00:02.300]一\n[>und]译\n[00:03.000]三\n[>und]三"},
	} {
		d, err := replayService(tt.body).Fetch(context.Background(), lyrics.Candidate{Source: tt.source, Ref: "native"})
		if err != nil || d.Text != tt.want {
			t.Fatalf("%s: %+v %v want %q", tt.source, d, err, tt.want)
		}
	}
	for _, tt := range []struct{ source, body string }{
		{"163", `{"code":200,"lrc":{"lyric":"[00:01.00]作曲 : 人\n[00:05.00]纯音乐，请欣赏"}}`},
		{"163", `{"code":301}`}, {"163", `{"code":200,"nolyric":true}`}, {"qq", `callback({"code":0,"lyric":""});alert(1)`}, {"qq", `{"code":1000}`}, {"qq", `{}`},
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
			if r.URL.Query().Get("id") != "full" {
				t.Fatal(r.URL)
			}
			if r.URL.Query().Get("fmt") == "krc" {
				body = `{"status":200,"error_code":0,"fmt":"krc","content":""}`
			} else {
				if r.URL.Query().Get("fmt") != "lrc" {
					t.Fatal(r.URL)
				}
				body = `{"status":200,"error_code":0,"fmt":"lrc","content":"` + base64.StdEncoding.EncodeToString([]byte("[00:01.00]歌词")) + `"}`
			}
		default:
			t.Fatal(r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	d, err := sourceService(client).Fetch(context.Background(), lyrics.Candidate{Source: "kugou", Ref: "hash", DurationMS: 269000})
	if err != nil || count != 3 || d.Format != "xlrc" {
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

// runLiveLyricsRound uses the actual App session/prefetch path. Waiting for
// registered work is bounded by the production 60-second round; no polling,
// second search, explicit fetch of a fourth item, or fresh send budget occurs.
func runLiveLyricsRound(ctx context.Context, a *App, trackID int64, source string, query lyrics.Query) (id string, snapshot *LyricsProgress, err error) {
	id, err = a.CreateLyricsSearch(trackID)
	if err != nil {
		return
	}
	if _, err = a.SearchLyrics(id, source, query); err != nil {
		return
	}
	if _, err = a.StartLyricsPrefetch(id); err != nil {
		return
	}
	done := make(chan struct{})
	go func() { a.lyricWorkers.Wait(); close(done) }()
	select {
	case <-ctx.Done():
		a.CancelLyricsSearch(id)
		<-done
		err = ctx.Err()
		return
	case <-done:
	}
	snapshot, err = a.LyricsSearchSnapshot(id)
	return
}

// Opt-in manual verification; only logs identity metadata, states, scores and
// byte counts. A failure stops the entire authorized run, not just a provider.
func TestLiveSources(t *testing.T) {
	requireLive(t, "SONEXA_LYRICS_LIVE", os.Getenv("SONEXA_LYRICS_LIVE"))
	a, audit := newLiveApp(t, 40)
	defer func() { t.Logf("total_actual_sends=%d budget=40", len(audit.snapshot())) }()
	matched := false
	for _, source := range a.ListLyricsSources() {
		if only := os.Getenv("SONEXA_LYRICS_SOURCE"); only != "" && source.ID != only {
			continue
		}
		matched = true
		if !t.Run(source.ID, func(t *testing.T) {
			ctx, cancel := liveContext(a, audit, 65*time.Second)
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
			query := lyrics.Query{Title: title, Artist: artist, Album: os.Getenv("SONEXA_LYRICS_ALBUM"), DurationMS: duration}
			before := len(audit.snapshot())
			id, snapshot, err := runLiveLyricsRound(ctx, a, lyricTrack(t, a), source.ID, query)
			defer a.CancelLyricsSearch(id)
			if err != nil {
				t.Fatalf("round_failed error_kind=%s error_type=%T sends=%d", lyricFailureKind(err), err, len(audit.snapshot())-before)
			}
			t.Logf("phase=%s searched=%d selected=%d success=%d no_lyrics=%d failed=%d incomplete=%d sends=%d", snapshot.Phase, snapshot.Searched, snapshot.Selected, snapshot.Success, snapshot.NoLyrics, snapshot.Failed, snapshot.Incomplete, len(audit.snapshot())-before)
			for index, item := range snapshot.Candidates {
				if !item.Prefetch {
					continue
				}
				candidate := item.Candidate
				t.Logf("candidate=%d title=%q artist=%q album=%q duration=%d match=%d match_terms=%+v high_confidence=%t state=%s error_kind=%s stop_reason=%s", index+1, candidate.Title, candidate.Artist, candidate.Album, candidate.DurationMS, candidate.MatchScore, candidate.MatchTerms, candidate.HighConfidence, item.FetchState, item.ErrorKind, item.StopReason)
				if candidate.TotalScore != nil {
					t.Logf("candidate=%d total=%d terms=%+v auto_preview=%t version=%d", index+1, *candidate.TotalScore, candidate.ScoreTerms, candidate.AutoPreviewEligible, item.DocumentVersion)
				}
				if item.FetchState != "ready" {
					continue
				}
				// Fetch now reads the exact retained document, with no extra send.
				preview, err := a.FetchLyrics(id, candidate.ID)
				if err != nil {
					t.Fatalf("preview_failed error_kind=%s error_type=%T", lyricFailureKind(err), err)
				}
				d := preview.Document
				t.Logf("candidate=%d format=%s bytes=%d retained_bytes=%d original=%s translation=%s romanization=%s wordTiming=%s warnings=%d", index+1, d.Format, len(d.Text), lyricdoc.RetainedBytes(d), channelSummary(d.Original), channelSummary(d.Translation), channelSummary(d.Romanization), d.WordTiming, len(d.Warnings))
			}
			if audit.gate() != nil || snapshot.Phase == "stopped" {
				t.Fatal(errLiveStopped)
			}
			if snapshot.Success == 0 {
				t.Fatal("no usable lyrics in frozen Top 3; no replacement search")
			}
		}) {
			return
		}
	}
	if !matched {
		t.Fatal("unknown SONEXA_LYRICS_SOURCE")
	}
}

func TestLiveLyricsRoundUsesFrozenTopThree(t *testing.T) {
	p := &prefetchSource{id: "only", count: 5}
	a, _, done := prefetchApp(t, p)
	defer done()
	id, snapshot, err := runLiveLyricsRound(context.Background(), a, lyricTrack(t, a), "only", lyrics.Query{Title: "Song"})
	defer a.CancelLyricsSearch(id)
	if err != nil || snapshot.Selected != 3 || snapshot.Success != 3 || len(p.calls()) != 3 {
		t.Fatal("live entry skipped or expanded frozen Top 3", err)
	}
	for _, item := range snapshot.Candidates[:3] {
		if _, err := a.FetchLyrics(id, item.Candidate.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.calls()) != 3 {
		t.Fatal("reading live summaries refetched")
	}
}
