package lyrics

import (
	"strings"
	"unicode"

	"github.com/norhua/sonexa/internal/lyricdoc"
)

type ContentScore struct {
	TotalScore          *int        `json:"totalScore"`
	Terms               []ScoreTerm `json:"terms"`
	AutoPreviewEligible bool        `json:"autoPreviewEligible"`
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
func ScoreContent(c Candidate, d *lyricdoc.Document, durationMS int) ContentScore {
	result := ContentScore{}
	if d == nil || !lyricdoc.AvailableContent(d.Original) {
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
	for _, l := range t.Lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		lines++
		chars += letters(l.Text)
		if !lyricdoc.InvalidOffset(t) && l.StartMS != nil {
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
			if !lyricdoc.InvalidOffset(t) {
				check(w.StartMS)
				check(w.EndMS)
			}
		}
		if valid && !lyricdoc.InvalidOffset(t) && words >= 2 && different {
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
	if lyricdoc.AvailableContent(d.Translation) {
		translation = 15
	}
	if lyricdoc.AvailableContent(d.Romanization) {
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
	result.AutoPreviewEligible = c.HighConfidence && !abnormal && !lyricdoc.InvalidOffset(t)
	return result
}

func WithContentScore(c Candidate, d *lyricdoc.Document, durationMS int) Candidate {
	s := ScoreContent(c, d, durationMS)
	c.TotalScore, c.ScoreTerms, c.AutoPreviewEligible = s.TotalScore, s.Terms, s.AutoPreviewEligible
	return c
}
