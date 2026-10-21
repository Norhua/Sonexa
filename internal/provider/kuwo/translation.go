package kuwo

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/norhua/sonexa/internal/lyrics"
)

// These are adapter-local source rows, before sorting/deduplication. Position
// is the zero-based index in lrclist, including blank timing rows and credits.
type lyricRow struct {
	position int
	timeMS   int64
	text     string
	metadata bool
}

type inferredLine struct {
	row, original int
	layer         int
}

type lineClass uint8

const (
	ignoredLine lineClass = iota
	foreignLine
	hanLine
	mixedLine
)

type scripts struct{ han, ascii, latin, eastAsian int }

func countScripts(text string) scripts {
	var out scripts
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han):
			out.han++
		case unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			out.eastAsian++
		case unicode.In(r, unicode.Latin):
			out.latin++
			if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
				out.ascii++
			}
		}
	}
	return out
}

// Recognized labels are exclusion evidence only: their text is never deleted.
// Keep this list narrow; ordinary lyrics containing these words remain lyrics.
var creditLine = regexp.MustCompile(`(?i)^(?:作词|作曲|编曲|演唱|词|曲|翻译|译者|译文|歌词制作|制作人|版权声明|版权|词曲|lyricist|composer|arranger|lyrics by|composed by|translated by|translation|copyright)\s*[:：]|^(?:©|℗)|^(?:本歌词|本翻译|本译文|本作品|未经许可|未经授权|版权所有)`)
var singerLabel = regexp.MustCompile(`(?i)^[\[（(【]?\s*(?:男|女|合|男声|女声|合唱|male|female|all)\s*[:：]?\s*[\]）)】]?\s*[:：]?\s*$`)
var latinWord = regexp.MustCompile(`[A-Za-z]+(?:['’][A-Za-z]+)*`)

func blockedText(text string) bool {
	text = strings.TrimSpace(text)
	return creditLine.MatchString(text) || singerLabel.MatchString(text)
}

func classifyRow(row lyricRow) lineClass {
	if row.metadata || strings.TrimSpace(row.text) == "" || blockedText(row.text) {
		return ignoredLine
	}
	s := countScripts(row.text)
	if s.eastAsian > 0 || s.han == 0 && s.ascii >= 2 {
		return foreignLine
	}
	if s.han >= 2 && s.latin == 0 && s.eastAsian == 0 {
		return hanLine
	}
	return mixedLine
}

func borrowedWords(text, original string) bool {
	words := latinWord.FindAllString(strings.ToLower(text), -1)
	if len(words) == 0 {
		return false
	}
	allowed := map[string]bool{}
	for _, word := range latinWord.FindAllString(strings.ToLower(original), -1) {
		allowed[word] = true
	}
	for _, word := range words {
		if !allowed[word] {
			return false
		}
	}
	return true
}

func mixedWholeTranslation(text, original string) bool {
	if !borrowedWords(text, original) {
		return false
	}
	s := countScripts(latinWord.ReplaceAllString(text, ""))
	return s.han >= 2 && s.latin == 0 && s.eastAsian == 0
}

// wholeAlternation implements only the first layer. Integer comparisons keep
// inclusive 0.7/1.3 and 80% thresholds exact, independent of floating rounding.
func wholeAlternation(rows []lyricRow, c lyrics.Candidate) []inferredLine {
	classes := make([]lineClass, len(rows))
	foreign, han, follows, eastAsian := 0, 0, 0, 0
	previous := ignoredLine
	foreignAt := map[int64]int{}
	for i, row := range rows {
		kind := classifyRow(row)
		classes[i] = kind
		if kind == ignoredLine {
			continue
		}
		if kind == foreignLine {
			foreign++
			foreignAt[row.timeMS]++
			if countScripts(row.text).eastAsian > 0 {
				eastAsian++
			}
		} else if kind == hanLine {
			han++
			if previous == foreignLine {
				follows++
			}
		}
		previous = kind
	}
	foreignSong := strings.TrimSpace(c.Title) != "" && strings.TrimSpace(c.Artist) != "" && countScripts(c.Title).han == 0 && countScripts(c.Artist).han == 0
	if foreign < 8 || han < 8 || han*10 < foreign*7 || han*10 > foreign*13 || follows*5 < han*4 || !foreignSong && eastAsian*2 < foreign {
		return nil
	}
	out := []inferredLine{}
	original, previousContent := -1, -1
	for i, row := range rows {
		if strings.TrimSpace(row.text) == "" {
			continue
		}
		if row.metadata || blockedText(row.text) {
			original, previousContent = -1, -1
			continue
		}
		switch classes[i] {
		case foreignLine:
			original = i
			if foreignAt[row.timeMS] != 1 {
				original = -1
			}
		case hanLine:
			if original >= 0 {
				out = append(out, inferredLine{i, original, 1})
			}
		case mixedLine:
			if original >= 0 && previousContent == original && mixedWholeTranslation(row.text, rows[original].text) {
				out = append(out, inferredLine{i, original, 1})
			} else {
				original = -1
			}
		}
		previousContent = i
	}
	return out
}
