package matching

import (
	"regexp"
	"strings"
	"unicode"
)

// BracketKind 是括号内容的语义分类。
type BracketKind string

const (
	// KindCV 角色与声优：拆为两个艺术家 + 一条配音关系。
	KindCV BracketKind = "cv"
	// KindVersion 版本标识：必须保留，是曲名的一部分。
	KindVersion BracketKind = "version"
	// KindTranslation 翻译或异写：生成查询变体。
	KindTranslation BracketKind = "translation"
	// KindFeature 附加艺术家：从曲名剥离并并入艺术家条件。
	KindFeature BracketKind = "feature"
	// KindDisambiguation 歧义消解：保留为艺术家的歧义说明。
	KindDisambiguation BracketKind = "disambiguation"
	// KindFallback 保守兜底：保留原串同时生成剥离变体，剥离变体扣分。
	KindFallback BracketKind = "fallback"
)

// Rules 是可配置的模式表。
//
// 新增一种前缀写法只需扩表并补金样例，不需要改动解析流程的结构。
type Rules struct {
	CVPrefixes        []string // 配音前缀，如 CV / 声優 / Vo.
	VersionKeywords   []string // 版本关键词，如 Remix / Live
	TranslationHints  []string // 翻译提示词，如 翻译 / Romaji
	FeatureKeywords   []string // 附加艺术家前缀，如 feat. / ft.
	DisambiguationPat []string // 歧义消解形态，如两位字母国家码

	cvRe          *regexp.Regexp
	versionRe     *regexp.Regexp
	translationRe *regexp.Regexp
	featureRe     *regexp.Regexp
	disambigRe    *regexp.Regexp
}

// defaultRules 是包级共享的默认规则表。
//
// 括号分类是纯函数式的查表，没有每次重建的理由；而 Weights 那边的判定
// 需要用到它，又不适合为此在每个值类型上挂一份规则表。
var defaultRules = DefaultRules()

// DefaultRules 返回默认的模式表。
func DefaultRules() *Rules {
	r := &Rules{
		CVPrefixes: []string{
			`CV`, `cv`, `ＣＶ`, `C\.V\.`, `声優`, `声优`, `声`, `配音`, `Vo\.`,
		},
		VersionKeywords: []string{
			`Remix`, `Mix`, `Edit`, `Ver\.?`, `Version`, `Live`, `Acoustic`,
			`Instrumental`, `Inst\.?`, `Cover`, `feat\.?`, `ft\.?`, `with`,
			`Off ?Vocal`, `Karaoke`, `カラオケ`, `TV ?Size`, `Short`, `Full`,
			`Extended`, `Radio`, `Original`, `Demo`, `Reprise`, `-cut`, `字幕`, `翻唱`,
		},
		TranslationHints: []string{
			`翻译`, `译名`, `訳`, `Translation`, `中文名`, `英文名`,
			`片假名`, `平假名`, `罗马音`, `Romaji`,
		},
		FeatureKeywords: []string{`feat\.?`, `ft\.?`, `featuring`, `with`},
		DisambiguationPat: []string{
			`^[A-Z]{2}$`,     // 国家码，如 US / JP
			`^\d{4}$`,        // 年份
			`^[a-z ]+ band$`, // 乐队说明
		},
	}
	r.compile()
	return r
}

// compile 预编译模式表中的全部正则。
func (r *Rules) compile() {
	join := func(parts []string) string { return strings.Join(parts, "|") }

	// CV 前缀后可跟分隔符：(CV:平野綾) / (CV.平野綾) / (声優：平野綾)
	r.cvRe = regexp.MustCompile(`(?i)^\s*(?:` + join(r.CVPrefixes) + `)\s*` + separatorPattern + `?\s*(.+)$`)
	// 版本关键词出现在内层任意位置即命中：(DJLIU Remix) 的关键词在末尾
	r.versionRe = regexp.MustCompile(`(?i)(?:^|[\s\-_/])(?:` + join(r.VersionKeywords) + `)(?:$|[\s\-_/0-9])`)
	r.translationRe = regexp.MustCompile(`(?i)(?:` + join(r.TranslationHints) + `)`)
	r.featureRe = regexp.MustCompile(`(?i)^\s*(?:` + join(r.FeatureKeywords) + `)\s+(.+)$`)
	r.disambigRe = regexp.MustCompile(`(?i)` + join(r.DisambiguationPat))
}

// Classification 是对一个括号段的分类结果。
type Classification struct {
	Kind    BracketKind
	Segment Segment
	// Payload 是该分类下提取出的有效内容：
	// CV 为声优名，Feature 为附加艺术家名，其余为内层原文。
	Payload string
}

// Classify 按固定顺序判定一个括号段的语义。
//
// 判定顺序写死，不可调整：
//
//  1. 内层匹配 CV 模式        → 角色/声优拆分
//  2. 内层命中版本关键词表      → 保留，不生成变体也不剥离
//  3. 内层命中翻译提示词        → 生成变体
//  4. 内外层主要文字系统不同    → 生成变体
//  5. 都不匹配                → 保守兜底：原串与剥离变体都查，剥离变体扣分
//
// 版本关键词必须在文字系统差异之前生效，否则日文曲名 + 拉丁文版本标识
// 会被误判为翻译异写，把 That Girl (DJLIU Remix) 搜成 That Girl。
func (r *Rules) Classify(seg Segment) Classification {
	inner := strings.TrimSpace(seg.Inner)

	// [1] 角色与声优
	if m := r.cvRe.FindStringSubmatch(inner); m != nil {
		return Classification{Kind: KindCV, Segment: seg, Payload: strings.TrimSpace(m[1])}
	}

	// [2] 版本标识——必须先于文字系统差异判定
	if r.versionRe.MatchString(inner) {
		// feat. 类虽在版本关键词表中，但语义是「附加艺术家」：
		// 从曲名剥离并并入艺术家条件，而不是保留为曲名的一部分。
		if m := r.featureRe.FindStringSubmatch(inner); m != nil {
			return Classification{Kind: KindFeature, Segment: seg, Payload: strings.TrimSpace(m[1])}
		}
		return Classification{Kind: KindVersion, Segment: seg, Payload: inner}
	}

	// [3] 翻译提示词
	if r.translationRe.MatchString(inner) {
		return Classification{Kind: KindTranslation, Segment: seg, Payload: inner}
	}

	// 歧义消解：形如 (US) (2011) 的短说明，保留为艺术家的歧义说明。
	// 放在文字系统判定之前，否则拉丁文说明 + 日文外层会被误判为翻译。
	if r.disambigRe.MatchString(inner) {
		return Classification{Kind: KindDisambiguation, Segment: seg, Payload: inner}
	}

	// [4] 内外层主要文字系统不同
	outerScript, innerScript := DominantScript(seg.Outer), DominantScript(inner)
	if outerScript != innerScript && outerScript != ScriptUnknown && innerScript != ScriptUnknown {
		return Classification{Kind: KindTranslation, Segment: seg, Payload: inner}
	}
	// 内外层同属汉字时再比字形变体：日文汉字与中文汉字同属 CJK 区，
	// 码位相同但简体/繁体的独有字可分辨，使「夕立（驟雨）」这类异写被识别出来。
	if outerScript == ScriptHan && innerScript == ScriptHan {
		if ov, iv := DetectHanVariant(seg.Outer), DetectHanVariant(inner); ov != iv &&
			!(ov == HanUnknown && iv == HanUnknown) {
			return Classification{Kind: KindTranslation, Segment: seg, Payload: inner}
		}
	}

	// [5] 保守兜底
	return Classification{Kind: KindFallback, Segment: seg, Payload: inner}
}

// Script 是文字系统。
type Script string

const (
	ScriptUnknown  Script = ""
	ScriptKana     Script = "kana"     // 平假名 / 片假名
	ScriptHan      Script = "han"      // CJK 汉字
	ScriptLatin    Script = "latin"    // 拉丁
	ScriptHangul   Script = "hangul"   // 谚文
	ScriptCyrillic Script = "cyrillic" // 西里尔
	ScriptThai     Script = "thai"     // 泰文
)

// DominantScript 返回串中占比最高的文字系统。
//
// 日文汉字与中文汉字同属 CJK 区，无法靠码位区分，
// 因此含假名时整体判为 kana——「外层是否含假名」正是那个辅助判据。
func DominantScript(s string) Script {
	counts := map[Script]int{}
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			counts[ScriptKana]++
		case unicode.Is(unicode.Han, r):
			counts[ScriptHan]++
		case unicode.Is(unicode.Hangul, r):
			counts[ScriptHangul]++
		case unicode.Is(unicode.Cyrillic, r):
			counts[ScriptCyrillic]++
		case unicode.Is(unicode.Thai, r):
			counts[ScriptThai]++
		case unicode.IsLetter(r) && r < 0x0250:
			counts[ScriptLatin]++
		}
	}
	// 含假名即判为日文：日文串中的汉字往往多于假名，
	// 按数量取最高会把日文误判为 han，与中文无法区分。
	if counts[ScriptKana] > 0 {
		return ScriptKana
	}

	best, bestCount := ScriptUnknown, 0
	for _, script := range []Script{ScriptHan, ScriptLatin, ScriptHangul, ScriptCyrillic, ScriptThai} {
		if counts[script] > bestCount {
			best, bestCount = script, counts[script]
		}
	}
	return best
}
