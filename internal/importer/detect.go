package importer

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DelimiterResult 是分隔符探测的结果。
type DelimiterResult struct {
	Delimiter rune
	// FromExtension 为真表示结果与扩展名的猜测一致。
	FromExtension bool
	Confidence    float64
}

// candidateDelimiters 是被统计的候选分隔符。
var candidateDelimiters = []rune{',', ';', '\t', '|'}

// delimiterSampleLines 是统计所取的行数。
const delimiterSampleLines = 50

// DetectDelimiter 探测分隔符。
//
// 先按扩展名猜测（.tsv → Tab，.csv → 逗号），再在前 50 行统计各候选的
// 每行出现次数方差，取方差最小且计数大于零者。
//
// 内容统计优先于扩展名：扩展名为 .csv 但实际以分号分隔时，
// 统计结果使分号胜出。
func DetectDelimiter(text, filename string) DelimiterResult {
	guess := delimiterFromExtension(filename)

	lines := sampleLines(text, delimiterSampleLines)
	if len(lines) == 0 {
		return DelimiterResult{Delimiter: guess, FromExtension: true, Confidence: 0.5}
	}

	type stat struct {
		delim    rune
		variance float64
		mean     float64
	}
	var stats []stat
	for _, d := range candidateDelimiters {
		counts := make([]float64, 0, len(lines))
		for _, line := range lines {
			counts = append(counts, float64(strings.Count(line, string(d))))
		}
		mean, variance := meanVariance(counts)
		if mean <= 0 {
			continue // 计数为零的候选不参与
		}
		stats = append(stats, stat{delim: d, variance: variance, mean: mean})
	}
	if len(stats) == 0 {
		return DelimiterResult{Delimiter: guess, FromExtension: true, Confidence: 0.5}
	}

	// 方差最小者胜出；方差相同时取每行出现次数更多的
	best := stats[0]
	for _, s := range stats[1:] {
		if s.variance < best.variance || (s.variance == best.variance && s.mean > best.mean) {
			best = s
		}
	}

	confidence := 0.95
	if best.variance > 0.5 {
		confidence = 0.7
	}
	return DelimiterResult{
		Delimiter:     best.delim,
		FromExtension: best.delim == guess,
		Confidence:    confidence,
	}
}

// delimiterFromExtension 按扩展名猜测分隔符。
func delimiterFromExtension(filename string) rune {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".tsv", ".tab":
		return '\t'
	default:
		return ','
	}
}

// meanVariance 返回样本的均值与方差。
func meanVariance(xs []float64) (mean, variance float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean = sum / float64(len(xs))

	for _, x := range xs {
		d := x - mean
		variance += d * d
	}
	variance /= float64(len(xs))
	return mean, variance
}

// sampleLines 取前 n 行非空行。
func sampleLines(text string, n int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= n {
			break
		}
	}
	return out
}

// headerDictionary 是表头词典，用于判定首行是否为表头以及列映射。
//
// 同时包含中文与英文的常见写法。
var headerDictionary = map[Field][]string{
	FieldTitle:    {"曲名", "歌名", "标题", "题目", "title", "track", "song", "song name", "name"},
	FieldArtist:   {"艺术家", "歌手", "艺人", "演唱", "artist", "singer", "performer", "vocalist"},
	FieldAlbum:    {"专辑", "唱片", "碟", "歌集", "album", "collection", "release"},
	FieldYear:     {"年份", "发行年", "年", "year", "date", "released"},
	FieldTrackNo:  {"音轨", "曲序", "序号", "track no", "track number", "trackno", "#", "no", "no.", "track_no"},
	FieldDiscNo:   {"碟号", "disc", "disc no", "disc number", "discno", "disc_no"},
	FieldDuration: {"时长", "长度", "duration", "length", "time"},
}

// Field 是导入的目标字段。
type Field string

const (
	FieldIgnore   Field = ""
	FieldTitle    Field = "title"
	FieldArtist   Field = "artist"
	FieldAlbum    Field = "album"
	FieldYear     Field = "year"
	FieldTrackNo  Field = "track_no"
	FieldDiscNo   Field = "disc_no"
	FieldDuration Field = "duration"
)

// AllFields 返回列映射下拉中可选的字段。
func AllFields() []Field {
	return []Field{
		FieldIgnore, FieldTitle, FieldArtist, FieldAlbum,
		FieldYear, FieldTrackNo, FieldDiscNo, FieldDuration,
	}
}

// FieldDisplayName 返回字段的中文名。
func FieldDisplayName(f Field) string {
	switch f {
	case FieldTitle:
		return "曲名"
	case FieldArtist:
		return "艺术家"
	case FieldAlbum:
		return "专辑名"
	case FieldYear:
		return "年份"
	case FieldTrackNo:
		return "音轨号"
	case FieldDiscNo:
		return "碟号"
	case FieldDuration:
		return "时长"
	default:
		return "忽略"
	}
}

// HeaderResult 是表头探测的结果。
type HeaderResult struct {
	HasHeader  bool
	Confidence float64
	// MatchedDictionary 为真表示首行命中了表头词典。
	MatchedDictionary bool
}

// DetectHeader 判定首行是否为表头。
//
// 依据两点：首行与后续行的类型分布差异（首行全为文本而后续行含数字或日期），
// 或首行是否命中表头词典。
func DetectHeader(rows [][]string) HeaderResult {
	if len(rows) == 0 {
		return HeaderResult{}
	}
	first := rows[0]

	// 命中词典即判为表头
	matched := 0
	for _, cell := range first {
		if MapColumn(cell) != FieldIgnore {
			matched++
		}
	}
	if matched >= 2 || (len(first) > 0 && matched == len(first)) {
		return HeaderResult{HasHeader: true, Confidence: 0.95, MatchedDictionary: true}
	}

	// 类型分布差异：首行全文本，后续行含数字
	if len(rows) < 2 {
		// 只有一行时，命中词典才算表头
		return HeaderResult{HasHeader: matched > 0, Confidence: 0.6, MatchedDictionary: matched > 0}
	}

	firstAllText := true
	for _, cell := range first {
		if isNumericOrDate(cell) {
			firstAllText = false
			break
		}
	}

	laterHasNumeric := false
	for _, row := range rows[1:min(len(rows), 20)] {
		for _, cell := range row {
			if isNumericOrDate(cell) {
				laterHasNumeric = true
				break
			}
		}
		if laterHasNumeric {
			break
		}
	}

	if firstAllText && laterHasNumeric {
		return HeaderResult{HasHeader: true, Confidence: 0.85}
	}
	// 首行与后续行类型分布一致且未命中词典 → 无表头
	return HeaderResult{HasHeader: false, Confidence: 0.8}
}

var (
	numericPattern  = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	datePattern     = regexp.MustCompile(`^\d{4}([-/年]\d{1,2}([-/月]\d{1,2}日?)?)?$`)
	durationPattern = regexp.MustCompile(`^\d{1,2}:\d{2}(:\d{2})?$`)
)

// isNumericOrDate 报告单元格是否为数字、日期或时长。
func isNumericOrDate(cell string) bool {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return false
	}
	return numericPattern.MatchString(cell) ||
		datePattern.MatchString(cell) ||
		durationPattern.MatchString(cell)
}

// minSubstringAliasLen 是允许参与「包含匹配」的别名最短长度。
//
// 更短的别名（no / # / 年 / 碟）只在完全相等时命中——
// 否则 Note 会因为含 "no" 而被误判为音轨号。
const minSubstringAliasLen = 3

// MapColumn 按表头词典把列名映射到字段；未命中返回 FieldIgnore。
//
// 未命中的列默认映射为忽略，用户可手动改选。
func MapColumn(header string) Field {
	normalized := normalizeHeader(header)
	if normalized == "" {
		return FieldIgnore
	}

	// 先找完全相等的，避免「专辑」被「碟」之类的短词抢先命中
	for field, aliases := range headerDictionary {
		for _, alias := range aliases {
			if normalized == normalizeHeader(alias) {
				return field
			}
		}
	}
	// 再找包含关系。短别名（如 no / # / 年 / 碟）只允许完全相等，
	// 否则 Note 会因为含 "no" 而被误判为音轨号。
	for _, field := range []Field{
		FieldTitle, FieldArtist, FieldAlbum, FieldYear,
		FieldTrackNo, FieldDiscNo, FieldDuration,
	} {
		for _, alias := range headerDictionary[field] {
			a := normalizeHeader(alias)
			if len([]rune(a)) < minSubstringAliasLen {
				continue
			}
			if strings.Contains(normalized, a) {
				return field
			}
		}
	}
	return FieldIgnore
}

// normalizeHeader 标准化列名：转小写、去空白与常见标点。
func normalizeHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "", "_", "", "-", "", "　", "").Replace(s)
	return s
}

// MapColumns 为全部列生成映射。
func MapColumns(headers []string) []Field {
	out := make([]Field, len(headers))
	for i, h := range headers {
		out[i] = MapColumn(h)
	}
	return out
}

// ParseDuration 把 3:41 / 221 / 221000 这类写法解析为毫秒。
func ParseDuration(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// mm:ss 或 hh:mm:ss
	if durationPattern.MatchString(s) {
		parts := strings.Split(s, ":")
		var total int
		for _, p := range parts {
			n, err := strconv.Atoi(p)
			if err != nil {
				return nil
			}
			total = total*60 + n
		}
		ms := total * 1000
		return &ms
	}
	// 纯数字：小于 10000 视为秒，否则视为毫秒
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		ms := n
		if n < 10000 {
			ms = n * 1000
		}
		return &ms
	}
	return nil
}

// ParseYear 从 2006 / 2006-12-18 这类写法中取出年份。
func ParseYear(s string) *int {
	s = strings.TrimSpace(s)
	if len(s) < 4 {
		return nil
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil || y < 1000 || y > 3000 {
		return nil
	}
	return &y
}

// ParseInt 解析整数；无法解析时返回 nil。
func ParseInt(s string) *int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	// 「2/12」这类写法取前一个数
	if i := strings.IndexByte(s, '/'); i > 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return nil
	}
	return &n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
