package lyrics

import (
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// Lyrics are manually adopted, not recording identities. An exact title with
// close duration may remain for confirmation despite a credit mismatch.
func rankCandidates(q Query, candidates []Candidate) []Candidate {
	w := matching.DefaultWeights()
	pq := provider.Query{Title: q.Title, Artist: q.Artist, Album: q.Album}
	if q.DurationMS > 0 {
		pq.DurationMS = &q.DurationMS
	}
	out := []Candidate{}
	for _, c := range candidates {
		if matching.Normalize(q.Title) != "" && accompanimentTitle(q.Title) != accompanimentTitle(c.Title) {
			continue
		}
		pc := provider.Candidate{Title: &c.Title, ArtistName: &c.Artist, AlbumTitle: &c.Album}
		if c.DurationMS > 0 {
			pc.DurationMS = &c.DurationMS
		}
		// Compare script-equivalent artist keys without changing the submitted
		// query or the provider's display credit (all sources share this rule).
		comparisonQuery := pq
		queryArtist, pcArtist := lyricCreditKeys(q.Artist, c.Artist)
		comparisonQuery.Artist = queryArtist
		pc.ArtistName = &pcArtist
		score := w.ScoreCandidate(comparisonQuery, pc)
		delta := absDuration(q.DurationMS - c.DurationMS)
		comparableDuration := q.DurationMS > 0 && c.DurationMS > 0
		if score.Rejected {
			continue
		}
		if matching.Normalize(q.Title) == "" {
			if score.CreditMatch == matching.MatchNone && score.AlbumMatch == matching.MatchNone {
				continue
			}
		} else if !score.Plausible && !(score.TitleMatch == matching.MatchExact && comparableDuration && delta <= 10000) {
			continue
		}
		c.MatchScore = int(score.Total)
		c.MatchTerms = matchTerms(score, w, q, c)
		c.SortKey = hex.EncodeToString([]byte(c.Ref))
		c.HighConfidence = score.HighConf && score.TitleMatch == matching.MatchExact
		c.MatchNotes = []string{}
		if score.TitleMatch == matching.MatchExact {
			c.MatchNotes = append(c.MatchNotes, "曲名相符")
		}
		if score.TitleMatch == matching.MatchFuzzy {
			c.MatchNotes = append(c.MatchNotes, "曲名近似，需确认")
		}
		if score.CreditMatch != matching.MatchNone {
			c.MatchNotes = append(c.MatchNotes, "艺术家相符")
		} else if matching.Normalize(q.Artist) != "" {
			c.HighConfidence = false
			c.MatchNotes = append(c.MatchNotes, "艺术家不同或缺失，需确认")
		}
		if score.AlbumMatch != matching.MatchNone {
			c.MatchNotes = append(c.MatchNotes, "专辑相符")
		}
		if comparableDuration {
			if delta <= 3000 {
				c.MatchNotes = append(c.MatchNotes, "时长相近")
			} else if delta > 10000 {
				c.HighConfidence = false
				penalty := min(20, c.MatchScore)
				c.MatchScore -= penalty
				c.MatchTerms = append(c.MatchTerms, ScoreTerm{"duration_penalty", "时长差异超过 10 秒", -penalty})
				c.MatchNotes = append(c.MatchNotes, "时长差异超过 10 秒，需确认")
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return matchLess(out[i], out[j]) })
	seen := map[string]bool{}
	unique := out[:0]
	for _, c := range out {
		key := c.Source + "\x00" + c.Ref
		// Empty refs remain usable for pure matching tests; Service rejects them.
		if c.Ref != "" && seen[key] {
			continue
		}
		seen[key] = true
		unique = append(unique, c)
	}
	return unique
}

type ScoreTerm struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	Value int    `json:"value"`
}

func matchTerms(s matching.Score, w matching.Weights, q Query, c Candidate) []ScoreTerm {
	title, credit, album, duration := 0, 0, 0, 0
	if s.TitleMatch != matching.MatchNone {
		title = int(w.Title)
	}
	if s.CreditMatch == matching.MatchExact {
		credit = int(w.CreditExact)
	} else if s.CreditMatch == matching.MatchFuzzy {
		credit = int(w.CreditFuzzy)
	}
	if s.AlbumMatch == matching.MatchExact {
		album = int(w.AlbumExact)
	} else if s.AlbumMatch == matching.MatchFuzzy {
		album = int(w.AlbumFuzzy)
	}
	if q.DurationMS > 0 && c.DurationMS > 0 {
		delta := absDuration(q.DurationMS - c.DurationMS)
		if delta <= 3000 {
			duration = 5
		} else if delta <= 10000 {
			duration = 2
		}
	}
	return []ScoreTerm{{"title", "曲名", title}, {"artist", "艺术家", credit}, {"album", "专辑", album}, {"duration", "时长", duration}}
}

func matchLess(a, b Candidate) bool {
	if a.MatchScore != b.MatchScore {
		return a.MatchScore > b.MatchScore
	}
	if a.HighConfidence != b.HighConfidence {
		return a.HighConfidence
	}
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	if a.SortKey != b.SortKey {
		return a.SortKey < b.SortKey
	}
	// Deterministic winner among duplicated source identities.
	return fmt.Sprintf("%s\x00%s\x00%s\x00%012d", a.Title, a.Artist, a.Album, a.DurationMS) < fmt.Sprintf("%s\x00%s\x00%s\x00%012d", b.Title, b.Artist, b.Album, b.DurationMS)
}

const PrefetchLimit = 3

// MatchTop returns a copy; the backend session freezes it before content can
// change final ranking. Missing or failed lyrics never fill from fourth place.
func MatchTop(candidates []Candidate) []Candidate {
	rows := append([]Candidate(nil), candidates...)
	sort.Slice(rows, func(i, j int) bool { return matchLess(rows[i], rows[j]) })
	out := []Candidate{}
	seen := map[string]bool{}
	for _, c := range rows {
		key := c.Source + "\x00" + c.Ref
		if c.Ref == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
		if len(out) == PrefetchLimit {
			break
		}
	}
	return out
}

var accompaniment = regexp.MustCompile(`(?:^|[\s（(【\[])(?:伴奏|伴奏版|纯音乐|純音樂)(?:$|[\s）)】\]])`)

func accompanimentTitle(title string) bool { return accompaniment.MatchString(title) }
func absDuration(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
