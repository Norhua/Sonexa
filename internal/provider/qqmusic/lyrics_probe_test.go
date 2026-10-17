package qqmusic

import (
	"fmt"
	"os"
	"testing"

	"github.com/norhua/sonexa/internal/lyrics"
)

func channelSummary(c *lyrics.Channel) string {
	if c == nil {
		return "unknown"
	}
	return fmt.Sprintf("%s bytes=%d %s", c.Status, len(c.Text), c.Message)
}

// Replays local, uncommitted upstream probe data without storing song lyrics in
// the repository. The regular suite below uses synthetic public-domain text.
func TestLocalQQProbe(t *testing.T) {
	path := os.Getenv("SONEXA_QQ_PROBE")
	if path == "" {
		t.Skip("set SONEXA_QQ_PROBE to a GetPlayLyricInfo response")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	channels := parseQQPlay(body)
	for _, ch := range []*lyrics.Channel{channels.Original, channels.Translation, channels.Romanization} {
		t.Log(channelSummary(ch))
	}
	t.Log(channels.WordTiming, channels.Warnings)
	doc, err := lyrics.DocumentFromPayload(channels)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Format != "enhanced_lrc" || doc.Translation.Status != "available" || doc.Romanization.Status != "available" {
		t.Fatalf("unexpected channels: format=%s warnings=%v", doc.Format, doc.Warnings)
	}
}
