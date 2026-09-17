package itunes

import (
	"context"
	"strconv"
	"strings"
	"time"

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
	if r.ArtworkURL100 != "" {
		thumb := ArtworkURL(r.ArtworkURL100, 250)
		c.ThumbURL = &thumb
		c.HasCover = true
	}
	// 以下字段 iTunes 不提供，保持 nil：
	//   PrimaryType / MediumFormat / Country / UpstreamScore /
	//   ReleaseStatus / FirstReleaseDate / ReleaseGroupRef / RecordingRef / ISRC
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
	if col.WrapperType == "collection" || col.CollectionID != 0 {
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
		if r.TrackID == 0 {
			continue
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
	return d
}

// SearchCovers 实现 provider.Provider。
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

// ArtworkSizes 是 iTunes 提供的四档尺寸。
//
// 只提供有限的若干档：大图靠构造地址属未文档化行为，可能随时失效，
// 档位越多风险面越大。
var ArtworkSizes = []int{3000, 1200, 600, 100}

// ArtworkLadder 按从高到低构造四档尺寸。
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

// ArtworkURL 把 artworkUrl100 中的 100x100bb 替换为目标尺寸。
func ArtworkURL(artworkURL string, size int) string {
	dim := strconv.Itoa(size)
	for _, known := range []string{"100x100", "60x60", "30x30"} {
		if strings.Contains(artworkURL, known) {
			return strings.Replace(artworkURL, known, dim+"x"+dim, 1)
		}
	}
	return artworkURL
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
