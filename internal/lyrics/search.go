package lyrics

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
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
func parseSearch(source string, body []byte, album string) ([]Candidate, error) {
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
	// 专辑作为排序提示，不因版本/译名差异丢掉可由用户辨认的候选。
	if album = strings.TrimSpace(album); album != "" {
		sort.SliceStable(out, func(i, j int) bool {
			return strings.EqualFold(out[i].Album, album) && !strings.EqualFold(out[j].Album, album)
		})
	}
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
