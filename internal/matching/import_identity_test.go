package matching

import "testing"

func TestSameImportTextAnnotationsAndSafety(t *testing.T) {
	base := Input{Title: "ツバサ", Artist: "4円", Album: "side-c"}
	cases := []struct {
		name  string
		input Input
		want  bool
	}{
		{"译名注音", Input{Title: "ツバサ（羽翼）", Artist: "4円（ヨエン）", Album: "side-c（专辑译名）"}, true},
		{"不同专辑", Input{Title: "ツバサ", Artist: "4円", Album: "another"}, false},
		{"版本", Input{Title: "ツバサ (Live)", Artist: "4円", Album: "side-c"}, false},
		{"专辑版本", Input{Title: "ツバサ", Artist: "4円", Album: "side-c (Remix)"}, false},
		{"重制版", Input{Title: "ツバサ", Artist: "4円", Album: "side-c (Remastered)"}, false},
		{"豪华版", Input{Title: "ツバサ", Artist: "4円", Album: "side-c (Deluxe Edition)"}, false},
		{"缺专辑", Input{Title: "ツバサ", Artist: "4円"}, false},
		{"截断", Input{Title: "ツバサ", Artist: "4円", Album: "side-c..."}, false},
		{"附加艺术家", Input{Title: "ツバサ", Artist: "4円 feat. Another", Album: "side-c"}, false},
		{"同声优不同角色", Input{Title: "ツバサ", Artist: "角色(CV:4円)", Album: "side-c"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SameImportText(base, tc.input); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	one, two := 1, 2
	a, b := base, base
	a.TrackNo = &one
	b.TrackNo = &two
	if SameImportText(a, b) {
		t.Fatal("different release positions merged")
	}
}

func TestAutomaticCandidatesPreferCompleteEvidenceNotScore(t *testing.T) {
	full := highConf("studio", sptr("single"))
	full.Score.AlbumMatch = MatchExact
	full.Score.Total = 95
	partial := highConf("live", sptr("live"))
	partial.Score.Total = 100
	got := DefaultLadderConfig().Decide([]ScoredCandidate{partial, full})
	if got.Selected == nil || got.Selected.MatchRef != "studio" {
		t.Fatalf("%+v", got)
	}
	partial.Score.AlbumMatch = MatchExact
	got = DefaultLadderConfig().Decide([]ScoredCandidate{partial, full})
	if got.Selected != nil {
		t.Fatal("two fully matching groups must remain ambiguous")
	}
}
