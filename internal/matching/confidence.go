package matching

import (
	"github.com/norhua/sonexa/internal/provider"
)

// MatchDegree 是两个文本的匹配程度三级判定之一。
type MatchDegree int

const (
	MatchNone  MatchDegree = iota // 不匹配
	MatchFuzzy                    // 模糊
	MatchExact                    // 精确
)

// Weights 是评分的阈值与权重，可在设置中调整。
type Weights struct {
	Title          float64
	CreditExact    float64
	CreditFuzzy    float64
	AlbumExact     float64
	AlbumFuzzy     float64
	ScoreScale     float64 // 上游 relevance score 的系数
	ScoreCap       float64 // 上游 score 的封顶
	JaroWinklerMin float64
	// PrefixMinLen 与 PrefixMinRatio 是前缀截断判定的门槛。
	PrefixMinLen   int
	PrefixMinRatio float64
}

// DefaultWeights 返回默认权重（与 5.5 一致，满分 100）。
func DefaultWeights() Weights {
	return Weights{
		Title:          55,
		CreditExact:    20,
		CreditFuzzy:    12,
		AlbumExact:     20,
		AlbumFuzzy:     12,
		ScoreScale:     0.05,
		ScoreCap:       5,
		JaroWinklerMin: 0.90,
		PrefixMinLen:   4,
		PrefixMinRatio: 0.70,
	}
}

// CompareText 判定两个文本的匹配程度。该判定被所有上游的评分算法共用。
func (w Weights) CompareText(a, b string) MatchDegree {
	na, nb := Normalize(a), Normalize(b)
	if na == "" || nb == "" {
		return MatchNone
	}
	if na == nb {
		return MatchExact
	}

	// 词元集合包含：一方词元 ⊆ 另一方
	ta, tb := tokenSet(a), tokenSet(b)
	if len(ta) > 0 && len(tb) > 0 && (subset(ta, tb) || subset(tb, ta)) {
		return MatchFuzzy
	}

	// 前缀截断：一方是另一方前缀，且长度达到门槛
	if prefixMatch(na, nb, w.PrefixMinLen, w.PrefixMinRatio) {
		return MatchFuzzy
	}

	// 字符串相似度
	if jaroWinkler(na, nb) >= w.JaroWinklerMin {
		return MatchFuzzy
	}
	return MatchNone
}

// Score 是一条候选的评分结果。
type Score struct {
	Total       float64
	TitleMatch  MatchDegree
	CreditMatch MatchDegree
	AlbumMatch  MatchDegree
	HighConf    bool
	Rejected    bool // 曲名不匹配即丢弃
}

// ScoreCandidate 按候选来源选择评分算法，返回统一满分 100 的分值。
//
// 曲名不匹配即丢弃是所有上游共同的准入门槛。
func (w Weights) ScoreCandidate(q provider.Query, c provider.Candidate) Score {
	titleMatch := w.CompareText(q.Title, deref(c.Title))
	if titleMatch == MatchNone {
		return Score{Rejected: true, TitleMatch: MatchNone}
	}

	s := Score{TitleMatch: titleMatch}
	s.Total += w.Title // 曲名匹配即得满额（准入已保证匹配）

	// 署名
	s.CreditMatch = w.CompareText(q.Artist, deref(c.ArtistName))
	switch s.CreditMatch {
	case MatchExact:
		s.Total += w.CreditExact
	case MatchFuzzy:
		s.Total += w.CreditFuzzy
	}

	// 歌集
	s.AlbumMatch = w.CompareText(q.Album, deref(c.AlbumTitle))
	switch s.AlbumMatch {
	case MatchExact:
		s.Total += w.AlbumExact
	case MatchFuzzy:
		s.Total += w.AlbumFuzzy
	}

	// 剩余权重：有 relevance score 用 score，否则用时长接近度
	if c.UpstreamScore != nil {
		bonus := float64(*c.UpstreamScore) * w.ScoreScale
		if bonus > w.ScoreCap {
			bonus = w.ScoreCap
		}
		s.Total += bonus
	} else if q.DurationMS != nil && c.DurationMS != nil {
		s.Total += durationBonus(*q.DurationMS, *c.DurationMS)
	}

	s.HighConf = w.IsHighConfidence(s)
	return s
}

// durationBonus 是 iTunes 的时长补分：|Δ| ≤ 3s 得 5，≤ 10s 得 2。
// 输入无时长时不计分，也不因此扣分——由调用方保证仅在有时长时调用。
func durationBonus(a, b int) float64 {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	switch {
	case diff <= 3000:
		return 5
	case diff <= 10000:
		return 2
	default:
		return 0
	}
}

// IsHighConfidence 判定高置信——这是结构性条件，不是分数线。
//
//	高置信 ≡ 曲名匹配 AND (署名精确匹配 OR 歌集精确匹配)
//
// 55+12+12=79 分但署名与歌集都只是模糊匹配 → 不算高置信（最容易搞错的那种）；
// 而最低的高置信是 55+20=75 分（署名精确）。分数高不等于高置信。
func (w Weights) IsHighConfidence(s Score) bool {
	if s.TitleMatch == MatchNone {
		return false
	}
	return s.CreditMatch == MatchExact || s.AlbumMatch == MatchExact
}

// --- 辅助 ---

func tokenSet(s string) map[string]bool {
	set := map[string]bool{}
	for _, tok := range Tokens(s) {
		set[tok] = true
	}
	return set
}

func subset(a, b map[string]bool) bool {
	if len(a) == 0 {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func prefixMatch(a, b string, minLen int, minRatio float64) bool {
	short, long := a, b
	if len(short) > len(long) {
		short, long = long, short
	}
	if short == "" || len(short) == len([]rune(long)) {
		return false
	}
	sr, lr := []rune(short), []rune(long)
	if len(sr) > len(lr) {
		sr, lr = lr, sr
	}
	for i := range sr {
		if sr[i] != lr[i] {
			return false
		}
	}
	// 是前缀：要求短串长度达到门槛
	return len(sr) >= minLen || float64(len(sr)) >= minRatio*float64(len(lr))
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
