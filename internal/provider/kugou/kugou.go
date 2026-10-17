// Package kugou implements native song and lyric download protocols.
package kugou

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

type Provider struct{ client *provider.Client }

func New(client *provider.Client) *Provider { return &Provider{client: client} }
func (*Provider) ID() provider.ProviderID   { return "kugou" }
func (*Provider) DisplayName() string       { return "酷狗音乐" }
func (p *Provider) request(ctx context.Context, base, path string, params map[string]string) ([]byte, error) {
	return p.client.Get(ctx, provider.GetRequest{Provider: provider.KugouQuota, BaseURL: base, Endpoint: path, Params: params, CacheMode: provider.NoStore, MaxResponseBytes: 4 << 20, Redirects: &provider.RedirectPolicy{Hosts: []string{"mobileservice.kugou.com", "lyrics.kugou.com"}, MaxRedirects: 4}})
}
func (p *Provider) SearchLyrics(ctx context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	body, err := p.request(ctx, "https://mobileservice.kugou.com", "/api/v3/search/song", map[string]string{"keyword": lyrics.SearchTerm(q), "page": "1", "pagesize": "20"})
	if err != nil {
		return nil, err
	}
	var r struct {
		Status, Errcode int
		Data            *struct {
			Info []struct {
				Hash, Songname, Singername string
				Album                      string `json:"album_name"`
				Duration                   int
			}
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != 1 || r.Errcode != 0 || r.Data == nil || r.Data.Info == nil {
		return nil, fmt.Errorf("酷狗搜索响应异常（status=%d, code=%d）", r.Status, r.Errcode)
	}
	out := []lyrics.Candidate{}
	for _, s := range r.Data.Info {
		out = append(out, lyrics.Candidate{Ref: s.Hash, Title: s.Songname, Artist: s.Singername, Album: s.Album, DurationMS: s.Duration * 1000})
	}
	return out, nil
}
func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyrics.Payload, error) {
	body, err := p.request(ctx, "https://lyrics.kugou.com", "/search", map[string]string{"ver": "1", "man": "yes", "client": "pc", "keyword": c.Title + " " + c.Artist, "hash": c.Ref, "duration": strconv.Itoa(c.DurationMS)})
	if err != nil {
		return nil, err
	}
	var r struct {
		Status     int
		Candidates []struct {
			ID, Accesskey string
			Duration      int
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("酷狗歌词候选接口错误（status=%d）", r.Status)
	}
	if len(r.Candidates) == 0 {
		return nil, lyrics.ErrNoLyrics
	}
	if c.DurationMS > 0 {
		sort.SliceStable(r.Candidates, func(i, j int) bool {
			return math.Abs(float64(r.Candidates[i].Duration-c.DurationMS)) < math.Abs(float64(r.Candidates[j].Duration-c.DurationMS))
		})
	}
	candidate := r.Candidates[0]
	if candidate.ID == "" || candidate.Accesskey == "" {
		return nil, fmt.Errorf("酷狗歌词候选缺少下载标识")
	}
	body, err = p.request(ctx, "https://lyrics.kugou.com", "/download", map[string]string{"ver": "1", "client": "pc", "id": candidate.ID, "accesskey": candidate.Accesskey, "fmt": "lrc", "charset": "utf8"})
	if err != nil {
		return nil, err
	}
	var d struct {
		Status       int
		ErrorCode    int `json:"error_code"`
		Fmt, Content string
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if d.Status != 200 || d.ErrorCode != 0 || d.Fmt != "lrc" {
		return nil, fmt.Errorf("酷狗没有返回可用的 LRC 歌词")
	}
	text, err := base64.StdEncoding.DecodeString(d.Content)
	if err != nil {
		return nil, fmt.Errorf("酷狗歌词 Base64 解码失败: %w", err)
	}
	return &lyrics.Payload{Text: string(text)}, nil
}

var _ lyrics.Provider = (*Provider)(nil)
