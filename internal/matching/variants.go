package matching

import "strings"

// MaxVariants 是查询变体组合的数量上限。
//
// MB 限速 1 req/s：总是查全部变体会把 1000 行的导入从约 17 分钟拖到约 67 分钟。
const MaxVariants = 4

// Variant 是一个查询变体组合。
type Variant struct {
	Title  string
	Artist string
	Album  string
	// Penalty 是该变体的得分惩罚。
	// 兜底分支的剥离变体扣分，防止误剥离时压过正确结果。
	Penalty float64
	// ExtraArtists 是从曲名中剥离出的附加艺术家（feat. X），并入艺术家条件。
	ExtraArtists []string
	// Label 供界面展示「已尝试变体」。
	Label string
}

// fieldVariants 是单个字段（曲名或艺术家）的变体取值。
type fieldVariants struct {
	Original string   // 原串
	Stripped string   // 剥离括号后的串；与原串相同时表示不可剥离
	Inner    string   // 括号内层内容；空表示无内层变体
	Penalty  float64  // 剥离变体的惩罚分
	Extra    []string // 附加艺术家
}

// StripPenalty 是兜底分支中剥离变体的固定惩罚分。
const StripPenalty = 3.0

// fieldVariantsFor 按分类结果计算单个字段的变体取值。
func (r *Rules) fieldVariantsFor(s string) fieldVariants {
	fv := fieldVariants{Original: s, Stripped: s}
	segs := FindBracketSegments(s)
	if len(segs) == 0 {
		return fv
	}

	// 只按第一个括号段决定该字段的变体形态：
	// 多段括号时继续拆会让组合数爆炸，而上限只有 4 个。
	c := r.Classify(segs[0])
	switch c.Kind {
	case KindVersion:
		// 版本标识是曲名的一部分，既不生成变体也不剥离
		return fv

	case KindCV:
		// 角色与声优：查询用声优名更可能命中上游
		fv.Stripped = strings.TrimSpace(c.Segment.Outer)
		fv.Inner = c.Payload
		return fv

	case KindFeature:
		// 附加艺术家：从曲名剥离并并入艺术家条件
		fv.Stripped = strings.TrimSpace(c.Segment.Outer)
		fv.Extra = append(fv.Extra, c.Payload)
		return fv

	case KindDisambiguation:
		// 歧义消解不参与查询
		fv.Stripped = strings.TrimSpace(c.Segment.Outer)
		return fv

	case KindTranslation:
		fv.Stripped = strings.TrimSpace(c.Segment.Outer)
		fv.Inner = c.Payload
		return fv

	default: // KindFallback
		// 保守处理：保留原串，同时生成一个剥离变体，剥离变体扣分
		fv.Stripped = strings.TrimSpace(c.Segment.Outer)
		fv.Penalty = StripPenalty
		return fv
	}
}

// GenerateVariants 由曲名与艺术家的括号处理结果生成查询变体组合。
//
// 组合按固定优先级排列并截断为最多 4 个：
//
//	V1 原串        ("夕立（驟雨）", "3L（スリーエル）")
//	V2 双剥离      ("夕立",        "3L")              <- 通常命中这个
//	V3 取内层      ("驟雨",        "3L")
//	V4 混合        ("夕立",        "スリーエル")
func (r *Rules) GenerateVariants(title, artist string) []Variant {
	tv := r.fieldVariantsFor(title)
	av := r.fieldVariantsFor(artist)

	extra := append(append([]string{}, tv.Extra...), av.Extra...)
	penalty := tv.Penalty + av.Penalty

	// 候选组合按优先级排列，随后去重并截断
	type combo struct {
		title, artist string
		penalty       float64
		label         string
	}
	combos := []combo{
		{tv.Original, av.Original, 0, "原串"},
		{tv.Stripped, av.Stripped, penalty, "双剥离"},
	}
	if tv.Inner != "" {
		combos = append(combos, combo{tv.Inner, av.Stripped, penalty, "取内层"})
	}
	if av.Inner != "" {
		combos = append(combos, combo{tv.Stripped, av.Inner, penalty, "混合"})
	}

	seen := map[string]bool{}
	out := make([]Variant, 0, MaxVariants)
	for _, c := range combos {
		if len(out) >= MaxVariants {
			break
		}
		if strings.TrimSpace(c.title) == "" && strings.TrimSpace(c.artist) == "" {
			continue
		}
		key := c.title + "\x00" + c.artist
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Variant{
			Title:        c.title,
			Artist:       c.artist,
			Penalty:      c.penalty,
			ExtraArtists: extra,
			Label:        c.label,
		})
	}
	return out
}

// VariantLabels 返回变体的展示串，供复核队列的「已尝试变体」提示使用。
func VariantLabels(variants []Variant) []string {
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		out = append(out, v.Title)
	}
	return out
}
