package matching

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

type MetadataField string

const (
	MetadataTitle  MetadataField = "title"
	MetadataArtist MetadataField = "artist"
	MetadataAlbum  MetadataField = "album"
)

// MetadataTextContext carries corroboration from OTHER fields or target facts.
// It must not be derived from the alias under comparison or a search score.
type MetadataTextContext struct {
	Field              MetadataField
	Source             string
	IndependentSupport bool
}

var metadataAliasHint = regexp.MustCompile(`(?i)^(?:译名|翻译|中文名|英文名|原名|原題|原题|読み|读音|讀音|片假名|平假名|罗马音|羅馬音|romaji|translation|original(?: name)?|reading)\s*[:：.。]?\s*(.+)$`)

func ParseMetadataText(raw string, field MetadataField) TextProfile {
	p := TextProfile{Raw: raw, Body: strings.TrimSpace(raw)}
	text := norm.NFKC.String(strings.TrimSpace(raw))
	if strings.HasSuffix(text, "...") || strings.HasSuffix(text, "…") {
		p.Truncated = true
		text = strings.TrimRight(text, ".… ")
	}
	segments := FindBracketSegments(text)
	body := text
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		inner := strings.TrimSpace(seg.Inner)
		if len(FindBracketSegments(inner)) > 0 {
			p.Unknown = append(p.Unknown, inner)
			continue
		}
		qualifier, isQualifier := metadataQualifier(inner, field)
		switch {
		case isQualifier:
			p.Qualifiers = append(p.Qualifiers, qualifier)
		case field == MetadataTitle && strings.EqualFold(inner, "vocal"):
			p.Aliases = append(p.Aliases, NameForm{Basis: "ordinary_vocal", Verified: true})
		case metadataAliasHint.MatchString(inner):
			payload := metadataAliasHint.FindStringSubmatch(inner)[1]
			p.Aliases = append(p.Aliases, NameForm{Text: payload, Basis: "explicit_alias", Verified: true})
		default:
			// The old classifier is safe only as a source of possible aliases,
			// never as proof that a translation is correct.
			cl := defaultRules.Classify(seg)
			if cl.Kind == KindTranslation {
				p.Aliases = append(p.Aliases, NameForm{Text: inner, Basis: "script_alternative"})
			} else {
				p.Unknown = append(p.Unknown, inner)
				continue
			}
		}
		body = body[:seg.Start] + body[seg.End:]
	}
	p.Body = strings.TrimSpace(collapseSpaces(body))
	var outerQualifiers []Qualifier
	p.Body, outerQualifiers = metadataOuterQualifier(p.Body, field)
	p.Qualifiers = append(p.Qualifiers, outerQualifiers...)
	// An unmatched bracket is not a verified alias, even if the query layer
	// can search its prefix. Keep the fragment for later explanations.
	for _, r := range p.Body {
		if isBracketRune(r) {
			p.Unknown = append(p.Unknown, p.Body)
			break
		}
	}
	return p
}

// WithMetadataAliases attaches only explicitly provided source aliases. The
// caller must pass their provenance; query variants are not source aliases.
func WithMetadataAliases(p TextProfile, source string, aliases ...string) TextProfile {
	p.Aliases = append([]NameForm(nil), p.Aliases...)
	if source == "" {
		return p
	}
	for _, alias := range aliases {
		if Normalize(alias) != "" {
			p.Aliases = append(p.Aliases, NameForm{Text: alias, Basis: source, Verified: true})
		}
	}
	return p
}

func CompareMetadataText(a, b TextProfile, ctx MetadataTextContext) FieldEvidence {
	e := FieldEvidence{Level: EvidenceUnknown, InputForm: a.Raw, FactForm: b.Raw, Source: ctx.Source,
		Reason: DecisionReason{Code: "missing_text", Field: string(ctx.Field)}}
	finish := func(level EvidenceLevel, code, left, right string) FieldEvidence {
		e.Level, e.Reason.Code, e.InputForm, e.FactForm = level, code, left, right
		return e
	}
	na, nb := NormalizeKeepBrackets(a.Raw), NormalizeKeepBrackets(b.Raw)
	if na == "" || nb == "" {
		return e
	}
	if a.Truncated || b.Truncated {
		pa, pb := Normalize(a.Body), Normalize(b.Body)
		if len(a.Unknown) == 0 && len(b.Unknown) == 0 && pa != "" && pb != "" &&
			((a.Truncated && strings.HasPrefix(pb, pa)) || (b.Truncated && strings.HasPrefix(pa, pb))) {
			return finish(EvidenceCompatibleTruncation, "compatible_truncation", a.Body, b.Body)
		}
		if len(a.Unknown) == 0 && len(b.Unknown) == 0 && a.Truncated != b.Truncated && pa != "" && pb != "" && DominantScript(a.Body) == DominantScript(b.Body) {
			return finish(EvidenceConflict, "different_truncated_prefix", a.Body, b.Body)
		}
		return finish(EvidenceUnknown, "unresolved_truncation", a.Body, b.Body)
	}
	if !sameMetadataQualifiers(a, b) {
		return finish(EvidenceConflict, "different_qualifier", a.Raw, b.Raw)
	}
	if na == nb {
		return finish(EvidenceExact, "normalized_text", a.Raw, b.Raw)
	}
	if len(a.Unknown) > 0 || len(b.Unknown) > 0 {
		return finish(EvidenceUnknown, "unknown_annotation", a.Raw, b.Raw)
	}
	pa, pb := Normalize(a.Body), Normalize(b.Body)
	if pa != "" && pa == pb {
		if metadataUnverified(a) || metadataUnverified(b) {
			if !ctx.IndependentSupport {
				return finish(EvidenceUnknown, "uncorroborated_alias", a.Body, b.Body)
			}
		}
		return finish(EvidenceVerifiedAlias, "shared_complete_name", a.Body, b.Body)
	}
	for _, left := range metadataForms(a) {
		for _, right := range metadataForms(b) {
			if Normalize(left.Text) != "" && Normalize(left.Text) == Normalize(right.Text) {
				return finish(EvidenceVerifiedAlias, "verified_alias", left.Text, right.Text)
			}
		}
	}
	if sa, sb := DominantScript(a.Body), DominantScript(b.Body); sa != sb && sa != ScriptUnknown && sb != ScriptUnknown {
		return finish(EvidenceUnknown, "unverified_translation", a.Body, b.Body)
	}
	if jaroWinkler(pa, pb) >= DefaultWeights().JaroWinklerMin {
		return finish(EvidenceFuzzy, "similar_text", a.Body, b.Body)
	}
	return finish(EvidenceConflict, "different_text", a.Body, b.Body)
}

func metadataUnverified(p TextProfile) bool {
	for _, f := range p.Aliases {
		if !f.Verified {
			return true
		}
	}
	return false
}

func metadataForms(p TextProfile) []NameForm {
	var out []NameForm
	// Unmarked translations cannot use the stripped body as a standalone
	// verified form. Only the common-body + independent-support path can.
	if !metadataUnverified(p) {
		out = append(out, NameForm{Text: p.Body, Verified: true})
	}
	for _, f := range p.Aliases {
		if f.Verified && f.Text != "" {
			out = append(out, f)
		}
	}
	return out
}
