package itunes

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"strconv"
)

func CoverImages(artwork, title, artist string) []domain.CoverImage {
	if artwork == "" {
		return nil
	}
	image := domain.CoverImage{SourceImageID: ArtworkURL(artwork, 250), OriginalURL: artwork,
		PrimaryType: "Front", Types: []string{"Front"}, Front: true, Title: title, Artist: artist}
	for _, size := range ArtworkSizes {
		image.Variants = append(image.Variants, domain.CoverVariant{Spec: strconv.Itoa(size), URL: ArtworkURL(artwork, size), Constructed: true})
	}
	return []domain.CoverImage{image}
}

func (p *Provider) ArtworkStrategy() provider.ArtworkStrategy {
	return provider.ArtworkStrategy{Provider: provider.ITunes, Source: "itunes", ObjectType: "album",
		Validate: func(kind, ref string) error {
			if kind != "album" {
				return fmt.Errorf("无效专辑类型")
			}
			_, err := (refCodec{}).ParseRelease(ref, "")
			return err
		},
		Load: func(ctx context.Context, key domain.CoverCatalogKey, io provider.ArtworkIO) (*domain.CoverCatalogInput, error) {
			d, err := io.Detail(ctx, provider.Ref{Provider: provider.ITunes, ID: key.SourceRef})
			if err != nil {
				return nil, err
			}
			if d == nil || d.Provider != provider.ITunes || d.ReleaseRef == nil || *d.ReleaseRef != key.SourceRef {
				return nil, fmt.Errorf("iTunes 响应未匹配目标专辑")
			}
			return &domain.CoverCatalogInput{Key: key, Images: CoverImages(artText(d.ArtworkURL), artText(d.AlbumTitle), artText(d.AlbumCredit))}, nil
		},
		Candidate: func(c provider.Candidate) *domain.CoverCatalogInput {
			if c.ReleaseRef == nil || c.ThumbURL == nil {
				return nil
			}
			return &domain.CoverCatalogInput{Key: domain.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: *c.ReleaseRef}, Images: CoverImages(*c.ThumbURL, artText(c.AlbumTitle), artText(c.AlbumCredit))}
		},
		Specs: func(detail bool) []string {
			if detail {
				return []string{"1200", "3000", "600", "250", "100"}
			}
			return []string{"250", "100", "600", "1200", "3000"}
		},
		LegacyLadder: func(ref, art, group string) []string {
			var urls []string
			for _, v := range ArtworkLadder(art) {
				urls = append(urls, v.URL)
			}
			return urls
		},
	}
}
func artText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
