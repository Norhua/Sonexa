package lyrics

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

type named struct {
	Name string `json:"name"`
}

func names(in []named) string {
	out := []string{}
	for _, n := range in {
		out = append(out, n.Name)
	}
	return strings.Join(out, " / ")
}
func number(n json.Number) int { v, _ := strconv.Atoi(n.String()); return v }
func parseSearch(source string, body []byte, q Query) ([]Candidate, error) {
	out := []Candidate{}
	switch source {
	case "qq":
		var err error
		out, err = parseQQSongs(body)
		if err != nil {
			return nil, err
		}
	case "163":
		var r struct {
			Code   int
			Result *struct {
				SongCount *int
				Songs     []struct {
					ID   json.Number
					Name string
					Ar   []named
					Al   named
					Dt   int
				}
			}
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		if r.Code != 200 || r.Result == nil || (r.Result.Songs == nil && (r.Result.SongCount == nil || *r.Result.SongCount != 0)) {
			return nil, fmt.Errorf("网易云搜索响应异常（code=%d）", r.Code)
		}
		for _, s := range r.Result.Songs {
			out = append(out, Candidate{Ref: s.ID.String(), Title: s.Name, Artist: names(s.Ar), Album: s.Al.Name, DurationMS: s.Dt})
		}
	case "kugou":
		var r struct {
			Status  int
			Errcode int
			Data    *struct {
				Info []struct {
					Hash       string
					Songname   string
					Singername string
					Album      string `json:"album_name"`
					Duration   int
				}
			}
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		if r.Status != 1 || r.Errcode != 0 || r.Data == nil || r.Data.Info == nil {
			return nil, fmt.Errorf("酷狗搜索响应异常（status=%d, code=%d）", r.Status, r.Errcode)
		}
		for _, s := range r.Data.Info {
			out = append(out, Candidate{Ref: s.Hash, Title: s.Songname, Artist: s.Singername, Album: s.Album, DurationMS: s.Duration * 1000})
		}
	case "kuwo":
		var r struct {
			Total   json.Number
			Abslist []struct {
				RID      string `json:"MUSICRID"`
				Name     string
				Artist   string
				Album    string
				Duration json.Number
			}
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		if r.Total == "" || (r.Abslist == nil && number(r.Total) != 0) {
			return nil, fmt.Errorf("酷我搜索响应缺少歌曲列表")
		}
		for _, s := range r.Abslist {
			out = append(out, Candidate{Ref: strings.TrimPrefix(s.RID, "MUSIC_"), Title: html.UnescapeString(s.Name), Artist: html.UnescapeString(s.Artist), Album: html.UnescapeString(s.Album), DurationMS: number(s.Duration) * 1000})
		}
	}
	out = rankCandidates(q, out)
	seen := map[string]bool{}
	filtered := []Candidate{}
	for _, c := range out {
		if c.Ref == "" || seen[c.Ref] {
			continue
		}
		seen[c.Ref] = true
		c.Source = source
		filtered = append(filtered, c)
		if len(filtered) == MaxResults {
			break
		}
	}
	return filtered, nil
}

// Lyrics are manually adopted, not recording identities. An exact title with
// close duration may remain for confirmation despite a group/vocalist credit
// mismatch; it must never be automatically previewed as a confident match.
func rankCandidates(q Query, candidates []Candidate) []Candidate {
	w := matching.DefaultWeights()
	pq := provider.Query{Title: q.Title, Artist: q.Artist, Album: q.Album}
	if q.DurationMS > 0 {
		pq.DurationMS = &q.DurationMS
	}
	out := []Candidate{}
	for _, c := range candidates {
		// Shared metadata rules do not yet classify Chinese accompaniment tags.
		// Keep this lyric-specific guard out of automatic metadata matching.
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
			// Missing candidate metadata is not evidence for artist/album-only searches.
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
