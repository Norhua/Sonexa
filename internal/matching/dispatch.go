package matching

import (
	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
)

// Outcome 是一条待匹配条目在上游查询结束后的分流结果。
type Outcome string

const (
	// OutcomeImportUnique 仅一条高置信结果 → 直接入库。
	OutcomeImportUnique Outcome = "import_unique"
	// OutcomeImportLadder 多条高置信且同属一个 release group → 经优选阶梯选出后入库。
	OutcomeImportLadder Outcome = "import_ladder"
	// OutcomeReviewCrossGroup 多条高置信但跨 release group → 进复核队列。
	OutcomeReviewCrossGroup Outcome = "review_cross_group"
	// OutcomeReviewNoMatch 无结果或全部低置信 → 进复核队列。
	OutcomeReviewNoMatch Outcome = "review_no_match"
)

// Decision 是分流判定的结果。
type Decision struct {
	Outcome Outcome
	// Selected 是被选中入库的候选；进复核时为 nil。
	Selected *provider.Candidate
	// HighConfidence 是全部达到高置信的候选，供复核队列展示。
	HighConfidence []provider.Candidate
}

// Decide 按分流规则判定一条目的去向。
//
//	仅一条高置信                      → 直接入库
//	多条高置信 & 同一 release group   → 优选阶梯选出一条后入库
//	多条高置信 & 跨 release group     → 复核队列（不自动选择）
//	无结果 / 全部低置信               → 复核队列
//
// 跨 release group 不自动选择：这时的分歧不是「同一张专辑的哪个版本」，
// 而是「到底是哪首歌」，没有客观依据可裁决，必须交用户判断。
func (cfg LadderConfig) Decide(scored []ScoredCandidate) Decision {
	var high []provider.Candidate
	for _, s := range scored {
		if s.Score.HighConf {
			high = append(high, s.Candidate)
		}
	}

	switch len(high) {
	case 0:
		return Decision{Outcome: OutcomeReviewNoMatch}
	case 1:
		c := high[0]
		return Decision{Outcome: OutcomeImportUnique, Selected: &c, HighConfidence: high}
	}

	if len(GroupByReleaseGroup(high)) > 1 {
		return Decision{Outcome: OutcomeReviewCrossGroup, HighConfidence: high}
	}
	return Decision{
		Outcome:        OutcomeImportLadder,
		Selected:       cfg.SelectRelease(high),
		HighConfidence: high,
	}
}

// ScoredCandidate 是一条候选及其评分。
type ScoredCandidate struct {
	Candidate provider.Candidate
	Score     Score
	// MatchedVariant 记录取得该分值的查询变体，供界面展示。
	MatchedVariant string
}

// WeightsFromSettings 由设置构造评分权重，使阈值改动即时生效。
func WeightsFromSettings(m config.MatchingSettings) Weights {
	w := DefaultWeights()
	if m.TitleWeight > 0 {
		w.Title = m.TitleWeight
	}
	if m.CreditExactWeight > 0 {
		w.CreditExact = m.CreditExactWeight
	}
	if m.CreditFuzzyWeight > 0 {
		w.CreditFuzzy = m.CreditFuzzyWeight
	}
	if m.AlbumExactWeight > 0 {
		w.AlbumExact = m.AlbumExactWeight
	}
	if m.AlbumFuzzyWeight > 0 {
		w.AlbumFuzzy = m.AlbumFuzzyWeight
	}
	if m.UpstreamScoreScale > 0 {
		w.ScoreScale = m.UpstreamScoreScale
	}
	if m.UpstreamScoreCap > 0 {
		w.ScoreCap = m.UpstreamScoreCap
	}
	if m.JaroWinklerMin > 0 {
		w.JaroWinklerMin = m.JaroWinklerMin
	}
	return w
}

// LadderFromSettings 由设置构造优选阶梯配置。
func LadderFromSettings(m config.MatchingSettings) LadderConfig {
	cfg := DefaultLadderConfig()
	if len(m.PreferredCountries) > 0 {
		cfg.PreferredCountries = m.PreferredCountries
	}
	if len(m.MediumPriority) > 0 {
		cfg.MediumPriority = m.MediumPriority
	}
	return cfg
}

// StripPenaltyFromSettings 返回兜底剥离变体的惩罚分。
func StripPenaltyFromSettings(m config.MatchingSettings) float64 {
	if m.StripPenalty > 0 {
		return m.StripPenalty
	}
	return StripPenalty
}
