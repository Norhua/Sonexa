package netease

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

func TestDirectCapabilityReturnsCompleteYRCAndPartialDocument(t *testing.T) {
	for _, original := range []string{`"yrc":{"lyric":"[1000,2000](1000,300,0)Hello (1500,500,0)world"}`, `"lrc":{"lyric":"plain original"}`} {
		calls := 0
		client := provider.NewClient(nil, nil, "test")
		client.HTTP.Transport = protocolTransport(func(*http.Request) (*http.Response, error) {
			calls++
			body := `{"code":200,` + original + `,"tlyric":{"lyric":"[00:99]bad time"},"romalrc":{"lyric":"roma"}}`
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		var capability lyrics.Provider = New(client)
		d, err := capability.FetchLyrics(context.Background(), lyrics.Candidate{Ref: "123"})
		if err != nil {
			t.Fatal(err)
		}
		if err := lyricdoc.Validate(d); err != nil {
			t.Fatal(err)
		}
		if calls != 1 || d.Translation.Status != "error" || d.Romanization.Status != "available" || len(d.Warnings) == 0 || d.Rows[0].Text == "" {
			t.Fatal(d, calls)
		}
		if strings.Contains(original, "yrc") && (d.Original.Track.Lines[0].EndMS == nil || *d.Original.Track.Lines[0].EndMS != 3000) {
			t.Fatal("native line end lost")
		}
	}
}
