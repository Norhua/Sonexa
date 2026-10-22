package lyricdoc

import (
	"strings"
	"unicode"
)

// scriptLanguage is deliberately not a general language detector. Kana and
// Hangul provide limited Japanese/Korean evidence; Han alone is shared across
// languages, and Latin alone cannot identify English or a romanized language.
func scriptLanguage(text string) string {
	kana, hangul, other, letters := 0, 0, 0, 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			letters++
		}
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			kana++
		case unicode.Is(unicode.Hangul, r):
			hangul++
		case unicode.IsLetter(r) && !unicode.In(r, unicode.Han, unicode.Latin):
			other++
		}
	}
	// Require a meaningful share of the line, not an isolated foreign quote
	// in an otherwise Chinese translation. Uncertain results use the caller's
	// translation fallback or remain unknown for romanization.
	if other == 0 && kana >= 2 && kana*5 >= letters && hangul == 0 {
		return "ja"
	}
	if other == 0 && hangul >= 2 && hangul*5 >= letters && kana == 0 {
		return "ko"
	}
	return ""
}

func knownLanguage(lang string) bool {
	return lang != "" && !strings.EqualFold(strings.Split(lang, "-")[0], "und")
}

func originalLanguage(l Line) string {
	if knownLanguage(l.Language) {
		return l.Language
	}
	return scriptLanguage(l.Text)
}

// romanizedLanguage describes the source language written in Latin script,
// not the language of the Latin letters themselves. Drop the original script
// and region rather than emit contradictory tags such as zh-Hans-Latn.
func romanizedLanguage(lang string) string {
	if !knownLanguage(lang) {
		return "und-Latn"
	}
	return strings.ToLower(strings.Split(lang, "-")[0]) + "-Latn"
}

// labelLanguages runs after duplicate-original rejection and association, on
// builder-owned copies only. Defaults must not turn a duplicate original into
// confirmed extra content. Explicit languages always take precedence.
func labelLanguages(channels []channelTrack, translationFallback string) {
	original := channels[0].channel.Track
	resolve := parentResolver(original)
	wholeLanguage := ""
	mixed := false
	languages := make([]string, len(original.Lines))
	for i, l := range original.Lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		lang := originalLanguage(l)
		languages[i] = lang
		if lang == "" {
			continue
		}
		base := strings.ToLower(strings.Split(lang, "-")[0])
		if wholeLanguage != "" && wholeLanguage != base {
			mixed = true
		}
		wholeLanguage = base
	}
	if mixed {
		wholeLanguage = ""
	}
	for _, entry := range channels {
		if !AvailableContent(entry.channel) || entry.key != "translation" && entry.key != "romanization" {
			continue
		}
		for i := range entry.channel.Track.Lines {
			l := &entry.channel.Track.Lines[i]
			if knownLanguage(l.Language) {
				continue
			}
			if entry.key == "translation" {
				lang := scriptLanguage(l.Text)
				if lang == "" {
					lang = translationFallback
				}
				if lang != "" {
					l.Language = lang
				}
				continue
			}
			lang := wholeLanguage
			if parent := resolve(*l); parent >= 0 {
				if local := languages[parent]; local != "" {
					lang = local
				}
			}
			l.Language = romanizedLanguage(lang)
		}
	}
}
