package matching

import (
	"regexp"
	"strings"

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

// CompareTitle 判定查询曲名与候选曲名是否是**同一条录音的名字**。
//
// 它比 CompareText 严：曲名是准入门槛，宽了就会把 `Tetra. (off vocal)` 当成
// `Tetra.` 收下——那是两条不同的录音，替用户定成同一条正是要根除的错误。
//
//	[1] 版本标记不一致 ⟹ 不匹配。无论其余部分多像
//	[2] 剥掉非版本注释后相等 ⟹ 精确（`綿雪の夢（繁雪之梦）` == `綿雪の夢`）
//	[3] 字符串相似度达标 ⟹ 模糊
//
// 与 CompareText 的关键差别是**不用词元包含**：`{tetra}` ⊆ `{tetra, offvocal}`
// 正是上面那个错误的来源。署名与歌集仍走 CompareText——那两处宽一点是合适的，
// 它们是旁证而不是准入。
func (w Weights) CompareTitle(query, candidate string) MatchDegree {
	if !sameMarkers(VersionMarkers(query), VersionMarkers(candidate)) {
		return MatchNone
	}

	nq, nc := Normalize(query), Normalize(candidate)
	if nq == "" || nc == "" {
		return MatchNone
	}
	if nq == nc {
		return MatchExact
	}

	// 剥掉译名/读音这类平台附加的注释后再比一次
	sq := Normalize(StripNonVersionAnnotations(query))
	sc := Normalize(StripNonVersionAnnotations(candidate))
	if sq != "" && sc != "" && sq == sc {
		return MatchExact
	}
	if (sq != "" && sq == nc) || (sc != "" && nq == sc) {
		return MatchExact
	}

	// 括号里装的可能正是上游用的那个写法。中文平台常写成「译名（原名）」，
	// 此时主干是译名、上游用的是括号里的原名——不比这一层，
	// 取内层那一级就算查到了也会被自己判成不匹配。
	//
	// 版本标记不在 Alternates 里（它是曲名的一部分，不是另一种写法），
	// 且标记集合的判定在最前面已经拦过一道，因此这里不会放宽版本的区分。
	for _, alt := range Alternates(query) {
		if n := Normalize(alt); n != "" && n == nc {
			return MatchExact
		}
	}
	for _, alt := range Alternates(candidate) {
		if n := Normalize(alt); n != "" && n == nq {
			return MatchExact
		}
	}

	if jaroWinkler(nq, nc) >= w.JaroWinklerMin {
		return MatchFuzzy
	}
	return MatchNone
}

// CompareCredit 判定查询署名与候选署名是否指同一位（或同一组）艺术家。
//
// 比 CompareText 宽一档，因为导出表格里署名的写法差异极大，而差异几乎总是
// 三种固定形态之一：
//
//	括号里装着另一种写法   `原田瞳（原田ひとみ）` ↔ `原田ひとみ`
//	括号里装着声优         `茅野愛衣` ↔ `霞ヶ丘詩羽(CV.茅野愛衣)`
//	末尾挂着 feat. 谁      `Kirara Magic` ↔ `Kirara Magic feat. Kitsui Akira`
//
// 这三种都是同一位艺术家的不同写法，判成不匹配会让一批本该直接入库的行
// 白白落进复核队列——署名从来不单独决定入库，它是曲名之外的**旁证**，
// 宽一点是合适的。曲名那一侧仍然很严（见 CompareTitle）。
func (w Weights) CompareCredit(query, candidate string) MatchDegree {
	nq, nc := Normalize(query), Normalize(candidate)
	if nq == "" || nc == "" {
		return MatchNone
	}
	if nq == nc {
		return MatchExact
	}

	// 主要署名：去掉括号注释与末尾的 feat. 从句
	pq := Normalize(primaryCredit(query))
	pc := Normalize(primaryCredit(candidate))
	if pq != "" && pc != "" && pq == pc {
		return MatchExact
	}
	if (pq != "" && pq == nc) || (pc != "" && nq == pc) {
		return MatchExact
	}

	// 括号里的另一种写法（含 CV 的声优名）：逐段比一遍
	for _, alt := range Alternates(query) {
		n := Normalize(alt)
		if n != "" && (n == nc || n == pc) {
			return MatchExact
		}
	}
	for _, alt := range Alternates(candidate) {
		n := Normalize(alt)
		if n != "" && (n == nq || n == pq) {
			return MatchExact
		}
	}

	return w.CompareText(query, candidate)
}

// featureSuffixRe 匹配署名末尾的 feat. 从句。
//
// 被 feat 的人是这条录音的附加参与者，不是另一位主署名——
// `Kirara Magic` 与 `Kirara Magic feat. Kitsui Akira` 是同一个主署名。
var featureSuffixRe = regexp.MustCompile(`(?i)\s*[\(（]?\s*(?:feat\.?|ft\.?|featuring|with)\s+.*$`)

// primaryCredit 返回署名的主干：剥掉括号注释与末尾的 feat. 从句。
func primaryCredit(s string) string {
	out := featureSuffixRe.ReplaceAllString(StripBrackets(s), "")
	out = collapseSpaces(out)
	if strings.TrimSpace(out) == "" {
		return s
	}
	return out
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
	titleMatch := w.CompareTitle(q.Title, deref(c.Title))
	if titleMatch == MatchNone {
		return Score{Rejected: true, TitleMatch: MatchNone}
	}

	s := Score{TitleMatch: titleMatch}
	s.Total += w.Title // 曲名匹配即得满额（准入已保证匹配）

	// 署名
	s.CreditMatch = w.CompareCredit(q.Artist, deref(c.ArtistName))
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
