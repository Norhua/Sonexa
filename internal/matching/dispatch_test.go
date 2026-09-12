package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
)

func highConf(ref string, rg *string) ScoredCandidate {
	return ScoredCandidate{
		Candidate: provider.Candidate{
			MatchRef: ref, ReleaseGroupRef: rg,
			ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
		},
		Score: Score{TitleMatch: MatchExact, CreditMatch: MatchExact, HighConf: true, Total: 75},
	}
}

func lowConf(ref string) ScoredCandidate {
	return ScoredCandidate{
		Candidate: provider.Candidate{MatchRef: ref},
		Score:     Score{TitleMatch: MatchExact, CreditMatch: MatchFuzzy, AlbumMatch: MatchFuzzy, Total: 79},
	}
}

// TestDecideUniqueHighConfidence 覆盖「唯一高置信」→ 直接入库。
func TestDecideUniqueHighConfidence(t *testing.T) {
	cfg := DefaultLadderConfig()
	got := cfg.Decide([]ScoredCandidate{highConf("a", sptr("rg-1")), lowConf("b")})

	if got.Outcome != OutcomeImportUnique {
		t.Errorf("分流 = %q, 期望 import_unique", got.Outcome)
	}
	if got.Selected == nil || got.Selected.MatchRef != "a" {
		t.Errorf("选中 %+v, 期望 a", got.Selected)
	}
}

// TestDecideSameGroupUsesLadder 覆盖「同组多条高置信」→ 经优选阶梯入库。
func TestDecideSameGroupUsesLadder(t *testing.T) {
	cfg := DefaultLadderConfig()
	rg := sptr("rg-1")
	a := highConf("aaa", rg)
	b := highConf("bbb", rg)
	b.Candidate.MediumFormat = sptr("Digital Media") // 载体更差

	got := cfg.Decide([]ScoredCandidate{b, a})
	if got.Outcome != OutcomeImportLadder {
		t.Errorf("分流 = %q, 期望 import_ladder", got.Outcome)
	}
	if got.Selected == nil || got.Selected.MatchRef != "aaa" {
		t.Errorf("选中 %+v, 期望 aaa（CD 优于 Digital Media）", got.Selected)
	}
}

// TestDecideCrossGroupGoesToReview 覆盖 4.7：
// 跨 release group 的多个高置信不自动选择，整条进复核队列。
func TestDecideCrossGroupGoesToReview(t *testing.T) {
	cfg := DefaultLadderConfig()
	got := cfg.Decide([]ScoredCandidate{
		highConf("a", sptr("rg-1")),
		highConf("b", sptr("rg-2")),
	})

	if got.Outcome != OutcomeReviewCrossGroup {
		t.Errorf("分流 = %q, 期望 review_cross_group", got.Outcome)
	}
	if got.Selected != nil {
		t.Errorf("跨组时不应自动选择, 却选中了 %+v", got.Selected)
	}
	if len(got.HighConfidence) != 2 {
		t.Errorf("应把两条高置信都带给复核队列, 得到 %d 条", len(got.HighConfidence))
	}
}

// TestDecideAllLowConfidence 覆盖「无结果 / 全部低置信」→ 复核队列。
func TestDecideAllLowConfidence(t *testing.T) {
	cfg := DefaultLadderConfig()

	if got := cfg.Decide([]ScoredCandidate{lowConf("a"), lowConf("b")}); got.Outcome != OutcomeReviewNoMatch {
		t.Errorf("全部低置信分流 = %q, 期望 review_no_match", got.Outcome)
	}
	if got := cfg.Decide(nil); got.Outcome != OutcomeReviewNoMatch {
		t.Errorf("无结果分流 = %q, 期望 review_no_match", got.Outcome)
	}
}

// TestWeightsFromSettings 覆盖 4.9：改动阈值后打分结果随之变化。
func TestWeightsFromSettings(t *testing.T) {
	q := provider.Query{Title: "X", Artist: "Y", Album: "Z"}
	c := provider.Candidate{Title: sptr("X"), ArtistName: sptr("Y"), AlbumTitle: sptr("Z")}

	defaults := config.Defaults().Matching
	base := WeightsFromSettings(defaults).ScoreCandidate(q, c)
	if base.Total != 95 {
		t.Fatalf("默认权重下得分 = %v, 期望 95", base.Total)
	}

	// 调高曲名权重
	tweaked := defaults
	tweaked.TitleWeight = 70
	got := WeightsFromSettings(tweaked).ScoreCandidate(q, c)
	if got.Total != 110 {
		t.Errorf("调整曲名权重后得分 = %v, 期望 110", got.Total)
	}
	if got.Total == base.Total {
		t.Error("改动阈值后打分结果应随之变化")
	}
}

// TestLadderFromSettings 覆盖偏好国家与载体优先级可调。
func TestLadderFromSettings(t *testing.T) {
	settings := config.Defaults().Matching
	settings.PreferredCountries = []string{"DE"}
	settings.MediumPriority = []string{"Vinyl", "CD"}

	cfg := LadderFromSettings(settings)
	official := sptr("Official")
	got := cfg.SelectRelease([]provider.Candidate{
		{MatchRef: "cd-jp", ReleaseStatus: official, MediumFormat: sptr("CD"), Country: sptr("JP")},
		{MatchRef: "vinyl-de", ReleaseStatus: official, MediumFormat: sptr("Vinyl"), Country: sptr("DE")},
	})
	if got.MatchRef != "vinyl-de" {
		t.Errorf("选中 %q, 期望 vinyl-de（设置把 Vinyl 与 DE 排在前）", got.MatchRef)
	}
}

// TestCrossGroupYieldsToSameRecording 覆盖跨发行组的例外 [A]：
// 全部高置信候选是同一条录音时，分歧只是「收录在哪张碟」，交优选阶梯裁决。
func TestCrossGroupYieldsToSameRecording(t *testing.T) {
	cfg := DefaultLadderConfig()
	rec := "2283ceda-0000-0000-0000-000000000000"
	scored := []ScoredCandidate{
		{
			Candidate: provider.Candidate{
				MatchRef: "a", RecordingRef: &rec,
				ReleaseGroupRef: sptr("rg-1"), ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
			},
			Score: Score{HighConf: true, AlbumMatch: MatchFuzzy},
		},
		{
			Candidate: provider.Candidate{
				MatchRef: "b", RecordingRef: &rec,
				ReleaseGroupRef: sptr("rg-2"), ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
			},
			Score: Score{HighConf: true, AlbumMatch: MatchNone},
		},
	}

	got := cfg.Decide(scored)
	if got.Outcome != OutcomeImportLadder {
		t.Errorf("分流 = %q, 期望 import_ladder（同一条录音不是「哪首歌」的分歧）", got.Outcome)
	}
	if got.Selected == nil {
		t.Fatal("应选出一条")
	}
}

// TestCrossGroupYieldsToSoleAlbumExact 覆盖跨发行组的例外 [B]：
// 恰好一条歌集精确时，用户在导入行里写的歌集就是客观依据。
func TestCrossGroupYieldsToSoleAlbumExact(t *testing.T) {
	cfg := DefaultLadderConfig()
	scored := []ScoredCandidate{
		{
			Candidate: provider.Candidate{
				MatchRef: "single", RecordingRef: sptr("rec-1"),
				ReleaseGroupRef: sptr("rg-1"), AlbumTitle: sptr("3253212"),
			},
			Score: Score{HighConf: true, AlbumMatch: MatchExact},
		},
		{
			Candidate: provider.Candidate{
				MatchRef: "live", RecordingRef: sptr("rec-2"),
				ReleaseGroupRef: sptr("rg-2"), AlbumTitle: sptr("GaLLERy on live"),
			},
			Score: Score{HighConf: true, AlbumMatch: MatchNone},
		},
	}

	got := cfg.Decide(scored)
	if got.Outcome != OutcomeImportLadder {
		t.Errorf("分流 = %q, 期望 import_ladder", got.Outcome)
	}
	if got.Selected == nil || got.Selected.MatchRef != "single" {
		t.Errorf("选中 = %v, 期望歌集精确的那一条", got.Selected)
	}
}

// TestCrossGroupStillReviewsWhenAmbiguous 确保两个例外都没有把规则整体放松：
// 不同录音、且歌集无法唯一裁决时，仍须交用户判断。
func TestCrossGroupStillReviewsWhenAmbiguous(t *testing.T) {
	cfg := DefaultLadderConfig()

	t.Run("都不精确", func(t *testing.T) {
		scored := []ScoredCandidate{
			{
				Candidate: provider.Candidate{MatchRef: "a", RecordingRef: sptr("rec-1"), ReleaseGroupRef: sptr("rg-1")},
				Score:     Score{HighConf: true, AlbumMatch: MatchFuzzy},
			},
			{
				Candidate: provider.Candidate{MatchRef: "b", RecordingRef: sptr("rec-2"), ReleaseGroupRef: sptr("rg-2")},
				Score:     Score{HighConf: true, AlbumMatch: MatchFuzzy},
			},
		}
		if got := cfg.Decide(scored); got.Outcome != OutcomeReviewCrossGroup {
			t.Errorf("分流 = %q, 期望 review_cross_group", got.Outcome)
		}
	})

	t.Run("两条都精确", func(t *testing.T) {
		scored := []ScoredCandidate{
			{
				Candidate: provider.Candidate{MatchRef: "a", RecordingRef: sptr("rec-1"), ReleaseGroupRef: sptr("rg-1")},
				Score:     Score{HighConf: true, AlbumMatch: MatchExact},
			},
			{
				Candidate: provider.Candidate{MatchRef: "b", RecordingRef: sptr("rec-2"), ReleaseGroupRef: sptr("rg-2")},
				Score:     Score{HighConf: true, AlbumMatch: MatchExact},
			},
		}
		if got := cfg.Decide(scored); got.Outcome != OutcomeReviewCrossGroup {
			t.Errorf("分流 = %q, 期望 review_cross_group（歌集没能唯一裁决）", got.Outcome)
		}
	})
}
