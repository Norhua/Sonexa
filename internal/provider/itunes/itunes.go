// Package itunes 接入 iTunes Search API 作为免费无鉴权的补充上游。
//
// 它的字段远少于 MusicBrainz，地区参数直接决定返回的标题语言，
// 且其数据受 Apple 条款约束不得长期留存（必须设 TTL）。
package itunes

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// BaseURL 是 iTunes Search API 的地址。
const BaseURL = "https://itunes.apple.com"

// DefaultRatePerMin 是默认速率：约每分钟 20 次（按 IP），无鉴权无 API key。
const DefaultRatePerMin = 20.0

// DefaultTTLDays 是 iTunes 数据的默认缓存有效期。
//
// Apple 条款要求其元数据仅可为性能目的短期缓存，
// 不得作为长期事实来源留存。
const DefaultTTLDays = 30

// Config 是 iTunes 的配置。
type Config struct {
	// DefaultCountry 为空时跟随系统区域设置。
	DefaultCountry string
	RatePerMin     float64
	TTLDays        int
}

// Provider 是 iTunes 的 Provider 实现。
type Provider struct {
	client     *provider.Client
	cfg        Config
	hashParams func(map[string]string) string
}

// New 建立一个 iTunes provider。
func New(client *provider.Client, cfg Config, hashParams func(map[string]string) string) *Provider {
	if cfg.RatePerMin <= 0 {
		cfg.RatePerMin = DefaultRatePerMin
	}
	if cfg.TTLDays <= 0 {
		cfg.TTLDays = DefaultTTLDays
	}
	return &Provider{client: client, cfg: cfg, hashParams: hashParams}
}

// ID 实现 provider.Provider。
func (p *Provider) ID() provider.ProviderID { return provider.ITunes }

// DisplayName 实现 provider.Provider。
func (p *Provider) DisplayName() string { return "iTunes" }

// Capabilities 如实反映 iTunes 不提供的字段。
//
// 与 3.2 节对照表一致：次要类型、演唱者与器乐演奏者、作曲、作词、
// 角色与配音关系、主要类型、国家或地区、载体格式、厂牌、目录号、ISRC、
// 上游相关度分值——iTunes 全都没有。
//
// 如实声明的意义：这些列在只搜 iTunes 时被隐藏，聚合搜索时填占位短横线，
// 而不是显示空字符串或被误当作有效值参与评分。
func (p *Provider) Capabilities() provider.Capabilities {
	// 只有封面与多规格封面（尺寸靠构造地址得到）
	return provider.CapCoverArt | provider.CapMultiSizeCover
}

// RateLimit 实现 provider.Provider：默认 20 req/min，可配置。
//
// 搜索端点与清单查询端点共用同一个令牌桶——它们打的是同一个受限 API。
func (p *Provider) RateLimit() (int, time.Duration) {
	per := time.Duration(float64(time.Minute) / p.cfg.RatePerMin)
	return 1, per
}

// IsConfigured 实现 provider.Provider。
//
// iTunes 本身无需任何密钥或账号。但出网前置条件是全局的：
// 未填写 MusicBrainz 联系方式时 iTunes 入口同样禁用，
// 避免用户误以为 MusicBrainz 也已配置完成——该判定由调用方统一施加。
func (p *Provider) IsConfigured() error { return nil }

// TTL 返回 iTunes 数据的缓存有效期。
func (p *Provider) TTL() time.Duration {
	return time.Duration(p.cfg.TTLDays) * 24 * time.Hour
}

// SearchTracks 调用 search 端点。
//
// 只传 country，不传 lang：lang 仅支持 en_us / ja_jp，
// 而真正决定返回原生标题的是 country。
func (p *Provider) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	term := strings.TrimSpace(strings.Join([]string{q.Title, q.Artist, q.Album}, " "))
	if term == "" {
		return nil, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}
	country := p.ResolveCountry(q.Country, term)

	params := map[string]string{
		"media":   "music",
		"entity":  "song",
		"term":    term,
		"country": country,
		"limit":   strconv.Itoa(limit),
	}

	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider:   provider.ITunes,
		Endpoint:   "/search",
		BaseURL:    BaseURL,
		Params:     params,
		ParamsHash: p.hash(params),
		// iTunes 数据必须设 TTL，过期即重取
		TTL: p.TTL(),
	})
	if err != nil {
		return nil, err
	}

	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 iTunes 搜索响应失败: %w", err)
	}
	return ParseSearchResults(&resp, country), nil
}

// FetchDetail 调用 lookup 端点取回某专辑的完整曲目清单。
func (p *Provider) FetchDetail(ctx context.Context, ref provider.Ref) (*provider.Detail, error) {
	collectionID, country := SplitRef(ref.ID)
	if country == "" {
		country = ref.Country
	}
	if country == "" {
		country = p.ResolveCountry("", "")
	}
	if collectionID == "" {
		return nil, fmt.Errorf("缺少 collection 标识")
	}

	params := map[string]string{
		"id":      collectionID,
		"entity":  "song",
		"country": country,
		// limit 必须大于该专辑声明的曲目总数，否则清单被截断。
		// 200 覆盖了绝大多数专辑（含 120 首的合集）。
		"limit": strconv.Itoa(lookupLimit),
	}

	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider:   provider.ITunes,
		Endpoint:   "/lookup",
		BaseURL:    BaseURL,
		Params:     params,
		ParamsHash: p.hash(params),
		TTL:        p.TTL(),
	})
	if err != nil {
		return nil, err
	}

	var resp lookupResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 iTunes 清单响应失败: %w", err)
	}
	d := ParseLookup(&resp, country, ref.TrackRef)
	if ref.TrackRef != "" && d.MatchRef == "" {
		return nil, fmt.Errorf("专辑 %s 中未找到目标曲目 %s", ref.ID, ref.TrackRef)
	}
	return d, nil
}

// lookupLimit 是清单查询的返回条数上限。
const lookupLimit = 200

func (p *Provider) hash(params map[string]string) string {
	if p.hashParams == nil {
		return ""
	}
	return p.hashParams(params)
}

// MakeRef 按 {country}:{trackId} 构造匹配标识。
//
// 同一首歌在不同地区 storefront 的 trackId 不同，
// 不带 country 会让日本区与美国区的同一首歌互相覆盖。
func MakeRef(country string, trackID int64) string {
	return fmt.Sprintf("%s:%d", strings.ToLower(country), trackID)
}

// SplitRef 把 {country}:{id} 拆回两部分。
func SplitRef(ref string) (id, country string) {
	parts := strings.SplitN(ref, ":", 2)
	if len(parts) != 2 {
		return ref, ""
	}
	return parts[1], parts[0]
}

var _ provider.Provider = (*Provider)(nil)
