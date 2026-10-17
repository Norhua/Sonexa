// Package kuwo implements native song search and timed lyric conversion.
package kuwo

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"math"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

type Provider struct{ client *provider.Client }

func New(client *provider.Client) *Provider { return &Provider{client: client} }
func (*Provider) ID() provider.ProviderID   { return "kuwo" }
func (*Provider) DisplayName() string       { return "酷我音乐" }
func (p *Provider) request(ctx context.Context, base, path string, params map[string]string) ([]byte, error) {
	return p.client.Get(ctx, provider.GetRequest{Provider: provider.KuwoQuota, BaseURL: base, Endpoint: path, Params: params, CacheMode: provider.NoStore, MaxResponseBytes: 4 << 20, Headers: map[string]string{"Referer": "https://www.kuwo.cn/"}, Redirects: &provider.RedirectPolicy{Hosts: []string{"search.kuwo.cn", "m.kuwo.cn"}, MaxRedirects: 4}})
}
func number(n json.Number) int { v, _ := strconv.Atoi(n.String()); return v }
func (p *Provider) SearchLyrics(ctx context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	body, err := p.request(ctx, "https://search.kuwo.cn", "/r.s", map[string]string{"all": lyrics.SearchTerm(q), "ft": "music", "pn": "0", "rn": "20", "rformat": "json", "encoding": "utf8", "pcjson": "1"})
	if err != nil {
		return nil, err
	}
	var r struct {
		Total   json.Number
		Abslist []struct {
			RID                 string `json:"MUSICRID"`
			Name, Artist, Album string
			Duration            json.Number
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Total == "" || (r.Abslist == nil && number(r.Total) != 0) {
		return nil, fmt.Errorf("酷我搜索响应缺少歌曲列表")
	}
	out := []lyrics.Candidate{}
	for _, s := range r.Abslist {
		out = append(out, lyrics.Candidate{Ref: strings.TrimPrefix(s.RID, "MUSIC_"), Title: html.UnescapeString(s.Name), Artist: html.UnescapeString(s.Artist), Album: html.UnescapeString(s.Album), DurationMS: number(s.Duration) * 1000})
	}
	return out, nil
}
func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyrics.Payload, error) {
	body, err := p.request(ctx, "https://m.kuwo.cn", "/newh5/singles/songinfoandlrc", map[string]string{"musicId": c.Ref})
	if err != nil {
		return nil, err
	}
	return parseLyrics(body)
}
func parseLyrics(body []byte) (*lyrics.Payload, error) {
	var r struct {
		Status int
		Msg    string
		Data   *struct {
			Lrclist []struct {
				Time      json.RawMessage
				LineLyric string
			}
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("酷我歌词接口错误（status=%d）：%s", r.Status, r.Msg)
	}
	if r.Data == nil || r.Data.Lrclist == nil {
		return nil, lyrics.ErrNoLyrics
	}
	timed := []lyrics.TimedLine{}
	plain := []string{}
	for _, l := range r.Data.Lrclist {
		text := html.UnescapeString(l.LineLyric)
		if strings.TrimSpace(text) == "" {
			continue
		}
		t, err := strconv.ParseFloat(strings.Trim(string(l.Time), `"`), 64)
		if err != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t > 24*3600 {
			plain = append(plain, text)
			continue
		}
		timed = append(timed, lyrics.TimedLine{MS: int(math.Round(t * 1000)), Text: text})
	}
	if len(plain) > 0 {
		plain = nil
		for _, l := range r.Data.Lrclist {
			plain = append(plain, html.UnescapeString(l.LineLyric))
		}
		return &lyrics.Payload{Text: strings.Join(plain, "\n")}, nil
	}
	text, err := lyrics.RenderTimed(timed)
	if err != nil {
		return nil, err
	}
	return &lyrics.Payload{Text: text}, nil
}

var _ lyrics.Provider = (*Provider)(nil)
