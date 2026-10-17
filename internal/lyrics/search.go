package lyrics

import (
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
		score := w.ScoreCandidate(pq, pc)
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
		c.Score = int(score.Total)
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
				c.Score = max(0, c.Score-20)
				c.MatchNotes = append(c.MatchNotes, "时长差异超过 10 秒，需确认")
			}
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].HighConfidence != out[j].HighConfidence {
			return out[i].HighConfidence
		}
		return out[i].Score > out[j].Score
	})
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
