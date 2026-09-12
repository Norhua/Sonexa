package matching

import "strings"

// MaxVariants 是查询变体组合的数量上限。
//
// MB 限速 1 req/s：总是查全部变体会把 1000 行的导入从约 17 分钟拖到约 67 分钟。
const MaxVariants = 3

// Variant 是一个查询变体组合。
type Variant struct {
	Title  string
	Artist string
	Album  string
	// Penalty 是该变体的得分惩罚。保留字段以免绑定层与测试同时改动；
	// 现在恒为 0——曲名是否对得上改由 CompareTitle 判定，不再靠扣分压制误剥离。
	Penalty float64
	// TitleOnly 为真时只以曲名检索，不带锚点——阶梯的兜底一级。
	TitleOnly bool
	// ArtistAlternates 是艺术家的备选写法，作为并联锚点之一。
	ArtistAlternates []string
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

// GenerateVariants 生成检索阶梯，精度从高到低。调用方按顺序发、有候选过闸即止。
//
//	L1 锚定并联  曲名主干 AND（歌集 OR 歌手 OR 歌手备选写法）   <- 绝大多数只花这一次
//	L2 取内层    译名那一层作曲名，锚点不变（`驟雨` / `繁雪之梦`）
//	L3 曲名独行  连歌集带歌手都对不上时，只用曲名，交本地闸门判
//
// **没有「原串」那一级**：上游的引号短语是子短语匹配，主干的结果集是原串结果集的
// 超集（`recording:"That Girl"` 会返回 `That Girl (DJLIU Remix)`），
// 原串那一级只可能命中同一批或更少——它在十条输入上白花 9 次请求。
//
// 剥离不再扣分：曲名是否对得上由 CompareTitle 判定，那里版本标记不一致直接不匹配，
// 不需要再用分数去压制误剥离。
func (r *Rules) GenerateVariants(title, artist, album string) []Variant {
	tv := r.fieldVariantsFor(title)
	av := r.fieldVariantsFor(artist)

	extra := append(append([]string{}, tv.Extra...), av.Extra...)
	// 艺术家的备选写法：括号里那种写法，上游用的往往正是它
	alternates := append(append([]string{}, r.Alternates(artist)...), extra...)

	coreTitle := r.SearchCore(title)
	coreArtist := r.SearchCore(artist)
	coreAlbum := r.SearchCore(album)

	type combo struct {
		title, artist, album string
		titleOnly            bool
		label                string
	}
	combos := []combo{
		{coreTitle, coreArtist, coreAlbum, false, "锚定"},
	}
	if tv.Inner != "" && !strings.EqualFold(tv.Inner, coreTitle) {
		combos = append(combos, combo{tv.Inner, coreArtist, coreAlbum, false, "取内层"})
	}
	combos = append(combos, combo{coreTitle, "", "", true, "仅曲名"})

	seen := map[string]bool{}
	out := make([]Variant, 0, MaxVariants)
	for _, c := range combos {
		if len(out) >= MaxVariants {
			break
		}
		if strings.TrimSpace(c.title) == "" {
			continue
		}
		key := c.title + "\x00" + c.artist + "\x00" + c.album + "\x00" + boolKey(c.titleOnly)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Variant{
			Title:            c.title,
			Artist:           c.artist,
			Album:            c.album,
			TitleOnly:        c.titleOnly,
			ArtistAlternates: alternates,
			ExtraArtists:     extra,
			Label:            c.label,
		})
	}
	return out
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// VariantLabels 返回变体的展示串，供复核队列的「已尝试变体」提示使用。
func VariantLabels(variants []Variant) []string {
	out := make([]string, 0, len(variants))
	for _, v := range variants {
		out = append(out, v.Title)
	}
	return out
}
