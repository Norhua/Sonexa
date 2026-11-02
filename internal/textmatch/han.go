// Package textmatch contains conservative text evidence shared by search consumers.
package textmatch

import (
	_ "embed"
	"strings"
	"sync"
)

//go:embed data/opencc/TSCharacters.txt
var hanDictionary string

var hanVariants = sync.OnceValue(func() map[rune]rune {
	out := map[rune]rune{}
	for line := range strings.SplitSeq(hanDictionary, "\n") {
		fields := strings.Fields(line)
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

// HanEquivalent compares normalized text using direct unambiguous character
// mappings, not a shared folded key (發 and 髮 must not become the same name).
// Consumers decide where this may count as evidence; it never rewrites text.
func HanEquivalent(a, b string) bool {
	left, right := []rune(a), []rune(b)
	if len(left) == 0 || len(left) != len(right) {
		return false
	}
	variants := hanVariants()
	for i, l := range left {
		r := right[i]
		toL, mappedL := variants[l]
		toR, mappedR := variants[r]
		if l != r && !(mappedL && toL == r) && !(mappedR && toR == l) {
			return false
		}
	}
	return true
}
