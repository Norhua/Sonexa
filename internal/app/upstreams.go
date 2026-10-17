package app

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/caa"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/kugou"
	"github.com/norhua/sonexa/internal/provider/kuwo"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

type imageSource interface {
	provider.Upstream
	provider.ImagePolicySource
}

func buildImagePolicies(inputs ...imageSource) (*provider.ImagePolicies, error) {
	entries := map[string]*provider.ImagePolicy{}
	for _, input := range inputs {
		policy, err := input.ImagePolicy()
		if err != nil {
			return nil, err
		}
		id := string(input.ID())
		if _, exists := entries[id]; exists {
			return nil, fmt.Errorf("重复图片来源 %q", id)
		}
		entries[id] = policy
	}
	return provider.NewImagePolicies(entries)
}

func defaultImagePolicies() (*provider.ImagePolicies, error) {
	return buildImagePolicies(&caa.Provider{}, &itunes.Provider{}, &qqmusic.Provider{}, &netease.Provider{})
}

func buildCoverSources(client *provider.Client, mb caa.ReleaseSearcher, it *itunes.Provider, qq *qqmusic.Provider, svc *covers.Service) (*sources.Registry, error) {
	return sources.NewRegistry(
		sources.Entry{Provider: caa.New(mb, func(ctx context.Context, key domain.CoverCatalogKey, force bool) (domain.CoverCatalogKey, error) {
			catalog, err := svc.Catalog(ctx, key, force)
			if err != nil {
				return domain.CoverCatalogKey{}, err
			}
			return catalog.CoverCatalogKey, nil
		}), Name: "CAA"},
		sources.Entry{Provider: it, Name: "iTunes", NeedsCountry: true},
		sources.Entry{Provider: qq, Name: "QQ"},
		sources.Entry{Provider: netease.New(client), Name: "网易云"},
	)
}

// buildLyricsService is shared by production assembly and composition tests.
// Existing sessions retain this immutable service when settings rebuild it.
func buildLyricsService(client *provider.Client, qq *qqmusic.Provider) (*lyrics.Service, error) {
	r, err := lyrics.NewRegistry(
		lyrics.Entry{Provider: netease.New(client), Name: "网易云音乐"},
		lyrics.Entry{Provider: qq, Name: "QQ 音乐"},
		lyrics.Entry{Provider: kugou.New(client), Name: "酷狗音乐"},
		lyrics.Entry{Provider: kuwo.New(client), Name: "酷我音乐"},
	)
	if err != nil {
		return nil, err
	}
	return &lyrics.Service{Sources: r}, nil
}
