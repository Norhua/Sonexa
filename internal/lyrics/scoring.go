package lyrics

import (
	"strings"
	"unicode"
)

type ContentScore struct {
	TotalScore          *int        `json:"totalScore"`
	Terms               []ScoreTerm `json:"terms"`
	AutoPreviewEligible bool        `json:"autoPreviewEligible"`
}

// AvailableContent is shared by channel presence and content bonuses. A
// provider must identify upstream content; unknown/additional text is retained
// but never promoted to a confirmed translation or romanization here.
func AvailableContent(ch *Channel) bool {
	if ch == nil || ch.Status != "available" || ch.Origin != "independent" && ch.Origin != "embedded" || ch.Track == nil {
		return false
	}
	for _, l := range ch.Track.Lines {
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

func letters(text string) int {
	n := 0
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			n++
		}
	}
	return n
}

// ScoreContent is lyric-local, not matching confidence or a probability.
// Duration penalty from song matching is already included in MatchScore.
func ScoreContent(c Candidate, d *Document, durationMS int) ContentScore {
	result := ContentScore{}
	if d == nil || !AvailableContent(d.Original) {
		return result
	}
	t := d.Original.Track
	lines, timed, chars, covered := 0, 0, 0, 0
	abnormal := false
	check := func(n *int64) {
		if n != nil && durationMS > 0 && *n-t.OffsetMS > int64(durationMS)+5000 {
			abnormal = true
		}
	}
	entries := make([]taggedLine, len(t.Lines))
	for i, l := range t.Lines {
		entries[i] = taggedLine{l, "original"}
	}
	unique, err := deduplicate(entries)
	if err != nil {
		return result
	}
	for _, e := range unique {
		l := e.line
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		lines++
		chars += letters(l.Text)
		if !invalidOffset(t) && l.StartMS != nil {
			timed++
			check(l.StartMS)
		}
		words, wordChars := 0, 0
		var previous *int64
		different, valid := false, true
		for _, w := range l.Parts {
			if w.StartMS == nil || strings.TrimSpace(w.Text) == "" {
				continue
			}
			if w.EndMS != nil && *w.EndMS < *w.StartMS || previous != nil && *w.StartMS < *previous {
				valid = false
			}
			if previous != nil && *w.StartMS != *previous {
				different = true
			}
			previous = w.StartMS
			words++
			wordChars += letters(w.Text)
			if !invalidOffset(t) {
				check(w.StartMS)
				check(w.EndMS)
			}
		}
		if valid && !invalidOffset(t) && words >= 2 && different {
			covered += wordChars
		}
	}
	if lines == 0 {
		return result
	}
	sync, word, translation, romanization, penalty := 0, 0, 0, 0, 0
	if lines > 0 {
		sync = 10 * timed / lines
	}
	if chars > 0 {
		word = 20 * covered / chars
	}
	if AvailableContent(d.Translation) {
		translation = 15
	}
	if AvailableContent(d.Romanization) {
		romanization = 5
	}
	if abnormal {
		penalty = -30
	}
	result.Terms = []ScoreTerm{{"match", "歌曲匹配", c.MatchScore}, {"sync", "同步覆盖", sync}, {"word", "逐字覆盖", word}, {"translation", "上游翻译", translation}, {"romanization", "上游罗马音", romanization}, {"timeline", "时间轴越界", penalty}}
	total := 0
	for _, term := range result.Terms {
		total += term.Value
	}
	result.TotalScore = &total
	result.AutoPreviewEligible = c.HighConfidence && !abnormal && !invalidOffset(t)
	return result
}

func WithContentScore(c Candidate, d *Document, durationMS int) Candidate {
	s := ScoreContent(c, d, durationMS)
	c.TotalScore, c.ScoreTerms, c.AutoPreviewEligible = s.TotalScore, s.Terms, s.AutoPreviewEligible
	return c
}
