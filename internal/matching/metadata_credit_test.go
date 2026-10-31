package matching

import "testing"

func TestMetadataCreditsPreserveIdentity(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want EvidenceLevel
	}{
		{"角色A(CV.X)", "角色B(CV.X)", EvidenceConflict},
		{"角色A(CV.X)", "角色A(CV.Y)", EvidenceConflict},
		{"角色A(CV.X)", "X", EvidenceConflict},
		{"角色A(CV.X)", "角色A（ＣＶ：X）", EvidenceExact},
		{"Singer feat. A", "Singer feat. B", EvidenceConflict},
		{"Singer with A", "Singer ft. A", EvidenceVerifiedAlias},
		{"Singer feat. A", "Singer", EvidenceConflict},
		{"Artist（原名: Original）", "Original", EvidenceVerifiedAlias},
	} {
		t.Run(tt.a+"/"+tt.b, func(t *testing.T) {
			got := CompareMetadataCredit(ParseMetadataCredit(tt.a), ParseMetadataCredit(tt.b), MetadataCreditContext{})
			if got.Level != tt.want {
				t.Fatalf("got %+v want %s", got, tt.want)
			}
		})
	}
}

func TestMetadataCreditSourceExplanation(t *testing.T) {
	rel := CreditRelation{Kind: "release_credit", From: "Group", To: "Member", Source: "fixture:release:credits"}
	for _, tt := range []struct {
		name            string
		album, position bool
		relations       []CreditRelation
		want            bool
	}{
		{"explicit release credit", true, true, []CreditRelation{rel}, true},
		{"no position", true, false, []CreditRelation{rel}, false},
		{"no album", false, true, []CreditRelation{rel}, false},
		{"album alone", true, true, nil, false},
		{"no provenance", true, true, []CreditRelation{{Kind: "release_credit", From: "Group", To: "Member"}}, false},
		{"wrong member", true, true, []CreditRelation{{Kind: "group_member", From: "Group", To: "Other", Source: "fixture"}}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareMetadataCredit(ParseMetadataCredit("Group"), ParseMetadataCredit("Member"), MetadataCreditContext{AlbumVerified: tt.album, PositionVerified: tt.position, Relations: tt.relations})
			if got.Reliable() != tt.want {
				t.Fatalf("got %+v", got)
			}
		})
	}
	got := CompareMetadataCredit(ParseMetadataCredit("角色A(CV.X)"), ParseMetadataCredit("角色B(CV.X)"), MetadataCreditContext{AlbumVerified: true, PositionVerified: true, Relations: []CreditRelation{{Kind: "release_credit", From: "角色A(CV.X)", To: "角色B(CV.X)", Source: "fixture"}}})
	if got.Reliable() {
		t.Fatal("release relation erased character identity")
	}
}
