package matching

import (
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// ConfirmedTargetUnchanged compares a previously approved TARGET with its
// current facts, not the incomplete input which the human already resolved.
// This is only a validity check for an existing confirmation. It neither
// matches a new input to a candidate nor changes automatic version selection.
func ConfirmedTargetUnchanged(approved, current Input) bool {
	for _, field := range []struct {
		before, after string
		album         bool
	}{{approved.Title, current.Title, false}, {approved.Album, current.Album, true}} {
		if field.before == "" { // Previously unknown information may be enriched.
			continue
		}
		if field.after == "" || confirmationVersionKey(field.before, field.album) != confirmationVersionKey(field.after, field.album) {
			return false
		}
	}
	for _, pair := range [][2]*int{{approved.DiscNo, current.DiscNo}, {approved.TrackNo, current.TrackNo}} {
		if pair[0] != nil && (pair[1] == nil || *pair[0] != *pair[1]) {
			return false
		}
	}
	return approved.DurationMS == nil || (current.DurationMS != nil && DurationWithinTolerance(*approved.DurationMS, *current.DurationMS))
}

// Compare full delimited version descriptions, not just the word "Ver":
// Singer A Ver. must not silently turn into Singer B Ver. after confirmation.
// Non-version display renames/annotations do not invalidate an exact binding.
func confirmationVersionKey(text string, album bool) string {
	text = norm.NFKC.String(text)
	markers := VersionMarkers(text)
	if album {
		markers = importAlbumMarkers(text)
	}
	var descriptions []string
	rest := text
	segments := FindBracketSegments(text)
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if len(VersionMarkers(seg.Inner)) > 0 || importEditionPattern.MatchString(seg.Inner) {
			descriptions = append(descriptions, Normalize(seg.Inner))
		}
		rest = rest[:seg.Start] + rest[seg.End:]
	}
	remaining := VersionMarkers(rest)
	if album {
		remaining = importAlbumMarkers(rest)
	}
	if len(remaining) > 0 || importEditionPattern.MatchString(rest) {
		// Only delimited suffixes are separable. A bare version word within a
		// title is conservatively protected as part of the complete title.
		version := rest
		for i, ch := range rest {
			if strings.ContainsRune("-–—~〜", ch) && (len(VersionMarkers(rest[i:])) > 0 || importEditionPattern.MatchString(rest[i:])) {
				version = rest[i:]
				break
			}
		}
		descriptions = append(descriptions, Normalize(version))
	}
	for marker := range markers {
		descriptions = append(descriptions, "marker:"+marker)
	}
	sort.Strings(descriptions)
	return strings.Join(descriptions, "|")
}
