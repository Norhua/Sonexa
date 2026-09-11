package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"

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
// 因此 MB 侧的规格是「读出来的」而非构造的，不会出现点开是 404 的情形。
func (p *Provider) SearchCovers(ctx context.Context, q provider.Query) ([]provider.CoverGroup, error) {
	candidates, err := p.SearchTracks(ctx, q)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var out []provider.CoverGroup
	for _, c := range candidates {
		// 候选是 track 粒度，同一发行会出现多次，按发行去重
		releaseRef := releaseRefOf(c)
		if releaseRef == "" || seen[releaseRef] {
			continue
		}
		seen[releaseRef] = true
		if !c.HasCover {
			continue
		}
		group := provider.CoverGroup{
			Provider:  provider.MusicBrainz,
			SourceRef: releaseRef,
			Title:     c.AlbumTitle,
			Artist:    c.ArtistName,
			// MB 的规格来自索引的真实枚举，不是构造出来的
			Constructed: false,
			Images:      CAAFrontLadder(releaseRef, derefStr(c.ReleaseGroupRef)),
		}
		out = append(out, group)
		if len(out) >= 10 {
			break
		}
	}
	return out, nil
}

// FetchCoverIndex 读取某发行的 CAA 索引，枚举可用图片及其类型。
//
// 详情抽屉据此可展示 Front 之外的图（Back / Booklet），而列表缩略图只用 Front。
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
		out = append(out, provider.CoverImage{Type: imgType, URL: img.Image})

		// 缩略图各档也作为可选规格列出
		for _, size := range []string{"1200", "500", "250"} {
			if url, ok := img.Thumbnails[size]; ok && url != "" {
				out = append(out, provider.CoverImage{Type: imgType, URL: url})
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

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
