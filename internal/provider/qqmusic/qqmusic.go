// Package qqmusic 接入 QQ 音乐作为标准元数据上游。
//
// 使用 musicu.fcg 的站点内部服务（匿名、无签名），并非公开稳定契约：
// 顶层与各请求块的 code 必须逐一检查，遇到鉴权或风控提示直接报告不可用，
// 不尝试规避限制。调研见 docs/qqmusic-availability.md。
package qqmusic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// 接口主机。测试可替换。
//
// provider.Client 以 BaseURL+Endpoint 拼出地址，Endpoint 必须是真实路径；
// 同一路径的不同请求靠请求体进入 ParamsHash 区分缓存。
var (
	MusicuBase = "https://u.y.qq.com"
	LyricBase  = "https://c.y.qq.com"
)

const (
	musicuPath = "/cgi-bin/musicu.fcg"
	lyricPath  = "/lyric/fcgi-bin/fcg_query_lyric_new.fcg"
)

// DefaultRatePerMin 是默认速率：调研时以不小于 1.2 秒的间隔串行请求，
// 这里再保守一些。官方额度未公开。
const DefaultRatePerMin = 30.0

// songPageSize 是专辑清单每页条数。
const songPageSize = 100

// maxSongPages 限制清单分页次数，防止异常的 totalNum 造成无界请求。
const maxSongPages = 10

// Config 是 QQ 音乐的配置。
type Config struct {
	RatePerMin float64
}

// Provider 是 QQ 音乐的 Provider 实现。
type Provider struct {
	client     *provider.Client
	cfg        Config
	hashParams func(map[string]string) string
}

// New 建立一个 QQ 音乐 provider。
func New(client *provider.Client, cfg Config, hashParams func(map[string]string) string) *Provider {
	if cfg.RatePerMin <= 0 {
		cfg.RatePerMin = DefaultRatePerMin
	}
	return &Provider{client: client, cfg: cfg, hashParams: hashParams}
}

// ID 实现 provider.Provider。
func (p *Provider) ID() provider.ProviderID { return provider.QQMusic }

// DisplayName 实现 provider.Provider。
func (p *Provider) DisplayName() string { return "QQ 音乐" }

// Capabilities 实现 provider.Provider。
//
// 歌曲详情给出作词、作曲、演唱、编曲、制作与乐器演奏等角色，专辑详情给出
// 厂牌与专辑类型。不声明目录号（recordNum 与实体目录号的对应未核实）、
// ISRC、国家、载体与次要类型。
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.CapComposer | provider.CapLyricist | provider.CapPerformerRoles |
		provider.CapLabel | provider.CapPrimaryType | provider.CapCoverArt |
		provider.CapMultiSizeCover | provider.CapLyrics
}

// RateLimit 实现 provider.Provider。搜索、详情与歌词共用同一个令牌桶。
func (p *Provider) RateLimit() (int, time.Duration) {
	return 1, time.Duration(float64(time.Minute) / p.cfg.RatePerMin)
}

// IsConfigured 实现 provider.Provider：无需密钥。全局出网条件由调用方施加。
func (p *Provider) IsConfigured() error { return nil }

// SearchTracks 实现 provider.Provider。
func (p *Provider) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	term := strings.TrimSpace(strings.Join([]string{q.Title, q.Artist, q.Album}, " "))
	if q.TitleOnly {
		term = strings.TrimSpace(q.Title)
	}
	if term == "" {
		return nil, nil
	}
	limit := q.Limit
	if limit <= 0 || limit > 50 {
		limit = 25
	}
	body, err := p.musicu(ctx, q.SkipCache, map[string]any{
		"req": map[string]any{
			"module": "music.search.SearchCgiService",
			"method": "DoSearchForQQMusicDesktop",
			"param":  map[string]any{"query": term, "search_type": 0, "num_per_page": limit, "page_num": 1},
		},
	})
	if err != nil {
		return nil, err
	}
	return ParseSearch(body)
}

// FetchDetail 实现 provider.Provider。ref.ID 为专辑 MID，ref.TrackRef 为歌曲 MID。
//
// 专辑详情、首页清单与歌曲详情合并为一次请求；歌曲详情未带歌词时
// 再请求独立歌词接口。
func (p *Provider) FetchDetail(ctx context.Context, ref provider.Ref) (*provider.Detail, error) {
	albumMID := strings.TrimSpace(ref.ID)
	if !ValidMID(albumMID) {
		return nil, fmt.Errorf("缺少有效的 QQ 音乐专辑标识")
	}
	if ref.TrackRef != "" && !ValidMID(ref.TrackRef) {
		return nil, fmt.Errorf("无效的 QQ 音乐歌曲标识 %q", ref.TrackRef)
	}
	reqs := map[string]any{
		"album": map[string]any{
			"module": "music.musichallAlbum.AlbumInfoServer", "method": "GetAlbumDetail",
			"param": map[string]any{"albumMid": albumMID},
		},
		"songs": songsBlock(albumMID, 0),
	}
	if ref.TrackRef != "" {
		reqs["song"] = map[string]any{
			"module": "music.pf_song_detail_svr", "method": "get_song_detail",
			"param": map[string]any{"song_mid": ref.TrackRef},
		}
	}
	body, err := p.musicu(ctx, ref.SkipCache, reqs)
	if err != nil {
		return nil, err
	}
	parts, err := parseDetailParts(body, ref.TrackRef != "")
	if err != nil {
		return nil, err
	}
	// 按已取数量判断是否取完；空页即停，避免 totalNum 异常时无界请求。
	for n := 1; len(parts.songs) < parts.total && n < maxSongPages; n++ {
		body, err := p.musicu(ctx, ref.SkipCache, map[string]any{"songs": songsBlock(albumMID, len(parts.songs))})
		if err != nil {
			return nil, err
		}
		more, err := decodeBlocks(body, "songs")
		if err != nil {
			return nil, err
		}
		page, err := parseSongPage(more["songs"])
		if err != nil {
			return nil, err
		}
		if len(page.SongList) == 0 {
			break
		}
		for _, s := range page.SongList {
			parts.songs = append(parts.songs, s.SongInfo)
		}
	}
	d, err := buildDetail(albumMID, ref.TrackRef, *parts)
	if err != nil {
		return nil, err
	}
	if ref.TrackRef != "" && d.Lyrics == nil {
		// 歌词缺失不影响其他字段写入。
		if lyric, err := p.fetchLyric(ctx, ref.TrackRef, ref.SkipCache); err == nil {
			d.Lyrics = lyric
		}
	}
	return d, nil
}

// SearchCovers 实现 provider.Provider：按专辑 MID 去重，构造各尺寸档位。
func (p *Provider) SearchCovers(ctx context.Context, q provider.Query) ([]provider.CoverGroup, error) {
	candidates, err := p.SearchTracks(ctx, q)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []provider.CoverGroup
	for _, c := range candidates {
		if c.ReleaseRef == nil || seen[*c.ReleaseRef] {
			continue
		}
		seen[*c.ReleaseRef] = true
		group := provider.CoverGroup{
			Provider: provider.QQMusic, SourceRef: *c.ReleaseRef,
			Title: c.AlbumTitle, Artist: c.ArtistName, Constructed: true,
		}
		for i := len(CoverSizes) - 1; i >= 0; i-- {
			group.Images = append(group.Images, provider.CoverImage{Type: "Front", URL: CoverURL(*c.ReleaseRef, CoverSizes[i])})
		}
		out = append(out, group)
		if len(out) >= 10 {
			break
		}
	}
	return out, nil
}

func (p *Provider) fetchLyric(ctx context.Context, songMID string, skipCache bool) (*string, error) {
	params := map[string]string{"songmid": songMID, "g_tk": "5381", "format": "json", "nobase64": "1"}
	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider: provider.QQMusic, Endpoint: lyricPath, BaseURL: LyricBase,
		Params: params, ParamsHash: p.hash(params), SkipCache: skipCache,
		Headers: map[string]string{"Referer": "https://y.qq.com/"},
	})
	if err != nil {
		return nil, err
	}
	return ParseLyric(body)
}

// musicu 发起一次 musicu.fcg POST。请求体参与缓存键。
func (p *Provider) musicu(ctx context.Context, skipCache bool, reqs map[string]any) ([]byte, error) {
	payload := map[string]any{"comm": map[string]any{"ct": "19", "cv": "1859", "uin": "0"}}
	for k, v := range reqs {
		payload[k] = v
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return p.client.Get(ctx, provider.GetRequest{
		Provider: provider.QQMusic, Endpoint: musicuPath, BaseURL: MusicuBase,
		Body: body, ParamsHash: p.hash(map[string]string{"body": string(body)}), SkipCache: skipCache,
		Headers: map[string]string{"Referer": "https://y.qq.com/"},
	})
}

// ParseDetailJSON 把一次合并详情响应解析为 Detail（不做清单分页）。
// 导出以便离线重放测试。
func ParseDetailJSON(body []byte, albumMID, trackRef string) (*provider.Detail, error) {
	parts, err := parseDetailParts(body, trackRef != "")
	if err != nil {
		return nil, err
	}
	return buildDetail(albumMID, trackRef, *parts)
}

// parseDetailParts 解析合并请求的 album / songs / song 三个块。
func parseDetailParts(body []byte, withSong bool) (*detailParts, error) {
	names := []string{"album", "songs"}
	if withSong {
		names = append(names, "song")
	}
	blocks, err := decodeBlocks(body, names...)
	if err != nil {
		return nil, err
	}
	var parts detailParts
	if err := json.Unmarshal(blocks["album"], &parts.album); err != nil {
		return nil, fmt.Errorf("解析 QQ 音乐专辑详情失败: %w", err)
	}
	page, err := parseSongPage(blocks["songs"])
	if err != nil {
		return nil, err
	}
	parts.total = page.TotalNum
	for _, s := range page.SongList {
		parts.songs = append(parts.songs, s.SongInfo)
	}
	if withSong {
		var detail songDetailData
		if err := json.Unmarshal(blocks["song"], &detail); err != nil {
			return nil, fmt.Errorf("解析 QQ 音乐歌曲详情失败: %w", err)
		}
		parts.detail = &detail
	}
	return &parts, nil
}

func songsBlock(albumMID string, begin int) map[string]any {
	return map[string]any{
		"module": "music.musichallAlbum.AlbumSongList", "method": "GetAlbumSongList",
		"param": map[string]any{"albumMid": albumMID, "begin": begin, "num": songPageSize},
	}
}

func parseSongPage(raw json.RawMessage) (*songListData, error) {
	var page songListData
	if err := json.Unmarshal(raw, &page); err != nil {
		return nil, fmt.Errorf("解析 QQ 音乐专辑清单失败: %w", err)
	}
	if page.SongList == nil {
		return nil, fmt.Errorf("QQ 音乐专辑清单响应缺少歌曲列表")
	}
	return &page, nil
}

func (p *Provider) hash(params map[string]string) string {
	if p.hashParams == nil {
		return ""
	}
	return p.hashParams(params)
}

var _ provider.Provider = (*Provider)(nil)
