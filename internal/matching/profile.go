package matching

import "github.com/norhua/sonexa/internal/domain"

// MetadataRuleVersion versions persisted input evidence, not search scores or
// lyrics rules. Derived evidence must be rebuilt when this version changes.
const MetadataRuleVersion = 1

type EvidenceLevel string

const (
	EvidenceExact                EvidenceLevel = "exact"
	EvidenceVerifiedAlias        EvidenceLevel = "verified_alias"
	EvidenceCompatibleTruncation EvidenceLevel = "compatible_truncation"
	EvidenceFuzzy                EvidenceLevel = "fuzzy"
	EvidenceUnknown              EvidenceLevel = "unknown"
	EvidenceConflict             EvidenceLevel = "conflict"
)

// FieldEvidence never equates missing, fuzzy or truncated text with identity.
// Forms are the actual comparison strings; original text lives in the profile.
type FieldEvidence struct {
	Level     EvidenceLevel  `json:"level"`
	InputForm string         `json:"inputForm"`
	FactForm  string         `json:"factForm"`
	Source    string         `json:"source"`
	Reason    DecisionReason `json:"reason"`
}

func (e FieldEvidence) Reliable() bool {
	return e.Level == EvidenceExact || e.Level == EvidenceVerifiedAlias
}

type DecisionReason = domain.DecisionReason

type ValueState string

const (
	ValueMissing ValueState = "missing"
	ValueKnown   ValueState = "known"
	ValueInvalid ValueState = "invalid"
)

// NumberInput preserves parsing failures and source units. Precision is in the
// canonical unit (milliseconds for duration); it is not an identity tolerance.
type NumberInput struct {
	Raw       string     `json:"raw"`
	State     ValueState `json:"state"`
	Value     int64      `json:"value"`
	Unit      string     `json:"unit"`
	Precision int64      `json:"precision"`
	Semantic  string     `json:"semantic"`
	Error     string     `json:"error,omitempty"`
}

func MissingNumber(unit, semantic string) NumberInput {
	return NumberInput{State: ValueMissing, Unit: unit, Semantic: semantic}
}

type NameForm struct {
	Text     string `json:"text"`
	Basis    string `json:"basis"`
	Verified bool   `json:"verified"`
}

// Qualifier preserves the entire payload (author, venue, year, etc.), rather
// than just a marker like "remix". Raw is never replaced by the comparison key.
type Qualifier struct {
	Kind    string `json:"kind"`
	Payload string `json:"payload"`
	Raw     string `json:"raw"`
}

type TextProfile struct {
	Raw        string      `json:"raw"`
	Body       string      `json:"body"`
	Aliases    []NameForm  `json:"aliases,omitempty"`
	Truncated  bool        `json:"truncated"`
	Unknown    []string    `json:"unknown,omitempty"`
	Qualifiers []Qualifier `json:"qualifiers,omitempty"`
}

type CreditPart struct {
	Name  string `json:"name"`
	Role  string `json:"role"`
	Voice string `json:"voice,omitempty"`
}

type CreditEvidence struct {
	Text  TextProfile  `json:"text"`
	Parts []CreditPart `json:"parts,omitempty"`
}

// InputProfile is evidence for this input, never a cached previous selection.
// Recording variants belong to Title, release qualifiers to Album.
type InputProfile struct {
	RuleVersion int            `json:"ruleVersion"`
	Title       TextProfile    `json:"title"`
	Artist      CreditEvidence `json:"artist"`
	Album       TextProfile    `json:"album"`
	Year        NumberInput    `json:"year"`
	Disc        NumberInput    `json:"disc"`
	Track       NumberInput    `json:"track"`
	Duration    NumberInput    `json:"duration"`
}

func NewInputProfile(title, artist, album string) InputProfile {
	return InputProfile{
		RuleVersion: MetadataRuleVersion,
		Title:       ParseMetadataText(title, MetadataTitle),
		Artist:      ParseMetadataCredit(artist),
		Album:       ParseMetadataText(album, MetadataAlbum),
		Year:        MissingNumber("year", "unspecified_year"),
		Disc:        MissingNumber("number", "disc"),
		Track:       MissingNumber("number", "track"),
		Duration:    MissingNumber("ms", "duration"),
	}
}
