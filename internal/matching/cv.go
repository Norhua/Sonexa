package matching

import (
	"strings"
)

// ParsedArtist 是从署名中解析出的一位艺术家。
type ParsedArtist struct {
	Name string
	// Type 为 character（角色）或 person（人物）。
	Type string
	// Role 是该艺术家在这首曲目上的参与角色。
	Role string
	// Start / End 是该艺术家名在署名原串中的字节区间，供生成可点击区间使用。
	Start int
	End   int
}

// CreditParse 是对一条署名原串的解析结果。
type CreditParse struct {
	// Raw 是署名原串，原样保真——显示与写音频标签时以它为准。
	Raw string
	// Artists 是解析出的艺术家。
	Artists []ParsedArtist
	// VoiceOf 记录配音关系：键为角色名，值为声优名。
	VoiceOf map[string]string
	// Uncertain 为真时表示解析结果不确定，不应据此建立艺术家记录，
	// 该条目应标记并送入复核队列由用户处置。
	Uncertain bool
}

// Span 是署名串中的一段可点击区间。
type Span struct {
	Start    int   `json:"start"`
	End      int   `json:"end"`
	ArtistID int64 `json:"artist_id"`
}

// CreditSpans 是 credit_spans_json 的结构：纯渲染辅助数据，不参与查询。
type CreditSpans struct {
	Text  string `json:"text"`
	Spans []Span `json:"spans"`
}

// ParseCredit 解析署名原串。
//
// 仅当括号内层带有配音前缀时才拆分为角色与人物两个艺术家，
// 避免误伤 "Artist (Remix)" / "Song (feat. X)" / "Band (US)"。
//
// 拆分不确定时不拆：保留原串并置 Uncertain，交由复核队列让用户处置，
// 而不是猜测性地拆分。
func (r *Rules) ParseCredit(credit string) CreditParse {
	out := CreditParse{Raw: credit, VoiceOf: map[string]string{}}

	segs := FindBracketSegments(credit)
	if len(segs) == 0 {
		// 无括号：整串就是一位艺术家
		name := strings.TrimSpace(credit)
		if name == "" {
			return out
		}
		out.Artists = append(out.Artists, ParsedArtist{
			Name:  name,
			Type:  "person",
			Role:  "vocal",
			Start: leadingSpaceLen(credit),
			End:   leadingSpaceLen(credit) + len(name),
		})
		return out
	}

	seg := segs[0]
	c := r.Classify(seg)
	if c.Kind != KindCV {
		// 非配音括号：不拆分，整串作为一位艺术家的署名
		name := strings.TrimSpace(credit)
		out.Artists = append(out.Artists, ParsedArtist{
			Name: name, Type: "person", Role: "vocal",
			Start: leadingSpaceLen(credit), End: leadingSpaceLen(credit) + len(name),
		})
		return out
	}

	characterName := strings.TrimSpace(seg.Outer)
	actorName := strings.TrimSpace(c.Payload)

	// 形态可疑时不拆：任一侧为空，或内层还含有版本关键词
	// （既像配音又像版本标识），保留原串并标记进复核。
	if characterName == "" || actorName == "" || r.versionRe.MatchString(actorName) {
		out.Uncertain = true
		return out
	}

	// 区间取自原串中的实际位置，在解析时一并生成——
	// 渲染时已经没有解析上下文，无法反向猜测。
	charStart := strings.Index(credit, characterName)
	actorStart := indexAfter(credit, actorName, seg.Start)
	if charStart < 0 || actorStart < 0 {
		out.Uncertain = true
		return out
	}

	out.Artists = []ParsedArtist{
		{
			Name: characterName, Type: "character", Role: "character",
			Start: charStart, End: charStart + len(characterName),
		},
		{
			Name: actorName, Type: "person", Role: "vocal",
			Start: actorStart, End: actorStart + len(actorName),
		},
	}
	out.VoiceOf[characterName] = actorName
	return out
}

// BuildSpans 由解析结果与已落库的艺术家 id 生成可点击区间。
//
// 对不上 artist 的片段不生成区间，渲染时保持纯文本——不猜测、不隐藏。
func BuildSpans(parse CreditParse, artistIDs map[string]int64) CreditSpans {
	out := CreditSpans{Text: parse.Raw}
	for _, a := range parse.Artists {
		id, ok := artistIDs[a.Name]
		if !ok {
			continue
		}
		out.Spans = append(out.Spans, Span{Start: a.Start, End: a.End, ArtistID: id})
	}
	return out
}

// leadingSpaceLen 返回串首空白的字节长度。
func leadingSpaceLen(s string) int {
	return len(s) - len(strings.TrimLeft(s, " \t　"))
}

// indexAfter 在 from 之后查找 substr 的字节位置。
func indexAfter(s, substr string, from int) int {
	if from < 0 || from > len(s) {
		return strings.Index(s, substr)
	}
	idx := strings.Index(s[from:], substr)
	if idx < 0 {
		return -1
	}
	return from + idx
}
