package domain

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
	"strings"
)

const GroupNormalizerVersion = 1

// NormalizeGroupName 只统一字符形式、大小写与空白，保留版本后缀、CV 和 feat。
func NormalizeGroupName(value string) string {
	return strings.Join(strings.Fields(cases.Fold().String(norm.NFKC.String(value))), " ")
}
func NameEvidence(title Field[string], credit Field[Credit]) (string, string) {
	if title.State != Value || credit.State != Value {
		return "", ""
	}
	t, c := NormalizeGroupName(title.Value), NormalizeGroupName(credit.Value.Text)
	if t == "" || c == "" {
		return "", ""
	}
	return t, c
}
func SameGroup(a, b ReleaseSummary) bool {
	if a.Group != nil && b.Group != nil && a.Group.Provider != "" && a.Group.Ref != "" && *a.Group == *b.Group {
		return true
	}
	at, ac := NameEvidence(a.Title, a.Credit)
	bt, bc := NameEvidence(b.Title, b.Credit)
	return at != "" && ac != "" && at == bt && ac == bc
}

// LooseGroupName 用于提示：去掉括号段与 feat. 附注之后再规范化。
// 它不参与任何判组决定——名称近似不是同一组的证据。
func LooseGroupName(value string) string {
	var out []rune
	depth := 0
	for _, r := range norm.NFKC.String(value) {
		switch r {
		case '(', '[', '【':
			depth++
			continue
		case ')', ']', '】':
			if depth > 0 {
				depth--
			}
			continue
		}
		if depth == 0 {
			out = append(out, r)
		}
	}
	text := string(out)
	lower := strings.ToLower(text)
	for _, marker := range []string{" feat.", " feat ", " ft.", " ft ", " featuring "} {
		if i := strings.Index(lower, marker); i >= 0 {
			text, lower = text[:i], lower[:i]
		}
	}
	return NormalizeGroupName(text)
}
