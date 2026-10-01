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
