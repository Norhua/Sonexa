package matching

import (
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// metadataAlbumPhrase is recall corroboration only, never album identity. A
// complete, meaningful album name can occur inside a platform's longer product
// description. Unlike Tokens (space-delimited), this also works for unspaced
// East Asian text without permitting substrings inside Latin words.
func metadataAlbumPhrase(a, b string) bool {
	if !completeEqualOrUntruncated(a, b) || confirmationVersionKey(a, true) != confirmationVersionKey(b, true) {
		return false
	}
	x, _ := albumPhraseText(a)
	y, boundaries := albumPhraseText(b)
	short := a
	if len(x) > len(y) {
		x, y = y, x
		_, boundaries = albumPhraseText(a)
		short = b
	}
	if len(x) == 0 || len(x) == len(y) || !meaningfulAlbumPhrase(short) {
		return false
	}
	boundary := func(i int) bool {
		if i == 0 || i == len(y) || boundaries[i] {
			return true
		}
		// Do not cut sequels/volume numbers off the end of an unspaced name.
		if unicode.IsNumber(y[i-1]) || unicode.IsNumber(y[i]) {
			return false
		}
		return eastAsianLetter(y[i-1]) || eastAsianLetter(y[i])
	}
	for i := 0; i+len(x) <= len(y); i++ {
		if !boundary(i) || !boundary(i+len(x)) {
			continue
		}
		if slices.Equal(x, y[i:i+len(x)]) {
			return true
		}
	}
	return false
}

// Keep original word boundaries alongside normalized runes; normalizing the
// entire string first would erase the boundary distinction between "Blue Moon"
// and "BlueMoon" when looking for a shorter name such as "Moon".
func albumPhraseText(s string) ([]rune, map[int]bool) {
	var text []rune
	boundaries := map[int]bool{}
	for _, word := range albumPhraseWords(s) {
		boundaries[len(text)] = true
		text = append(text, []rune(word)...)
	}
	return text, boundaries
}

func albumPhraseWords(s string) []string {
	parts := strings.FieldsFunc(norm.NFKC.String(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	})
	for i := range parts {
		parts[i] = Normalize(parts[i])
	}
	return parts
}

func eastAsianLetter(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

// Type/marketing labels alone are not corroboration. The threshold counts
// letters, not UTF-8 bytes or volume numbers, and does not affect CompareAlbum.
func meaningfulAlbumPhrase(s string) bool {
	letters := 0
	for _, word := range albumPhraseWords(s) {
		if genericAlbumWords[word] {
			continue
		}
		for _, r := range word {
			if unicode.IsLetter(r) {
				letters++
			}
		}
	}
	return letters >= 4
}

var genericAlbumWords = func() map[string]bool {
	words := strings.Fields(`the a an of and album single ep ost soundtrack original score best greatest hits collection compilation music songs vol volume disc cd tv anime game motion picture
		ベスト アルバム ベストアルバム サウンドトラック オリジナルサウンドトラック 主題歌集 音楽集
		专辑 專輯 精选 精選 精选集 精選集 原声 原聲 原声带 原聲帶 影视原声 影視原聲
		앨범 베스트 사운드트랙`)
	out := make(map[string]bool, len(words))
	for _, word := range words {
		out[Normalize(word)] = true
	}
	return out
}()
