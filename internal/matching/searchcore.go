package matching

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// 从「导出表格里的一行」到「上游那条录音」之间隔着两层噪音：
// 平台加的括号注释（译名/读音/异写），与导出时的截断省略号。
// 这个文件负责把它们剥掉，得到可以拿去检索的主干形态。
//
// 剥离分两种用途，条件不同，因此是两组函数：
//
//	检索用（SearchCore）    —— 都可以剥，多一次召回的机会
//	判定用（CompareTitle）  —— 版本标记不能剥，剥了就换了一首歌
//
// 混成一个函数会让「查得到」与「查对了」互相牵制。

// ellipsisPattern 匹配结尾的截断省略号。
//
// 表格导出常把过长的字段截断成 `心の羽根（ドラゴンボール改ver.）（心...`。
// 带着省略号做短语检索必然一无所获——那几个点不是内容，是「这里被切掉了」。
var ellipsisPattern = regexp.MustCompile(`(?:\.{2,}|…+)\s*$`)

// danglingOpenPattern 匹配结尾一个未闭合的左括号及其后续。
//
// 截断常常正好切在注释中间，去掉省略号后还剩 `（心` 这样一截。
var danglingOpenPattern = regexp.MustCompile(`[（(［\[【〔｛{][^）)］\]】〕｝}]*$`)

// IsTruncated 报告字段是否被导出方截断过。
//
// 截断的字段不该被要求「完全相等」——它缺的那一截不是差异，是根本没给出来。
func IsTruncated(s string) bool {
	return ellipsisPattern.MatchString(strings.TrimSpace(s))
}

// TrimEllipsis 去掉结尾的截断省略号。
func TrimEllipsis(s string) string {
	return strings.TrimSpace(ellipsisPattern.ReplaceAllString(strings.TrimSpace(s), ""))
}

// SearchCore 返回一个字段拿去检索时的主干形态。
//
// 依次：去截断省略号 → 抹掉截断在注释中间留下的半个左括号 → 剥掉**非版本**的括号注释。
//
// 版本标记那一段留着：它是这条录音身份的一部分，剥掉就换了一首歌。
// `That Girl (DJLIU Remix)` 必须带着 remix 去查——否则上游返回的几百条裸
// `That Girl` 里根本没有那条 remix，再宽的阶梯也捞不回来。
//
// 其余注释（译名/读音/异写）都可以剥：上游的引号短语是**子短语**匹配，
// 主干的结果集是原串结果集的超集，用主干去查只会查到更多。
// 判定「曲名对上了没有」用的是 CompareTitle，那边的条件更严。
func (r *Rules) SearchCore(s string) string {
	core := TrimEllipsis(s)
	core = strings.TrimSpace(danglingOpenPattern.ReplaceAllString(core, ""))
	if core == "" {
		core = TrimEllipsis(s)
	}
	stripped := r.StripNonVersionAnnotations(core)
	// 剥空了就退回去：整个字段就是一段括号内容时（`（インスト）`），
	// 剥成空串会让两条毫不相干的记录互相判成同一首。
	if strings.TrimSpace(stripped) == "" {
		return core
	}
	return collapseSpaces(stripped)
}

// SearchCore 是包级入口，用默认规则表。
func SearchCore(s string) string { return defaultRules.SearchCore(s) }

// Alternates 返回一个字段里各段**非版本标记**的括号内容。
//
// 平台习惯把另一种写法装在括号里（`原田瞳（原田ひとみ）`、`3L（スリーエル）`），
// 而上游用的往往是括号里那个。因此它们必须能作为备选写法进查询。
func (r *Rules) Alternates(s string) []string {
	var out []string
	for _, seg := range FindBracketSegments(s) {
		inner := strings.TrimSpace(seg.Inner)
		if inner == "" {
			continue
		}
		switch c := r.Classify(seg); c.Kind {
		case KindVersion, KindDisambiguation:
			// 版本标记是曲名的一部分，歧义消解不是写法
		case KindCV, KindFeature:
			out = append(out, c.Payload)
		default:
			out = append(out, inner)
		}
	}
	return out
}

// Alternates 是包级入口，用默认规则表。
func Alternates(s string) []string { return defaultRules.Alternates(s) }

// versionMarkerRe 只匹配**真正的版本标记**，不含 feat. / ft. / with。
//
// 后三个在 VersionKeywords 表里，但它们标的是「参与者」而不是「哪一版录音」：
// 上游把 `By the Seaside (feat. Kitsui Akira)` 的录音名记作 `By the Seaside`，
// 把 feat. 当版本标记会让这条正确的候选被判成另一首歌。
var versionMarkerRe = buildVersionMarkerRe()

func buildVersionMarkerRe() *regexp.Regexp {
	return regexp.MustCompile(
		`(?i)(?:^|[\s\-_/（(［\[【\p{Han}\p{Hiragana}\p{Katakana}])(` + strings.Join(DefaultRules().versionOnlyKeywords(), "|") + `)(?:$|[\s\-_/0-9）)］\]】\p{Han}\p{Hiragana}\p{Katakana}])`)
}

// VersionMarkers 返回串中出现的版本标记词集合（标准化后）。
//
// 不止看括号内：`Tetra. -movie ver.-` 用的是破折号，而它与 `Tetra.` 同样
// 是两条不同的录音。
func VersionMarkers(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range versionMarkerRe.FindAllStringSubmatch(norm.NFKC.String(s), -1) {
		if key := Normalize(m[1]); key != "" {
			out[key] = true
		}
	}
	return out
}

// sameMarkers 报告两个标记集合是否相同。
func sameMarkers(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// StripNonVersionAnnotations 剥掉不属于版本标记的括号注释，保留版本标记那段。
//
// `綿雪の夢（繁雪之梦）` → `綿雪の夢`（译名可剥）
// `That Girl (DJLIU Remix)` → 原样（版本标记不可剥）
func (r *Rules) StripNonVersionAnnotations(s string) string {
	segs := FindBracketSegments(s)
	if len(segs) == 0 {
		return s
	}
	out := s
	// 从后往前删，避免前面的删除改变后面段的偏移
	for i := len(segs) - 1; i >= 0; i-- {
		if r.Classify(segs[i]).Kind == KindVersion {
			continue
		}
		out = out[:segs[i].Start] + out[segs[i].End:]
	}
	out = collapseSpaces(out)
	if strings.TrimSpace(out) == "" {
		return s
	}
	return out
}

// StripNonVersionAnnotations 是包级入口，用默认规则表。
func StripNonVersionAnnotations(s string) string {
	return defaultRules.StripNonVersionAnnotations(s)
}

// SplitVersionDescriptor 把曲名拆成「主干」与「版本描述」两段。
//
//	`星の在り処 Harmonica short Ver.` → 主干 `星の在り処 Harmonica`，版本描述 `short Ver.`
//	`That Girl (DJLIU Remix)`        → 主干 `That Girl`，版本描述 `DJLIU Remix`
//	`Tetra.`                         → 不可拆分
//
// 版本描述是这条录音身份的一部分（见 SearchCore），但它的**写法**并不稳定：
// 同一条录音在上游的录音名里记作 `星の在り処 (Harmonica Short version)`，
// 在专辑的曲目名里记作 `星の在り処 Harmonica short Ver.`——加不加括号、
// `Ver.` 还是 `version`，各处各写各的。拿整串去做逐词相邻的短语检索，
// 一处写法不同就整条落空；而主干是两种写法共有的那一段。
//
// 这个函数只陈述「曲名里哪一段是版本描述」这件事，不决定怎么用它：
// 判定「是不是同一版」仍是 CompareTitle 的事，那里版本标记不一致照旧不匹配；
// 检索时要不要放宽、放宽到什么程度，由各上游按自己的检索语义决定。
//
// 识别两种形态：
//
//	[1] 括号里的版本段      `雪 (Live)`、`Tetra. (off vocal)`
//	[2] 结尾未加括号的版本词串 `君の知らない物語 TV Size`、`Tetra. -movie ver.-`
//
// 第 2 种只认**一直延续到结尾**的版本词：`Live and Let Die`、`Radio Ga Ga`
// 里的版本词是曲名本身，后面还跟着别的词，不拆。主干剥空时同样不拆——
// 整个曲名就是一个版本词（`Live`）时没有主干可言。
func (r *Rules) SplitVersionDescriptor(title string) (base, descriptor string, ok bool) {
	rest := title
	var parts []string

	// [1] 括号里的版本段。从后往前删，避免前面的删除改变后面段的偏移
	segs := FindBracketSegments(rest)
	for i := len(segs) - 1; i >= 0; i-- {
		if r.Classify(segs[i]).Kind != KindVersion {
			continue
		}
		if inner := strings.TrimSpace(segs[i].Inner); inner != "" {
			parts = append([]string{inner}, parts...)
		}
		rest = rest[:segs[i].Start] + " " + rest[segs[i].End:]
	}
	rest = collapseSpaces(rest)

	// [2] 结尾未加括号的版本词串
	if loc := r.trailingVersionRe.FindStringSubmatchIndex(rest); loc != nil {
		if tail := trimVersionDelimiters(rest[loc[2]:]); tail != "" {
			parts = append([]string{tail}, parts...)
		}
		rest = rest[:loc[2]]
	}

	base = trimVersionDelimiters(rest)
	if len(parts) == 0 || Normalize(base) == "" {
		return title, "", false
	}
	return base, strings.Join(parts, " "), true
}

// SplitVersionDescriptor 是包级入口，用默认规则表。
func SplitVersionDescriptor(title string) (base, descriptor string, ok bool) {
	return defaultRules.SplitVersionDescriptor(title)
}

// trimVersionDelimiters 去掉两端的空白与版本分隔符（`-movie ver.-` 外面那对破折号）。
//
// 句点只在开头去：结尾的句点可能是内容（`Tetra.`、`Ver.`）。
func trimVersionDelimiters(s string) string {
	isDelimiter := func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("-–—−_/~〜～", r)
	}
	return strings.TrimFunc(s, isDelimiter)
}
