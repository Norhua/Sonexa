package itunes

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

// ParseSearchResults 把 search 响应解析为候选。导出以便离线重放测试。
func ParseSearchResults(resp *searchResponse, country string) []provider.Candidate {
	out := make([]provider.Candidate, 0, len(resp.Results))
	for _, r := range resp.Results {
		// 只取曲目条目
		if r.WrapperType != "" && r.WrapperType != "track" {
			continue
		}
		if r.TrackID == 0 {
			continue
		}
		out = append(out, candidateFrom(r, country))
	}
	return out
}

// candidateFrom 由一条结果构造候选。
//
// iTunes 不支持的字段一律留空（nil），不用空串或零值伪装——
// 否则「署名精确匹配」在两边都缺失时会误判为匹配成功。
func candidateFrom(r result, country string) provider.Candidate {
	c := provider.Candidate{
		Provider: provider.ITunes,
		// match_ref 必须带 country：同一首歌在不同地区 trackId 不同
		MatchRef:   MakeRef(country, r.TrackID),
		Title:      strPtr(r.TrackName),
		ArtistName: strPtr(r.ArtistName),
		AlbumTitle: strPtr(r.CollectionName),
	}
	if r.CollectionID != 0 {
		ref := MakeRef(country, r.CollectionID)
		c.ReleaseRef = &ref
	}
	if r.TrackTimeMillis > 0 {
		d := r.TrackTimeMillis
		c.DurationMS = &d
	}
	if r.TrackNumber > 0 {
		n := r.TrackNumber
		c.TrackNumber = &n
	}
	if r.TrackCount > 0 {
		n := r.TrackCount
		c.TrackTotal = &n
	}
	if r.DiscNumber > 0 {
		n := r.DiscNumber
		c.DiscNumber = &n
	}
	if y := parseYear(r.ReleaseDate); y != nil {
		c.Year = y
	}
	artwork := r.ArtworkURL100
	if artwork == "" {
		artwork = r.ArtworkURL60
	}
	if artwork != "" {
		thumb := ArtworkURL(artwork, 250)
		c.ThumbURL = &thumb
		c.HasCover = true
	}
	// 以下字段 iTunes 不提供，保持 nil：
	//   PrimaryType / MediumFormat / Country / UpstreamScore /
	//   ReleaseStatus / FirstReleaseDate / ReleaseGroupRef / RecordingRef / ISRC
	c.ArtworkCatalog = (&Provider{}).ArtworkStrategy().Candidate(c)
	return c
}

// ParseLookup 把 lookup 响应解析为 Detail（含完整曲目清单）。
//
// results[0] 是 collection 自身，被解析为发行信息；
// 其后各条才是曲目，被解析为清单位置。混淆两者会让清单多出一条假曲目。
func ParseLookup(resp *lookupResponse, country, trackRef string) *provider.Detail {
	d := &provider.Detail{
		Provider:  provider.ITunes,
		FetchedAt: time.Now().UTC(),
	}
	if len(resp.Results) == 0 {
		return d
	}

	// 首条：collection 自身
	col := resp.Results[0]
	if col.WrapperType == "collection" {
		d.ArtworkURL = strPtr(col.ArtworkURL100)
		if d.ArtworkURL == nil {
			d.ArtworkURL = strPtr(col.ArtworkURL60)
		}
		d.AlbumTitle = strPtr(col.CollectionName)
		d.AlbumCredit = strPtr(col.ArtistName)
		if col.CollectionID != 0 {
			ref := MakeRef(country, col.CollectionID)
			d.ReleaseRef = &ref
		}
		if col.ReleaseDate != "" {
			d.ReleaseDate = strPtr(col.ReleaseDate)
			d.Year = parseYear(col.ReleaseDate)
		}
		if col.PrimaryGenreName != "" {
			// iTunes 的风格被 country 本地化，作为 iTunes 来源写入并记录来源，
			// 不当作权威值覆盖其他上游的风格。
			d.Genre = strPtr(col.PrimaryGenreName)
		}
		if col.TrackCount > 0 {
			n := col.TrackCount
			d.TrackTotal = &n
			d.ReleaseTrackTotal = &n
		}
		if col.DiscCount > 0 {
			n := col.DiscCount
			d.DiscTotal = &n
		}
	}

	// 其后各条：曲目，构成清单
	for _, r := range resp.Results[1:] {
		if r.WrapperType != "" && r.WrapperType != "track" {
			continue
		}
		if r.TrackID == 0 || col.WrapperType != "collection" || (r.CollectionID != 0 && r.CollectionID != col.CollectionID) {
			continue
		}
		// 正确专辑的曲目也可能提供 collection 未带的 artwork。
		if d.ArtworkURL == nil && col.CollectionID != 0 && r.CollectionID == col.CollectionID {
			d.ArtworkURL = strPtr(r.ArtworkURL100)
			if d.ArtworkURL == nil {
				d.ArtworkURL = strPtr(r.ArtworkURL60)
			}
		}
		disc := r.DiscNumber
		if disc == 0 {
			disc = 1
		}
		ref := MakeRef(country, r.TrackID)
		entry := provider.TracklistEntry{
			DiscNumber:  disc,
			TrackNumber: r.TrackNumber,
			TrackRef:    &ref,
			Title:       r.TrackName,
			TrackCredit: strPtr(r.ArtistName),
		}
		if r.TrackTimeMillis > 0 {
			ms := r.TrackTimeMillis
			entry.DurationMS = &ms
		}
		d.Tracklist = append(d.Tracklist, entry)

		// 目标曲目位置的字段
		if trackRef != "" && ref == trackRef {
			d.MatchRef = ref
			d.Title = strPtr(r.TrackName)
			d.TrackCredit = strPtr(r.ArtistName)
			if r.TrackNumber > 0 {
				n := r.TrackNumber
				d.TrackNumber = &n
			}
			d.DiscNumber = &disc
			if r.TrackTimeMillis > 0 {
				ms := r.TrackTimeMillis
				d.DurationMS = &ms
			}
		}
	}
	d.TracklistCompleteness = domain.Partial
	if d.ReleaseTrackTotal != nil && len(d.Tracklist) == *d.ReleaseTrackTotal {
		d.TracklistCompleteness = domain.Complete
		counts := map[int]int{}
		maxDisc := 0
		for _, entry := range d.Tracklist {
			counts[entry.DiscNumber]++
			if entry.DiscNumber > maxDisc {
				maxDisc = entry.DiscNumber
			}
		}
		for disc := 1; disc <= maxDisc; disc++ {
			m := domain.Medium{Number: disc}
			if counts[disc] > 0 {
				m.TrackTotal = domain.Known(counts[disc])
			}
			d.Media = append(d.Media, m)
		}
		if maxDisc > 0 && d.DiscTotal == nil {
			d.DiscTotal = &maxDisc
		}
		if d.DiscNumber != nil && counts[*d.DiscNumber] > 0 {
			n := counts[*d.DiscNumber]
			d.MediumTrackTotal = &n
		}
	}

	return d
}

// SearchCovers 是旧封面搜索入口。
//
// iTunes 只返回一个 artworkUrl100，大图尺寸靠构造地址得到（未文档化行为），
// 因此 Constructed 为真——选中某档时需先验证其可用性，不可用时自动退档。
func (p *Provider) SearchCovers(ctx context.Context, q provider.Query) ([]provider.CoverGroup, error) {
	candidates, err := p.SearchTracks(ctx, q)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []provider.CoverGroup
	for _, c := range candidates {
		if c.ThumbURL == nil || c.ReleaseRef == nil || seen[*c.ReleaseRef] {
			continue
		}
		seen[*c.ReleaseRef] = true
		out = append(out, provider.CoverGroup{
			Provider:    provider.ITunes,
			SourceRef:   *c.ReleaseRef,
			Title:       c.AlbumTitle,
			Artist:      c.ArtistName,
			Constructed: true,
			Images:      ArtworkLadder(*c.ThumbURL),
		})
		if len(out) >= 10 {
			break
		}
	}
	return out, nil
}

// ArtworkSizes 是供封面目录使用的有限构造档位，250 用于自动缩略图。
// 构造地址是未文档化行为，实际可用性与尺寸必须经下载解码确认。
var ArtworkSizes = []int{3000, 1200, 600, 250, 100}

// ArtworkLadder 按从高到低构造有限尺寸。
func ArtworkLadder(artworkURL string) []provider.CoverImage {
	out := make([]provider.CoverImage, 0, len(ArtworkSizes))
	for _, size := range ArtworkSizes {
		out = append(out, provider.CoverImage{
			Type: "Front",
			URL:  ArtworkURL(artworkURL, size),
		})
	}
	return out
}

var artworkDimensions = regexp.MustCompile(`^\d+x\d+([a-zA-Z]*\.[a-zA-Z0-9]+)$`)

// ArtworkURL 只改最后一个路径段的尺寸；也能从 250/1200 等已构造地址再次转换。
// 不修改图片身份路径里的数字、裁剪后缀或查询参数，未知格式保持原样。
func ArtworkURL(artworkURL string, size int) string {
	u, err := url.Parse(artworkURL)
	if err != nil || size <= 0 {
		return artworkURL
	}
	path := u.EscapedPath()
	start := strings.LastIndex(path, "/") + 1
	parts := artworkDimensions.FindStringSubmatch(path[start:])
	if parts == nil {
		return artworkURL
	}
	dim := strconv.Itoa(size)
	u.RawPath = path[:start] + dim + "x" + dim + parts[1]
	u.Path, err = url.PathUnescape(u.RawPath)
	if err != nil {
		return artworkURL
	}
	return u.String()
}

// VerifyArtwork 验证构造出的地址确实可用，不可用时应退到下一档。
//
// 图片请求不占用 iTunes 的令牌桶——它不是被限流的那个端点。
func (p *Provider) VerifyArtwork(ctx context.Context, url string) error {
	return p.client.Head(ctx, url)
}

// ResolveArtwork 依次验证各档，返回第一个可用的地址。
// 全部不可得时返回空串，由调用方退到占位图。
func (p *Provider) ResolveArtwork(ctx context.Context, artworkURL string) string {
	for _, img := range ArtworkLadder(artworkURL) {
		if err := p.VerifyArtwork(ctx, img.URL); err == nil {
			return img.URL
		}
	}
	return ""
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
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
