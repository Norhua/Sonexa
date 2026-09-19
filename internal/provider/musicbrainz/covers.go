package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/norhua/sonexa/internal/provider"
)

// CoverArtArchiveBaseURL 是封面档案服务的地址。
//
// 它是独立服务，其请求不占用 MusicBrainz 的 1 req/s 配额，
// 改由封面工作池的并发上限约束。
const CoverArtArchiveBaseURL = "https://coverartarchive.org"

// caaIndex 是 CAA 的 JSON 索引响应，真实枚举了可用图片及其类型。
type caaIndex struct {
	Images  []caaImage `json:"images"`
	Release string     `json:"release"`
}

type caaImage struct {
	ID         json.Number       `json:"id"`
	Image      string            `json:"image"`
	Front      bool              `json:"front"`
	Back       bool              `json:"back"`
	Types      []string          `json:"types"` // Front | Back | Booklet | Medium | Tray
	Thumbnails map[string]string `json:"thumbnails"`
}

// SearchCovers 按条件搜索封面。
//
// 先搜出候选发行，再读各自的 CAA 索引——索引真实枚举可用图片，
// 因此 MB 侧的规格是「读出来的」而非构造的。索引与图片的可用性仍可能随时间变化。
func (p *Provider) SearchCovers(ctx context.Context, q provider.Query) ([]provider.CoverGroup, error) {
	candidates, err := p.SearchTracks(ctx, q)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []provider.CoverGroup
	var failure error
	for _, c := range candidates {
		// 候选是 track 粒度，同一发行会出现多次，按发行去重
		releaseRef := releaseRefOf(c)
		if releaseRef == "" || seen[releaseRef] {
			continue
		}
		// 最多检查十张发行；无图时也有界，避免空结果触发无界串行请求。
		if len(seen) >= 10 {
			break
		}
		seen[releaseRef] = true
		// 搜索响应的 artwork 标记可能缺失；以 CAA 索引为准。
		images, err := p.FetchCoverIndex(ctx, releaseRef)
		if err != nil {
			var httpErr *provider.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusNotFound {
				failure = err
			}
			continue
		}
		if len(images) == 0 {
			continue
		}
		group := provider.CoverGroup{
			Provider:  provider.MusicBrainz,
			SourceRef: releaseRef,
			Title:     c.AlbumTitle,
			Artist:    c.ArtistName,
			// MB 的规格来自索引的真实枚举，不是构造出来的
			Constructed: false,
			Images:      images,
		}
		out = append(out, group)
	}
	if len(out) == 0 && failure != nil {
		return nil, failure
	}
	return out, nil
}

// FetchCoverIndex 读取某发行的 CAA 索引，枚举可用图片及其类型。
//
// 详情抽屉据此可展示 Front 之外的图（Back / Booklet / Medium）。
// 自动缩略图先尝试 Front，耗尽后才回退到索引中实际存在的图片。
func (p *Provider) FetchCoverIndex(ctx context.Context, releaseMBID string) ([]provider.CoverImage, error) {
	params := map[string]string{}
	body, err := p.client.Get(ctx, provider.GetRequest{
		// 用一个独立的 provider 键，使 CAA 不占用 MusicBrainz 的令牌桶
		Provider:   coverArtProviderKey,
		Endpoint:   "/release/" + releaseMBID,
		BaseURL:    CoverArtArchiveBaseURL,
		Params:     params,
		ParamsHash: p.hash(map[string]string{"release": releaseMBID}),
		TTL:        0,
	})
	if err != nil {
		return nil, err
	}

	var index caaIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("解析封面索引失败: %w", err)
	}
	// 未标记 Front 的图片也可用于无正面图时的回退。
	// nil Images（字段缺失或 null）是无效索引；[] 才是明确的空索引。
	if index.Images == nil {
		return nil, fmt.Errorf("封面索引缺少 images 数组")
	}
	return ParseCoverIndex(&index), nil
}

// coverArtProviderKey 是 CAA 在令牌桶中的独立键。
//
// CAA 的 JSON 索引虽属元数据，但由 CAA 服务提供，不占 MB 的桶，
// 与图片一起受封面池的并发上限约束。
const coverArtProviderKey provider.ProviderID = "coverartarchive"

// CoverArtProviderKey 导出该键，供封面工作池配置其并发。
func CoverArtProviderKey() provider.ProviderID { return coverArtProviderKey }

// ParseCoverIndex 把 CAA 索引解析为图片列表。导出以便离线重放测试。
func ParseCoverIndex(index *caaIndex) []provider.CoverImage {
	var out []provider.CoverImage
	for _, img := range index.Images {
		imgType := "Other"
		if len(img.Types) > 0 {
			imgType = img.Types[0]
		}
		if img.Front {
			imgType = "Front"
		}
		if img.Image != "" {
			out = append(out, provider.CoverImage{Type: imgType, URL: secureCAAURL(img.Image)})
		}

		// 缩略图各档也作为可选规格列出
		for _, size := range []string{"1200", "500", "250"} {
			if url, ok := img.Thumbnails[size]; ok && url != "" {
				out = append(out, provider.CoverImage{Type: imgType, URL: secureCAAURL(url)})
			}
		}
	}
	return out
}

// CAAFrontLadder 返回封面的尺寸降级链。
//
//	/release/{mbid}/front-1200
//	  → /release/{mbid}/front-500
//	  → /release-group/{rgid}/front-1200
//	  → 占位图（由调用方处理）
//
// CAA 返回 404 是常态——大量发行没有图，必须优雅降级。
func CAAFrontLadder(releaseMBID, releaseGroupMBID string) []provider.CoverImage {
	var out []provider.CoverImage
	if releaseMBID != "" {
		out = append(out,
			provider.CoverImage{Type: "Front", URL: CAAFrontURL(releaseMBID, 1200)},
			provider.CoverImage{Type: "Front", URL: CAAFrontURL(releaseMBID, 500)},
		)
	}
	if releaseGroupMBID != "" {
		out = append(out, provider.CoverImage{
			Type: "Front",
			URL:  fmt.Sprintf("%s/release-group/%s/front-1200", CoverArtArchiveBaseURL, releaseGroupMBID),
		})
	}
	return out
}

// CAAFrontURL 构造发行级正面封面的地址。列表缩略图直接用 front-250。
func CAAFrontURL(releaseMBID string, size int) string {
	return fmt.Sprintf("%s/release/%s/front-%d", CoverArtArchiveBaseURL, releaseMBID, size)
}

// CAAThumbnailURL 返回列表缩略图地址（250px）。
func CAAThumbnailURL(releaseMBID string) string { return CAAFrontURL(releaseMBID, 250) }

// releaseRefOf 从候选中取出发行标识。
func releaseRefOf(c provider.Candidate) string {
	if c.ReleaseRef != nil {
		return *c.ReleaseRef
	}
	return ""
}

// CAAIndexFallbackURLs 只在正面图降级链耗尽后读取实际索引。
func CAAIndexFallbackURLs(releaseMBID, releaseGroupMBID string) []string {
	var urls []string
	if releaseMBID != "" {
		urls = append(urls, CoverArtArchiveBaseURL+"/release/"+releaseMBID)
	}
	if releaseGroupMBID != "" {
		urls = append(urls, CoverArtArchiveBaseURL+"/release-group/"+releaseGroupMBID)
	}
	return urls
}

// ParseCAAThumbnailIndex 由封面池调用；不把索引 JSON 当作图片缓存。
// 保留 Front 优先；无 Front 时允许 Medium / Back 等实际存在的图片。
func ParseCAAThumbnailIndex(rawURL string, body []byte) ([]string, bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host != "coverartarchive.org" {
		return nil, false, nil
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || (parts[0] != "release" && parts[0] != "release-group") {
		return nil, false, nil
	}
	var index caaIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, true, fmt.Errorf("解析封面索引失败: %w", err)
	}
	if index.Images == nil {
		return nil, true, fmt.Errorf("封面索引缺少 images 数组")
	}
	var urls []string
	seen := map[string]bool{}
	for _, front := range []bool{true, false} {
		for _, img := range index.Images {
			if img.Front != front {
				continue
			}
			for _, raw := range []string{img.Thumbnails["250"], img.Thumbnails["500"], img.Thumbnails["1200"], img.Image} {
				if raw == "" {
					continue
				}
				imageURL := secureCAAURL(raw)
				if !seen[imageURL] {
					seen[imageURL] = true
					urls = append(urls, imageURL)
				}
			}
		}
	}
	return urls, true, nil
}

// CAA 的索引仍可能给出 http 地址；这两个官方图片主机均支持 HTTPS。
func secureCAAURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Scheme == "http" && (u.Host == "coverartarchive.org" || u.Host == "archive.org" || strings.HasSuffix(u.Host, ".archive.org")) {
		u.Scheme = "https"
		return u.String()
	}
	return raw
}
