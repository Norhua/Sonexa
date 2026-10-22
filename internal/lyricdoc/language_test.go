package lyricdoc

import (
	"strings"
	"testing"
)

func TestOnlineLanguageLabels(t *testing.T) {
	for _, tc := range []struct {
		name, original, translation, roma, wantTranslation, wantRoma string
	}{
		{"Japanese", "[00:10]かなの歌", "[00:10]这是中文翻译", "[00:10]kana no uta", "zh", "ja-Latn"},
		{"Korean", "[00:10]안녕하세요", "[00:10]你好", "[00:10]annyeong", "zh", "ko-Latn"},
		{"known Chinese", "[lang:zh-Hans]\n[00:10]原文", "[00:10]译文", "[00:10]yuan wen", "zh", "zh-Latn"},
		{"Han ambiguous", "[00:10]世界", "[00:10]翻译", "[00:10]sekai", "zh", "und-Latn"},
		{"Latin ambiguous", "[00:10]song", "[00:10]unidentified words", "[00:10]roma", "zh", "und-Latn"},
		{"quoted Japanese in Chinese", "[00:10]Original", "[00:10]这是一段带有日语称呼的中文翻译「かな」", "[00:10]roma", "zh", "und-Latn"},
		{"Japanese translation", "[00:10]Original", "[00:10]こんにちは", "[00:10]roma", "ja", "und-Latn"},
		{"Korean translation", "[00:10]Original", "[00:10]안녕하세요", "[00:10]roma", "ko", "und-Latn"},
		{"explicit wins", "[00:10]かなかな", "[lang:en]\n[00:10]hello", "[lang:ko-Latn]\n[00:10]roma", "en", "ko-Latn"},
		{"unknown explicit", "[00:10]かなかな", "[lang:und]\n[00:10]译文", "[lang:und-Latn]\n[00:10]roma", "zh", "ja-Latn"},
		{"unknown script", "[00:10]かな한글", "[00:10]かな한글", "[00:10]roma", "", "und-Latn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := BuildOnline(&Input{Text: tc.original, Translation: TextChannel(tc.translation, nil), Romanization: TextChannel(tc.roma, nil)}, "zh")
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantTranslation == "" {
				if d.Translation.Status != "unavailable" {
					t.Fatal("duplicate promoted by inferred language")
				}
			} else if d.Translation.Track.Lines[0].Language != tc.wantTranslation || !strings.Contains(d.Text, "[>"+tc.wantTranslation+"]") {
				t.Fatal("translation language", d.Text)
			}
			if d.Romanization.Track.Lines[0].Language != tc.wantRoma || !strings.Contains(d.Text, "[>"+tc.wantRoma+"]") {
				t.Fatal("romanization language", d.Text)
			}
			assertOnlineRoundtrip(t, d)
		})
	}
}

func TestOnlineLanguageUsesParentAndDoesNotInventMixedTrackLanguage(t *testing.T) {
	d, err := BuildOnline(&Input{Text: "[00:10]かなかな\n[00:20]안녕하세요\n[00:30]world", Romanization: TextChannel("[00:09.5]kana\n[00:20.3]annyeong\n[00:30.5]world roma\nuntimed", nil)}, "zh")
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"ja-Latn", "ko-Latn", "und-Latn", "und-Latn"} {
		if d.Romanization.Track.Lines[i].Language != want {
			t.Fatal(i, d.Romanization.Track.Lines[i].Language)
		}
	}
	assertOnlineRoundtrip(t, d)
	if _, err := BuildOnline(&Input{Text: "text"}, "bad:tag"); err == nil {
		t.Fatal("invalid fallback accepted")
	}
}
