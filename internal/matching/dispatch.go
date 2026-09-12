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
// 跨 release group 默认不自动选择：这时的分歧通常不是「同一张专辑的哪个版本」，
// 而是「到底是哪首歌」，没有客观依据可裁决，必须交用户判断。
//
// 但有两种情形是有客观依据的，可以就地裁决，不必打扰用户——它们都不是
// 「随便挑一个」，而是「用户或上游已经把答案指出来了」：
//
//	[A] 全部高置信候选其实是**同一条录音**（recording MBID 相同），只是收录在
//	    不同发行里。既然是同一首歌，选哪个发行是优选阶梯的事，不是「哪首歌」的分歧。
//	    （`you` 那条：两个候选同一 recording，分属两张合辑）
//	[B] 高置信候选里**恰好只有一条**歌集精确匹配。用户在导入行里写的歌集就是
//	    客观依据，它把答案唯一地指了出来。（`3253212` 那条：只有同名单曲的
//	    那一条歌集精确，几张 live 专辑都不精确）
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
		// [A] 同一条录音的不同发行：交优选阶梯选发行
		if sameRecording(highCands) {
			return Decision{
				Outcome:        OutcomeImportLadder,
				Selected:       cfg.SelectRelease(highCands),
				HighConfidence: highCands,
			}
		}
		// [B] 恰好一条歌集精确：用户写的歌集把答案指了出来
		if only := soleAlbumExact(high); only != nil {
			return Decision{
				Outcome:        OutcomeImportLadder,
				Selected:       only,
				HighConfidence: highCands,
			}
		}
		return Decision{Outcome: OutcomeReviewCrossGroup, HighConfidence: highCands}
	}
	return Decision{
		Outcome:        OutcomeImportLadder,
		Selected:       cfg.SelectRelease(highCands),
		HighConfidence: highCands,
	}
}

// sameRecording 报告一组候选是否全部指向同一条录音（recording MBID 相同）。
//
// 缺 recording 标识的（iTunes 无此概念）不参与判定：任一条缺失就当作无法确认。
func sameRecording(cands []provider.Candidate) bool {
	var ref string
	for i, c := range cands {
		if c.RecordingRef == nil || *c.RecordingRef == "" {
			return false
		}
		if i == 0 {
			ref = *c.RecordingRef
		} else if *c.RecordingRef != ref {
			return false
		}
	}
	return ref != ""
}

// soleAlbumExact 在一组高置信候选里找歌集精确匹配的那一条；恰好一条时返回它，
// 零条或多于一条都返回 nil（多于一条时歌集没有把答案唯一地指出来）。
func soleAlbumExact(high []ScoredCandidate) *provider.Candidate {
	var found *provider.Candidate
	for i := range high {
		if high[i].Score.AlbumMatch != MatchExact {
			continue
		}
		if found != nil {
			return nil // 不止一条精确，歌集没能唯一裁决
		}
		found = &high[i].Candidate
	}
	return found
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
