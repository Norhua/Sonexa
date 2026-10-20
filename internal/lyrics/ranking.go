package lyrics

import "sort"

// RankFinal shares its ASCII SortKey contract with frontend/lyricsRanking.ts.
// Source IDs and hex-encoded UTF-8 refs sort without locale or random IDs.
func RankFinal(candidates []Candidate) []Candidate {
	rows := append([]Candidate(nil), candidates...)
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if (a.TotalScore != nil) != (b.TotalScore != nil) {
			return a.TotalScore != nil
		}
		if a.TotalScore != nil && *a.TotalScore != *b.TotalScore {
			return *a.TotalScore > *b.TotalScore
		}
		return matchLess(a, b)
	})
	return rows
}
