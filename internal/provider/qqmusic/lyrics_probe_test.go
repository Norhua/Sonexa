package qqmusic

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/provider/lyricfetch"
)

func channelSummary(c *lyricdoc.Channel) string {
	if c == nil {
		return "unknown"
	}
	return fmt.Sprintf("%s origin=%s sync=%s bytes=%d", c.Status, c.Origin, c.SyncStatus, len(c.Text))
}

// Replays local, uncommitted upstream probe data without storing song lyrics in
// the repository. The regular suite below uses synthetic public-domain text.
func TestLocalQQProbe(t *testing.T) {
	path := os.Getenv("SONEXA_QQ_PROBE")
	if path == "" {
		t.Skip("set SONEXA_QQ_PROBE to a GetPlayLyricInfo response")
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal("cannot open local response")
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, (4<<20)+1))
	if err != nil || len(body) > 4<<20 {
		t.Fatal("local response unreadable or exceeds 4 MiB")
	}
	channels, _ := parseQQPlay(body)
	for _, ch := range []*lyricdoc.Channel{channels.Original, channels.Translation, channels.Romanization} {
		t.Log(channelSummary(ch))
	}
	t.Logf("wordTiming=%s warnings=%d", channels.WordTiming, len(channels.Warnings))
	doc, err := lyricdoc.Build(channels)
	if err != nil {
		t.Fatalf("document unavailable error_type=%T", lyricfetch.Normalize(err))
	}
	t.Logf("format=%s bytes=%d warnings=%d", doc.Format, len(doc.Text), len(doc.Warnings))
}

func TestQQProbeSummaryDoesNotExposeTextOrErrorMessage(t *testing.T) {
	ch := &lyricdoc.Channel{Status: "error", Text: "private-lyric", Message: "private-token"}
	if summary := channelSummary(ch); strings.Contains(summary, "private") || !strings.Contains(summary, "bytes=") {
		t.Fatal("unsafe probe summary")
	}
}
