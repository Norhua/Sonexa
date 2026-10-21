package lyricdoc

import (
	"fmt"
	"strings"
)

// Input is adapter-local construction data, never a successful capability
// result. Build chooses one original and finishes every output projection.
type Input struct {
	Text         string
	Original     *Channel
	WordOriginal *Channel
	Translation  *Channel
	Romanization *Channel
	Additional   []*Channel
	WordTiming   string
	Warnings     []string
}

func Build(p *Input) (*Document, error) {
	if p == nil {
		return nil, fmt.Errorf("%w：来源未返回正文", ErrNoLyrics)
	}
	original := p.Original
	if original == nil && p.Text != "" {
		original = TextChannel(p.Text, nil)
	}
	warnings := append([]string(nil), p.Warnings...)
	timing := p.WordTiming
	if p.WordOriginal != nil {
		word := normalizeChannel(p.WordOriginal)
		if word.Status == "available" && hasWords(word.Track) {
			original = word
		} else if word.Status == "error" {
			if original == nil || normalizeChannel(original).Status != "available" {
				return nil, fmt.Errorf("逐字原文解析失败：%w", word.Err)
			}
			timing = "error"
			warnings = append(warnings, "逐字原文解析失败："+word.Message)
		} else if original == nil || normalizeChannel(original).Status != "available" {
			original = word
		}
	}
	return buildDocument(original, p.Translation, p.Romanization, p.Additional, timing, warnings)
}

// AvailableContent is the single presence predicate used by completed
// channels and business scoring. Unknown/additional content never earns a
// confirmed translation or romanization bonus.
func AvailableContent(ch *Channel) bool {
	if ch == nil || ch.Status != "available" || ch.Origin != "independent" && ch.Origin != "embedded" || ch.Track == nil {
		return false
	}
	return hasContent(ch.Track)
}

func hasContent(t *Track) bool {
	for _, l := range t.Lines {
		if strings.TrimSpace(l.Text) != "" {
			return true
		}
	}
	return false
}

func duplicateOriginal(original, extra *Track) bool {
	visible := func(t *Track) []string {
		out := []string{}
		for _, l := range t.Lines {
			if strings.TrimSpace(l.Text) != "" {
				out = append(out, l.Text)
			}
		}
		return out
	}
	a, b := visible(original), visible(extra)
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return len(a) > 0
}
