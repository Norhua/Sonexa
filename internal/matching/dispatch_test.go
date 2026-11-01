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

// TestDecideUniqueHighConfidence 仅验证旧展示分类，不授权自动入库。
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

// TestDecideSameGroupSolePhysical 验证展示载体偏好不参与自动选版。
func TestDecideSameGroupSolePhysical(t *testing.T) {
	cfg := DefaultLadderConfig()
	rg := sptr("rg-1")
	cd, digital := highConf("aaa", rg), highConf("bbb", rg)
	digital.Candidate.MediumFormat = sptr("Digital Media")
	got := cfg.Decide([]ScoredCandidate{digital, cd})
	if got.Selected != nil || got.Outcome != OutcomeReviewManyReleases {
		t.Fatal("display medium preference selected admission", got)
	}
}

// TestDecideSameGroupManyPhysicalGoesToReview 保留多发行展示分类。
// 自动选版由 library 按完整输入与总曲数等事实决定，不消费此 Outcome。
func TestDecideSameGroupManyPhysicalGoesToReview(t *testing.T) {
	cfg := DefaultLadderConfig()
	rg := sptr("rg-4")
	var scored []ScoredCandidate
	for _, ref := range []string{"t4-a", "t4-b", "t4-c"} {
		scored = append(scored, highConf(ref, rg))
	}

	got := cfg.Decide(scored)
	if got.Outcome != OutcomeReviewManyReleases {
		t.Errorf("分流 = %q, 期望 review_many_releases", got.Outcome)
	}
	if got.Selected != nil {
		t.Errorf("无客观依据时不应自动选择, 却选中了 %+v", got.Selected)
	}
	if len(got.HighConfidence) != 3 {
		t.Errorf("应把三条高置信都带给复核队列, 得到 %d 条", len(got.HighConfidence))
	}
}

// TestDecideSameGroupNoPhysicalGoesToReview 保留缺少载体时的展示分类。
func TestDecideSameGroupNoPhysicalGoesToReview(t *testing.T) {
	cfg := DefaultLadderConfig()
	rg := sptr("rg-5")
	a, b := highConf("a", rg), highConf("b", rg)
	a.Candidate.MediumFormat = sptr("Digital Media")
	b.Candidate.MediumFormat = nil // 上游没给载体信息

	if got := cfg.Decide([]ScoredCandidate{a, b}); got.Outcome != OutcomeReviewManyReleases {
		t.Errorf("分流 = %q, 期望 review_many_releases", got.Outcome)
	}
}

// TestDecideCrossGroupGoesToReview 仅验证未筛选展示池的旧分类。
// 真实输入优先分流见 importer/input_priority_test.go，不以这里决定复核。
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
