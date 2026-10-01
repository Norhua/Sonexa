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

// TestDecideSameGroupSolePhysical 覆盖规则 3：同组多条高置信，
// 过滤实体载体后恰好剩一条 → 入库那一条。
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

// TestDecideSameGroupManyPhysicalGoesToReview 覆盖规则 3 的收尾：
// 同一发行组内过滤实体载体后仍剩多条时不自动裁决。
//
// 这是用户报告的那条 `世界中の雨`：MusicBrainz 里同一条录音挂在 11 张
// 同名 CD 发行上，载体、国家、发行日、发行状态全同。优选阶梯只能靠 MBID
// 字典序收尾，那是「随便挑一个」——必须交人工。
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

// TestDecideSameGroupNoPhysicalGoesToReview 覆盖规则 3 的另一端：
// 同组多条但一条实体发行都没有时，同样没有裁决依据。
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

// TestCrossGroupNeverAutoSelects 守住规则 4 没有例外。
//
// 曾经为跨发行组开过两个就地裁决的口子：全部候选同属一条 recording、
// 恰好一条候选的歌集精确匹配。它们把 `you`、`3253212` 两行从复核提到了入库，
// 代价是规则不再可预期——用户读到「多条高置信 → 复核」，实现却替他做了决定。
// 两个口子都已撤销，这条用例是它们不会被重新打开的守卫。
func TestCrossGroupNeverAutoSelects(t *testing.T) {
	cfg := DefaultLadderConfig()
	rec := "2283ceda-0000-0000-0000-000000000000"

	t.Run("全部候选同一条recording", func(t *testing.T) {
		scored := []ScoredCandidate{
			{
				Candidate: provider.Candidate{
					MatchRef: "a", RecordingRef: &rec, ReleaseGroupRef: sptr("rg-1"),
					ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
				},
				Score: Score{HighConf: true, AlbumMatch: MatchFuzzy},
			},
			{
				Candidate: provider.Candidate{
					MatchRef: "b", RecordingRef: &rec, ReleaseGroupRef: sptr("rg-2"),
					ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
				},
				Score: Score{HighConf: true, AlbumMatch: MatchNone},
			},
		}
		got := cfg.Decide(scored)
		if got.Outcome != OutcomeReviewCrossGroup {
			t.Errorf("分流 = %q, 期望 review_cross_group", got.Outcome)
		}
		if got.Selected != nil {
			t.Errorf("跨发行组不应自动选择, 却选中了 %+v", got.Selected)
		}
	})

	t.Run("恰好一条歌集精确", func(t *testing.T) {
		scored := []ScoredCandidate{
			{
				Candidate: provider.Candidate{
					MatchRef: "single", RecordingRef: sptr("rec-1"),
					ReleaseGroupRef: sptr("rg-1"), AlbumTitle: sptr("3253212"),
					ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
				},
				Score: Score{HighConf: true, AlbumMatch: MatchExact},
			},
			{
				Candidate: provider.Candidate{
					MatchRef: "live", RecordingRef: sptr("rec-2"),
					ReleaseGroupRef: sptr("rg-2"), AlbumTitle: sptr("GaLLERy on live"),
					ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
				},
				Score: Score{HighConf: true, AlbumMatch: MatchNone},
			},
		}
		got := cfg.Decide(scored)
		if got.Outcome != OutcomeReviewCrossGroup {
			t.Errorf("分流 = %q, 期望 review_cross_group", got.Outcome)
		}
		if got.Selected != nil {
			t.Errorf("歌集精确不足以替用户裁决, 却选中了 %+v", got.Selected)
		}
	})
}
