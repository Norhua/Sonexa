package lyrics

import (
	_ "embed"
	"strings"
	"sync"
	"unicode"

	"github.com/norhua/sonexa/internal/matching"
)

// Official OpenCC data only; see data/opencc/README.md for provenance/license.
// No network, converter dependency, or rewriting of query/display names.
//
//go:embed data/opencc/TSCharacters.txt
var lyricCreditDictionary string

var lyricCreditVariants = sync.OnceValue(func() map[rune]rune {
	out := map[rune]rune{}
	for line := range strings.SplitSeq(lyricCreditDictionary, "\n") {
		fields := strings.Fields(line)
		// Ambiguous entries require linguistic context; never pick the first.
		if len(fields) != 2 {
			continue
		}
		from, to := []rune(fields[0]), []rune(fields[1])
		if len(from) == 1 && len(to) == 1 {
			out[from[0]] = to[0]
		}
	}
	return out
})

// Only exact script-equivalent credits add evidence. Each differing character
// must directly map to its counterpart: two traditional names that merely
// collapse to the same simplified form are not equated (e.g. 發 vs 髮).
// Do not extend fuzzy matching to converted names or alter metadata matching.
func lyricCreditKeys(query, candidate string) (string, string) {
	nq, nc := matching.Normalize(query), matching.Normalize(candidate)
	if nq == "" || nc == "" || nq == nc || hasKanaOrHangul(query+candidate) {
		return query, candidate
	}
	q, c := []rune(nq), []rune(nc)
	if len(q) != len(c) {
		return query, candidate
	}
	variants := lyricCreditVariants()
	for i, a := range q {
		b := c[i]
		toA, mappedA := variants[a]
		toB, mappedB := variants[b]
		if a != b && !(mappedA && toA == b) && !(mappedB && toB == a) {
			return query, candidate
		}
	}
	return nq, nq
}

func hasKanaOrHangul(s string) bool {
	for _, r := range s {
		if unicode.In(r, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}
