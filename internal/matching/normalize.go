package matching

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// romanNumerals 把 Unicode 的罗马数字字符映射为等价的拉丁字母写法。
//
// NFKC 已能处理大部分（Ⅲ → III），此表覆盖 NFKC 不转换的形态，
// 使「Ⅲ」与「III」在标准化后一定相等。
var romanNumerals = map[rune]string{
	'Ⅰ': "i", 'Ⅱ': "ii", 'Ⅲ': "iii", 'Ⅳ': "iv", 'Ⅴ': "v",
	'Ⅵ': "vi", 'Ⅶ': "vii", 'Ⅷ': "viii", 'Ⅸ': "ix", 'Ⅹ': "x",
	'Ⅺ': "xi", 'Ⅻ': "xii", 'Ⅼ': "l", 'Ⅽ': "c", 'Ⅾ': "d", 'Ⅿ': "m",
	'ⅰ': "i", 'ⅱ': "ii", 'ⅲ': "iii", 'ⅳ': "iv", 'ⅴ': "v",
	'ⅵ': "vi", 'ⅶ': "vii", 'ⅷ': "viii", 'ⅸ': "ix", 'ⅹ': "x",
	'ⅺ': "xi", 'ⅻ': "xii", 'ⅼ': "l", 'ⅽ': "c", 'ⅾ': "d", 'ⅿ': "m",
}

// Normalize 把文本标准化为可比较的形式，依次执行：
//
//  1. Unicode NFKC 规范化（顺带统一全角与半角）
//  2. 转小写
//  3. 罗马数字统一（Ⅲ → III）
//  4. 日文假名统一（片假名 → 平假名）
//  5. 去除空白与标点
//
// Normalize 不剥离括号内容：标准化是无语义的机械变换，
// 括号处理是有语义判定的独立步骤（见 brackets.go）。
// 混在一起会让两者都无法单独测试。
func Normalize(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if replacement, ok := romanNumerals[r]; ok {
			b.WriteString(replacement)
			continue
		}
		// 片假名 → 平假名：两者在 Unicode 中相隔固定的 0x60。
		// 长音符「ー」(U+30FC) 不在此区间，保持原样。
		if r >= 0x30A1 && r <= 0x30F6 {
			b.WriteRune(r - 0x60)
			continue
		}
		// 去除空白与标点；符号（如 ♪）一并去除，它们同样不影响曲名的同一性。
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NormalizeKeepBrackets 与 Normalize 相同，但保留括号字符本身。
// 供需要在标准化后仍识别括号结构的场合使用。
func NormalizeKeepBrackets(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if replacement, ok := romanNumerals[r]; ok {
			b.WriteString(replacement)
			continue
		}
		if r >= 0x30A1 && r <= 0x30F6 {
			b.WriteRune(r - 0x60)
			continue
		}
		if isBracketRune(r) {
			b.WriteRune(r)
			continue
		}
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Tokens 返回标准化后的词元集合，用于「词元包含」的模糊匹配判定。
// 按空白切分后各自标准化，因此保留了词的边界。
func Tokens(s string) []string {
	fields := strings.Fields(norm.NFKC.String(s))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if n := Normalize(f); n != "" {
			out = append(out, n)
		}
	}
	return out
}
