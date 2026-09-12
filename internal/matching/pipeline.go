package matching

import (
	"context"
	"fmt"
	"sort"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
)

// Searcher 是管线所需的上游查询能力。
//
// 只依赖这一个方法而非整个 Provider 接口，使管线可对着桩件测试。
type Searcher interface {
	SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error)
}

// Input 是一条待匹配条目的输入。
type Input struct {
	Title      string
	Artist     string
	Album      string
	DurationMS *int
}

// Result 是一条目的匹配结果。
type Result struct {
	Decision Decision
	// Scored 是候选池，按分值降序。
	Scored []ScoredCandidate
	// TriedVariants 是已尝试的查询变体，供复核队列显示。
	TriedVariants []string
	// LocalDuplicate 非零时表示本地去重检查判定为疑似重复。
	LocalDuplicate int64
}

// Engine 把文本处理、评分、优选阶梯串成一条管线。
type Engine struct {
	Rules    *Rules
	Weights  Weights
	Ladder   LadderConfig
	Strategy config.VariantStrategy
}

// NewEngine 按设置构造管线。
func NewEngine(m config.MatchingSettings) *Engine {
	return &Engine{
		Rules:    DefaultRules(),
		Weights:  WeightsFromSettings(m),
		Ladder:   LadderFromSettings(m),
		Strategy: m.VariantStrategy,
	}
}

// Run 对一条输入执行完整的匹配流程。
//
//	[1] 本地去重检查 —— 疑似重复直接进复核，库中曲目作为一条候选
//	[2] 上游查询 —— 按变体策略执行（默认渐进式）
//	[3] 置信度评分
//	[4] 分流
func (e *Engine) Run(ctx context.Context, s Searcher, lookup LocalLookup, in Input) (*Result, error) {
	res := &Result{}

	variants := e.Rules.GenerateVariants(in.Title, in.Artist, in.Album)
	if len(variants) == 0 {
		variants = []Variant{{Title: in.Title, Artist: in.Artist, Album: in.Album, Label: "原串"}}
	}

	// 候选池按 match_ref 去重，每条取跨变体的最高分
	pool := map[string]ScoredCandidate{}

	for _, v := range variants {
		q := provider.Query{
			Title:            v.Title,
			Artist:           v.Artist,
			Album:            v.Album,
			ArtistAlternates: v.ArtistAlternates,
			TitleOnly:        v.TitleOnly,
			DurationMS:       in.DurationMS,
		}
		// 附加艺术家（feat. X）并入艺术家条件
		if len(v.ExtraArtists) > 0 && q.Artist == "" {
			q.Artist = v.ExtraArtists[0]
		}

		candidates, err := s.SearchTracks(ctx, q)
		res.TriedVariants = append(res.TriedVariants, v.Title)
		if err != nil {
			return nil, err
		}

		for _, c := range candidates {
			// 评分用**原始输入**而不是剥离后的变体：变体是为了查得到，
			// 判定「查对了没有」要拿用户给的那一行去比。
			score := e.Weights.ScoreCandidate(provider.Query{
				Title: in.Title, Artist: in.Artist, Album: in.Album, DurationMS: in.DurationMS,
			}, c)
			if score.Rejected {
				continue // 曲名不匹配即丢弃，不进候选池
			}

			key := string(c.Provider) + "|" + c.MatchRef
			if existing, ok := pool[key]; ok && existing.Score.Total >= score.Total {
				continue // 保留跨变体的最高分
			}
			pool[key] = ScoredCandidate{
				Candidate:      c,
				Score:          score,
				MatchedVariant: v.Label,
			}
		}

		// 阶梯的推进条件是「这一级有没有过得了闸的候选」，而不是「上游返回了几条」。
		//
		// 这个区别是实测逼出来的：`That Girl (DJLIU Remix)` 的锚定级会返回几百条
		// 同名的 `That Girl`，一条都不是那个 remix。按「返回了就停」，
		// 阶梯会停在一堆用不上的候选上，更宽的一级永远不会被试到。
		if len(pool) > 0 {
			if e.Strategy != config.VariantExhaustive || v.TitleOnly {
				break
			}
		}
	}

	res.Scored = sortedPool(pool)
	res.Decision = e.Ladder.Decide(res.Scored)
	return res, nil
}

// CheckLocalDuplicate 执行本地去重前置检查。
//
// 判定为疑似重复时该条目进复核队列，且库中那首曲目作为一条候选
// 出现在同一列表里——不新建曲目。
func (e *Engine) CheckLocalDuplicate(lookup LocalLookup, in Input) (int64, error) {
	if lookup == nil {
		return 0, nil
	}
	normTitle := Normalize(in.Title)
	normArtist := Normalize(in.Artist)
	if normTitle == "" || normArtist == "" {
		return 0, nil
	}
	return lookup.FindUnmatchedByText(normTitle, normArtist, in.DurationMS)
}

// sortedPool 把候选池按分值降序排列；同分时以标识字典序保证稳定。
func sortedPool(pool map[string]ScoredCandidate) []ScoredCandidate {
	out := make([]ScoredCandidate, 0, len(pool))
	for _, sc := range pool {
		out = append(out, sc)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score.Total != out[j].Score.Total {
			return out[i].Score.Total > out[j].Score.Total
		}
		return out[i].Candidate.MatchRef < out[j].Candidate.MatchRef
	})
	return out
}

// --- 6.3 各分支的提示文案 ---

// LogLine 返回某条目的分支提示文案。
//
// 六种分支各自可区分，且提示包含足以辨认该条目的输入信息。
func LogLine(outcome Outcome, in Input, sc *ScoredCandidate) string {
	switch outcome {
	case OutcomeImportUnique:
		return fmt.Sprintf("[直接入库] %s %s %s 置信率: %.0f%%",
			describe(sc.Candidate.Title, in.Title),
			describe(sc.Candidate.ArtistName, in.Artist),
			describe(sc.Candidate.AlbumTitle, in.Album),
			sc.Score.Total)
	case OutcomeImportLadder:
		// 与唯一高置信的情形可区分：明确标出它是优选出的发行
		return fmt.Sprintf("[直接入库] %s %s %s 实体release 置信率: %.0f%%",
			describe(sc.Candidate.Title, in.Title),
			describe(sc.Candidate.ArtistName, in.Artist),
			describe(sc.Candidate.AlbumTitle, in.Album),
			sc.Score.Total)
	case OutcomeReviewCrossGroup:
		return fmt.Sprintf("[进入复核]: %s", inputSummary(in))
	case OutcomeReviewNoMatch:
		return fmt.Sprintf("[匹配失败]: %s", inputSummary(in))
	}
	return ""
}

// DuplicateLogLine 返回疑似重复的提示：同时输出输入信息与库中已有曲目的信息。
func DuplicateLogLine(in Input, existing string) string {
	return fmt.Sprintf("[疑似重复]: 输入信息: %s；当前曲目库信息: %s", inputSummary(in), existing)
}

// ErrorLogLine 返回发生错误时的提示。
func ErrorLogLine(err error) string {
	return fmt.Sprintf("[发生错误]: 返回队列，%v", err)
}

// SummaryLine 返回完成或暂停时的统计文案。
func SummaryLine(imported, review, unmatched, failed int) string {
	return fmt.Sprintf(
		"[导入完成/暂停]: 本次导入 %d 条，进入复核 %d 条，未匹配 %d 条，失败 %d 条\n"+
			"（连续 5 次匹配均发生错误的条目不再返回队列）",
		imported, review, unmatched, failed)
}

func inputSummary(in Input) string {
	artist := in.Artist
	if artist == "" {
		artist = "—"
	}
	album := in.Album
	if album == "" {
		album = "—"
	}
	return fmt.Sprintf("%s · %s · %s", in.Title, artist, album)
}

func describe(upstream *string, fallback string) string {
	if upstream != nil && *upstream != "" {
		return *upstream
	}
	if fallback != "" {
		return fallback
	}
	return "—"
}
