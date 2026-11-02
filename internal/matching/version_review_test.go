package matching

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestMetadataRecallIsReviewOnly(t *testing.T) {
	w := DefaultWeights()
	for _, tt := range []struct {
		name, query, title, artist, album string
		alternates                        []string
		keep                              bool
	}{
		{"translated query", "歌曲（Song）", "Song (Live)", "Artist", "Album", nil, true},
		{"missing corroboration fields", "Song", "Song (Live)", "", "", nil, true},
		{"truncated base", "Song...", "Song (Live)", "Artist", "Album", nil, false},
		{"dash version", "Song", "Song -Singer Ver.-", "Artist", "Album", nil, true},
		{"bracket version", "Song", "Song (Singer Version)", "Artist", "Album", nil, true},
		{"instrumental", "Song", "Song -Instrumental-", "Artist", "Album", nil, true},
		{"multiple versions", "Song", "Song -movie ver.- (off vocal)", "Artist", "Album", nil, true},
		{"fullwidth", "Song", "Song（Ｓｉｎｇｅｒ Ｖｅｒ．）", "Artist", "Album", nil, true},
		{"CJK boundary", "悲しくて lulululu", "悲しくて lulululu -ハーサカver.-", "Artist", "Album", nil, true},
		{"source title", "歌曲", "Ge Qu", "Artist", "Album", []string{"歌曲"}, true},
		{"different base", "Song", "Another Song (Live)", "Artist", "Album", nil, false},
		{"fuzzy base", "Song", "Songs (Live)", "Artist", "Album", nil, false},
		{"bare version word", "Let Die", "Live and Let Die", "Artist", "Album", nil, false},
		{"explicit version", "Song (Live)", "Song (Remix)", "Artist", "Album", nil, false},
		{"reverse missing version", "Song (Live)", "Song", "Artist", "Album", nil, false},
		{"alternate cannot erase conflict", "Song (Live)", "Song (Remix)", "Artist", "Album", []string{"Song (Live)"}, false},
		{"no corroboration", "Song", "Song (Live)", "Someone Else", "Elsewhere", nil, false},
		{"short title not substring", "A", "Another (Live)", "Artist", "Album", nil, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			q := provider.Query{Title: tt.query, Artist: "Artist", Album: "Album"}
			c := provider.Candidate{Title: sptr(tt.title), ArtistName: sptr(tt.artist), AlbumTitle: sptr(tt.album), SearchTitles: tt.alternates}
			s := w.ScoreMetadataCandidate(q, c)
			if (!s.Rejected && s.Plausible) != tt.keep || s.HighConf || tt.keep && !s.ReviewOnly {
				t.Fatalf("score=%+v keep=%t", s, tt.keep)
			}
			if w.ScoreCandidate(q, c).Plausible {
				t.Fatal("strict scoring unexpectedly adopted recall policy")
			}
			if SameImportText(Input{Title: q.Title, Artist: q.Artist, Album: q.Album}, Input{Title: tt.title, Artist: tt.artist, Album: tt.album}) {
				t.Fatal("review recall became identity equivalence")
			}
		})
	}
}

func TestHanCreditSearchEvidenceDoesNotChangeIdentity(t *testing.T) {
	for _, names := range [][2]string{
		{"早坂爱", "早坂 愛(CV.花守ゆみり)"}, {"刘德华", "劉德華"}, {"张学友", "張學友"},
	} {
		q := provider.Query{Title: "Song", Artist: names[0]}
		c := provider.Candidate{Title: sptr("Song"), ArtistName: sptr(names[1])}
		s := DefaultWeights().ScoreMetadataCandidate(q, c)
		if !s.Plausible || s.CreditMatch != MatchFuzzy || !s.ReviewOnly || s.HighConf {
			t.Fatalf("%v: %+v", names, s)
		}
		if Normalize(names[0]) == Normalize(names[1]) || DefaultWeights().CompareCredit(names[0], names[1]) == MatchExact {
			t.Fatal("search folding changed exact identity")
		}
	}
	if hanCreditEquivalent("早坂爱", "早坂優") || hanCreditEquivalent("", "") || hanCreditEquivalent("乾", "干") || hanCreditEquivalent("發", "髮") {
		t.Fatal("different, empty or ambiguous Han names used as corroboration")
	}
}

func TestReviewCandidatesNeverBecomeAutomatic(t *testing.T) {
	for _, onlyReview := range []bool{true, false} {
		version := highConfCandidate("version", "Song (Live)", "Artist", "Album", sptr("group"))
		candidates := []provider.Candidate{version}
		if !onlyReview {
			candidates = append(candidates, highConfCandidate("original", "Song", "Artist", "Album", sptr("group")))
		}
		s := &stubSearcher{byTitle: map[string][]provider.Candidate{"Song": candidates}}
		res, err := testEngine().Run(context.Background(), s, nil, Input{Title: "Song", Artist: "Artist", Album: "Album"})
		if err != nil || len(res.Scored) != len(candidates) || len(s.queries) != 1 {
			t.Fatal(res, err, s.queries)
		}
		if onlyReview && res.Decision.Selected != nil || !onlyReview && (res.Decision.Selected == nil || res.Decision.Selected.MatchRef != "original") {
			t.Fatal(res.Decision)
		}
	}
	// Defense in depth if a serialized score accidentally says HighConf=true.
	if got := AutomaticCandidates([]ScoredCandidate{{Score: Score{HighConf: true, ReviewOnly: true}}}); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestCJKVersionMarkersAreRetained(t *testing.T) {
	for _, title := range []string{"曲 (ハーサカver.)", "曲 -ハーサカver.-", "曲 (Ｓｉｎｇｅｒ Ｖｅｒ．)"} {
		if len(VersionMarkers(title)) == 0 || SearchCore(title) != title || DefaultWeights().CompareTitle("曲", title) != MatchNone {
			t.Fatal(title)
		}
	}
}
