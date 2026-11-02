package matching

import "regexp"

// 发行版本描述不是翻译；即使不是录音版本词，也不得在去重时剥掉。
var importEditionPattern = regexp.MustCompile(`(?i)\b(?:deluxe|remaster(?:ed)?|mono|stereo|edition|expanded|bonus|anniversary)\b`)

func sameImportEdition(a, b string) bool {
	key := func(text string) string {
		var parts string
		for _, seg := range FindBracketSegments(text) {
			if importEditionPattern.MatchString(seg.Inner) {
				parts += "|" + Normalize(seg.Inner)
			}
		}
		return parts
	}
	return key(a) == key(b)
}

// AutomaticCandidates separates evidence strength from display score. A complete
// exact match is not made ambiguous by results that only match the artist (e.g.
// a studio single alongside a live album). Ties still go through track/group and
// release admission checks; this never selects the highest numeric score.
func AutomaticCandidates(scored []ScoredCandidate) []ScoredCandidate {
	var high, exact []ScoredCandidate
	for _, sc := range scored {
		if !sc.Score.HighConf {
			continue
		}
		high = append(high, sc)
		if sc.Score.TitleMatch == MatchExact && sc.Score.CreditMatch == MatchExact && sc.Score.AlbumMatch == MatchExact {
			exact = append(exact, sc)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return high
}

// SameImportText is deliberately stricter than search scoring. It accepts
// annotations/alternate spellings but not fuzzy prefixes, missing fields or
// recording identity alone. Version markers remain significant on both axes.
func SameImportText(a, b Input) bool {
	return SameTrackContext(a, b) && completeEqualOrUntruncated(a.Artist, b.Artist) && sameImportCredit(a.Artist, b.Artist)
}

// SameTrackContext verifies the release-track context after an exact upstream
// binding is known. Credited artist and performer can differ; title and album
// must still agree and positions/durations must not conflict.
func SameTrackContext(a, b Input) bool {
	w := DefaultWeights()
	return completeEqualOrUntruncated(a.Title, b.Title) && completeEqualOrUntruncated(a.Album, b.Album) &&
		w.CompareTitle(a.Title, b.Title) == MatchExact && w.CompareTitle(a.Album, b.Album) == MatchExact &&
		sameImportEdition(a.Title, b.Title) && sameImportEdition(a.Album, b.Album) && CompatibleImportPosition(a, b)
}
func completeEqualOrUntruncated(a, b string) bool {
	return Normalize(a) != "" && Normalize(b) != "" && (!(IsTruncated(a) || IsTruncated(b)) || a == b)
}

// sameImportCredit does not borrow a CV actor or drop featured artists. Those
// are useful search evidence, but not proof that two full credits are identical.
func sameImportCredit(a, b string) bool {
	if Normalize(a) == Normalize(b) {
		return true
	}
	for _, text := range []string{a, b} {
		for _, seg := range FindBracketSegments(text) {
			kind := defaultRules.Classify(seg).Kind
			if kind == KindCV || kind == KindFeature || kind == KindDisambiguation {
				return false
			}
		}
	}
	return DefaultWeights().CompareTitle(a, b) == MatchExact
}

func CompatibleImportPosition(a, b Input) bool {
	for _, pair := range [][2]*int{{a.DiscNo, b.DiscNo}, {a.TrackNo, b.TrackNo}} {
		if pair[0] != nil && pair[1] != nil && *pair[0] != *pair[1] {
			return false
		}
	}
	return a.DurationMS == nil || b.DurationMS == nil || DurationWithinTolerance(*a.DurationMS, *b.DurationMS)
}

// CompareAlbum recognizes translations without treating fuzzy album names as
// exact. Search can still offer fuzzy matches; automatic evidence cannot.
func (w Weights) CompareAlbum(a, b string) MatchDegree {
	if sameImportEdition(a, b) && w.CompareTitle(a, b) == MatchExact {
		return MatchExact
	}
	return w.CompareText(a, b)
}
