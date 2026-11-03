package matching

import (
	"strings"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/textmatch"
	"golang.org/x/text/unicode/norm"
)

// ScoreMetadataCandidate separates recall for manual review from identity evidence.
// ScoreCandidate/CompareTitle remain strict for lyrics, deduplication and binding.
// Any rescue is explicitly review-only, regardless of score or corroboration.
func (w Weights) ScoreMetadataCandidate(q provider.Query, c provider.Candidate) Score {
	s := w.ScoreCandidate(q, c)
	if !s.Rejected && s.Plausible {
		return s
	}
	degree := s.TitleMatch
	if Normalize(q.Title) != "" && degree == MatchNone {
		if unspecifiedVersionTitle(w, q.Title, deref(c.Title)) {
			degree = MatchFuzzy
		}
		// Search-only titles must not override an explicitly requested version.
		if len(VersionMarkers(q.Title)) > 0 {
			return s
		}
		for _, title := range c.SearchTitles {
			if w.CompareTitle(q.Title, title) == MatchExact || unspecifiedVersionTitle(w, q.Title, title) {
				degree = MatchFuzzy
				break
			}
		}
		if degree == MatchNone {
			return s
		}
	}
	s = w.scoreCandidate(q, c, degree)
	// Fold Han variants only as fuzzy search corroboration. Never change stored
	// names, global Normalize, or the exact identity/deduplication comparators.
	if s.CreditMatch == MatchNone && hanCreditEquivalent(q.Artist, deref(c.ArtistName)) {
		s.CreditMatch = MatchFuzzy
		s.Total += w.CreditFuzzy
	}
	// A complete album name embedded in a longer platform description can
	// corroborate an exact title. Do not compound this weaker hint with a
	// fuzzy/alternate title or an unspecified recording version.
	if s.AlbumMatch == MatchNone && s.TitleMatch == MatchExact &&
		!IsTruncated(q.Title) && !IsTruncated(deref(c.Title)) &&
		metadataAlbumPhrase(q.Album, deref(c.AlbumTitle)) {
		s.AlbumMatch = MatchFuzzy
		s.Total += w.AlbumFuzzy
	}
	s.Plausible = w.IsPlausible(q, c, s)
	s.ReviewOnly = true
	s.HighConf = false
	return s
}

// unspecifiedVersionTitle requires an exact base, not arbitrary substring or
// fuzzy similarity. Strip only delimited version descriptions, never bare words
// in ordinary titles (Live and Let Die). An explicit query version is not relaxed.
func unspecifiedVersionTitle(w Weights, query, candidate string) bool {
	if len(VersionMarkers(query)) != 0 || IsTruncated(query) || IsTruncated(candidate) || Normalize(query) == "" {
		return false
	}
	core := norm.NFKC.String(candidate)
	removed := false
	segments := FindBracketSegments(core)
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if len(VersionMarkers(seg.Inner)) > 0 {
			core = core[:seg.Start] + core[seg.End:]
			removed = true
		}
	}
	core = StripNonVersionAnnotations(core)
	if removed && w.CompareTitle(query, core) == MatchExact {
		return true
	}
	for i, ch := range core {
		if !strings.ContainsRune("-–—~〜", ch) {
			continue
		}
		if len(VersionMarkers(core[i:])) > 0 && w.CompareTitle(query, strings.TrimSpace(core[:i])) == MatchExact {
			return true
		}
	}
	return false
}

func hanCreditEquivalent(a, b string) bool {
	forms := func(s string) []string {
		return append([]string{s, primaryCredit(s)}, Alternates(s)...)
	}
	for _, left := range forms(a) {
		for _, right := range forms(b) {
			if textmatch.HanEquivalent(Normalize(left), Normalize(right)) {
				return true
			}
		}
	}
	return false
}
