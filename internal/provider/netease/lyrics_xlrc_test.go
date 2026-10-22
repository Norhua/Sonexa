package netease

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func pairedResponse(t *testing.T, yrc, ytlrc, yroma string) string {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"code":    200,
		"lrc":     lyricText{Lyric: "[00:41.360]かなの歌\n[00:47.920]また歌う"},
		"tlyric":  lyricText{Lyric: "[00:41.360]普通译一\n[00:47.920]普通译二"},
		"romalrc": lyricText{Lyric: "[00:41.360]ordinary roma one\n[00:47.920]ordinary roma two"},
		"yrc":     lyricText{Lyric: yrc}, "ytlrc": lyricText{Lyric: ytlrc}, "yromalrc": lyricText{Lyric: yroma},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestNetEaseSelectsAuxiliaryTimelineWithOriginal(t *testing.T) {
	const wordOriginal = "[42570,3000](42570,3000,0)かなの歌\n[48330,3000](48330,3000,0)また歌う"
	for _, tc := range []struct {
		name, yrc, ytlrc, yroma, wantTranslation, wantRoma string
		start                                              int64
	}{
		{"paired YRC", wordOriginal, "[00:42.570]逐字译一\n[00:48.330]逐字译二", "[00:42.8]kana no uta\n[00:48.5]mata utau", "逐字译一", "kana no uta", 42570},
		{"ordinary auxiliaries with YRC", wordOriginal, "", "", "普通译一", "ordinary roma one", 42570},
		{"bad paired auxiliaries", wordOriginal, "[00:99]broken", "[00:99]broken", "普通译一", "ordinary roma one", 42570},
		{"missing YRC", "", "[00:42.570]逐字译一", "[00:42.8]yroma", "普通译一", "ordinary roma one", 41360},
		{"bad YRC", "[0,1](0,20,0)bad", "[00:42.570]逐字译一", "[00:42.8]yroma", "普通译一", "ordinary roma one", 41360},
		{"wordless YRC", "[42570,3000]かなの歌", "[00:42.570]逐字译一", "[00:42.8]yroma", "普通译一", "ordinary roma one", 41360},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := pairedResponse(t, tc.yrc, tc.ytlrc, tc.yroma)
			calls := 0
			client := provider.NewClient(nil, nil, "test")
			client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if calls != 1 || r.URL.Query().Get("ytv") != "-1" || r.URL.Query().Get("yrv") != "-1" {
					t.Fatal("unexpected request", r.URL)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			d, err := New(client).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "123"})
			if err != nil || calls != 1 {
				t.Fatal(err, calls)
			}
			if len(d.Rows) != 2 || *d.Rows[0].TimeMS != tc.start || d.Rows[0].Translation[0] != tc.wantTranslation || d.Rows[0].Romanization[0] != tc.wantRoma {
				t.Fatal("wrong auxiliary timeline", d.Rows)
			}
			if !strings.Contains(d.Text, "[>zh]"+tc.wantTranslation) || !strings.Contains(d.Text, "[>ja-Latn]"+tc.wantRoma) || strings.Contains(d.Text, "[x-channel:") {
				t.Fatal("extras not subordinate", d.Text)
			}
			round, err := lyricdoc.ParseXLRC(d.Text)
			if err != nil || !reflect.DeepEqual(round.Rows, d.Rows) {
				t.Fatal("roundtrip failed", err)
			}
		})
	}
}

func TestNetEaseOrdinaryEndpointFallbackPrefersItsOwnAuxiliaries(t *testing.T) {
	first := `{"code":200,"yrc":{"lyric":"[0,1](0,20,0)bad"},"ytlrc":{"lyric":"[00:42.570]wrong timeline"},"yromalrc":{"lyric":"[00:42.570]wrong roma"}}`
	second := string(lyricResponse(t, "[00:41.36]かなかな", "[00:41.36]普通翻译", "[00:41.36]kana kana"))
	calls := 0
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = protocolTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := first
		if calls == 2 {
			body = second
			if r.URL.Path != "/weapi/song/lyric" {
				t.Fatal(r.URL)
			}
		} else if calls > 2 {
			t.Fatal("too many requests")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	d, err := New(client).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "123"})
	if err != nil || calls != 2 || len(d.Rows) != 1 || d.Rows[0].Translation[0] != "普通翻译" || d.Rows[0].Romanization[0] != "kana kana" {
		t.Fatal(d, err, calls)
	}
}

// Replays an operator-provided response locally; never prints song text or
// performs network requests, and the normal suite only uses synthetic lyrics.
func TestLocalNetEaseProbe(t *testing.T) {
	path := os.Getenv("SONEXA_NETEASE_PROBE")
	if path == "" {
		t.Skip("set SONEXA_NETEASE_PROBE to a local v1 response")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("cannot open local response")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		t.Fatal("response unreadable or too large")
	}
	input, err := parseLyrics(body)
	if err != nil {
		t.Fatal("response parse failed")
	}
	for _, online := range []bool{false, true} {
		var d *lyricdoc.Document
		if online {
			d, err = lyricdoc.BuildOnline(input, "zh")
		} else {
			d, err = lyricdoc.Build(input)
		}
		if err != nil {
			t.Fatal("document build failed")
		}
		for _, entry := range []struct {
			key string
			ch  *lyricdoc.Channel
		}{{"translation", d.Translation}, {"romanization", d.Romanization}} {
			independent, attached := 0, 0
			for _, r := range d.Rows {
				if r.Channel == entry.key && strings.TrimSpace(r.Text) != "" {
					independent++
				}
				for _, a := range r.Annotations {
					if a.Channel == entry.key && strings.TrimSpace(a.Text) != "" {
						attached++
					}
				}
			}
			t.Logf("online=%v channel=%s status=%s attached=%d independent=%d", online, entry.key, entry.ch.Status, attached, independent)
		}
		round, err := lyricdoc.ParseXLRC(d.Text)
		if err != nil || !reflect.DeepEqual(round.Rows, d.Rows) {
			t.Fatal("roundtrip failed")
		}
	}
}
