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
	return build(p, buildOptions{})
}

// BuildOnline enables bounded timing/language inference for native online
// adapters. The adapter, not the parser or business consumer, supplies its
// translation-language fallback. Local imports continue to use Build/ParseText.
func BuildOnline(p *Input, translationFallback string) (*Document, error) {
	if translationFallback != "" && !languageTag.MatchString(translationFallback) {
		return nil, ErrFormat
	}
	return build(p, buildOptions{infer: true, translationFallback: translationFallback})
}

type buildOptions struct {
	infer               bool
	translationFallback string
}

func build(p *Input, options buildOptions) (*Document, error) {
	if p == nil {
		return nil, fmt.Errorf("%w：来源未返回正文", ErrNoLyrics)
	}
	if p.Text != "" && p.Original == nil && isXLRCText(p.Text) {
		d, err := ParseXLRC(p.Text)
		if err != nil {
			return nil, err
		}
		copy := *p
		copy.Text, copy.Original = "", d.Original
		if copy.Translation == nil {
			copy.Translation = d.Translation
		}
		if copy.Romanization == nil {
			copy.Romanization = d.Romanization
		}
		copy.Additional = append(append([]*Channel(nil), d.Additional...), p.Additional...)
		return build(&copy, options)
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
	d, err := buildDocumentWithOptions(original, p.Translation, p.Romanization, p.Additional, timing, warnings, options)
	if err != nil {
		return nil, err
	}
	d.Text, err = SerializeXLRC(d)
	if err != nil {
		return nil, err
	}
	d.Format = "xlrc"
	if err := seal(d); err != nil {
		return nil, err
	}
	return d, nil
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
	for _, l := range extra.Lines {
		if l.ParentID != "" || l.Language != "" {
			return false
		}
	}
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
