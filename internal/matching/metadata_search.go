package matching

import "github.com/norhua/sonexa/internal/provider"

// CompareMetadataSearchTitle applies the metadata version vocabulary before
// loose display-name matching. Only recognized, equal full qualifier payloads
// can be removed for comparison; unknown annotations keep the legacy recall
// behavior and never gain automatic identity evidence from this display gate.
// Lyrics continue to call CompareTitle directly.
func (w Weights) CompareMetadataSearchTitle(query, candidate string) MatchDegree {
	a, b := ParseMetadataText(query, MetadataTitle), ParseMetadataText(candidate, MetadataTitle)
	if len(a.Qualifiers) == 0 && len(b.Qualifiers) == 0 {
		return w.CompareTitle(query, candidate)
	}
	if !sameMetadataQualifiers(a, b) {
		return MatchNone
	}
	return w.CompareTitle(a.Body, b.Body)
}

// ScoreMetadataCandidate retains configured display weights and auxiliary
// checks, while keeping metadata version semantics out of the lyrics scorer.
// Candidates and queries are not rewritten or used to fill source snapshots.
func (w Weights) ScoreMetadataCandidate(q provider.Query, c provider.Candidate) Score {
	title := MatchNone
	if Normalize(q.Title) != "" {
		title = w.CompareMetadataSearchTitle(q.Title, deref(c.Title))
		if title == MatchNone {
			return Score{Rejected: true, TitleMatch: MatchNone}
		}
	}
	auxiliary := q
	auxiliary.Title = ""
	score := w.ScoreCandidate(auxiliary, c)
	score.TitleMatch = title
	if title != MatchNone {
		score.Total += w.Title
	}
	score.HighConf = w.IsHighConfidence(score)
	score.Plausible = w.IsPlausible(q, c, score)
	return score
}
