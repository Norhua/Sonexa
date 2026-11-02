package matching

import (
	"regexp"
	"strings"
	"unicode"
)

// ImportEvidence is a decision explanation, not a confidence score. ExactRef
// describes an independently verified provider/track binding, never an ISRC.
type ImportEvidence struct {
	Kind  string   `json:"kind"`
	Rank  int      `json:"rank"`
	Facts []string `json:"facts"`
}

// ImportVersionCompatible keeps recording and edition markers as hard facts.
func ImportVersionCompatible(a, b Input) bool {
	return sameMarkers(VersionMarkers(a.Title), VersionMarkers(b.Title)) &&
		sameMarkers(importAlbumMarkers(a.Album), importAlbumMarkers(b.Album)) &&
		sameImportEdition(a.Title, b.Title) && sameImportEdition(a.Album, b.Album)
}

var originalSoundtrackType = regexp.MustCompile(`(?i)\boriginal\s+(?:(?:game|motion picture|television|tv|anime)\s+)?(?:soundtrack|score)\b`)

func importAlbumMarkers(text string) map[string]bool {
	markers := VersionMarkers(text)
	// “Original Soundtrack” describes an album type, not an alternate cut.
	if originalSoundtrackType.MatchString(text) {
		delete(markers, "original")
	}
	return markers
}

// MatchOwnedInput compares one coherent view of a collected track. Current
// values and a valid upstream snapshot are evaluated separately by the caller.
// Missing metadata never contributes evidence; explicit conflicts always veto.
func MatchOwnedInput(in, track Input, exactRef bool) ImportEvidence {
	if !CompatibleImportPosition(in, track) || !ImportVersionCompatible(in, track) {
		return ImportEvidence{}
	}
	if SameImportText(in, track) {
		rank := 1
		kind := "complete_metadata"
		facts := []string{"title", "full_credit", "album", "no_position_or_duration_conflict"}
		if exactRef {
			rank = 2
			kind = "exact_track_metadata"
			facts = append(facts, "provider_track_ref")
		}
		return ImportEvidence{Kind: kind, Rank: rank, Facts: facts}
	}
	if !exactRef || !completeEqualOrUntruncated(in.Title, track.Title) || DefaultWeights().CompareTitle(in.Title, track.Title) != MatchExact {
		return ImportEvidence{}
	}
	// Track identity permits exported release credits to differ from performer
	// credits only when the album itself is exact. It does not forgive a
	// different album just because the search returned a single result.
	if SameTrackContext(in, track) {
		if in.Artist != "" && track.Artist != "" && DefaultWeights().CompareCredit(in.Artist, track.Artist) == MatchNone {
			return ImportEvidence{}
		}
		return ImportEvidence{Kind: "exact_track_context", Rank: 2, Facts: []string{"provider_track_ref", "title", "album", "no_position_or_duration_conflict"}}
	}
	if completeEqualOrUntruncated(in.Artist, track.Artist) && sameImportCredit(in.Artist, track.Artist) && AlbumAbbreviation(in.Album, track.Album) {
		return ImportEvidence{Kind: "exact_track_album_abbreviation", Rank: 2, Facts: []string{"provider_track_ref", "title", "full_credit", "bounded_album_phrase", "no_position_or_duration_conflict"}}
	}
	return ImportEvidence{}
}

// AlbumAbbreviation accepts meaningful complete word sequences, not edit
// distance, arbitrary character substrings or generic release-type labels.
// Version markers must agree before punctuation is removed.
func AlbumAbbreviation(a, b string) bool {
	if !completeEqualOrUntruncated(a, b) || !sameMarkers(importAlbumMarkers(a), importAlbumMarkers(b)) || !sameImportEdition(a, b) {
		return false
	}
	words := func(text string) []string {
		parts := strings.FieldsFunc(StripNonVersionAnnotations(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if p = Normalize(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	x, y := words(a), words(b)
	if len(x) > len(y) {
		x, y = y, x
	}
	if len(x) == 0 || len(x) == len(y) {
		return false
	}
	generic := map[string]bool{"album": true, "single": true, "ep": true, "ost": true, "soundtrack": true, "original": true, "best": true, "collection": true, "vol": true, "volume": true, "disc": true, "cd": true}
	meaningful := false
	for _, word := range x {
		if !generic[word] && len([]rune(word)) >= 4 {
			meaningful = true
		}
	}
	if !meaningful {
		return false
	}
	for i := 0; i+len(x) <= len(y); i++ {
		match := true
		for j := range x {
			if x[j] != y[i+j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
