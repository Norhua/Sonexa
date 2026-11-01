package matching

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"testing/quick"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func problemInput(title, artist, album string) ProblemInput {
	return ProblemInput{Profile: NewInputProfile(title, artist, album)}
}

func TestProblemStrongTextProof(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b ProblemInput
		want bool
	}{
		{"same", problemInput("Song", "Artist", "Album"), problemInput("Song", "Artist", "Album"), true},
		{"format", problemInput("Ｓｏｎｇ", "ＡＲＴＩＳＴ", "Ａｌｂｕｍ"), problemInput("song", "artist", "album"), true},
		{"three_explicit_aliases", problemInput("Song（译名：歌曲）", "Artist（原名：歌手）", "Album（译名：专辑）"), problemInput("Song", "Artist", "Album"), true},
		{"inner_original", problemInput("歌曲（原名：Song）", "Artist", "Album"), problemInput("Song", "Artist", "Album"), true},
		{"supported_reading", problemInput("Song", "3L（スリーエル）", "Album"), problemInput("Song", "3L", "Album"), true},
		{"ordinary_vocal", problemInput("(Vocal) you", "Artist", "Album"), problemInput("you(Vocal)", "Artist", "Album"), true},
		{"same_unknown", problemInput("Song (annotation)", "Artist", "Album"), problemInput("Song (annotation)", "Artist", "Album"), true},
		{"unknown_removed", problemInput("Song (annotation)", "Artist", "Album"), problemInput("Song", "Artist", "Album"), false},
		{"unknown_changed", problemInput("Song (first note)", "Artist", "Album"), problemInput("Song (second note)", "Artist", "Album"), false},
		{"pure_translation", problemInput("歌曲", "Artist", "Album"), problemInput("Song", "Artist", "Album"), false},
		{"missing_title", problemInput("", "Artist", "Album"), problemInput("", "Artist", "Album"), false},
		{"missing_artist", problemInput("Song", "", "Album"), problemInput("Song", "", "Album"), false},
		{"missing_album", problemInput("Song", "Artist", ""), problemInput("Song", "Artist", ""), false},
		{"different_album", problemInput("Song", "Artist", "Bonus CD"), problemInput("Song", "Artist", "Compilation"), false},
		{"title_truncation", problemInput("Song...", "Artist", "Album"), problemInput("Song...", "Artist", "Album"), false},
		{"album_truncation", problemInput("Song", "Artist", "Album..."), problemInput("Song", "Artist", "Album Deluxe"), false},
		{"remix_payload", problemInput("Song (A Remix)", "Artist", "Album"), problemInput("Song (B Remix)", "Artist", "Album"), false},
		{"off_vocal", problemInput("Song (Off Vocal)", "Artist", "Album"), problemInput("Song", "Artist", "Album"), false},
		{"deluxe", problemInput("Song", "Artist", "Album (Deluxe Edition)"), problemInput("Song", "Artist", "Album"), false},
		{"same_character", problemInput("Song", "角色(CV.X)", "Album"), problemInput("Song", "角色(CV：X)", "Album"), true},
		{"different_character", problemInput("Song", "角色 A(CV.X)", "Album"), problemInput("Song", "角色 B(CV.X)", "Album"), false},
		{"collaboration", problemInput("Song", "Artist feat. A", "Album"), problemInput("Song", "Artist with B", "Album"), false},
		{"same_collaboration", problemInput("Song", "Artist feat. A", "Album"), problemInput("Song", "Artist with A", "Album"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before, _ := json.Marshal(tc)
			for _, inputs := range [][]ProblemInput{{tc.a, tc.b}, {tc.b, tc.a}} {
				proof := ProveProblem(inputs)
				if got := proof.Kind == ProblemStrongText; got != tc.want {
					t.Fatalf("want equivalent=%v got %+v", tc.want, proof)
				}
				anchor, _ := NewProblemAnchor(inputs[0])
				_, extension, ok := ExtendProblemAnchor(anchor, inputs[1])
				if ok != tc.want || extension.Kind != proof.Kind {
					t.Fatal("extension disagrees", extension, ok)
				}
			}
			after, _ := json.Marshal(tc)
			if string(before) != string(after) {
				t.Fatal("proof modified raw input")
			}
		})
	}
}

func TestProblemNumbersAreRequirementsNotTolerance(t *testing.T) {
	for _, scenario := range []string{"equal_units", "within_tolerance", "missing", "invalid", "year", "precision", "stale_rule"} {
		t.Run(scenario, func(t *testing.T) {
			a := problemInput("Song", "Artist", "Album")
			a.Profile.Duration = NumberInput{Raw: "10:20", State: ValueKnown, Value: 620000, Unit: "ms", Semantic: "duration", Precision: 1000}
			b := a
			want := false
			switch scenario {
			case "equal_units", "precision":
				b.Profile.Duration.Raw, b.Profile.Duration.Precision = "620000ms", 1
				want = true
			case "within_tolerance":
				b.Profile.Duration.Value += 2000
			case "missing":
				b.Profile.Duration = MissingNumber("ms", "duration")
			case "invalid":
				a.Profile.Track.State, b.Profile.Track.State = ValueInvalid, ValueInvalid
			case "year":
				b.Profile.Year = NumberInput{State: ValueKnown, Value: 2006, Unit: "year", Semantic: "unspecified_year"}
			case "stale_rule":
				b.Profile.RuleVersion--
			}
			proof := ProveProblem([]ProblemInput{a, b})
			if (proof.Kind == ProblemStrongText) != want {
				t.Fatal(proof)
			}
		})
	}
}

func TestProblemAnchorRejectsNonTransitiveAliasChains(t *testing.T) {
	// A/B share Alpha, B/C share Beta, A/C have no common evidence.
	a, b, c := problemInput("Alpha", "Artist", "Album"), problemInput("Alpha（原名：Beta）", "Artist", "Album"), problemInput("Beta", "Artist", "Album")
	if ProveProblem([]ProblemInput{a, b}).Kind != ProblemStrongText || ProveProblem([]ProblemInput{b, c}).Kind != ProblemStrongText {
		t.Fatal("fixture must establish both local alias correspondences")
	}
	for _, inputs := range [][]ProblemInput{{a, b, c}, {b, a, c}, {b, c, a}, {c, b, a}} {
		anchor, _ := NewProblemAnchor(inputs[0])
		two, _, ok := ExtendProblemAnchor(anchor, inputs[1])
		if !ok {
			t.Fatal("pair unexpectedly rejected")
		}
		if _, proof, ok := ExtendProblemAnchor(two, inputs[2]); ok || proof.Kind != ProblemIndependent {
			t.Fatal("pairwise alias closure admitted unrelated third member", proof)
		}
	}
	// Also reject pairwise clique with no single witness: {A,B}, {B,C}, {C,A}.
	a = problemInput("Alpha（原名：Beta）", "Artist", "Album")
	b = problemInput("Beta（原名：Gamma）", "Artist", "Album")
	c = problemInput("Gamma（原名：Alpha）", "Artist", "Album")
	if proof := ProveProblem([]ProblemInput{a, b, c}); proof.Kind != ProblemIndependent {
		t.Fatal("no common name witness", proof)
	}
}

func exactProblemFixture(t *testing.T, profile InputProfile) ProblemInput {
	t.Helper()
	sources, lookup, _, c := poolFixture(t)
	pool := BuildCandidatePool(LocalRecallResult{}, []provider.Candidate{c}, sources, lookup)
	return ProblemInput{Profile: profile, Pool: &pool}
}

func TestProblemExactIdentityNeedsCurrentUnambiguousEvidence(t *testing.T) {
	for _, scenario := range []string{"same_target", "missing_album_proven", "different_track", "different_release", "unresolved_competitor", "preferred_from_multiple", "truncated_pool", "partial_local", "stale_facts", "invalid_identity", "explicit_conflict", "same_pool_different_intent", "duration_tolerance", "contradictory_source_views"} {
		t.Run(scenario, func(t *testing.T) {
			// Different missing masks cannot use the text path. Identity path
			// permits it only with an independent exact current proof for both.
			a := exactProblemFixture(t, NewInputProfile("Song", "Artist", "Album"))
			b := exactProblemFixture(t, NewInputProfile("Song", "Artist", ""))
			want := false
			switch scenario {
			case "same_target", "missing_album_proven":
				want = true
			case "different_track":
				b.Pool.Targets[0].Identity.Track.Ref = "another-track"
				b.Pool.Targets[0].Facts[0].Metadata.Candidate.MatchRef = "another-track"
			case "different_release":
				b.Pool.Targets[0].Identity.Release.Ref = "another-release"
				b.Pool.Targets[0].Facts[0].Metadata.Candidate.ReleaseRef = sptr("another-release")
			case "unresolved_competitor", "preferred_from_multiple":
				other := exactProblemFixture(t, b.Profile).Pool.Targets[0]
				other.Identity.Track.Ref = "another-track"
				other.Facts[0].Metadata.Candidate.MatchRef = "another-track"
				if scenario == "unresolved_competitor" {
					other.Facts[0].Metadata.Candidate.Title = nil
				}
				b.Pool.Targets = append(b.Pool.Targets, other)
			case "truncated_pool":
				b.Pool.Truncated = true
			case "partial_local":
				b.Pool.Targets[0].Facts[0].Kind = "snapshot"
			case "stale_facts":
				b.Pool.Targets[0].Issues = []DecisionReason{{Code: "stale_local_facts"}}
			case "invalid_identity":
				b.Pool.Targets[0].Identity.State = IdentityUnresolved
			case "explicit_conflict", "same_pool_different_intent":
				b.Profile.Album = ParseMetadataText("Compilation", MetadataAlbum)
			case "contradictory_source_views":
				b.Profile.Album = ParseMetadataText("Compilation", MetadataAlbum)
				b.Pool.Targets[0].Facts[0].Metadata.Candidate.AlbumTitle = sptr("Compilation")
			case "duration_tolerance":
				a.Profile.Duration = NumberInput{State: ValueKnown, Value: 620000, Unit: "ms", Semantic: "duration"}
				b.Profile.Duration = NumberInput{State: ValueKnown, Value: 622000, Unit: "ms", Semantic: "duration"}
			}
			proof := ProveProblem([]ProblemInput{a, b})
			if (proof.Kind == ProblemExactTrack) != want {
				t.Fatal(proof)
			}
			if want && (proof.Target == nil || proof.Target.Track.Ref != "track") {
				t.Fatal(proof)
			}
		})
	}
}

func TestProblemProofPropertiesAndSerialization(t *testing.T) {
	property := func(n uint16) bool {
		title := fmt.Sprintf("Track %d", n)
		a, b, c := problemInput(title, "Artist", "Album"), problemInput(title+"（译名：歌曲）", "Artist", "Album"), problemInput(title, "Artist（原名：歌手）", "Album")
		for _, inputs := range [][]ProblemInput{{a, b, c}, {c, a, b}, {b, c, a}} {
			proof := ProveProblem(inputs)
			if proof.Kind != ProblemStrongText || len(proof.Inputs) != 3 {
				return false
			}
			anchor, _ := makeProblemAnchor(inputs)
			body, _ := json.Marshal(anchor)
			var restored ProblemAnchor
			if json.Unmarshal(body, &restored) != nil || !reflect.DeepEqual(proof, ProveProblem(restored.Inputs)) {
				return false
			}
		}
		// Any change of the explicit numeric requirement prevents equivalence.
		c.Profile.Track = NumberInput{State: ValueKnown, Value: int64(n) + 1, Unit: "number", Semantic: "track"}
		return ProveProblem([]ProblemInput{a, c}).Kind == ProblemIndependent
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 100}); err != nil {
		t.Fatal(err)
	}
	input := problemInput("Song", "Artist", "Album")
	anchor, proof := NewProblemAnchor(input)
	anchor.Inputs[0].Profile.Title.Raw = "changed"
	if proof.Inputs[0] == ProveProblem(anchor.Inputs).Inputs[0] || input.Profile.Title.Raw != "Song" {
		t.Fatal("proof did not bind raw evidence or retained a mutable alias")
	}
}

func TestProblemIdentityCannotUseRepeatedRecordingPosition(t *testing.T) {
	sources, lookup, record, _ := poolFixture(t)
	record.TrackSnapshot.Recording = domain.Known(domain.Identity{Provider: "one", Ref: "recording"})
	entry := record.ReleaseSnapshot.Tracklist.Value[0]
	entry.Recording = record.TrackSnapshot.Recording
	record.ReleaseSnapshot.Tracklist.Value[0] = entry
	entry.Track = domain.Known(domain.Identity{Provider: "one", Ref: "uncollected"})
	entry.TrackNumber++
	record.ReleaseSnapshot.Tracklist.Value = append(record.ReleaseSnapshot.Tracklist.Value, entry)
	pool := BuildCandidatePool(LocalRecallResult{Records: []LocalRecord{record}}, nil, sources, lookup)
	input := ProblemInput{Profile: NewInputProfile("Song", "Artist", "Album"), Pool: &pool}
	if ExactProblemTarget(input) != nil {
		t.Fatal("collected position preferred despite repeated recording")
	}
	input.Profile.Disc = NumberInput{State: ValueKnown, Value: 2, Unit: "number", Semantic: "disc"}
	input.Profile.Track = NumberInput{State: ValueKnown, Value: 3, Unit: "number", Semantic: "track"}
	if ExactProblemTarget(input) == nil {
		t.Fatal("explicit position did not resolve repeated recording")
	}
}
