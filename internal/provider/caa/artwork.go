package caa

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"net/url"
	"regexp"
	"strings"
)

// ArtworkStrategy supplies the CAA protocol; metadata association is set by its caller.
func ArtworkStrategy() provider.ArtworkStrategy {
	return provider.ArtworkStrategy{Source: "caa", ObjectType: "release",
		CatalogKeys: func(ref, group string) []domain.CoverCatalogKey {
			if !uuidRef.MatchString(ref) {
				return nil
			}
			keys := []domain.CoverCatalogKey{{Source: "caa", ObjectType: "release", SourceRef: ref}}
			if uuidRef.MatchString(group) {
				keys = append(keys, domain.CoverCatalogKey{Source: "caa", ObjectType: "release-group", SourceRef: group})
			}
			return keys
		},
		Validate: func(kind, ref string) error {
			if !uuidRef.MatchString(ref) || (kind != "release" && kind != "release-group") {
				return fmt.Errorf("无效 MusicBrainz 发行身份")
			}
			return nil
		},
		Load: func(ctx context.Context, key domain.CoverCatalogKey, io provider.ArtworkIO) (*domain.CoverCatalogInput, error) {
			body, err := io.Fetch(ctx, CoverArtArchiveBaseURL+"/"+key.ObjectType+"/"+key.SourceRef)
			if err != nil {
				return nil, err
			}
			index, err := ParseCoverCatalog(body)
			if err != nil {
				return nil, err
			}
			out := &domain.CoverCatalogInput{Key: key, ActualReleaseURL: index.Release}
			for _, image := range index.Images {
				entry := domain.CoverImage{SourceImageID: image.ID, Types: image.Types, PrimaryType: image.Type, Front: image.Front, Back: image.Back, OriginalURL: image.Original}
				for _, v := range image.Variants {
					entry.Variants = append(entry.Variants, domain.CoverVariant{Spec: v.Spec, URL: v.URL})
				}
				out.Images = append(out.Images, entry)
			}
			return out, nil
		},
		Specs: func(detail bool) []string {
			if detail {
				return []string{"1200", "original"}
			}
			return []string{"250", "500", "1200", "original"}
		},
		ImageURLs: ImageURLs,
		LegacyLadder: func(ref, art, group string) []string {
			urls := []string{CAAThumbnailURL(ref)}
			for _, v := range CAAFrontLadder(ref, group) {
				urls = append(urls, v.URL)
			}
			return append(urls, CAAIndexFallbackURLs(ref, group)...)
		},
	}
}

var uuidRef = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var caaImagePath = regexp.MustCompile(`^/release/([0-9a-f-]{36})/([0-9]+)(?:-(250|500|1200))?\.([a-z]+)$`)

// ImageURLs 把 CAA 图片地址改写为它最终 307 到的 archive.org 地址，省去一次
// coverartarchive.org 往返（每跳都需独立 TLS 握手，慢网络下单跳可达数秒）。
// 直连失败时退回规范地址，archive.org 布局变化只会让它变慢而不会取不到图。
func ImageURLs(raw string) []string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Hostname(), "coverartarchive.org") {
		return []string{raw}
	}
	m := caaImagePath.FindStringSubmatch(u.Path)
	if m == nil {
		return []string{raw}
	}
	name := "mbid-" + m[1] + "-" + m[2]
	if m[3] != "" {
		name += "_thumb" + m[3]
	}
	direct := "https://archive.org/download/mbid-" + m[1] + "/" + name + "." + m[4]
	return []string{direct, raw}
}
