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

// Decide 按分流规则判定一条目的去向。
//
//	仅一条高置信                              → 直接入库
//	多条高置信 & 同组 & 过滤实体载体后剩一条    → 入库（那一条）
//	多条高置信 & 同组 & 过滤后剩 0 条或多条     → 复核队列
//	多条高置信 & 跨 release group             → 复核队列
//	无结果 / 全部低置信                       → 复核队列
//
// **多条高置信一律不自动裁决**，这是整条规则的骨架，没有例外：
//
// 跨发行组时的分歧通常不是「同一张专辑的哪个版本」而是「到底是哪首歌」；
// 同一发行组内剩下多条时（`世界中の雨` 在 MusicBrainz 里有 11 张同名 CD 发行，
// 载体、国家、发行日、发行状态全同）没有任何客观依据能分出高下，
// 阶梯只能靠 MBID 字典序收尾——那是「随便挑一个」，不是判定。
//
// 曾经为跨发行组开过两个例外（全部候选同一 recording、恰好一条歌集精确），
// 它们把 `you`、`3253212` 两行从复核提到了入库，但代价是规则不再可预期：
// 用户读到「多条高置信 → 复核」，实现却在某些情况下替他做了决定。
// 有歧义就交人工，是这套规则唯一的收尾方式。
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

	// 同一发行组：先过滤载体，只留实体发行。剩唯一一条才入库。
	//
	// 「剩 0 条」（全是数字发行）与「剩多条」（同一张碟在上游有多份登记）
	// 都是没有客观依据可裁决的局面，一并交复核。
	physical := cfg.PhysicalOnly(highCands)
	if len(physical) == 1 {
		return Decision{
			Outcome:        OutcomeImportLadder,
			Selected:       &physical[0],
			HighConfidence: highCands,
		}
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
