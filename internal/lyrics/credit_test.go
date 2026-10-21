package lyrics

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
)

func TestLyricsArtistScriptEquivalence(t *testing.T) {
	for _, tt := range []struct {
		query, artist string
		credit        int
		high          bool
	}{
		{"周杰倫", "周杰伦", 20, true},
		{"周杰伦", "周杰倫", 20, true},
		{"周杰伦", "周杰伦", 20, true},
		{"  周杰倫　", "周杰伦", 20, true},
		{"陳奕迅", "陈奕迅", 20, true},
		{"孫燕姿", "孙燕姿", 20, true},
		{"張學友", "张学友", 20, true},
		{"周杰倫", "林俊杰", 0, false},
		{"周杰倫", "", 0, false},
		{"", "周杰伦", 0, true},        // Album corroboration still suffices when artist is unspecified.
		{"周杰倫", "周杰伦合唱团", 0, false}, // No new fuzzy matching of converted credits.
		{"周杰倫", "周杰伦 / 林俊杰", 0, false},
		{"Crest", "ハトリアヤ", 0, false},
		{"澤野ひろゆき", "泽野ひろゆき", 0, false}, // Do not infer Chinese conversion for Japanese/Korean text.
	} {
		t.Run(tt.query+"/"+tt.artist, func(t *testing.T) {
			t.Parallel()
			q := Query{Title: "七里香", Artist: tt.query, Album: "七里香", DurationMS: 299000}
			input := []Candidate{{Ref: "1", Title: q.Title, Artist: tt.artist, Album: q.Album, DurationMS: q.DurationMS}}
			before := append([]Candidate(nil), input...)
			got := rankCandidates(q, input)
			if len(got) != 1 || got[0].MatchTerms[1].Value != tt.credit || got[0].MatchScore != 80+tt.credit || got[0].HighConfidence != tt.high {
				t.Fatalf("unexpected score: %+v", got)
			}
			if got[0].Artist != tt.artist || !reflect.DeepEqual(input, before) {
				t.Fatal("rewrote display credit or input")
			}
			if tt.credit == 20 && !strings.Contains(strings.Join(got[0].MatchNotes, ";"), "艺术家相符") {
				t.Fatal("missing exact credit note", got)
			}
		})
	}
	if matching.DefaultWeights().CompareCredit("周杰倫", "周杰伦") != matching.MatchNone {
		t.Fatal("changed metadata comparison outside lyric search")
	}
}

func TestLyricsCreditDictionary(t *testing.T) {
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(lyricCreditDictionary))); got != "6b5a0a799bea2bb22c001f635eaa3fc2904310f0c08addbff275477a80ecf09a" {
		t.Fatal("unreviewed dictionary update", got)
	}
	variants := lyricCreditVariants()
	if variants['倫'] != '伦' || variants['陳'] != '陈' || variants['乾'] != 0 {
		t.Fatal("missing or ambiguous mapping")
	}
	for _, pair := range [][2]string{{"乾", "干"}, {"發", "髮"}, {"周杰倫", "周杰伦合唱团"}, {"澤野ひろゆき", "泽野ひろゆき"}} {
		q, c := lyricCreditKeys(pair[0], pair[1])
		if q != pair[0] || c != pair[1] {
			t.Fatal("broadened conversion evidence", pair, q, c)
		}
	}
}

func TestLyricsArtistOnlyScriptEquivalence(t *testing.T) {
	q := Query{Artist: "周杰倫"}
	got := rankCandidates(q, []Candidate{{Ref: "same", Artist: "周杰伦"}, {Ref: "other", Artist: "林俊杰"}, {Ref: "missing"}})
	if len(got) != 1 || got[0].Ref != "same" || got[0].MatchScore != 20 || got[0].HighConfidence {
		t.Fatal(got)
	}
}
