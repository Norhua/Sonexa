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
	// OutcomeImportLadder 多条高置信、同属一个 release group，且过滤实体载体后
	// 恰好剩一条 → 入库。
	OutcomeImportLadder Outcome = "import_ladder"
	// OutcomeReviewCrossGroup 多条高置信但跨 release group → 进复核队列。
	OutcomeReviewCrossGroup Outcome = "review_cross_group"
	// OutcomeReviewManyReleases 多条高置信、同属一个 release group，但过滤实体载体
	// 后并非只剩一条 → 进复核队列。
	OutcomeReviewManyReleases Outcome = "review_many_releases"
	// OutcomeReviewNoMatch 无结果或全部低置信 → 进复核队列。
	OutcomeReviewNoMatch Outcome = "review_no_match"
)

// IsImport 报告该分流是否直接入库。
func (o Outcome) IsImport() bool {
	return o == OutcomeImportUnique || o == OutcomeImportLadder
}

// Decision 是分流判定的结果。
type Decision struct {
	Outcome Outcome
	// Selected 是被选中入库的候选；进复核时为 nil。
	Selected *provider.Candidate
	// HighConfidence 是全部达到高置信的候选，供复核队列展示。
	HighConfidence []provider.Candidate
}

// Decide 仅保留置信度分类供旧读取 DTO 使用；最终选版及准入由 library 执行。
// 载体和国家偏好只用于展示代表，不在这里选择入库版本。
func (cfg LadderConfig) Decide(scored []ScoredCandidate) Decision {
	var high []ScoredCandidate
	for _, s := range scored {
		if s.Score.HighConf {
			high = append(high, s)
		}
	}
	highCands := make([]provider.Candidate, len(high))
	for i, s := range high {
		highCands[i] = s.Candidate
	}

	switch len(high) {
	case 0:
		return Decision{Outcome: OutcomeReviewNoMatch}
	case 1:
		c := high[0].Candidate
		return Decision{Outcome: OutcomeImportUnique, Selected: &c, HighConfidence: highCands}
	}

	if len(GroupByReleaseGroup(highCands)) > 1 {
		return Decision{Outcome: OutcomeReviewCrossGroup, HighConfidence: highCands}
	}

	return Decision{Outcome: OutcomeReviewManyReleases, HighConfidence: highCands}
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
