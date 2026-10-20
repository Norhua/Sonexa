package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
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

const lyricV1Path = "/api/song/lyric/v1"

func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyrics.Payload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = provider.WithStopRestrictedRetries(ctx)
	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider: provider.NetEaseQuota, BaseURL: baseURL, Endpoint: lyricV1Path,
		Params:    map[string]string{"id": c.Ref, "lv": "-1", "tv": "-1", "rv": "-1", "yv": "-1"},
		CacheMode: provider.NoStore, MaxResponseBytes: 4 << 20,
		Headers:   map[string]string{"Referer": baseURL + "/"},
		Redirects: &provider.RedirectPolicy{Hosts: []string{"music.163.com"}, MaxRedirects: 4},
	})
	var bundle *lyrics.Payload
	if err == nil {
		bundle, err = parseLyrics(body)
	}
	if err == nil {
		_, err = lyrics.DocumentFromPayload(bundle)
		if err == nil {
			return bundle, nil
		}
	}
	if !lyrics.CanFallback(err) {
		return nil, err
	}
	body, fallbackErr := p.request(ctx, "/weapi/song/lyric", map[string]any{"id": c.Ref, "lv": -1, "kv": -1, "tv": -1, "csrf_token": ""})
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	fallback, fallbackErr := parseLyrics(body)
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	if bundle != nil {
		fallback.Translation = lyrics.PreferredChannel(bundle.Translation, fallback.Translation)
		fallback.Romanization = lyrics.PreferredChannel(bundle.Romanization, fallback.Romanization)
		fallback.WordTiming = bundle.WordTiming
		fallback.Warnings = append(fallback.Warnings, bundle.Warnings...)
	}
	fallback.Warnings = append(fallback.Warnings, "YRC 接口未取得可用原文，已回退普通歌词")
	return fallback, nil
}

type lyricBundle struct {
	Code                                         int
	Nolyric, Uncollected                         bool
	Lrc, Tlyric, Romalrc, Yrc, Ytlyric, Yromalrc struct{ Lyric string }
}

func parseLyrics(body []byte) (*lyrics.Payload, error) {
	var r lyricBundle
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%w：网易云歌词响应无法解析", lyrics.ErrFormat)
	}
	if r.Code != 200 {
		return nil, &provider.BusinessError{Source: "网易云", Block: "lyrics", Code: r.Code}
	}
	if r.Nolyric || r.Uncollected {
		return nil, lyrics.ErrNoLyrics
	}
	original, word := lyricChannel(r.Lrc.Lyric), yrcChannel(r.Yrc.Lyric)
	if original.Status == "unavailable" && word.Status == "unavailable" {
		return nil, lyrics.ErrNoLyrics
	}
	out := &lyrics.Payload{Original: original, WordOriginal: word,
		Translation:  lyrics.PreferredChannel(lyricChannel(r.Ytlyric.Lyric), lyricChannel(r.Tlyric.Lyric)),
		Romanization: lyrics.PreferredChannel(lyricChannel(r.Yromalrc.Lyric), lyricChannel(r.Romalrc.Lyric)), WordTiming: "unavailable"}
	if word.Status == "error" {
		out.WordTiming = "error"
	}
	if word.Status == "available" {
		out.WordTiming = "available"
	}
	return out, nil
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
