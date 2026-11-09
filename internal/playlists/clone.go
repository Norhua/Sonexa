package playlists

import "slices"

func cloneValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
func cloneEntry(e Entry) Entry {
	e.Title = cloneValue(e.Title)
	e.Album = cloneValue(e.Album)
	e.DurationMS = cloneValue(e.DurationMS)
	e.Artists = slices.Clone(e.Artists)
	return e
}
func cloneSnapshot(s Snapshot) Snapshot {
	s.Entries = slices.Clone(s.Entries)
	for i := range s.Entries {
		s.Entries[i] = cloneEntry(s.Entries[i])
	}
	s.Excluded = slices.Clone(s.Excluded)
	s.Diagnostics = slices.Clone(s.Diagnostics)
	return s
}
func cloneChanges(changes []Change) []Change {
	out := slices.Clone(changes)
	for i := range out {
		out[i].Entry = cloneEntry(out[i].Entry)
	}
	return out
}
func cloneDifference(d Difference) Difference {
	d.Added = cloneChanges(d.Added)
	d.Removed = cloneChanges(d.Removed)
	d.Restored = cloneChanges(d.Restored)
	d.Ignored = cloneChanges(d.Ignored)
	d.Excluded = slices.Clone(d.Excluded)
	return d
}
