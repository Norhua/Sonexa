package kuwo

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (r roundTrip) RoundTrip(req *http.Request) (*http.Response, error) { return r(req) }
func TestOpenapiAndRestrictedH5Fallback(t *testing.T) {
	for _, tt := range []struct {
		name, body    string
		status, calls int
		fail          bool
	}{
		{"success", `{"code":200,"data":{"lrclist":[{"time":"1.2","lineLyric":"body"}]}}`, 200, 1, false},
		{"plain invalid time", `{"code":200,"data":{"lrclist":[{"time":"bad","lineLyric":"first"},{"time":"1","lineLyric":"second"}]}}`, 200, 1, false},
		{"missing", `{"code":200,"data":null}`, 200, 2, false},
		{"malformed", `{`, 200, 2, false},
		{"wrong status shape", `{"status":200}`, 200, 2, false},
		{"business", `{"code":301}`, 200, 1, true},
		{"limited", ``, 429, 1, true},
		{"auth", ``, 403, 1, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			client := provider.NewClient(nil, nil, "test")
			client.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Scheme != "https" || r.URL.Query().Get("musicId") != "123" || r.Header.Get("Referer") != "https://"+r.URL.Host+"/" {
					t.Fatal(r.URL, r.Header)
				}
				body, status := tt.body, tt.status
				if calls == 1 {
					if r.URL.Host != "www.kuwo.cn" || r.URL.Path != openapiLyricsPath {
						t.Fatal(r.URL)
					}
				} else {
					if calls > 2 || r.URL.Host != "m.kuwo.cn" || r.URL.Path != "/newh5/singles/songinfoandlrc" {
						t.Fatal(r.URL)
					}
					body, status = `{"status":200,"data":{"lrclist":[{"time":"2","lineLyric":"fallback"}]}}`, 200
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			p, err := New(client).FetchLyrics(context.Background(), lyrics.Candidate{Ref: "123"})
			if calls != tt.calls || (err != nil) != tt.fail {
				t.Fatal(calls, err)
			}
			if err == nil {
				if err := lyricdoc.Validate(p); err != nil {
					t.Fatal(err)
				}
				if tt.name == "plain invalid time" && (p.Text != "first\nsecond" || p.Format != "text" || p.WordTiming != "unsupported" || len(p.Warnings) == 0) {
					t.Fatal(p)
				}
			}
		})
	}
}
