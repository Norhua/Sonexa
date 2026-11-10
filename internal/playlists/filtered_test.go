package playlists

import "testing"

func TestIdentifiedUnpositionedExclusionsProtectEveryPriorOccurrence(t *testing.T) {
	previous := diffState(diffSnapshot("filtered", "filtered", "gone", "ignored", "absent"))
	previous.Entries[0].LocalState = Imported
	previous.Entries[3].LocalState = Ignored
	previous.Entries[4].LocalState = Imported
	previous.Entries[4].UpstreamPresent = false
	next := diffSnapshot("new")
	next.Excluded = []Excluded{{Kind: "unavailable_track", TrackRef: "filtered", Reason: "upstream filtered"}, {Kind: "unavailable_track", TrackRef: "ignored"}, {Kind: "unavailable_track", TrackRef: "absent"}}
	next.Completeness.RawSeen = 4
	next.Completeness.DeclaredTotal = 4
	next.Completeness.ExcludedCount = 3
	d, err := Diff(previous, next)
	if err != nil || len(d.Removed) != 1 || d.Removed[0].Entry.TrackRef != "gone" || len(d.Restored) != 0 || len(d.Ignored) != 0 || len(d.Added) != 1 {
		t.Fatal(d, err)
	}
	// No fabricated import for a filtered item during first binding.
	d, err = Diff(nil, next)
	if err != nil || len(d.Added) != 1 || len(d.Excluded) != 3 {
		t.Fatal(d, err)
	}
	next.Excluded[0].TrackRef = ""
	if next.Validate() == nil {
		t.Fatal("unpositioned unidentified exclusion accepted")
	}
}
