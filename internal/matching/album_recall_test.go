package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

// These are search hints, not album identity equivalence. In particular, CJK
// export descriptions often have no spaces around the complete release title.
func TestMetadataAlbumPhraseRecall(t *testing.T) {
	for _, tt := range []struct {
		name, query, candidate string
		keep                   bool
	}{
		{"screenshot spaced", "TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ-BEST-", "灼眼のシャナ -BEST-", true},
		{"screenshot unspaced", "TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ-BEST-", "灼眼のシャナ-BEST-", true},
		{"other Japanese description", "劇場版アニメ主題歌集星空の記憶", "星空の記憶", true},
		{"Chinese description", "电视剧原声精选时光的足迹", "时光的足迹", true},
		{"Korean description", "드라마음악모음푸른바다기억", "푸른바다기억", true},
		{"Latin suffix", "Original Game Soundtrack — Moon Garden", "Moon Garden", true},
		{"Latin prefix", "Moon Garden: Music from the Motion Picture", "Moon Garden", true},
		{"fullwidth kana", "TVアニメ音楽集ホシノキオク", "ﾎｼﾉｷｵｸ", true},
		{"generic English", "Television BEST", "BEST", false},
		{"generic multiword", "Songs: The Greatest Hits", "The Greatest Hits", false},
		{"generic Japanese", "TVアニメベストアルバム", "ベストアルバム", false},
		{"generic OST", "シリーズサウンドトラック", "サウンドトラック", false},
		{"volume only", "Series Vol. 12345", "Vol. 12345", false},
		{"short Japanese", "テレビ音楽夜空", "夜空", false},
		{"Latin embedded prefix", "Moon Gardenside", "Moon Garden", false},
		{"Latin embedded suffix", "BlueMoon Garden", "Moon Garden", false},
		{"Latin script boundary", "Moon Garden2", "Moon Garden", false},
		{"CJK sequel number", "テレビ音楽集星空の記憶2", "星空の記憶", false},
		{"short Hangul", "드라마음악바다", "바다", false},
		{"numbers do not meet length", "Collection Ab12", "Ab12", false},
		{"empty", "", "Moon Garden", false},
		{"no shared text", "テレビ主題歌集", "星空の記憶", false},
		{"noncontiguous fragments", "Moon Far Away Garden", "Moon Garden", false},
		{"truncated long", "TVアニメ音楽集星空の記憶...", "星空の記憶", false},
		{"truncated short", "TVアニメ音楽集星空の記憶", "星空の記憶…", false},
		{"live edition", "TVアニメ音楽集星空の記憶 (Live)", "星空の記憶", false},
		{"deluxe edition", "TVアニメ音楽集星空の記憶 (Deluxe Edition)", "星空の記憶", false},
		{"bare edition", "TVアニメ音楽集星空の記憶 - Remastered", "星空の記憶", false},
		{"different bracket editions", "TVアニメ音楽集星空の記憶 (20th Anniversary)", "星空の記憶 (10th Anniversary)", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, pair := range [][2]string{{tt.query, tt.candidate}, {tt.candidate, tt.query}} {
				if got := metadataAlbumPhrase(pair[0], pair[1]); got != tt.keep {
					t.Errorf("%q / %q: got %t, want %t", pair[0], pair[1], got, tt.keep)
				}
			}
		})
	}
}

func TestMetadataAlbumRecallIsNotIdentity(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{Title: "portamento", Artist: "川田麻美", Album: "TVアニメ「灼眼のシャナ」ベストアルバム灼眼のシャナ-BEST-"}
	c := provider.Candidate{Title: sptr("portamento"), ArtistName: sptr("川田まみ"), AlbumTitle: sptr("灼眼のシャナ -BEST-")}
	strict := w.ScoreCandidate(q, c)
	if strict.Plausible || strict.CreditMatch != MatchNone || strict.AlbumMatch != MatchNone {
		t.Fatalf("strict identity/lyrics scoring changed: %+v", strict)
	}
	s := w.ScoreMetadataCandidate(q, c)
	if s.Rejected || !s.Plausible || !s.ReviewOnly || s.HighConf || s.AlbumMatch != MatchFuzzy || s.CreditMatch != MatchNone || s.Total != w.Title+w.AlbumFuzzy {
		t.Fatalf("complete album phrase should corroborate manual recall only: %+v", s)
	}
	input := Input{Title: q.Title, Artist: q.Artist, Album: q.Album}
	candidate := Input{Title: *c.Title, Artist: *c.ArtistName, Album: *c.AlbumTitle}
	if SameImportText(input, candidate) || MatchOwnedInput(input, candidate, true).Rank != 0 || len(AutomaticCandidates([]ScoredCandidate{{Candidate: c, Score: s}})) != 0 {
		t.Fatal("manual album hint became automatic identity evidence")
	}
	for _, title := range []string{"portamenta", "portamento (Live)", "different song", "portamento...", ""} {
		q.Title = title
		if got := w.ScoreMetadataCandidate(q, c); !got.Rejected && got.Plausible {
			t.Fatalf("album phrase must require exact requested title, got %q: %+v", title, got)
		}
	}
	// Two weak rescues must not combine: missing-version title + album phrase.
	q.Title = "portamento"
	c.Title = sptr("portamento (Live)")
	if got := w.ScoreMetadataCandidate(q, c); !got.Rejected && got.Plausible {
		t.Fatalf("album hint compounded with missing-version recall: %+v", got)
	}
	c.Title, c.SearchTitles = sptr("different song"), []string{"portamento"}
	if got := w.ScoreMetadataCandidate(q, c); !got.Rejected && got.Plausible {
		t.Fatalf("album hint compounded with alternate-title recall: %+v", got)
	}
}

func TestMetadataAlbumRecallKeepsStrictCandidatesUnchanged(t *testing.T) {
	w := DefaultWeights()
	for _, q := range []provider.Query{
		{Title: "Song", Artist: "Artist", Album: "Moon Garden"},
		{Title: "Song", Artist: "Artist", Album: "Television collection Moon Garden"},
		{Title: "Song"},
	} {
		c := provider.Candidate{Title: sptr("Song"), ArtistName: sptr("Artist"), AlbumTitle: sptr("Moon Garden")}
		strict, metadata := w.ScoreCandidate(q, c), w.ScoreMetadataCandidate(q, c)
		if !strict.Plausible || strict != metadata {
			t.Fatalf("existing scoring changed: strict=%+v metadata=%+v", strict, metadata)
		}
	}
}
