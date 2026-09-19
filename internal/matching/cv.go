package matching

import (
	"regexp"
	"strings"
	"unicode"
)

// ParsedArtist 是署名中的一个身份；CV 署名保留完整名字，不派生角色/声优实体。
type ParsedArtist struct {
	Name string
	Type string
	// VoiceActor 仅用于把明确的演唱关系对到完整 CV 身份，不建立配音关系。
	VoiceActor string
	// Start / End 是原串中的字节区间。
	Start int
	End   int
}

type CreditParse struct {
	Raw       string
	Artists   []ParsedArtist
	Uncertain bool
}

type Span struct {
	Start    int   `json:"start"`
	End      int   `json:"end"`
	ArtistID int64 `json:"artist_id"`
}

type CreditSpans struct {
	Text  string `json:"text"`
	Spans []Span `json:"spans"`
}

// 只拆明确的合作连接词；不按逗号、斜杠或普通单词猜测乐队名。
var creditJoin = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring|&|×)\s+|[;；、]`)

// ParseCredit 用于没有结构化 artist-credit 的来源。括号内部的连接词不参与拆分。
func (r *Rules) ParseCredit(credit string) CreditParse {
	out := CreditParse{Raw: credit}
	segments := FindBracketSegments(credit)
	start := 0
	for _, loc := range creditJoin.FindAllStringIndex(credit, -1) {
		inside := false
		for _, seg := range segments {
			if loc[0] >= seg.Start && loc[0] < seg.End {
				inside = true
				break
			}
		}
		if inside {
			continue
		}
		r.appendCreditName(&out, credit[start:loc[0]], start)
		start = loc[1]
	}
	r.appendCreditName(&out, credit[start:], start)
	if out.Uncertain {
		out.Artists = nil // 不对部分可疑的署名猜测性建档
	}
	return out
}

// ParseCreditName 用于上游已经明确划分的一个艺术家。乐队名里的 & 等不是分隔符。
func (r *Rules) ParseCreditName(name string) CreditParse {
	out := CreditParse{Raw: name}
	r.appendCreditName(&out, name, 0)
	return out
}

func (r *Rules) appendCreditName(out *CreditParse, text string, offset int) {
	name := strings.TrimSpace(text)
	if name == "" {
		if strings.TrimSpace(out.Raw) != "" {
			out.Uncertain = true
		}
		return
	}
	start := offset + leadingSpaceLen(text)
	a := ParsedArtist{Name: name, Type: "person", Start: start, End: start + len(name)}
	for _, seg := range FindBracketSegments(name) {
		c := r.Classify(seg)
		if c.Kind != KindCV {
			continue
		}
		if strings.TrimSpace(seg.Outer) == "" || c.Payload == "" || r.versionRe.MatchString(c.Payload) {
			out.Uncertain = true
			return
		}
		a.VoiceActor = c.Payload
	}
	out.Artists = append(out.Artists, a)
}

// BuildSpans 只为已经落库的身份生成区间，原串及连接词不改写。
func BuildSpans(parse CreditParse, artistIDs map[string]int64) CreditSpans {
	out := CreditSpans{Text: parse.Raw}
	for _, a := range parse.Artists {
		if id, ok := artistIDs[a.Name]; ok {
			out.Spans = append(out.Spans, Span{Start: a.Start, End: a.End, ArtistID: id})
		}
	}
	return out
}

func leadingSpaceLen(s string) int {
	return len(s) - len(strings.TrimLeftFunc(s, unicode.IsSpace))
}
