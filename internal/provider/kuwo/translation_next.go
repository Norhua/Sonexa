package kuwo

import (
	"regexp"
	"strings"
)

var vocalSound = regexp.MustCompile(`^(?:o+h*|a+h*|u+h*|wo+a+h*|who+a+h*|ye+a+h*|h+m+|la+|na+)$`)

func onlyVocalSounds(text string) bool {
	s := countScripts(text)
	if s.han > 0 || s.eastAsian > 0 {
		return false
	}
	words := latinWord.FindAllString(strings.ToLower(text), -1)
	if len(words) == 0 {
		return false
	}
	for _, word := range words {
		if !vocalSound.MatchString(word) {
			return false
		}
	}
	return true
}

func nextTimeTranslation(text, original string) bool {
	if blockedText(text) {
		return false
	}
	s := countScripts(text)
	return s.han > 0 && s.eastAsian == 0 && (s.latin == s.ascii && s.ascii*2 <= s.han || borrowedWords(text, original))
}

// nextTimeAssociations consumes only source rows not extracted by layer one.
// Empty timed successors supply evidence; written labels are hard boundaries.
// Tail evidence never contributes to the four-strong-match admission count.
func nextTimeAssociations(rows []lyricRow, first []inferredLine) []inferredLine {
	removed := map[int]bool{}
	for _, match := range first {
		removed[match.row] = true
	}
	remaining := []int{}
	foreignAt := map[int64]int{}
	for i, row := range rows {
		if removed[i] {
			continue
		}
		remaining = append(remaining, i)
		if classifyRow(row) == foreignLine {
			foreignAt[row.timeMS]++
		}
	}
	matches := []inferredLine{}
	strong := 0
	chainNext := -1
	for i := 0; i+1 < len(remaining); i++ {
		original, candidate := remaining[i], remaining[i+1]
		f, h := rows[original], rows[candidate]
		if classifyRow(f) != foreignLine || foreignAt[f.timeMS] != 1 || h.metadata || !nextTimeTranslation(h.text, f.text) || h.timeMS <= f.timeMS {
			continue
		}
		vocal := onlyVocalSounds(f.text)
		if i+2 < len(remaining) {
			next := rows[remaining[i+2]]
			if next.metadata || blockedText(next.text) || h.timeMS != next.timeMS {
				continue
			}
			matches = append(matches, inferredLine{candidate, original, 2})
			if !vocal {
				strong++
			}
			chainNext = i + 2
		} else if chainNext == i && !vocal {
			matches = append(matches, inferredLine{candidate, original, 2})
		}
	}
	if len(first) == 0 && strong < 4 {
		return nil
	}
	return matches
}
