package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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
	original := lyricChannel(r.Lrc.Lyric)
	if original.Status == "unavailable" {
		return nil, lyrics.ErrNoLyrics
	}
	return &lyrics.Payload{Original: original, Translation: lyricChannel(r.Tlyric.Lyric), Romanization: lyricChannel(r.Romalrc.Lyric), WordTiming: "unsupported"}, nil
}

// 网易云会把无歌词状态放进 LRC 正文，而非只使用 nolyric 标志。
// 这是来源响应语义，不应由公共文本校验器应用到其他来源。
var lyricTimeTags = regexp.MustCompile(`\[\d{1,3}:[0-5]?\d(?:[.:]\d{1,3})?\]|<\d{1,3}:[0-5]?\d(?:[.:]\d{1,3})?>`)
var lyricPlaceholderPunctuation = strings.NewReplacer("，", "", ",", "", "。", "", " ", "", "！", "", "!", "")

func lyricChannel(text string) *lyrics.Channel {
	channel := lyrics.TextChannel(text, nil)
	if channel.Status != "available" {
		return channel
	}
	for _, line := range strings.Split(channel.Text, "\n") {
		plain := lyricPlaceholderPunctuation.Replace(strings.TrimSpace(lyricTimeTags.ReplaceAllString(line, "")))
		switch plain {
		case "纯音乐请欣赏", "纯音乐请您欣赏", "此歌曲为没有填词的纯音乐请您欣赏", "暂无歌词", "此歌曲暂无歌词":
			return lyrics.AbsentChannel()
		}
	}
	return channel
}

var _ lyrics.Provider = (*Provider)(nil)
