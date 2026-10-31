package matching

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMetadataEvidenceLevels(t *testing.T) {
	levels := []EvidenceLevel{EvidenceExact, EvidenceVerifiedAlias, EvidenceCompatibleTruncation, EvidenceFuzzy, EvidenceUnknown, EvidenceConflict}
	seen := map[EvidenceLevel]bool{}
	for _, level := range levels {
		if seen[level] {
			t.Fatalf("duplicate level %q", level)
		}
		seen[level] = true
		e := FieldEvidence{Level: level}
		if e.Reliable() != (level == EvidenceExact || level == EvidenceVerifiedAlias) {
			t.Errorf("%s incorrectly treated as reliable", level)
		}
	}
	if (FieldEvidence{}).Reliable() {
		t.Fatal("zero evidence cannot prove identity")
	}
}

func TestInputProfileJSONPreservesRawAndUnknown(t *testing.T) {
	p := InputProfile{
		RuleVersion: MetadataRuleVersion,
		Title:       TextProfile{Raw: " Ｓｏｎｇ（A Remix） ", Body: "Song", Qualifiers: []Qualifier{{Kind: "remix", Payload: "a", Raw: "A Remix"}}},
		Artist:      CreditEvidence{Text: TextProfile{Raw: "角色A(CV.X)"}, Parts: []CreditPart{{Name: "角色A", Role: "character", Voice: "X"}}},
		Album:       TextProfile{Raw: "专辑…", Body: "专辑", Truncated: true, Unknown: []string{"未闭合注释"}},
		Year:        NumberInput{Raw: "错误年份", State: ValueInvalid, Error: "invalid integer"},
		Disc:        MissingNumber("number", "disc"),
		Track:       NumberInput{Raw: "03", State: ValueKnown, Value: 3, Unit: "number", Semantic: "track"},
		Duration:    NumberInput{Raw: "10:20", State: ValueKnown, Value: 620000, Unit: "ms", Precision: 1000, Semantic: "duration"},
	}
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var got InputProfile
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(p, got) {
		t.Fatalf("profile changed on roundtrip: %s", data)
	}
	if got.Disc.State == got.Year.State || got.Disc.State == got.Track.State {
		t.Fatal("missing, invalid and known collapsed")
	}
}
