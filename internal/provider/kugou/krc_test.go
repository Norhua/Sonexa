package kugou

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func encryptedKRC(t *testing.T, text string) string {
	t.Helper()
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	if _, err := z.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(nil), b.Bytes()...)
	for i := range data {
		data[i] ^= krcXOR[i%len(krcXOR)]
	}
	return base64.StdEncoding.EncodeToString(append([]byte("krc1"), data...))
}
func TestKRCRelativeTimesAndBounds(t *testing.T) {
	p, err := krcPayload(encryptedKRC(t, "[10000,2000]<-250,200,0>A <250,500,0>B (aside) 😀"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := lyrics.DocumentFromPayload(p)
	if err != nil || d.WordTiming != "available" || !strings.Contains(d.Text, "<00:09.750>A <00:09.950><00:10.250>B (aside) 😀<00:10.750>") {
		t.Fatal(d, err)
	}
	for _, text := range []string{"[100,100]<-200,10,0>bad", "[86400000,0]<1,0,0>bad", "[9999999999999999999999,1]<0,1,0>bad", "[0,100]<0,99999999999999999999999,0>bad"} {
		if _, err := krcPayload(encryptedKRC(t, text)); !errors.Is(err, lyrics.ErrFormat) {
			t.Fatal(err)
		}
	}
	for _, s := range []string{"%%%", base64.StdEncoding.EncodeToString([]byte("bad header")), base64.StdEncoding.EncodeToString([]byte("krc1broken"))} {
		if _, err := decodeKRC(s); !errors.Is(err, lyrics.ErrFormat) {
			t.Fatal(err)
		}
	}
	if _, err := decodeKRC(encryptedKRC(t, strings.Repeat("x", lyrics.MaxTextBytes+1))); !errors.Is(err, lyrics.ErrTooLarge) {
		t.Fatal(err)
	}
}

func TestKRCLanguageUsesNativeRowsIncludingEmpty(t *testing.T) {
	for _, tt := range []struct {
		name, language            string
		wantTranslation, wantRoma string
		sync                      bool
	}{
		{"aligned", `{"content":[{"type":1,"lyricContent":[["译一"],["空行译文"],["译三"]]},{"type":0,"lyricContent":[["ro","ma"],[""],["ji"]]}]}`, "available", "available", true},
		{"mismatch", `{"content":[{"type":1,"lyricContent":[["one"]]}]}`, "available", "unavailable", false},
		{"phonetic Han", `{"content":[{"type":0,"lyricContent":[["中文谐音"]]}]}`, "unavailable", "unknown", false},
		{"broken type", `{"content":[{"type":1,"lyricContent":"bad"},{"type":0,"lyricContent":[["roma"]]}]}`, "error", "available", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			text := "[language:" + base64.StdEncoding.EncodeToString([]byte(tt.language)) + "]\n[1000,1000]<0,500,0>A\n[2000,1000]<0,500,0>\n[3000,1000]<0,500,0>C"
			p, err := krcPayload(encryptedKRC(t, text))
			if err != nil {
				t.Fatal(err)
			}
			d, err := lyrics.DocumentFromPayload(p)
			if err != nil {
				t.Fatal(err)
			}
			if d.Translation.Status != tt.wantTranslation || d.Romanization.Status != tt.wantRoma {
				t.Fatal(d)
			}
			if tt.sync && !strings.Contains(d.Text, "[00:02.000]空行译文") {
				t.Fatal(d.Text)
			}
			if tt.name == "mismatch" && (d.Translation.SyncStatus != "unsynced" || !strings.HasSuffix(d.Text, "one")) {
				t.Fatal(d)
			}
			if tt.name == "phonetic Han" && (len(d.Additional) != 1 || !strings.Contains(d.Text, "中文谐音")) {
				t.Fatal(d)
			}
			score := lyrics.ScoreContent(lyrics.Candidate{MatchScore: 80}, d, 10000)
			if score.TotalScore == nil || (score.Terms[3].Value > 0) != (d.Translation.Status == "available") || (score.Terms[4].Value > 0) != (d.Romanization.Status == "available") {
				t.Fatal("classification/score mismatch", score)
			}
		})
	}
	p, err := krcPayload(encryptedKRC(t, "[language:%%%]\n[0,1000]<0,500,0>A"))
	if err != nil || p.Translation.Status != "error" || p.Romanization.Status != "error" {
		t.Fatal(p, err)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }
func TestKRCDowloadStages(t *testing.T) {
	for _, tt := range []struct {
		name, content    string
		status, expected int
	}{
		{"success", encryptedKRC(t, "[0,1000]<0,400,0>A <500,400,0>B"), 200, 2},
		{"fallback with language", encryptedKRC(t, "[language:"+base64.StdEncoding.EncodeToString([]byte(`{"content":[{"type":1,"lyricContent":[["retained translation"]]}]}`))+"]\n[0,1]<-1,1,0>bad"), 200, 3},
		{"fallback", "bad", 200, 3},
		{"stop", "", 429, 2},
		{"too large", encryptedKRC(t, strings.Repeat("x", lyrics.MaxTextBytes+1)), 200, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := provider.NewClient(nil, nil, "test")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Host != "lyrics.kugou.com" {
					t.Fatal(r.URL)
				}
				status, body := 200, `{"status":200,"candidates":[{"id":"1","accesskey":"test","song":"Song"}]}`
				if calls == 1 {
					if r.URL.Query().Get("hash") != "hash" {
						t.Fatal(r.URL)
					}
				} else {
					format, content := r.URL.Query().Get("fmt"), tt.content
					if calls == 2 {
						if format != "krc" {
							t.Fatal(r.URL)
						}
						status = tt.status
					} else {
						if calls != 3 || format != "lrc" {
							t.Fatal(r.URL)
						}
						content = base64.StdEncoding.EncodeToString([]byte("[00:01]fallback"))
					}
					b, _ := json.Marshal(map[string]any{"status": 200, "fmt": format, "content": content})
					body = string(b)
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			p, err := New(client).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "hash", Title: "Song"})
			if calls != tt.expected {
				t.Fatal(calls, err)
			}
			if tt.name == "success" || strings.HasPrefix(tt.name, "fallback") {
				if err != nil {
					t.Fatal(err)
				}
				if d, err := lyrics.DocumentFromPayload(p); err != nil {
					t.Fatal(err)
				} else if tt.name == "fallback with language" && (!strings.Contains(d.Text, "retained translation") || d.Translation.SyncStatus != "unsynced") {
					t.Fatal(d)
				}
			} else if err == nil {
				t.Fatal("restriction hidden")
			}
		})
	}
}
