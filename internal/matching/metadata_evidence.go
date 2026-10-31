package matching

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/provider"
)

const (
	EvidenceSupported      EvidenceLevel = "supported"
	EvidenceWeakCompatible EvidenceLevel = "weak_compatible"
)

type MetadataEligibility string

const (
	MetadataEligible   MetadataEligibility = "eligible"
	MetadataExcluded   MetadataEligibility = "excluded"
	MetadataUnresolved MetadataEligibility = "unresolved"
)

// MetadataFacts describes one coherent target, not a merge of conflicting
// current values and upstream snapshots. Completeness is supplied by the fact
// source; a search result's rank or presence does not establish completeness.
type MetadataFacts struct {
	Candidate        provider.Candidate
	Names            *InputProfile
	Source           string
	AlbumComplete    bool
	PositionVerified bool
	CreditRelations  []CreditRelation
}

type MetadataEvidence struct {
	RuleVersion int                      `json:"ruleVersion"`
	Eligibility MetadataEligibility      `json:"eligibility"`
	Fields      map[string]FieldEvidence `json:"fields"`
	Reasons     []DecisionReason         `json:"reasons"`
}

func ProfileFromCandidate(c provider.Candidate) InputProfile {
	p := NewInputProfile(deref(c.Title), deref(c.ArtistName), deref(c.AlbumTitle))
	p.Year = metadataNumber(c.Year, "year", "unspecified_year")
	p.Disc = metadataNumber(c.DiscNumber, "number", "disc")
	p.Track = metadataNumber(c.TrackNumber, "number", "track")
	p.Duration = metadataNumber(c.DurationMS, "ms", "duration")
	return p
}

func metadataNumber(value *int, unit, semantic string) NumberInput {
	n := MissingNumber(unit, semantic)
	if value != nil {
		n.State, n.Value, n.Precision = ValueKnown, int64(*value), 1
		n.Raw = strconv.Itoa(*value)
		if *value <= 0 {
			n.State, n.Error = ValueInvalid, "nonpositive_value"
		}
	}
	return n
}

// CheckMetadataEvidence only checks this input against one target. It does NOT
// choose an edition, prove ownership, or authorize admission. All competitors
// must still be examined, including unresolved ones.
func CheckMetadataEvidence(input InputProfile, facts MetadataFacts) MetadataEvidence {
	out := MetadataEvidence{RuleVersion: MetadataRuleVersion, Eligibility: MetadataEligible, Fields: map[string]FieldEvidence{}}
	add := func(field string, e FieldEvidence) {
		out.Fields[field] = e
		out.Reasons = append(out.Reasons, e.Reason)
	}
	unresolved := func(field, code string) {
		if out.Eligibility != MetadataExcluded {
			out.Eligibility = MetadataUnresolved
		}
		out.Reasons = append(out.Reasons, DecisionReason{Code: code, Field: field})
	}
	if input.RuleVersion != MetadataRuleVersion {
		unresolved("profile", "stale_rule_version")
		return out
	}
	target := ProfileFromCandidate(facts.Candidate)
	if facts.Names != nil {
		target = *facts.Names
		if target.RuleVersion != MetadataRuleVersion {
			unresolved("profile", "stale_fact_version")
			return out
		}
	}
	ctx := func(field MetadataField, support bool) MetadataTextContext {
		return MetadataTextContext{Field: field, Source: facts.Source, IndependentSupport: support}
	}
	// First pass supplies independent anchors. Do not bootstrap three weak
	// script alternatives by letting each one corroborate the other two.
	title := CompareMetadataText(input.Title, target.Title, ctx(MetadataTitle, false))
	album := CompareMetadataText(input.Album, target.Album, ctx(MetadataAlbum, false))
	creditCtx := MetadataCreditContext{Text: ctx(MetadataArtist, false), AlbumVerified: album.Reliable(), PositionVerified: facts.PositionVerified, Relations: facts.CreditRelations}
	artist := CompareMetadataCredit(input.Artist, target.Artist, creditCtx)
	titleSupport := artist.Reliable() || album.Reliable()
	albumSupport := title.Reliable() && artist.Reliable()
	artistSupport := title.Reliable() && album.Reliable()
	title = CompareMetadataText(input.Title, target.Title, ctx(MetadataTitle, titleSupport))
	album = CompareMetadataText(input.Album, target.Album, ctx(MetadataAlbum, albumSupport))
	creditCtx.Text.IndependentSupport = artistSupport
	artist = CompareMetadataCredit(input.Artist, target.Artist, creditCtx)
	add("title", title)
	add("artist", artist)
	add("album", album)
	for _, e := range []FieldEvidence{title, artist, album} {
		if e.Level == EvidenceConflict {
			out.Eligibility = MetadataExcluded
		}
	}
	if !title.Reliable() {
		unresolved("title", "reliable_title_required")
	}
	albumGiven := Normalize(input.Album.Raw) != ""
	artistGiven := Normalize(input.Artist.Text.Raw) != ""
	albumPositive := album.Reliable()
	if album.Level == EvidenceCompatibleTruncation {
		// A long, mostly complete target prefix can constrain a release name,
		// never a version suffix. Short generic prefixes remain unresolved.
		albumPositive = distinctiveAlbumPrefix(input.Album, target.Album) && title.Reliable() && artist.Reliable() && facts.AlbumComplete && facts.PositionVerified
		if albumPositive {
			out.Reasons = append(out.Reasons, DecisionReason{Code: "distinctive_album_truncation", Field: "album"})
		}
	}
	if albumGiven && (!albumPositive || !facts.AlbumComplete) {
		unresolved("album", "target_album_unresolved")
	}
	if artistGiven && !artist.Reliable() {
		unresolved("artist", "credit_unresolved")
	}
	if !artist.Reliable() && !albumPositive {
		unresolved("profile", "supporting_name_required")
	}
	for _, axis := range []struct {
		field string
		a, b  NumberInput
	}{
		{"disc", input.Disc, target.Disc}, {"track", input.Track, target.Track},
		{"duration", input.Duration, target.Duration}, {"year", input.Year, target.Year},
	} {
		e := compareMetadataNumber(axis.a, axis.b, axis.field, facts.Source)
		add(axis.field, e)
		if e.Level == EvidenceConflict {
			out.Eligibility = MetadataExcluded
		}
		if axis.a.State != ValueMissing && axis.a.State != "" && e.Level == EvidenceUnknown {
			// Unspecified year is ancillary: first-release and edition years
			// are not interchangeable, so differing/missing years cannot pick
			// or reject a track. Invalid input still needs review.
			if axis.field != "year" || axis.a.State != ValueKnown {
				unresolved(axis.field, "numeric_evidence_unresolved")
			}
		}
	}
	if !facts.PositionVerified {
		unresolved("position", "position_unresolved")
	}
	return out
}

func distinctiveAlbumPrefix(input, target TextProfile) bool {
	if !input.Truncated || target.Truncated || len(input.Unknown) > 0 || len(target.Unknown) > 0 || !sameMetadataQualifiers(input, target) {
		return false
	}
	a, b := Normalize(input.Body), Normalize(target.Body)
	return utf8.RuneCountInString(a) >= 12 && strings.HasPrefix(b, a) &&
		float64(utf8.RuneCountInString(a))/float64(utf8.RuneCountInString(b)) >= 0.7
}

func compareMetadataNumber(a, b NumberInput, field, source string) FieldEvidence {
	e := FieldEvidence{Level: EvidenceUnknown, InputForm: a.Raw, FactForm: b.Raw, Source: source, Reason: DecisionReason{Field: field, Code: "missing_number"}}
	if a.State == ValueMissing || a.State == "" {
		return e
	}
	if a.State != ValueKnown || b.State != ValueKnown || a.Value <= 0 || b.Value <= 0 {
		e.Reason.Code = "unknown_number"
		return e
	}
	if a.Unit != b.Unit || a.Semantic != b.Semantic {
		e.Reason.Code = "different_numeric_semantics"
		return e
	}
	if a.Value == b.Value {
		e.Level, e.Reason.Code = EvidenceExact, "same_number"
		return e
	}
	if field == "year" {
		e.Reason.Code = "unspecified_year_semantics"
		return e
	}
	if field == "duration" {
		diff := a.Value - b.Value
		if diff < 0 {
			diff = -diff
		}
		// At most second-level rounding, regardless of track length. A
		// claimed arbitrary precision cannot create an unlimited tolerance.
		for _, precision := range []int64{a.Precision, b.Precision} {
			if precision > 1 && precision <= 1000 {
				diff -= precision / 2
			}
		}
		switch {
		case diff <= 3000:
			e.Level, e.Reason.Code = EvidenceSupported, "duration_within_3s"
		case diff <= 10000:
			e.Level, e.Reason.Code = EvidenceWeakCompatible, "duration_within_10s"
		default:
			e.Level, e.Reason.Code = EvidenceConflict, "duration_conflict"
		}
		return e
	}
	e.Level, e.Reason.Code = EvidenceConflict, "position_conflict"
	return e
}

type MetadataPool struct {
	Evidence   []MetadataEvidence
	Eligible   []int
	Unresolved []int
	Excluded   []int
}

func CheckMetadataPool(input InputProfile, candidates []MetadataFacts) MetadataPool {
	out := MetadataPool{}
	for i, c := range candidates {
		e := CheckMetadataEvidence(input, c)
		out.Evidence = append(out.Evidence, e)
		switch e.Eligibility {
		case MetadataEligible:
			out.Eligible = append(out.Eligible, i)
		case MetadataExcluded:
			out.Excluded = append(out.Excluded, i)
		default:
			out.Unresolved = append(out.Unresolved, i)
		}
	}
	return out
}

func (p MetadataPool) ReadyForSelection() bool { return len(p.Eligible) > 0 && len(p.Unresolved) == 0 }
