package matching

import "testing"

func TestOwnedImportEvidence(t *testing.T) {
	base := Input{Title: "Tetra.", Artist: "原田ひとみ", Album: "Trinoline Original Soundtrack ~LINES~"}
	short := Input{Title: "Tetra.", Artist: "原田瞳（原田ひとみ）", Album: "LINES"}
	for _, exact := range []bool{false, true} {
		got := MatchOwnedInput(short, base, exact)
		if (got.Rank > 0) != exact {
			t.Fatalf("exact=%v evidence=%+v", exact, got)
		}
	}
	for name, mutate := range map[string]func(*Input){
		"different album": func(i *Input) { i.Album = "Other Album" },
		"substring":       func(i *Input) { i.Album = "LINE" },
		"generic":         func(i *Input) { i.Album = "Soundtrack" },
		"artist":          func(i *Input) { i.Artist = "Another Artist" },
		"missing artist":  func(i *Input) { i.Artist = "" },
		"fuzzy title":     func(i *Input) { i.Title = "Tetras" },
		"version":         func(i *Input) { i.Title = "Tetra. (off vocal)" },
		"edition":         func(i *Input) { i.Album = "LINES (Deluxe Edition)" },
		"truncated":       func(i *Input) { i.Album = "LINE..." },
	} {
		t.Run(name, func(t *testing.T) {
			in := short
			mutate(&in)
			if got := MatchOwnedInput(in, base, true); got.Rank != 0 {
				t.Fatal(got)
			}
		})
	}
	// No song-specific strings in the rule: complete bounded album phrases work.
	if !AlbumAbbreviation("Moon Garden", "Original Game Soundtrack — Moon Garden") {
		t.Fatal("general abbreviation rejected")
	}
	if AlbumAbbreviation("Garden", "Gardenia") || AlbumAbbreviation("Album", "Artist Album") {
		t.Fatal("weak substring accepted")
	}
}

func TestOwnedInputExactRefDoesNotOverrideDifferentArtist(t *testing.T) {
	in := Input{Title: "Song", Artist: "Original artist", Album: "Album"}
	track := in
	track.Artist = "Unrelated performer"
	if e := MatchOwnedInput(in, track, true); e.Rank != 0 {
		t.Fatal(e)
	}
}

func TestOwnedImportMetadataAndHardConflicts(t *testing.T) {
	local := Input{Title: "ツバサ (羽翼)", Artist: "4円", Album: "side-c"}
	in := Input{Title: "ツバサ（羽翼）", Artist: "4円（ヨエン）", Album: "side-c"}
	if got := MatchOwnedInput(in, local, false); got.Kind != "complete_metadata" {
		t.Fatal(got)
	}
	one, two := 1, 2
	in.TrackNo = &one
	local.TrackNo = &two
	if got := MatchOwnedInput(in, local, true); got.Rank != 0 {
		t.Fatal("position", got)
	}
	in.TrackNo = nil
	local.TrackNo = nil
	a, b := 267000, 280000
	in.DurationMS = &a
	local.DurationMS = &b
	if got := MatchOwnedInput(in, local, true); got.Rank != 0 {
		t.Fatal("duration", got)
	}
}
