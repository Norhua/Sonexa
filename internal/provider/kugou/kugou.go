// Package kugou implements native song and lyric download protocols.
package kugou

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/matching"
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

type lyricCandidate struct {
	ID, Accesskey, Song, Singer string
	Duration                    int
}

func selectLyricCandidate(c lyrics.Candidate, candidates []lyricCandidate) (lyricCandidate, error) {
	valid := []lyricCandidate{}
	w := matching.DefaultWeights()
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate.ID) == "" || strings.TrimSpace(candidate.Accesskey) == "" {
			continue
		}
		if matching.Normalize(c.Title) != "" && matching.Normalize(candidate.Song) != "" && w.CompareTitle(c.Title, candidate.Song) == matching.MatchNone {
			continue
		}
		// Hash identifies the song; Singer can be an uploader and is not an
		// independent recording-identity constraint on this endpoint.
		valid = append(valid, candidate)
	}
	if len(valid) == 0 {
		return lyricCandidate{}, lyrics.ErrNoLyrics
	}
	sort.Slice(valid, func(i, j int) bool {
		a, b := valid[i], valid[j]
		if c.DurationMS > 0 {
			if (a.Duration > 0) != (b.Duration > 0) {
				return a.Duration > 0
			}
			da, db := math.Abs(float64(a.Duration)-float64(c.DurationMS)), math.Abs(float64(b.Duration)-float64(c.DurationMS))
			if da != db {
				return da < db
			}
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Accesskey < b.Accesskey
	})
	return valid[0], nil
}

func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyrics.Payload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ctx = provider.WithStopRestrictedRetries(ctx)
	body, err := p.request(ctx, "https://lyrics.kugou.com", "/search", map[string]string{"ver": "1", "man": "yes", "client": "pc", "keyword": c.Title + " " + c.Artist, "hash": c.Ref, "duration": strconv.Itoa(c.DurationMS)})
	if err != nil {
		return nil, err
	}
	var r struct {
		Status     int
		Candidates []lyricCandidate
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("酷狗歌词候选接口错误（status=%d）", r.Status)
	}
	candidate, err := selectLyricCandidate(c, r.Candidates)
	if err != nil {
		return nil, err
	}
	payload, err := p.download(ctx, candidate, "krc")
	if err == nil {
		_, err = lyrics.DocumentFromPayload(payload)
		if err == nil {
			return payload, nil
		}
	}
	if !lyrics.CanFallback(err) {
		return nil, err
	}
	fallback, fallbackErr := p.download(ctx, candidate, "lrc")
	if fallbackErr != nil {
		return nil, fallbackErr
	}
	fallback.WordTiming = "error"
	if errors.Is(err, lyrics.ErrNoLyrics) {
		fallback.WordTiming = "unavailable"
	}
	fallback.Warnings = append(fallback.Warnings, "KRC 未取得可用原文，已回退同候选 LRC")
	if payload != nil {
		fallback.Translation, fallback.Romanization, fallback.Additional = payload.Translation, payload.Romanization, payload.Additional
	}
	return fallback, nil
}

func (p *Provider) download(ctx context.Context, candidate lyricCandidate, format string) (*lyrics.Payload, error) {
	body, err := p.request(ctx, "https://lyrics.kugou.com", "/download", map[string]string{"ver": "1", "client": "pc", "id": candidate.ID, "accesskey": candidate.Accesskey, "fmt": format, "charset": "utf8"})
	if err != nil {
		return nil, err
	}
	var d struct {
		Status       int
		ErrorCode    int `json:"error_code"`
		Fmt, Content string
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("%w：酷狗歌词响应无法解析", lyrics.ErrFormat)
	}
	if d.Status != 200 || d.ErrorCode != 0 {
		return nil, &provider.BusinessError{Source: "酷狗", Block: "download", Code: d.Status}
	}
	if d.Content == "" {
		return nil, lyrics.ErrNoLyrics
	}
	if d.Fmt != format {
		return nil, fmt.Errorf("%w：酷狗歌词格式不符", lyrics.ErrFormat)
	}
	if format == "krc" {
		return krcPayload(d.Content)
	}
	text, err := base64.StdEncoding.DecodeString(d.Content)
	if err != nil {
		return nil, fmt.Errorf("%w：酷狗歌词 Base64 解码失败", lyrics.ErrFormat)
	}
	return &lyrics.Payload{Original: lyrics.TextChannel(string(text), nil), Translation: lyrics.UnknownChannel(), Romanization: lyrics.UnknownChannel(), WordTiming: "unavailable"}, nil
}

var _ lyrics.Provider = (*Provider)(nil)
