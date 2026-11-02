package matching

import "testing"

func TestConfirmedTargetUsesApprovedVersionNotOriginalInput(t *testing.T) {
	approved := Input{Title: "Song -Singer A Ver.-", Artist: "Singer A", Album: "Soundtrack", DiscNo: sptrInt(1), TrackNo: sptrInt(2), DurationMS: sptrInt(240000)}
	for _, tt := range []struct {
		name string
		edit func(*Input)
		want bool
	}{
		{"unchanged", func(*Input) {}, true},
		{"display rename", func(v *Input) { v.Title = "Custom Song -Singer A Ver.-" }, true},
		{"annotation", func(v *Input) { v.Title = "Song -Singer A Ver.-（译名）" }, true},
		{"fullwidth", func(v *Input) { v.Title = "Song －Ｓｉｎｇｅｒ Ａ Ｖｅｒ．－" }, true},
		{"other singer same version keyword", func(v *Input) { v.Title = "Song -Singer B Ver.-" }, false},
		{"instrumental", func(v *Input) { v.Title = "Song -Instrumental-" }, false},
		{"removed version", func(v *Input) { v.Title = "Song" }, false},
		{"deluxe album", func(v *Input) { v.Album = "Soundtrack (Deluxe Edition)" }, false},
		{"other position", func(v *Input) { v.TrackNo = sptrInt(3) }, false},
		{"cleared position", func(v *Input) { v.DiscNo = nil }, false},
		{"duration enriched within tolerance", func(v *Input) { v.DurationMS = sptrInt(242000) }, true},
		{"duration changed", func(v *Input) { v.DurationMS = sptrInt(244000) }, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := approved
			tt.edit(&current)
			if got := ConfirmedTargetUnchanged(approved, current); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
	unknown := approved
	unknown.DiscNo = nil
	unknown.TrackNo = nil
	unknown.DurationMS = nil
	if !ConfirmedTargetUnchanged(unknown, approved) {
		t.Fatal("newly known position rejected")
	}
	// No change to the predicate used for unconfirmed imports.
	if SameImportText(Input{Title: "Song", Artist: "Singer A", Album: "Soundtrack"}, approved) {
		t.Fatal("confirmation rule leaked into identity matching")
	}
}

func TestConfirmedTargetPreservesFullBracketVersion(t *testing.T) {
	for _, pair := range [][2]string{{"Song (Alice Version)", "Song (Bob Version)"}, {"Song (DJ One Remix)", "Song (DJ Two Remix)"}, {"Song (Remastered 2010)", "Song (Remastered 2020)"}} {
		if ConfirmedTargetUnchanged(Input{Title: pair[0]}, Input{Title: pair[1]}) {
			t.Fatal(pair)
		}
	}
}

func sptrInt(v int) *int { return &v }
