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

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/lyricfetch"
)

type Provider struct{ client *provider.Client }

func New(client *provider.Client) *Provider { return &Provider{client: client} }
func (*Provider) ID() provider.ProviderID   { return "kuwo" }
func (*Provider) DisplayName() string       { return "酷我音乐" }
func (p *Provider) request(ctx context.Context, base, path string, params map[string]string) ([]byte, error) {
	return p.client.Get(ctx, provider.GetRequest{Provider: provider.KuwoQuota, BaseURL: base, Endpoint: path, Params: params, CacheMode: provider.NoStore, MaxResponseBytes: 4 << 20, Headers: map[string]string{"Referer": base + "/"}, Redirects: &provider.RedirectPolicy{Hosts: []string{"search.kuwo.cn", "m.kuwo.cn", "www.kuwo.cn"}, MaxRedirects: 4}})
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

const openapiLyricsPath = "/openapi/v1/www/lyric/getlyric"

func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (doc *lyricdoc.Document, err error) {
	defer func() { err = lyricfetch.Normalize(err) }()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = provider.WithStopRestrictedRetries(ctx)
	body, err := p.request(ctx, "https://www.kuwo.cn", openapiLyricsPath, map[string]string{"musicId": c.Ref})
	if err == nil {
		var payload *lyricdoc.Input
		payload, err = parseLyricsResponse(body, true, c)
		if err == nil {
			var doc *lyricdoc.Document
			doc, err = lyricdoc.Build(payload)
			if err == nil {
				return doc, nil
			}
		}
	}
	if !lyricfetch.CanFallback(err) {
		return nil, err
	}
	body, err = p.request(ctx, "https://m.kuwo.cn", "/newh5/singles/songinfoandlrc", map[string]string{"musicId": c.Ref})
	if err != nil {
		return nil, err
	}
	payload, err := parseLyricsResponse(body, false, c)
	if err != nil {
		return nil, err
	}
	payload.Warnings = append(payload.Warnings, "openapi 未取得可用逐行歌词，已回退 HTTPS H5")
	return lyricdoc.Build(payload)
}
func parseLyricsResponse(body []byte, openapi bool, c lyrics.Candidate) (*lyricdoc.Input, error) {
	var r struct {
		Code   *int
		Status *int
		Msg    string
		Data   *struct {
			Lrclist []struct {
				Time      json.RawMessage
				LineLyric string
			}
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("%w：酷我歌词响应无法解析", lyricdoc.ErrFormat)
	}
	status := r.Status
	if openapi {
		status = r.Code
	}
	if status == nil {
		return nil, fmt.Errorf("%w：酷我歌词响应缺少业务状态", lyricdoc.ErrFormat)
	}
	if *status != 200 {
		return nil, &provider.BusinessError{Source: "酷我", Block: "lyrics", Code: *status}
	}
	if r.Data == nil || r.Data.Lrclist == nil {
		return nil, lyricdoc.ErrNoLyrics
	}
	if len(r.Data.Lrclist) == 0 {
		return nil, lyricdoc.ErrNoLyrics
	}
	rows := make([]lyricRow, 0, len(r.Data.Lrclist))
	validTimes := true
	for i, l := range r.Data.Lrclist {
		text := html.UnescapeString(l.LineLyric)
		t, err := strconv.ParseFloat(strings.Trim(string(l.Time), `"`), 64)
		if err != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t > 24*3600 {
			validTimes = false
			t = 0
		}
		rows = append(rows, lyricRow{position: i, timeMS: int64(math.Round(t * 1000)), text: text})
	}
	return prepareRows(rows, validTimes, c)
}

var _ lyrics.Provider = (*Provider)(nil)
