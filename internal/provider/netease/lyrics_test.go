package netease

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func lyricResponse(t *testing.T, original, translation, romanization string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"code": 200, "lrc": map[string]string{"lyric": original},
		"tlyric": map[string]string{"lyric": translation}, "romalrc": map[string]string{"lyric": romanization},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestYRCBundleGETAndBoundedFallback(t *testing.T) {
	for _, tt := range []struct {
		name, body    string
		status, calls int
	}{
		{"success", string(lyricResponse(t, "[00:01]original", "translation", "roma")), 200, 1},
		{"missing", `{"code":200,"nolyric":true}`, 200, 2},
		{"bad format", `not json`, 200, 2},
		{"endpoint missing", ``, 404, 2},
		{"auth", ``, 403, 1},
		{"rate", ``, 429, 1},
		{"business", `{"code":301}`, 200, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls, forms := 0, 0
			client := provider.NewClient(nil, nil, "test")
			p := New(client)
			p.form = func([]byte) (url.Values, error) { forms++; return url.Values{"params": {"synthetic"}}, nil }
			client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != "music.163.com" || r.Header.Get("Referer") != "https://music.163.com/" {
					t.Fatal(r.URL)
				}
				status, body := tt.status, tt.body
				if calls == 1 {
					if r.Method != "GET" || r.URL.Path != lyricV1Path || r.URL.Query().Get("id") != "123" || forms != 0 {
						t.Fatal(r.URL)
					}
					for _, key := range []string{"lv", "tv", "rv", "yv", "ytv", "yrv"} {
						if r.URL.Query().Get(key) != "-1" {
							t.Fatal(key)
						}
					}
				} else {
					if calls > 2 || r.URL.Path != "/weapi/song/lyric" || r.Method != "POST" {
						t.Fatal(r.URL)
					}
					status, body = 200, string(lyricResponse(t, "[00:01]fallback", "", ""))
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			doc, err := p.FetchLyrics(context.Background(), lyrics.Candidate{Ref: "123"})
			if err == nil {
				if err := lyricdoc.Validate(doc); err != nil {
					t.Fatal(err)
				}
			}
			if calls != tt.calls || forms != tt.calls-1 {
				t.Fatal(calls, forms, err)
			}
			if (tt.name == "auth" || tt.name == "rate" || tt.name == "business") != (err != nil) {
				t.Fatal(err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = protocolTransport(func(*http.Request) (*http.Response, error) { t.Fatal("sent after cancellation"); return nil, nil })
	if _, err := New(client).FetchLyrics(ctx, lyrics.Candidate{Ref: "123"}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestYRCBundleNormalization(t *testing.T) {
	for _, tt := range []struct {
		name, yrc, lrc, format, timing string
		fail                           bool
	}{
		{"yrc only", `[1000,2000](1000,300,0)Hello (1500,500,0)world`, "", "enhanced_lrc", "available", false},
		{"missing", "", "[00:01]ordinary", "lrc", "unavailable", false},
		{"broken", "[0,10](0,20,0)bad", "[00:01]ordinary", "lrc", "error", false},
		{"overflow", "[9999999999999999999999,10](0,20,0)bad", "[00:01]ordinary", "lrc", "error", false},
		{"credit", "{\"t\":0,\"c\":[{\"tx\":\"Credit: \"},{\"tx\":\"Person\"}]}\n[1000,2000](1000,300,0)Hello (1500,500,0)world", "", "enhanced_lrc", "available", false},
		{"invalid only", "[0,10](0,20,0)bad", "", "", "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{"code": 200, "yrc": map[string]string{"lyric": tt.yrc}, "lrc": map[string]string{"lyric": tt.lrc}, "tlyric": map[string]string{"lyric": "[00:05]translation"}, "romalrc": map[string]string{"lyric": "romanization"}})
			p, err := parseLyrics(body)
			if err != nil {
				t.Fatal(err)
			}
			d, err := lyricdoc.Build(p)
			if tt.fail {
				if !errors.Is(err, lyricdoc.ErrFormat) {
					t.Fatal(err)
				}
				return
			}
			if err != nil || d.Format != "xlrc" || d.WordTiming != tt.timing || d.Translation.Status != "available" || d.Romanization.SyncStatus != "unsynced" {
				t.Fatal(d, err)
			}
			if strings.Contains(d.Text, `"tx"`) || tt.name == "credit" && !strings.Contains(d.Text, "Credit: Person") {
				t.Fatal(d.Text)
			}
			if tt.yrc != "" && !tt.fail && tt.timing == "available" && !strings.Contains(d.Text, "<00:01.300><00:01.500>") {
				t.Fatal(d.Text)
			}
		})
	}
}

func TestYRCContentScoreUsesConfirmedChannels(t *testing.T) {
	body := []byte(`{"code":200,"yrc":{"lyric":"[1000,2000](1000,300,0)Hello (1500,500,0)world"},"tlyric":{"lyric":"translation"},"romalrc":{"lyric":"roma"}}`)
	p, err := parseLyrics(body)
	if err != nil {
		t.Fatal(err)
	}
	d, err := lyricdoc.Build(p)
	if err != nil {
		t.Fatal(err)
	}
	s := lyrics.ScoreContent(lyrics.Candidate{MatchScore: 90, HighConfidence: true}, d, 5000)
	if s.TotalScore == nil || *s.TotalScore != 140 || !s.AutoPreviewEligible {
		t.Fatal(s)
	}
	if d.Translation.SyncStatus != "unsynced" || d.Romanization.SyncStatus != "unsynced" {
		t.Fatal(d)
	}
}

func TestLyricsNormalizeNativePlaceholders(t *testing.T) {
	for _, text := range []string{
		"[00:01.00]作曲 : 人\n[00:05.00]纯音乐，请欣赏",
		"[00:01]纯音乐，请您欣赏！", "[00:01]此歌曲为没有填词的纯音乐，请您欣赏",
		"暂无歌词", "[00:01]此歌曲暂无歌词", "[00:01]<00:01>纯音乐，请欣赏",
	} {
		if payload, err := parseLyrics(lyricResponse(t, text, "translation", "roma")); !errors.Is(err, lyricdoc.ErrNoLyrics) || payload != nil {
			t.Fatalf("未归一化占位 %q: %+v %v", text, payload, err)
		}
	}
	payload, err := parseLyrics(lyricResponse(t, "[00:01]原文", "[00:01]暂无歌词", "纯音乐，请欣赏"))
	if err != nil || payload.Original.Status != "available" || payload.Translation.Status != "unavailable" || payload.Romanization.Status != "unavailable" {
		t.Fatalf("附加通道占位影响原文: %+v %v", payload, err)
	}
	doc, err := lyricdoc.Build(payload)
	if err != nil || doc.Text != "[xlrc:0.1]\n[x-sonexa:1]\n[00:01.000]原文" {
		t.Fatal(doc, err)
	}
}

func TestLyricsKeepTextAndValidationErrors(t *testing.T) {
	payload, err := parseLyrics(lyricResponse(t, "[00:01]原文", "[00:01]翻译", "[00:01]roma"))
	if err != nil || payload.Translation.Text != "[00:01]翻译" || payload.Romanization.Status != "available" {
		t.Fatal(payload, err)
	}
	for _, text := range []string{"[00:01]纯音乐，请欣赏\n" + strings.Repeat("x", lyricdoc.MaxTextBytes), "暂无歌词\x00"} {
		if ch := lyricChannel(text); ch.Status != "error" {
			t.Fatalf("占位不能掩盖格式或大小错误: %+v", ch)
		}
	}
}
