package qqmusic

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"strconv"
)

func CoverImages(albumMID, title, artist string) []domain.CoverImage {
	if !ValidMID(albumMID) {
		return nil
	}
	image := domain.CoverImage{SourceImageID: albumMID, PrimaryType: "Front", Types: []string{"Front"}, Front: true, Title: title, Artist: artist}
	for _, size := range CoverSizes {
		image.Variants = append(image.Variants, domain.CoverVariant{Spec: strconv.Itoa(size), Constructed: true, URL: CoverURL(albumMID, size)})
	}
	return []domain.CoverImage{image}
}

func (p *Provider) ArtworkStrategy() provider.ArtworkStrategy {
	return provider.ArtworkStrategy{Provider: provider.QQMusic, Source: "qq", ObjectType: "album",
		CatalogKeys: func(ref, group string) []domain.CoverCatalogKey {
			if !ValidMID(ref) {
				return nil
			}
			return []domain.CoverCatalogKey{{Source: "qq", ObjectType: "album", SourceRef: ref}}
		},
		Validate: func(kind, ref string) error {
			if kind != "album" || !ValidMID(ref) {
				return fmt.Errorf("无效 QQ 专辑身份")
			}
			return nil
		},
		Load: func(ctx context.Context, key domain.CoverCatalogKey, io provider.ArtworkIO) (*domain.CoverCatalogInput, error) {
			return &domain.CoverCatalogInput{Key: key, Images: CoverImages(key.SourceRef, "", "")}, nil
		},
		Candidate: func(c provider.Candidate) *domain.CoverCatalogInput { return nil },
		Specs: func(detail bool) []string {
			if detail {
				return []string{"1200", "800", "500", "300"}
			}
			return []string{"300", "500", "800", "1200"}
		},
		LegacyLadder: func(ref, art, group string) []string {
			if !ValidMID(ref) {
				return nil
			}
			var urls []string
			for _, size := range CoverSizes {
				urls = append(urls, CoverURL(ref, size))
			}
			return urls
		},
	}
}
