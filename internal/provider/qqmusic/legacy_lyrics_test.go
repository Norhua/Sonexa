package qqmusic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestLegacyLyricsDecodeAndPreserveMetadataRaw(t *testing.T) {
	for _, raw := range []string{"[00:01.00]a &amp; b", base64.StdEncoding.EncodeToString([]byte("[00:01.00]a &amp; b"))} {
		body, _ := json.Marshal(map[string]any{"code": 0, "retcode": 0, "subcode": 0, "lyric": raw, "trans": "[00:01.00]译文"})
		for _, wrapped := range [][]byte{body, []byte("MusicJsonCallback(" + string(body) + ");")} {
			parsed, err := parseLegacyLyrics(wrapped)
			if err != nil || parsed.Original != "[00:01.00]a & b" || parsed.Translation != "[00:01.00]译文" {
				t.Fatalf("parsed=%+v err=%v", parsed, err)
			}
			text, err := ParseLyric(wrapped)
			if err != nil || text == nil || *text != raw {
				t.Fatal("元数据原文被手动歌词解码规则改写", text, err)
			}
		}
	}
	for _, body := range []string{`{}`, `{"code":1}`, `{"code":0,"retcode":1}`, `{"code":0,"subcode":1}`, `invalid`} {
		if _, err := parseLegacyLyrics([]byte(body)); err == nil {
			t.Fatal("错误状态被接受", body)
		}
	}
	parsed, err := parseLegacyLyrics([]byte(`{"code":0,"lyric":"[00:01.00]ok","trans":"/w=="}`))
	if err != nil || parsed.OriginalError != nil || parsed.TranslationError == nil {
		t.Fatal("附加通道错误丢失", parsed, err)
	}
}

func TestMetadataLyricsFallbackPolicy(t *testing.T) {
	for _, tc := range []struct {
		name                string
		track, remove, fail bool
		wantFallback        int
	}{
		{"tracklist-only", false, true, false, 0}, {"detail-first", true, false, false, 0}, {"fallback", true, true, false, 1}, {"failure-is-partial", true, true, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := provider.NewClient(nil, nil, "test")
			client.Retry.MaxAttempts = 1
			fallbacks := 0
			body := string(fixture(t, "detail_world.json"))
			if tc.remove {
				body = strings.Replace(body, `"title":"歌词"`, `"title":"无关"`, 1)
				body = strings.Replace(body, `"type":"lyric"`, `"type":"x"`, 1)
			}
			client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
				response := body
				if r.URL.Path == lyricPath {
					fallbacks++
					if r.Method != "GET" || r.Header.Get("Referer") != "https://y.qq.com/" || r.URL.Query().Get("songmid") != "003RcjkV1XjQQy" || r.URL.Query().Get("format") != "json" || r.URL.Query().Get("nobase64") != "1" {
						t.Fatal("旧歌词请求改变", r.URL)
					}
					response = `{"code":0,"lyric":"[00:01.00]fallback"}`
					if tc.fail {
						response = `{"code":403}`
					}
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			ref := provider.Ref{ID: "000BDJvT2aWQYZ"}
			if tc.track {
				ref.TrackRef = "003RcjkV1XjQQy"
			}
			d, err := New(client, Config{}, nil).FetchDetail(context.Background(), ref)
			if err != nil || len(d.Tracklist) != 12 || d.AlbumTitle == nil || fallbacks != tc.wantFallback {
				t.Fatalf("detail=%+v fallbacks=%d err=%v", d, fallbacks, err)
			}
			if tc.fail && d.Lyrics != nil {
				t.Fatal("失败被伪装歌词")
			}
			if tc.wantFallback == 1 && !tc.fail && (d.Lyrics == nil || *d.Lyrics != "[00:01.00]fallback") {
				t.Fatal("回退歌词丢失")
			}
		})
	}
}
