package matching

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var metadataCreditJoin = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring|with|&|×)\s+|[;；、]`)
var metadataCV = regexp.MustCompile(`(?i)^\s*(?:c\.?v\.?|声優|声优|配音)\s*[:：.。]?\s*(.+)$`)

func ParseMetadataCredit(raw string) CreditEvidence {
	out := CreditEvidence{Text: ParseMetadataText(raw, MetadataArtist)}
	text := norm.NFKC.String(raw)
	segments := FindBracketSegments(text)
	start := 0
	appendPart := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		part := CreditPart{Name: s, Role: "credited"}
		for _, seg := range FindBracketSegments(s) {
			if m := metadataCV.FindStringSubmatch(seg.Inner); m != nil {
				part.Name, part.Role, part.Voice = seg.Outer, "character", strings.TrimSpace(m[1])
			}
		}
		out.Parts = append(out.Parts, part)
	}
	for _, loc := range metadataCreditJoin.FindAllStringIndex(text, -1) {
		inside := false
		for _, seg := range segments {
			if loc[0] >= seg.Start && loc[0] < seg.End {
				inside = true
				break
			}
		}
		if !inside {
			appendPart(text[start:loc[0]])
			start = loc[1]
		}
	}
	appendPart(text[start:])
	return out
}

// CreditRelation is an explicit source fact, not an inferred association from
// co-occurrence, a shared voice actor or the user's input. Target context must
// be independently verified before it can explain different flat credits.
type CreditRelation struct {
	Kind   string `json:"kind"` // release_credit or group_member
	From   string `json:"from"`
	To     string `json:"to"`
	Source string `json:"source"`
}

type MetadataCreditContext struct {
	Text             MetadataTextContext
	AlbumVerified    bool
	PositionVerified bool
	Relations        []CreditRelation
}

func CompareMetadataCredit(a, b CreditEvidence, ctx MetadataCreditContext) FieldEvidence {
	ctx.Text.Field = MetadataArtist
	e := CompareMetadataText(a.Text, b.Text, ctx.Text)
	if e.Level == EvidenceExact {
		return e
	}
	// Explicit ensembles and roles are constraints. A release-level relation
	// may explain a group credit, but cannot erase a different named character
	// or replace one collaborator with another.
	if metadataExplicitCredit(a) || metadataExplicitCredit(b) {
		if len(a.Parts) != len(b.Parts) {
			e.Level, e.Reason.Code = EvidenceConflict, "different_credit_members"
			return e
		}
		for i, left := range a.Parts {
			right := b.Parts[i]
			if left.Role != right.Role || Normalize(left.Voice) != Normalize(right.Voice) {
				e.Level, e.Reason.Code = EvidenceConflict, "different_credit_role"
				return e
			}
			part := CompareMetadataText(ParseMetadataText(left.Name, MetadataArtist), ParseMetadataText(right.Name, MetadataArtist), ctx.Text)
			if !part.Reliable() {
				e.Level, e.Reason.Code = part.Level, "different_credit_members"
				return e
			}
		}
		e.Level, e.Reason.Code = EvidenceVerifiedAlias, "same_credit_members"
		return e
	}
	if e.Reliable() {
		return e
	}
	if ctx.AlbumVerified && ctx.PositionVerified && len(a.Text.Unknown) == 0 && len(b.Text.Unknown) == 0 {
		for _, rel := range ctx.Relations {
			if rel.Source != "" && (rel.Kind == "release_credit" || rel.Kind == "group_member") &&
				Normalize(rel.From) == Normalize(a.Text.Body) && Normalize(rel.To) == Normalize(b.Text.Body) {
				e.Level, e.Source, e.Reason.Code = EvidenceVerifiedAlias, rel.Source, "source_credit_relation"
				return e
			}
		}
	}
	return e
}

func metadataExplicitCredit(c CreditEvidence) bool {
	if len(c.Parts) > 1 {
		return true
	}
	for _, p := range c.Parts {
		if p.Role == "character" {
			return true
		}
	}
	return false
}
