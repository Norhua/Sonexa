package matching

import (
	"regexp"
	"sort"
	"strings"
)

type qualifierRule struct {
	kind    string
	pattern *regexp.Regexp
}

// Patterns are semantic vocabulary, never song-name exceptions. The entire
// remaining annotation is the payload; sharing a marker is not equivalence.
var recordingQualifierRules = []qualifierRule{
	{"instrumental", regexp.MustCompile(`(?i)\b(?:off[ -]?vocal|instrumental|inst\.?|karaoke)\b|カラオケ|伴奏`)},
	{"remix", regexp.MustCompile(`(?i)\bremix\b`)},
	{"live", regexp.MustCompile(`(?i)\blive\b|现场版|現場版`)},
	{"remaster", regexp.MustCompile(`(?i)\bremaster(?:ed)?\b|重制|重製`)},
	{"rerecording", regexp.MustCompile(`(?i)\bre[ -]?record(?:ed|ing)?\b|重录|重錄`)},
	{"acoustic", regexp.MustCompile(`(?i)\bacoustic\b`)},
	{"version", regexp.MustCompile(`(?i)\b(?:mix|edit|ver\.?|version|cover|tv[ -]?size|short|extended|radio|demo|reprise)\b|翻唱`)},
}
var releaseQualifierRules = []qualifierRule{
	{"deluxe", regexp.MustCompile(`(?i)\bdeluxe(?:\s+edition)?\b|豪华版|豪華版`)},
	{"limited", regexp.MustCompile(`(?i)\blimited(?:\s+edition)?\b|限定版|初回限定盤`)},
	{"reissue", regexp.MustCompile(`(?i)\breissue(?:d)?\b|再版`)},
	{"remaster", regexp.MustCompile(`(?i)\bremaster(?:ed)?\b|重制|重製`)},
	{"format", regexp.MustCompile(`(?i)^(?:CD|digital|vinyl|LP|SACD|DVD|Blu-ray)$`)},
	{"country", regexp.MustCompile(`^(?:JP|US|GB|DE|FR|CA|AU|XW)$`)},
}
var metadataVariantSuffix = regexp.MustCompile(`(?i)\s+((?:\d{4}\s+)?(?:off[ -]?vocal|instrumental|karaoke|live|remix|remaster(?:ed)?|re[ -]?record(?:ed|ing)?|acoustic|deluxe(?:\s+edition)?|limited(?:\s+edition)?|reissue)(?:\s+.*)?)$`)
var metadataVariantDelimiter = regexp.MustCompile(`\s+[-–—:/]\s+`)

func metadataQualifier(text string, field MetadataField) (Qualifier, bool) {
	rules := recordingQualifierRules
	if field == MetadataAlbum {
		rules = releaseQualifierRules
	}
	if field == MetadataArtist {
		return Qualifier{}, false
	}
	for _, rule := range rules {
		if rule.pattern.MatchString(text) {
			payload := rule.pattern.ReplaceAllString(text, " ")
			if rule.kind == "country" || rule.kind == "format" || rule.kind == "version" {
				payload = text
			}
			// Cosmetic version suffixes do not erase author/venue/year details.
			payload = regexpMetadataEdition.ReplaceAllString(payload, " ")
			return Qualifier{Kind: rule.kind, Payload: Normalize(payload), Raw: text}, true
		}
	}
	return Qualifier{}, false
}

var regexpMetadataEdition = regexp.MustCompile(`(?i)\b(?:version|ver\.?|edition)\b`)

func metadataOuterQualifier(body string, field MetadataField) (string, []Qualifier) {
	// A delimiter gives a reliable boundary for the full qualifier payload.
	for _, loc := range metadataVariantDelimiter.FindAllStringIndex(body, -1) {
		if q, ok := metadataQualifier(body[loc[1]:], field); ok {
			return strings.TrimSpace(body[:loc[0]]), []Qualifier{q}
		}
	}
	if loc := metadataVariantSuffix.FindStringSubmatchIndex(body); loc != nil {
		if q, ok := metadataQualifier(body[loc[2]:loc[3]], field); ok {
			return strings.TrimSpace(body[:loc[0]]), []Qualifier{q}
		}
	}
	return body, nil
}

func metadataQualifierKeys(p TextProfile) []string {
	keys := make([]string, 0, len(p.Qualifiers))
	for _, q := range p.Qualifiers {
		keys = append(keys, q.Kind+":"+q.Payload)
	}
	sort.Strings(keys)
	return keys
}

func sameMetadataQualifiers(a, b TextProfile) bool {
	ka, kb := metadataQualifierKeys(a), metadataQualifierKeys(b)
	return strings.Join(ka, "\n") == strings.Join(kb, "\n")
}
