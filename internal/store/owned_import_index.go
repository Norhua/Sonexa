package store

import (
	"sort"

	"github.com/norhua/sonexa/internal/matching"
)

// Build once per database snapshot. Unrelated titles cannot yield any of the
// three evidence ranks, so avoid running regex/normalization against every track
// for every pending item. Confirmed aliases participate independently of current
// titles (including raw identical inputs with a missing title).
type ownedImportIndex struct {
	tracks    []ownedImportTrack
	confirmed []confirmedImport
	byTitle   map[string][]int
	byRaw     map[string][]int
}

func newOwnedImportIndex(tracks []ownedImportTrack, confirmed []confirmedImport) *ownedImportIndex {
	index := &ownedImportIndex{tracks: tracks, confirmed: confirmed, byTitle: map[string][]int{}, byRaw: map[string][]int{}}
	positions := map[int64]int{}
	add := func(title string, i int) {
		for _, key := range matching.ImportTitleKeys(title) {
			index.byTitle[key] = append(index.byTitle[key], i)
		}
	}
	for i, track := range tracks {
		positions[track.id] = i
		add(track.current.Title, i)
		if track.snapshotValid {
			add(track.snapshot.Title, i)
		}
	}
	for _, confirmation := range confirmed {
		if i, ok := positions[confirmation.trackID]; ok {
			add(stringValue(confirmation.input.InTitle), i)
			index.byRaw[confirmation.input.RawRow] = append(index.byRaw[confirmation.input.RawRow], i)
		}
	}
	return index
}

func (index *ownedImportIndex) decide(inputs []ImportItem, candidates []matching.ScoredCandidate) ownedImportDecision {
	if len(inputs) == 0 {
		return ownedImportDecision{}
	}
	seen := map[int]bool{}
	// Union, not intersection: every input must see all its competing targets,
	// otherwise a neighbour's shortlist could incorrectly resolve ambiguity.
	for _, input := range inputs {
		for _, key := range matching.ImportTitleKeys(stringValue(input.InTitle)) {
			for _, i := range index.byTitle[key] {
				seen[i] = true
			}
		}
		for _, i := range index.byRaw[input.RawRow] {
			seen[i] = true
		}
	}
	positions := make([]int, 0, len(seen))
	for i := range seen {
		positions = append(positions, i)
	}
	sort.Ints(positions)
	tracks := make([]ownedImportTrack, 0, len(positions))
	ids := map[int64]bool{}
	for _, i := range positions {
		tracks = append(tracks, index.tracks[i])
		ids[index.tracks[i].id] = true
	}
	confirmations := make([]confirmedImport, 0)
	for _, c := range index.confirmed {
		if ids[c.trackID] {
			confirmations = append(confirmations, c)
		}
	}
	return decideOwnedImport(inputs, tracks, confirmations, exactOwnedRefs(tracks, candidates))
}
