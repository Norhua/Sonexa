package matching

import "unicode"

// HanVariant 是汉字的字形变体。
//
// 日文汉字与中文汉字同属 CJK 统一表意文字区，无法靠码位区分文种；
// 但简体独有字与繁体/日文独有字是可以分辨的，这使得
// 「夕立（驟雨）」这类同属汉字、却是异写关系的括号能被判为翻译变体。
type HanVariant string

const (
	HanUnknown     HanVariant = ""
	HanSimplified  HanVariant = "simplified"
	HanTraditional HanVariant = "traditional"
)

// simplifiedOnly 是简体中文独有字（其繁体写法为不同码位）。
var simplifiedOnly = map[rune]bool{
	'骤': true, '门': true, '马': true, '车': true, '龙': true, '东': true,
	'爱': true, '讲': true, '说': true, '语': true, '员': true, '国': true,
	'学': true, '会': true, '实': true, '发': true, '样': true, '风': true,
	'电': true, '产': true, '业': true, '来': true, '过': true, '还': true,
	'进': true, '这': true, '时': true, '书': true, '长': true, '问': true,
	'开': true, '关': true, '战': true, '见': true, '认': true, '读': true,
	'录': true, '汉': true, '与': true, '专': true, '丽': true, '医': true,
}

// traditionalOnly 是繁体/日文独有字（其简体写法为不同码位）。
var traditionalOnly = map[rune]bool{
	'驟': true, '門': true, '馬': true, '車': true, '龍': true, '東': true,
	'愛': true, '講': true, '說': true, '語': true, '員': true, '國': true,
	'學': true, '會': true, '實': true, '發': true, '樣': true, '風': true,
	'電': true, '產': true, '業': true, '來': true, '過': true, '還': true,
	'進': true, '這': true, '時': true, '書': true, '長': true, '問': true,
	'開': true, '關': true, '戰': true, '見': true, '認': true, '讀': true,
	'錄': true, '漢': true, '與': true, '專': true, '麗': true, '醫': true,
}

// DetectHanVariant 判定串中汉字的字形变体；无独有字时返回 HanUnknown。
func DetectHanVariant(s string) HanVariant {
	var simplified, traditional int
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			continue
		}
		if simplifiedOnly[r] {
			simplified++
		}
		if traditionalOnly[r] {
			traditional++
		}
	}
	switch {
	case simplified > traditional:
		return HanSimplified
	case traditional > simplified:
		return HanTraditional
	default:
		return HanUnknown
	}
}
