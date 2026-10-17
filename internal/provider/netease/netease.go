// Package netease owns WEAPI transport and native response models. Lyrics and
// cover capabilities project these models independently; protocol parsing must
// not discard songs merely because another capability needs a different ID.
package netease

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/norhua/sonexa/internal/provider"
)

const baseURL = "https://music.163.com"
const searchPath = "/weapi/cloudsearch/pc"

// Provider implements the platform capabilities using injected shared facilities.
// Construction never creates or reconfigures a platform bucket.
type Provider struct {
	client *provider.Client
	form   func([]byte) (url.Values, error)
}

func New(client *provider.Client) *Provider { return &Provider{client: client, form: Form} }
func (*Provider) ID() provider.ProviderID   { return "163" }
func (*Provider) DisplayName() string       { return "网易云音乐" }

func (p *Provider) request(ctx context.Context, endpoint string, payload any) ([]byte, error) {
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	form, err := p.form(plain)
	if err != nil {
		return nil, err
	}
	return p.client.Get(ctx, provider.GetRequest{
		Provider: provider.NetEaseQuota, BaseURL: baseURL, Endpoint: endpoint,
		Body: []byte(form.Encode()), CacheMode: provider.NoStore, MaxResponseBytes: 4 << 20,
		Headers:   map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Referer": baseURL + "/"},
		Redirects: &provider.RedirectPolicy{Hosts: []string{"music.163.com"}, MaxRedirects: 4},
	})
}

type artist struct {
	Name string `json:"name"`
}
type album struct {
	ID     json.Number `json:"id"`
	Name   string      `json:"name"`
	PicURL string      `json:"picUrl"`
}
type song struct {
	ID         json.Number `json:"id"`
	Name       string      `json:"name"`
	Artists    []artist    `json:"ar"`
	Album      album       `json:"al"`
	DurationMS int         `json:"dt"`
}

func (p *Provider) searchSongs(ctx context.Context, term string, limit int) ([]song, error) {
	body, err := p.request(ctx, searchPath, map[string]any{"s": term, "type": 1, "limit": limit, "offset": 0, "total": true, "csrf_token": ""})
	if err != nil {
		return nil, err
	}
	return parseSongs(body)
}

func parseSongs(body []byte) ([]song, error) {
	var r struct {
		Code   int
		Result *struct {
			SongCount *int
			Songs     []song
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析网易云搜索响应失败: %w", err)
	}
	if r.Code != 200 || r.Result == nil || (r.Result.Songs == nil && (r.Result.SongCount == nil || *r.Result.SongCount != 0)) {
		return nil, fmt.Errorf("网易云搜索响应异常（code=%d）", r.Code)
	}
	return r.Result.Songs, nil
}
