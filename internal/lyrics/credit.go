package lyrics

import (
	"unicode"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/textmatch"
)

// Only exact script-equivalent credits add evidence. Each differing character
// must directly map to its counterpart: two traditional names that merely
// collapse to the same simplified form are not equated (e.g. 發 vs 髮).
// Do not extend fuzzy matching to converted names or alter metadata matching.
func lyricCreditKeys(query, candidate string) (string, string) {
	nq, nc := matching.Normalize(query), matching.Normalize(candidate)
	if nq == "" || nc == "" || nq == nc || hasKanaOrHangul(query+candidate) {
		return query, candidate
	}
	if !textmatch.HanEquivalent(nq, nc) {
		return query, candidate
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
