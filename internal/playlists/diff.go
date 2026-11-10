package playlists

import (
	"fmt"
	"sort"
)

type State struct {
	Binding Binding       `json:"binding"`
	Entries []StoredEntry `json:"entries"`
}

type Change struct {
	Entry       Entry  `json:"entry"`
	EntryID     int64  `json:"entryId"`
	Disposition string `json:"disposition"`
}

// Difference describes only confirmed membership changes. Rename and reordering
// update the snapshot but do not create another import attempt.
type Difference struct {
	Added    []Change   `json:"added"`
	Removed  []Change   `json:"removed"`
	Restored []Change   `json:"restored"`
	Ignored  []Change   `json:"ignored"`
	Excluded []Excluded `json:"excluded"`
}

func Diff(previous *State, next Snapshot) (Difference, error) {
	out := Difference{Added: []Change{}, Removed: []Change{}, Restored: []Change{}, Ignored: []Change{}, Excluded: append([]Excluded{}, next.Excluded...)}
	if err := next.Validate(); err != nil {
		return out, err
	}
	old := map[string]StoredEntry{}
	if previous != nil {
		if previous.Binding.Ref != next.Ref {
			return out, fmt.Errorf("歌单快照身份不一致")
		}
		for _, entry := range previous.Entries {
			if old[entry.EntryKey].ID != 0 {
				return out, fmt.Errorf("已保存歌单出现键重复")
			}
			old[entry.EntryKey] = entry
		}
	}
	protected := map[string]bool{}
	for _, excluded := range next.Excluded {
		if excluded.TrackRef != "" {
			protected[excluded.TrackRef] = true
		}
	}
	present := map[string]bool{}
	for _, entry := range next.Entries {
		present[entry.EntryKey] = true
		prior, exists := old[entry.EntryKey]
		change := Change{Entry: entry, EntryID: prior.ID}
		if exists && prior.TrackRef != entry.TrackRef {
			return out, Failure(Incomplete, "歌单出现身份改变，无法安全应用快照")
		}
		switch {
		case !exists:
			change.Disposition = "import"
			out.Added = append(out.Added, change)
		case !prior.UpstreamPresent:
			change.Disposition = "reuse"
			out.Restored = append(out.Restored, change)
		case prior.LocalState == Ignored:
			change.Disposition = "keep_ignored"
			out.Ignored = append(out.Ignored, change)
		}
	}
	if previous != nil {
		for _, prior := range OrderedEntries(previous.Entries) {
			if present[prior.EntryKey] || !prior.UpstreamPresent || protected[prior.TrackRef] {
				continue
			}
			change := Change{Entry: prior.Entry, EntryID: prior.ID, Disposition: "cancel_import"}
			if prior.LocalState == Imported {
				change.Disposition = "retain_absent"
			}
			if prior.LocalState == Ignored {
				change.Disposition = "forget_ignored"
			}
			out.Removed = append(out.Removed, change)
		}
	}
	return out, nil
}

// OrderedEntries preserves each occurrence, even when two rows share TrackID.
// Retained absent rows follow current upstream rows, with a stable local tie-break.
func OrderedEntries(entries []StoredEntry) []StoredEntry {
	out := append([]StoredEntry{}, entries...)
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.UpstreamPresent != b.UpstreamPresent {
			return a.UpstreamPresent
		}
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		return a.ID < b.ID
	})
	return out
}
