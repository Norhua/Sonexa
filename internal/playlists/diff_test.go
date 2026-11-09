package playlists

import (
	"reflect"
	"testing"
	"time"
)

func diffSnapshot(refs ...string) Snapshot {
	s := Snapshot{Ref: Ref{Provider: "only-playlist", ID: "list"}, Name: "Playlist", FetchedAt: time.Now().UTC(), Completeness: Completeness{Complete: true, DeclaredTotal: len(refs), RawSeen: len(refs), TrackCount: len(refs), Pages: 1}}
	counts := map[string]int{}
	for i, ref := range refs {
		counts[ref]++
		s.Entries = append(s.Entries, Entry{EntryKey: OccurrenceKey(ref, counts[ref]), TrackRef: ref, Position: i + 1})
	}
	return s
}
func diffState(snapshot Snapshot) *State {
	state := &State{Binding: Binding{ID: 1, Ref: snapshot.Ref, Revision: 1}}
	for i, e := range snapshot.Entries {
		state.Entries = append(state.Entries, StoredEntry{ID: int64(i + 1), PlaylistID: 1, Entry: e, UpstreamPresent: true, LocalState: Pending})
	}
	return state
}
func TestDiffUsesOccurrenceNotPositionOrTitle(t *testing.T) {
	previous := diffState(diffSnapshot("a", "a", "b"))
	for _, refs := range [][]string{{"new", "a", "a", "b"}, {"b", "a", "a"}, {"a", "b"}, {"a", "a", "a", "b"}} {
		next := diffSnapshot(refs...)
		title := "renamed"
		next.Name = "New playlist"
		next.Entries[0].Title = &title
		diff, err := Diff(previous, next)
		if err != nil {
			t.Fatal(err)
		}
		added, removed := 0, 0
		if len(refs) == 4 {
			added = 1
		}
		if len(refs) == 2 {
			removed = 1
		}
		if len(diff.Added) != added || len(diff.Removed) != removed || len(diff.Restored) != 0 {
			t.Fatalf("%v: %+v", refs, diff)
		}
	}
	// No absent snapshot was observed: identical identity cannot prove delete/readd.
	diff, err := Diff(previous, diffSnapshot("a", "a", "b"))
	if err != nil || len(diff.Added)+len(diff.Removed)+len(diff.Restored) != 0 {
		t.Fatal(diff, err)
	}
}
func TestDiffSeparatesRemovalRestorationAndIgnored(t *testing.T) {
	state := diffState(diffSnapshot("imported", "pending", "ignored", "absent"))
	trackID := int64(10)
	state.Entries[0].LocalState = Imported
	state.Entries[0].TrackID = &trackID
	state.Entries[2].LocalState = Ignored
	state.Entries[3].LocalState = Imported
	state.Entries[3].TrackID = &trackID
	state.Entries[3].UpstreamPresent = false
	next := diffSnapshot("absent", "ignored")
	diff, err := Diff(state, next)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Removed) != 2 || diff.Removed[0].Disposition != "retain_absent" || diff.Removed[1].Disposition != "cancel_import" || len(diff.Restored) != 1 || diff.Restored[0].EntryID != 4 || len(diff.Ignored) != 1 {
		t.Fatal(diff)
	}
	empty, err := Diff(state, diffSnapshot())
	if err != nil || len(empty.Removed) != 3 || empty.Removed[2].Disposition != "forget_ignored" {
		t.Fatal(empty, err)
	}
	// Stable absent ordering must not mutate input or collapse shared TrackID.
	state.Entries[0].UpstreamPresent = false
	state.Entries[0].Position = 4
	before := append([]StoredEntry{}, state.Entries...)
	ordered := OrderedEntries(state.Entries)
	if len(ordered) != 4 || ordered[0].ID != 2 || ordered[2].ID != 1 || ordered[3].ID != 4 || !reflect.DeepEqual(before, state.Entries) {
		t.Fatal(ordered)
	}
}
func TestDiffRejectsIncompleteAndReportsExcluded(t *testing.T) {
	s := diffSnapshot("a")
	s.Completeness.Complete = false
	if _, err := Diff(nil, s); Code(err) != Incomplete {
		t.Fatal(err)
	}
	s = diffSnapshot("a")
	s.Excluded = []Excluded{{Position: 2, Kind: "episode"}}
	s.Completeness.DeclaredTotal = 2
	s.Completeness.RawSeen = 2
	s.Completeness.ExcludedCount = 1
	diff, err := Diff(nil, s)
	if err != nil || len(diff.Added) != 1 || len(diff.Excluded) != 1 {
		t.Fatal(diff, err)
	}
	state := diffState(s)
	next := s
	next.Entries = append([]Entry{}, s.Entries...)
	next.Entries[0].TrackRef = "different"
	if _, err := Diff(state, next); err == nil {
		t.Fatal("reassigned occurrence accepted")
	}
}
