package matching

import "testing"

func TestMetadataTextAliases(t *testing.T) {
	for _, field := range []MetadataField{MetadataTitle, MetadataArtist, MetadataAlbum} {
		for _, tt := range []struct {
			a, b    string
			support bool
			want    EvidenceLevel
		}{
			{" ＡＢＣ　１２３ ", "abc 123", false, EvidenceExact},
			{"原名（译名：Translation）", "原名", false, EvidenceVerifiedAlias},
			{"译名（原名：Original）", "Original", false, EvidenceVerifiedAlias},
			{"名前（读音：なまえ）", "ナマエ", false, EvidenceVerifiedAlias},
			{"ツバサ（羽翼）", "ツバサ", false, EvidenceUnknown},
			{"ツバサ（羽翼）", "ツバサ", true, EvidenceVerifiedAlias},
			{"羽翼（ツバサ）", "ツバサ", true, EvidenceUnknown},
			{"Song (mystery)", "Song", true, EvidenceUnknown},
			{"Song (mystery)", "Song (other)", true, EvidenceUnknown},
			{"Song (mystery)", "Song (mystery)", false, EvidenceExact},
			{"Hello", "你好", true, EvidenceUnknown},
			{"", "", true, EvidenceUnknown},
			{"Name...", "Name", true, EvidenceCompatibleTruncation},
			{"Name (译名: 未闭合", "Name", true, EvidenceUnknown},
		} {
			t.Run(string(field)+"/"+tt.a+"/"+tt.b, func(t *testing.T) {
				a, b := ParseMetadataText(tt.a, field), ParseMetadataText(tt.b, field)
				got := CompareMetadataText(a, b, MetadataTextContext{Field: field, IndependentSupport: tt.support, Source: "fixture"})
				if got.Level != tt.want {
					t.Fatalf("got %+v, want %s", got, tt.want)
				}
				if a.Raw != tt.a || b.Raw != tt.b {
					t.Fatal("raw text rewritten")
				}
			})
		}
	}
}

func TestMetadataOrdinaryVocal(t *testing.T) {
	for _, name := range []string{"(Vocal) you", "you(Vocal)", "（Ｖｏｃａｌ） you", "(Vocal) Another Song"} {
		p := ParseMetadataText(name, MetadataTitle)
		base := "you"
		if name == "(Vocal) Another Song" {
			base = "Another Song"
		}
		if e := CompareMetadataText(p, ParseMetadataText(base, MetadataTitle), MetadataTextContext{Field: MetadataTitle}); !e.Reliable() {
			t.Fatalf("ordinary vocal %q: %+v", name, e)
		}
	}
}

func TestMetadataSourceAliasRequiresProvenance(t *testing.T) {
	a := ParseMetadataText("翼", MetadataTitle)
	b := ParseMetadataText("Wings", MetadataTitle)
	for _, source := range []string{"", "source:track:alias"} {
		fact := WithMetadataAliases(b, source, "翼")
		e := CompareMetadataText(a, fact, MetadataTextContext{Field: MetadataTitle})
		if e.Reliable() != (source != "") {
			t.Fatalf("source %q: %+v", source, e)
		}
	}
	if len(b.Aliases) != 0 {
		t.Fatal("source profile mutated")
	}
}
