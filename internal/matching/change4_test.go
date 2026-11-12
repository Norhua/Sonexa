package matching

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

// P1：首级只有仅供人工查看的候选时阶梯继续推进，下一级的精确候选可被自动选中。
func TestLadderContinuesPastReviewOnlyPool(t *testing.T) {
	live := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "live", Title: sptr("雪 (Live)"), ArtistName: sptr("歌手"), AlbumTitle: sptr("专辑"), ReleaseGroupRef: sptr("rg"), RecordingRef: sptr("rec-live"), ReleaseRef: sptr("rel")}
	real := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "real", Title: sptr("Yuki"), ArtistName: sptr("歌手"), AlbumTitle: sptr("专辑"), ReleaseGroupRef: sptr("rg"), RecordingRef: sptr("rec-real"), ReleaseRef: sptr("rel")}
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{"雪": {live}, "Yuki": {real}}}
	res, err := testEngine().Run(context.Background(), s, Input{Title: "雪（Yuki）", Artist: "歌手", Album: "专辑"})
	if err != nil {
		t.Fatal(err)
	}
	auto := AutomaticCandidates(res.Scored)
	if len(s.queries) != 2 || len(auto) != 1 || auto[0].Candidate.MatchRef != "real" {
		t.Fatalf("queries=%d auto=%v tried=%v", len(s.queries), auto, res.TriedVariants)
	}
}

// P4：模糊曲名不可自动入库，也不稀释精确候选。
func TestAutomaticCandidatesRequireExactTitle(t *testing.T) {
	w := DefaultWeights()
	q := provider.Query{Title: "Interlude", Artist: "Artist"}
	exact := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "a", Title: sptr("Interlude"), ArtistName: sptr("Artist")}
	fuzzy := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "b", Title: sptr("Interlude 2"), ArtistName: sptr("Artist")}
	score := func(c provider.Candidate) ScoredCandidate {
		return ScoredCandidate{Candidate: c, Score: w.ScoreMetadataCandidate(q, c)}
	}
	if f := score(fuzzy); !f.Score.HighConf || f.Score.TitleMatch != MatchFuzzy {
		t.Fatal("fixture no longer fuzzy high-confidence", f.Score)
	}
	if got := AutomaticCandidates([]ScoredCandidate{score(fuzzy)}); len(got) != 0 {
		t.Fatal("fuzzy title became automatic", got)
	}
	got := AutomaticCandidates([]ScoredCandidate{score(exact), score(fuzzy)})
	if len(got) != 1 || got[0].Candidate.MatchRef != "a" {
		t.Fatal(got)
	}
}

// P11：不同来源的相同组标识不是同一组。
func TestGroupKeyIncludesProvider(t *testing.T) {
	mb := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "t1", ReleaseGroupRef: sptr("same")}
	qq := provider.Candidate{Provider: provider.QQMusic, MatchRef: "t2", ReleaseGroupRef: sptr("same")}
	if n := len(GroupByReleaseGroup([]provider.Candidate{mb, qq})); n != 2 {
		t.Fatal(n)
	}
}
