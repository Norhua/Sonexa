package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// DefaultServerURL 是 MusicBrainz 的官方服务器地址。
const DefaultServerURL = "https://musicbrainz.org"

// DefaultRatePerSec 是默认速率：平均每秒 1 次（按 IP），无 API key。
const DefaultRatePerSec = 1.0

// detailInc 是详情请求的 inc 参数。
//
// 1 req/s 下必须一次拉满，绝不拆成多次调用。这一个请求即可取得：
// 曲目/发行级署名、演唱者与器乐演奏者（recording-level-rels）、
// 作曲/作词（work-rels + work-level-rels，因为它们挂在 Work 而非 recording 上）、
// 主要/次要类型、年份、曲序碟序总数、风格、国家、厂牌、目录号、载体、时长、ISRC、注释。
//
// 其中 recordings+media 使响应已包含该 release 的完整曲目清单——
// 清单是免费的，丢掉它等于以后要为同一份数据再花一次 1 req/s。
const detailInc = "artist-credits+labels+recordings+release-groups+media+" +
	"artist-rels+recording-level-rels+work-rels+work-level-rels+" +
	"genres+isrcs+annotation"

// searchInc 是搜索请求的 inc 参数。
const searchInc = "artist-credits+releases+isrcs"

// Config 是 MusicBrainz 的配置。
type Config struct {
	// Contact 是 User-Agent 中的联系方式，必填；为空时不发出任何请求。
	Contact    string
	ServerURL  string
	RatePerSec float64
	// CacheTTL 是响应缓存的有效期；0 表示不过期。
	// MB 数据为 CC0，默认可长期留存，此值只反映用户在设置中的选择。
	CacheTTL time.Duration
}

// Provider 是 MusicBrainz 的 Provider 实现。
type Provider struct {
	client *provider.Client
	cfg    Config
	// hashParams 由外部注入，通常为 store.ParamsHash。
	hashParams func(map[string]string) string
}

// New 建立一个 MusicBrainz provider。
func New(client *provider.Client, cfg Config, hashParams func(map[string]string) string) *Provider {
	if cfg.ServerURL == "" {
		cfg.ServerURL = DefaultServerURL
	}
	if cfg.RatePerSec <= 0 {
		cfg.RatePerSec = DefaultRatePerSec
	}
	return &Provider{client: client, cfg: cfg, hashParams: hashParams}
}

// ID 实现 provider.Provider。
func (p *Provider) ID() provider.ProviderID { return provider.MusicBrainz }

// DisplayName 实现 provider.Provider。
func (p *Provider) DisplayName() string { return "MusicBrainz" }

// Capabilities 声明 MusicBrainz 能提供的字段与能力。
//
// 与 3.2 节对照表一致：MB 提供演唱者/作曲/作词/厂牌/目录号/ISRC/载体/国家，
// 有上游 relevance score，封面来自 Cover Art Archive 且支持多规格。
func (p *Provider) Capabilities() provider.Capabilities {
	return provider.CapSecondaryTypes |
		provider.CapComposer |
		provider.CapLyricist |
		provider.CapPerformerRoles |
		provider.CapLabel |
		provider.CapCatalogNumber |
		provider.CapMediumFormat |
		provider.CapCountry |
		provider.CapISRC |
		provider.CapCoverArt |
		provider.CapMultiSizeCover |
		provider.CapUpstreamScore |
		provider.CapPrimaryType
}

// RateLimit 实现 provider.Provider：默认 1 req/s，可在设置中调整。
func (p *Provider) RateLimit() (int, time.Duration) {
	// 以毫秒为分母表达小数速率：0.5 req/s → 1 次 / 2000ms
	per := time.Duration(float64(time.Second) / p.cfg.RatePerSec)
	return 1, per
}

// ErrContactMissing 表示联系方式未配置。
var ErrContactMissing = errors.New("请先在设置中填写 MusicBrainz 联系方式后再使用出网功能")

// IsConfigured 实现 provider.Provider。
//
// 联系方式未配置时不发出任何请求——这既是 MB 的硬性要求，也是产品的全局前置条件。
func (p *Provider) IsConfigured() error {
	if strings.TrimSpace(p.cfg.Contact) == "" {
		return ErrContactMissing
	}
	return nil
}

// SearchTracks 搜索并展开为 track 粒度的候选。
func (p *Provider) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}

	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}
	params := map[string]string{
		"fmt":   "json",
		"limit": strconv.Itoa(limit),
		"inc":   searchInc,
		"query": buildLuceneQuery(q),
	}

	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider:   provider.MusicBrainz,
		Endpoint:   "/ws/2/recording",
		BaseURL:    p.cfg.ServerURL,
		Params:     params,
		ParamsHash: p.hash(params),
		// MB 数据为 CC0，默认可长期留存；用户在设置中给了上限时按上限
		TTL: p.cfg.CacheTTL,
	})
	if err != nil {
		return nil, err
	}

	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("解析 MusicBrainz 搜索响应失败: %w", err)
	}
	return expandToTrackCandidates(resp.Recordings), nil
}

// buildLuceneQuery 构造 MB 的 Lucene 查询串。留空的条件不参与查询。
func buildLuceneQuery(q provider.Query) string {
	var parts []string
	if t := strings.TrimSpace(q.Title); t != "" {
		parts = append(parts, `recording:"`+escapeLucene(t)+`"`)
	}
	if a := strings.TrimSpace(q.Artist); a != "" {
		parts = append(parts, `artist:"`+escapeLucene(a)+`"`)
	}
	if al := strings.TrimSpace(q.Album); al != "" {
		parts = append(parts, `release:"`+escapeLucene(al)+`"`)
	}
	return strings.Join(parts, " AND ")
}

// escapeLucene 转义 Lucene 的特殊字符，避免用户输入被当作查询语法。
func escapeLucene(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`, `"`, `\"`, `+`, `\+`, `-`, `\-`, `!`, `\!`,
		`(`, `\(`, `)`, `\)`, `{`, `\{`, `}`, `\}`, `[`, `\[`, `]`, `\]`,
		`^`, `\^`, `~`, `\~`, `*`, `\*`, `?`, `\?`, `:`, `\:`, `/`, `\/`,
	)
	return replacer.Replace(s)
}

// expandToTrackCandidates 把录音展开为 (recording × release × medium × track) 的候选。
//
// 同一录音收录于 3 张发行时产出 3 行，各行的载体、国家、年份、曲序可能不同——
// 这正是列表所需的粒度，也是 release 优选阶梯有意义的前提。
func expandToTrackCandidates(recordings []recording) []provider.Candidate {
	var out []provider.Candidate
	for _, rec := range recordings {
		credit := joinArtistCredit(rec.ArtistCredit)
		for _, rel := range rec.Releases {
			for _, med := range rel.Media {
				for _, trk := range med.Tracks {
					out = append(out, buildCandidate(rec, rel, med, trk, credit))
				}
			}
			// 搜索响应有时不带 media[].tracks[]，此时仍产出一行发行级候选，
			// 否则这条发行会整个从结果中消失。
			if !hasTracks(rel.Media) {
				out = append(out, buildReleaseLevelCandidate(rec, rel, credit))
			}
		}
	}
	return out
}

func hasTracks(media []medium) bool {
	for _, m := range media {
		if len(m.Tracks) > 0 {
			return true
		}
	}
	return false
}

// buildCandidate 由展开后的四元组构造一条 track 粒度候选。
func buildCandidate(rec recording, rel release, med medium, trk track, credit string) provider.Candidate {
	c := provider.Candidate{
		Provider: provider.MusicBrainz,
		// match_ref 用 track MBID：它是 (recording × release × medium × 位置)
		// 的唯一交点。用 recording MBID 会把 CD 版与数字版判为同一条。
		MatchRef:     trk.ID,
		Title:        strPtr(firstNonEmpty(trk.Title, rec.Title)),
		AlbumTitle:   strPtr(rel.Title),
		RecordingRef: strPtr(rec.ID),
		ReleaseRef:   strPtr(rel.ID),
	}
	if credit != "" {
		c.ArtistName = &credit
	}
	if trackCredit := joinArtistCredit(trk.ArtistCredit); trackCredit != "" {
		c.ArtistName = &trackCredit
	}
	if rec.Score > 0 {
		c.UpstreamScore = &rec.Score
	}
	if length := firstNonNilInt(trk.Length, rec.Length); length != nil {
		c.DurationMS = length
	}
	if trk.Position > 0 {
		pos := trk.Position
		c.TrackNumber = &pos
	}
	if med.TrackCount > 0 {
		total := med.TrackCount
		c.TrackTotal = &total
	}
	if med.Position > 0 {
		disc := med.Position
		c.DiscNumber = &disc
	}
	if med.Format != "" {
		c.MediumFormat = strPtr(med.Format)
	}
	if rel.Country != "" {
		c.Country = strPtr(rel.Country)
	}
	if rel.Status != "" {
		c.ReleaseStatus = strPtr(rel.Status)
	}
	if year := parseYear(rel.Date); year != nil {
		c.Year = year
	}
	if rel.ReleaseGroup != nil {
		if rel.ReleaseGroup.ID != "" {
			c.ReleaseGroupRef = strPtr(rel.ReleaseGroup.ID)
		}
		if rel.ReleaseGroup.PrimaryType != "" {
			c.PrimaryType = strPtr(rel.ReleaseGroup.PrimaryType)
		}
		if rel.ReleaseGroup.FirstReleaseDate != "" {
			c.FirstReleaseDate = strPtr(rel.ReleaseGroup.FirstReleaseDate)
		}
	}
	if rel.CoverArtArchive != nil {
		c.HasCover = rel.CoverArtArchive.Front || rel.CoverArtArchive.Artwork
	}
	if len(rec.ISRCs) > 0 {
		c.ISRC = strPtr(rec.ISRCs[0])
	}
	return c
}

// buildReleaseLevelCandidate 在搜索响应缺少 tracks[] 时构造发行级候选。
func buildReleaseLevelCandidate(rec recording, rel release, credit string) provider.Candidate {
	c := provider.Candidate{
		Provider:     provider.MusicBrainz,
		MatchRef:     rec.ID + "@" + rel.ID, // 无 track MBID 时的合成标识
		Title:        strPtr(rec.Title),
		AlbumTitle:   strPtr(rel.Title),
		RecordingRef: strPtr(rec.ID),
		ReleaseRef:   strPtr(rel.ID),
		DurationMS:   rec.Length,
	}
	if credit != "" {
		c.ArtistName = &credit
	}
	if rec.Score > 0 {
		c.UpstreamScore = &rec.Score
	}
	if rel.Country != "" {
		c.Country = strPtr(rel.Country)
	}
	if rel.Status != "" {
		c.ReleaseStatus = strPtr(rel.Status)
	}
	if year := parseYear(rel.Date); year != nil {
		c.Year = year
	}
	if rel.ReleaseGroup != nil {
		if rel.ReleaseGroup.ID != "" {
			c.ReleaseGroupRef = strPtr(rel.ReleaseGroup.ID)
		}
		if rel.ReleaseGroup.PrimaryType != "" {
			c.PrimaryType = strPtr(rel.ReleaseGroup.PrimaryType)
		}
		if rel.ReleaseGroup.FirstReleaseDate != "" {
			c.FirstReleaseDate = strPtr(rel.ReleaseGroup.FirstReleaseDate)
		}
	}
	if len(rec.ISRCs) > 0 {
		c.ISRC = strPtr(rec.ISRCs[0])
	}
	return c
}

// joinArtistCredit 按 join phrase 拼回署名原串。
//
// feat. / & / × 这类连接词无法从关系表无损重建，
// 而写音频标签时 ARTIST 取的正是这个原串，因此必须原样保真。
func joinArtistCredit(credits []artistCredit) string {
	var b strings.Builder
	for _, c := range credits {
		name := c.Name
		if name == "" {
			name = c.Artist.Name
		}
		b.WriteString(name)
		b.WriteString(c.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

func (p *Provider) hash(params map[string]string) string {
	if p.hashParams == nil {
		return ""
	}
	return p.hashParams(params)
}

// --- 小工具 ---

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstNonNilInt(vals ...*int) *int {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// parseYear 从 ISO 日期串中取出年份。
func parseYear(date string) *int {
	if len(date) < 4 {
		return nil
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return nil
	}
	return &y
}

// 编译期断言：必须满足 Provider 接口。
var _ provider.Provider = (*Provider)(nil)
