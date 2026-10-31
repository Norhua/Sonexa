package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func metadataTestFacts(album string) MetadataFacts {
	title, artist, release, track := "you", "Singer", "release", "track"
	disc, number, duration := 2, 3, 620000
	return MetadataFacts{Source: "fixture:track", AlbumComplete: true, PositionVerified: true, Candidate: provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: track, ReleaseRef: &release, Title: &title, ArtistName: &artist, AlbumTitle: &album,
		DiscNumber: &disc, TrackNumber: &number, DurationMS: &duration,
	}}
}

func TestMetadataEvidenceDoesNotUseSearchScore(t *testing.T) {
	p := NewInputProfile("you", "Singer", "Album")
	p.Duration = NumberInput{State: ValueKnown, Value: 620000, Unit: "ms", Precision: 1, Semantic: "duration", Raw: "620000 ms"}
	for _, tt := range []struct {
		diff     int
		want     EvidenceLevel
		eligible bool
	}{
		{0, EvidenceExact, true}, {3000, EvidenceSupported, true}, {3001, EvidenceWeakCompatible, true},
		{10000, EvidenceWeakCompatible, true}, {10001, EvidenceConflict, false},
		{-3000, EvidenceSupported, true}, {-10001, EvidenceConflict, false},
	} {
		fact := metadataTestFacts("Album")
		duration, score := 620000+tt.diff, 100
		fact.Candidate.DurationMS, fact.Candidate.UpstreamScore = &duration, &score
		got := CheckMetadataEvidence(p, fact)
		if got.Fields["duration"].Level != tt.want || (got.Eligibility == MetadataEligible) != tt.eligible {
			t.Fatalf("diff %d: %+v", tt.diff, got)
		}
	}
	fact := metadataTestFacts("Album")
	p.Duration.Precision = 1000
	duration := 630400
	fact.Candidate.DurationMS = &duration
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataEligible {
		t.Fatalf("second-level rounding lost: %+v", e)
	}
	duration = 631000
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataExcluded {
		t.Fatal("precision widened tolerance excessively", e)
	}
	p.Duration.Precision = 1000000
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataExcluded {
		t.Fatal("unbounded precision allowed", e)
	}
}

func TestMetadataPoolConstrainsAlbumBeforeUnknownCompetitors(t *testing.T) {
	p := NewInputProfile("you", "Singer", "冴えない彼女の育てかた 第4巻 特典C...")
	bonus := metadataTestFacts("冴えない彼女の育てかた 第4巻 特典CD")
	compilation := metadataTestFacts("冴えない彼女の育てかた 歌曲合集")
	pool := CheckMetadataPool(p, []MetadataFacts{bonus, compilation})
	if !pool.ReadyForSelection() || len(pool.Eligible) != 1 || len(pool.Excluded) != 1 || pool.Eligible[0] != 0 {
		t.Fatalf("input did not constrain album: %+v", pool)
	}
	if e := pool.Evidence[0].Fields["album"]; e.Level != EvidenceCompatibleTruncation || e.Reliable() {
		t.Fatal("truncation promoted to exact", e)
	}
	unknown := metadataTestFacts("")
	pool = CheckMetadataPool(p, []MetadataFacts{bonus, compilation, unknown})
	if pool.ReadyForSelection() || len(pool.Unresolved) != 1 {
		t.Fatal("unknown competitor discarded", pool)
	}
	bonus.AlbumComplete = false
	if e := CheckMetadataEvidence(p, bonus); e.Eligibility != MetadataUnresolved {
		t.Fatal("incomplete facts prove truncation", e)
	}
	for _, title := range []string{"Album...", "冴えない..."} {
		input := NewInputProfile("you", "Singer", title)
		if e := CheckMetadataEvidence(input, metadataTestFacts(title+" (Deluxe)")); e.Eligibility == MetadataEligible {
			t.Fatal("generic/version prefix accepted", e)
		}
	}
}

func TestMetadataPositionAndMissingEvidence(t *testing.T) {
	p := NewInputProfile("you", "Singer", "Album")
	p.Track = NumberInput{State: ValueKnown, Value: 3, Unit: "number", Semantic: "track", Raw: "3"}
	fact := metadataTestFacts("Album")
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataEligible || e.Fields["disc"].Level != EvidenceUnknown {
		t.Fatal("missing disc defaulted", e)
	}
	fact.PositionVerified = false
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataUnresolved {
		t.Fatal("unresolved repeated position accepted", e)
	}
	fact.PositionVerified = true
	p.Disc = NumberInput{State: ValueKnown, Value: 1, Unit: "number", Semantic: "disc", Raw: "1"}
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataExcluded {
		t.Fatal("wrong disc accepted", e)
	}
	p.Disc = NumberInput{State: ValueInvalid, Raw: "bad", Unit: "number", Semantic: "disc"}
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataUnresolved {
		t.Fatal("invalid disc ignored", e)
	}
	p = NewInputProfile("you", "", "Album")
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataEligible {
		t.Fatal("album cannot support missing credit", e)
	}
	p = NewInputProfile("you", "", "Album B")
	if e := CheckMetadataEvidence(p, fact); e.Eligibility == MetadataEligible {
		t.Fatal("fuzzy album promoted", e)
	}
	p = NewInputProfile("you", "Different performer", "Album")
	if e := CheckMetadataEvidence(p, fact); e.Eligibility == MetadataEligible {
		t.Fatal("album cancels credit conflict", e)
	}
	p = NewInputProfile("you", "", "")
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataUnresolved {
		t.Fatal("title alone authorized", e)
	}
	p = NewInputProfile("you", "Singer", "Album")
	year := 2001
	fact.Candidate.Year = &year
	p.Year = NumberInput{State: ValueKnown, Value: 2020, Unit: "year", Semantic: "unspecified_year", Raw: "2020"}
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataEligible || e.Fields["year"].Level != EvidenceUnknown {
		t.Fatal("unspecified year rejected track", e)
	}
}

func TestMetadataAliasesCannotBootstrapEachOther(t *testing.T) {
	p := NewInputProfile("you（你）", "Singer（歌手）", "Album（专辑）")
	fact := metadataTestFacts("Album")
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataUnresolved {
		t.Fatal("circular alias corroboration", e)
	}
	p.Artist = ParseMetadataCredit("Singer")
	p.Album = ParseMetadataText("Album", MetadataAlbum)
	if e := CheckMetadataEvidence(p, fact); e.Eligibility != MetadataEligible {
		t.Fatal("independent anchors not used", e)
	}
}
