package itunes

import (
	"os"
	"strings"

	"github.com/norhua/sonexa/internal/matching"
)

// CountrySuggestion 是文字系统探测给出的地区建议。
type CountrySuggestion struct {
	// Country 是建议的地区码；无探测结果时为空。
	Country string
	// ScriptName 是探测到的文字系统的中文名，用于界面提示。
	ScriptName string
	// Detected 为真表示确实探测到了文字系统特征。
	Detected bool
}

// Message 返回界面上的提示文案，如「检测到日文，已建议使用 日本 地区」。
//
// 探测结果仅作为建议，必须在界面上明确提示——
// 否则用户不理解为什么搜出来的结果是某个地区的。
func (s CountrySuggestion) Message() string {
	if !s.Detected {
		return ""
	}
	return "检测到" + s.ScriptName + "，已建议使用 " + CountryDisplayName(s.Country) + " 地区"
}

// DetectCountry 依据输入文本的文字系统给出地区建议。
//
//	含平假名/片假名 → jp
//	含谚文         → kr
//	含简体特有字    → cn
//	含繁体特有字    → tw
//	含西里尔字母    → ru
//	其余           → 退回默认地区（Detected 为 false）
func DetectCountry(text string) CountrySuggestion {
	switch matching.DominantScript(text) {
	case matching.ScriptKana:
		return CountrySuggestion{Country: "jp", ScriptName: "日文", Detected: true}
	case matching.ScriptHangul:
		return CountrySuggestion{Country: "kr", ScriptName: "韩文", Detected: true}
	case matching.ScriptCyrillic:
		return CountrySuggestion{Country: "ru", ScriptName: "西里尔文", Detected: true}
	case matching.ScriptHan:
		// 汉字同属 CJK 区，靠简繁独有字区分中国大陆与台湾地区
		switch matching.DetectHanVariant(text) {
		case matching.HanSimplified:
			return CountrySuggestion{Country: "cn", ScriptName: "简体中文", Detected: true}
		case matching.HanTraditional:
			return CountrySuggestion{Country: "tw", ScriptName: "繁体中文", Detected: true}
		}
		return CountrySuggestion{}
	default:
		return CountrySuggestion{}
	}
}

// ResolveCountry 按优先级确定最终使用的地区：
//
//	用户手动选择  >  文字系统探测建议  >  设置中的默认地区（默认跟随系统 locale）
//
// 手动选择优先意味着探测建议不会反复覆盖用户的选择。
func (p *Provider) ResolveCountry(manual, text string) string {
	if c := strings.TrimSpace(manual); c != "" {
		return strings.ToLower(c)
	}
	if s := DetectCountry(text); s.Detected {
		return s.Country
	}
	if c := strings.TrimSpace(p.cfg.DefaultCountry); c != "" {
		return strings.ToLower(c)
	}
	return SystemCountry()
}

// SystemCountry 从系统区域设置推断默认地区；无法判定时退回美国。
func SystemCountry() string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(key); v != "" {
			if c := countryFromLocale(v); c != "" {
				return c
			}
		}
	}
	return "us"
}

// countryFromLocale 从 zh_CN.UTF-8 / ja_JP 这类串中取出地区码。
func countryFromLocale(locale string) string {
	locale = strings.SplitN(locale, ".", 2)[0]
	parts := strings.FieldsFunc(locale, func(r rune) bool { return r == '_' || r == '-' })
	if len(parts) >= 2 && len(parts[1]) == 2 {
		return strings.ToLower(parts[1])
	}
	return ""
}

// countryNames 是地区码到中文名的映射，用于界面提示。
var countryNames = map[string]string{
	"jp": "日本", "cn": "中国大陆", "tw": "台湾地区", "hk": "中国香港",
	"kr": "韩国", "us": "美国", "gb": "英国", "ru": "俄罗斯",
	"de": "德国", "fr": "法国", "ca": "加拿大", "au": "澳大利亚",
}

// CountryDisplayName 返回地区码的中文名；未知时返回大写的地区码本身。
func CountryDisplayName(code string) string {
	if name, ok := countryNames[strings.ToLower(code)]; ok {
		return name
	}
	return strings.ToUpper(code)
}

// SupportedCountries 返回地区下拉的候选列表。
func SupportedCountries() []string {
	return []string{"jp", "cn", "tw", "hk", "kr", "us", "gb", "ru", "de", "fr", "ca", "au"}
}
