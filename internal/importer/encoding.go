package importer

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	textunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

// EncodingResult 是编码探测的结果。
type EncodingResult struct {
	// Name 是编码名，如 UTF-8 / GB18030 / Shift_JIS。
	Name string
	// Confidence 是置信度（0..1）。低于 0.8 时界面自动展开手动面板。
	Confidence float64
	// HadBOM 为真表示由字节序标记直接判定。
	HadBOM bool
	// Text 是按该编码解码后的文本。
	Text string
}

// candidateEncodings 是无 BOM 且非 UTF-8 时逐个尝试的候选集。
var candidateEncodings = []struct {
	Name string
	Enc  encoding.Encoding
}{
	{"GB18030", simplifiedchinese.GB18030},
	{"Shift_JIS", japanese.ShiftJIS},
	{"Big5", traditionalchinese.Big5},
	{"EUC-JP", japanese.EUCJP},
	{"UTF-16LE", textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM)},
	{"UTF-16BE", textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM)},
}

// DetectEncoding 探测字节流的编码。
//
// 顺序：
//  1. 先检查字节序标记（UTF-8 / UTF-16LE / UTF-16BE）
//  2. 无标记时严格校验是否为 UTF-8
//  3. 校验失败时按候选集逐个解码，以「CJK 字符占比」与
//     「非法或替换字符数」打分，取分值最高者
func DetectEncoding(data []byte) EncodingResult {
	// [1] 字节序标记
	if r, ok := detectBOM(data); ok {
		return r
	}

	// [2] 严格校验 UTF-8
	if utf8.Valid(data) {
		text := string(data)
		// 纯 ASCII 也是合法 UTF-8，置信度同样很高
		return EncodingResult{Name: "UTF-8", Confidence: 1.0, Text: text}
	}

	// [3] 候选集打分
	best := EncodingResult{Name: "UTF-8", Confidence: 0, Text: string(data)}
	bestScore := -1.0
	for _, c := range candidateEncodings {
		text, err := decodeWith(c.Enc, data)
		if err != nil {
			continue
		}
		score := scoreDecoded(text)
		if score > bestScore {
			bestScore = score
			best = EncodingResult{Name: c.Name, Confidence: normalizeScore(score), Text: text}
		}
	}
	return best
}

// detectBOM 按字节序标记判定编码。
func detectBOM(data []byte) (EncodingResult, bool) {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return EncodingResult{
			Name: "UTF-8", Confidence: 1.0, HadBOM: true,
			Text: string(data[3:]),
		}, true
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		text, err := decodeWith(textunicode.UTF16(textunicode.LittleEndian, textunicode.IgnoreBOM), data[2:])
		if err != nil {
			return EncodingResult{}, false
		}
		return EncodingResult{Name: "UTF-16LE", Confidence: 1.0, HadBOM: true, Text: text}, true
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		text, err := decodeWith(textunicode.UTF16(textunicode.BigEndian, textunicode.IgnoreBOM), data[2:])
		if err != nil {
			return EncodingResult{}, false
		}
		return EncodingResult{Name: "UTF-16BE", Confidence: 1.0, HadBOM: true, Text: text}, true
	}
	return EncodingResult{}, false
}

// decodeWith 用指定编码解码字节流。
func decodeWith(enc encoding.Encoding, data []byte) (string, error) {
	decoded, _, err := transform.Bytes(enc.NewDecoder(), data)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

// scoreDecoded 给解码结果打分。分值越高表示该编码越可能是正确的。
//
// 三类信号：
//
//	假名          极强的正信号。按错误编码解码几乎不会产出连续假名，
//	              因此它基本只出现在「日文文本 + 正确编码」这一种情形。
//	常用汉字      弱正信号。用错编码同样可能产出汉字（GB18030 会把
//	              Shift_JIS 的日文字节解成一串罕用汉字），故权重低于假名。
//	替换/控制/罕用字  强负信号，乱码的典型特征。
//
// 只数「CJK 占比」不足以区分：GB18030 解 Shift_JIS 的日文会得到
// 嬋柤傾乕僥 这类合法但荒谬的汉字串，其 CJK 占比反而高于正确解码的结果。
func scoreDecoded(text string) float64 {
	var kana, commonHan, rareHan, replacement, control, implausible, mixedScript, total int
	for _, r := range text {
		total++
		switch {
		case r == '\uFFFD':
			replacement++
		case isKana(r):
			kana++
		case isImplausible(r):
			implausible++
		case isHan(r):
			if isCommonHan(r) {
				commonHan++
			} else {
				rareHan++
			}
		case isHangul(r):
			commonHan++
		case isMixedScriptLetter(r):
			// CJK 文本里夹着西里尔/希腊/IPA 字母，是误解码的产物：
			// Big5 解 GB18030 的中文会得到 Ж靡 / ʊ坒 这类混排。
			mixedScript++
		case r < 0x20 && r != '\n' && r != '\r' && r != '\t':
			control++
		}
	}
	if total == 0 {
		return 0
	}

	t := float64(total)
	score := float64(kana)/t*3.0 + // 假名：强正信号
		float64(commonHan)/t*0.6 // 常用汉字：弱正信号
	score -= float64(rareHan) / t * 1.5                        // 罕用汉字：可疑
	score -= float64(mixedScript) / t * 4.0                    // 异文字混排：可疑
	score -= float64(replacement+control+implausible) / t * 10 // 乱码：强负信号
	return score
}

// isMixedScriptLetter 报告 r 是否为出现在 CJK 文本中即属异常的字母。
//
// 西里尔、希腊与 IPA 扩展字母不会出现在正常的中日文曲目信息里；
// 它们的出现说明这段字节被按错误的编码解释了。
// 拉丁字母不在其中——曲名中的英文是常态。
func isMixedScriptLetter(r rune) bool {
	switch {
	case unicode.Is(unicode.Cyrillic, r):
		return true
	case unicode.Is(unicode.Greek, r):
		return true
	case r >= 0x0250 && r <= 0x02AF: // IPA 扩展
		return true
	case r >= 0x02B0 && r <= 0x02FF: // 间距修饰字母
		return true
	}
	return false
}

// isImplausible 报告 r 是否落在正常曲目文本里几乎不会出现的区段。
//
// 这些区段是「按错误编码解码」的典型产物：
// 私用区、代理区、CJK 扩展区（罕用字）、以及各类符号区。
func isImplausible(r rune) bool {
	switch {
	case isHalfWidthKana(r): // 半角片假名：误解码的典型产物
		return true
	case r >= 0xE000 && r <= 0xF8FF: // 私用区
		return true
	case r >= 0xD800 && r <= 0xDFFF: // 代理区
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK 扩展 A（罕用）
		return true
	case r >= 0x20000: // CJK 扩展 B 及以上（罕用）
		return true
	case r >= 0x2E80 && r <= 0x2FFF: // 康熙部首 / CJK 笔画
		return true
	case r >= 0x3100 && r <= 0x312F: // 注音符号
		return true
	case r >= 0xA000 && r <= 0xA4CF: // 彝文
		return true
	case r >= 0x1100 && r <= 0x11FF: // 谚文字母（非音节）
		return true
	}
	return false
}

// normalizeScore 把打分映射到 0..1 的置信度。
func normalizeScore(score float64) float64 {
	switch {
	case score >= 0.3:
		return 0.95
	case score >= 0.1:
		return 0.85
	case score >= 0:
		return 0.7
	default:
		return 0.4
	}
}

// isKana 报告 r 是否为全角平假名或片假名。
//
// 半角片假名（U+FF66..U+FF9D）被排除在外：真实日文文本用全角假名，
// 而用 Shift_JIS 误解中文字节恰恰会大量产出半角片假名。
// 把它算作正信号会让 Shift_JIS 在 GB18030 / Big5 的样例上胜出。
func isKana(r rune) bool {
	return unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// isHalfWidthKana 报告 r 是否为半角片假名——按错误编码解码的典型产物。
func isHalfWidthKana(r rune) bool { return r >= 0xFF61 && r <= 0xFF9F }

// isHan 报告 r 是否为汉字。
func isHan(r rune) bool { return unicode.Is(unicode.Han, r) }

// isHangul 报告 r 是否为谚文音节。
func isHangul(r rune) bool { return unicode.Is(unicode.Hangul, r) }

// isCommonHan 报告 r 是否落在常用汉字区（CJK 统一表意文字基本区）。
func isCommonHan(r rune) bool { return r >= 0x4E00 && r <= 0x9FFF }

// isCJK 报告 r 是否为中日韩字符或假名。
func isCJK(r rune) bool { return isHan(r) || isKana(r) || isHangul(r) }

func unicodeIs(r rune, table *unicode.RangeTable) bool { return unicode.Is(table, r) }

// StripBOM 去掉文本开头的 UTF-8 字节序标记字符。
func StripBOM(s string) string { return strings.TrimPrefix(s, "\ufeff") }

// SupportedEncodings 返回手动面板中可选的编码列表。
func SupportedEncodings() []string {
	out := []string{"UTF-8"}
	for _, c := range candidateEncodings {
		out = append(out, c.Name)
	}
	return out
}

// DecodeAs 按指定编码名解码，供用户手动改写编码时使用。
func DecodeAs(name string, data []byte) (string, error) {
	if r, ok := detectBOM(data); ok && r.Name == name {
		return r.Text, nil
	}
	if name == "UTF-8" {
		return StripBOM(string(data)), nil
	}
	for _, c := range candidateEncodings {
		if c.Name == name {
			return decodeWith(c.Enc, data)
		}
	}
	return "", fmt.Errorf("不支持的编码 %q", name)
}
