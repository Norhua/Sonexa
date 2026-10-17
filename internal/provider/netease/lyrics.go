package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/norhua/sonexa/internal/lyrics"
)

func (p *Provider) SearchLyrics(ctx context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	songs, err := p.searchSongs(ctx, lyrics.SearchTerm(q), lyrics.MaxResults)
	if err != nil {
		return nil, err
	}
	out := []lyrics.Candidate{}
	for _, s := range songs {
		names := []string{}
		for _, a := range s.Artists {
			names = append(names, a.Name)
		}
		out = append(out, lyrics.Candidate{Ref: s.ID.String(), Title: s.Name, Artist: strings.Join(names, " / "), Album: s.Album.Name, DurationMS: s.DurationMS})
	}
	return out, nil
}
func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyrics.Payload, error) {
	body, err := p.request(ctx, "/weapi/song/lyric", map[string]any{"id": c.Ref, "lv": -1, "kv": -1, "tv": -1, "csrf_token": ""})
	if err != nil {
		return nil, err
	}
	return parseLyrics(body)
}
func parseLyrics(body []byte) (*lyrics.Payload, error) {
	var r struct {
		Code                 int
		Nolyric, Uncollected bool
		Lrc, Tlyric, Romalrc struct{ Lyric string }
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Code != 200 {
		return nil, fmt.Errorf("网易云歌词响应异常（code=%d）", r.Code)
	}
	if r.Nolyric || r.Uncollected || strings.TrimSpace(r.Lrc.Lyric) == "" {
		return nil, lyrics.ErrNoLyrics
	}
	return &lyrics.Payload{Original: lyrics.TextChannel(r.Lrc.Lyric, nil), Translation: lyrics.TextChannel(r.Tlyric.Lyric, nil), Romanization: lyrics.TextChannel(r.Romalrc.Lyric, nil), WordTiming: "unsupported"}, nil
}

var _ lyrics.Provider = (*Provider)(nil)
