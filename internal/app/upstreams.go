package app

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/caa"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/kugou"
	"github.com/norhua/sonexa/internal/provider/kuwo"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
	"github.com/norhua/sonexa/internal/store"
	"time"
)

type upstreamServices struct {
	client   *provider.Client
	metadata *provider.Registry
	lyrics   *lyrics.Service
	covers   *sources.Registry
	images   *provider.ImagePolicies
	artwork  []provider.ArtworkStrategy
}

// assembleUpstreams has no publication, network, disk, or limiter side effects.
func assembleUpstreams(cur config.Settings, limiters *provider.Limiters, st *store.Store, svc *covers.Service, userAgent string) (*upstreamServices, error) {
	if limiters == nil || st == nil || svc == nil {
		return nil, fmt.Errorf("上游装配依赖未就绪")
	}
	client := provider.NewClient(limiters, st.AsProviderCache(), userAgent)
	mb := musicbrainz.New(client, musicbrainz.Config{Contact: cur.MusicBrainz.Contact, ServerURL: cur.MusicBrainz.ServerURL, RatePerSec: cur.MusicBrainz.RatePerSec, CacheTTL: time.Duration(cur.Cache.ProviderCacheTTLh) * time.Hour}, store.ParamsHash)
	it := itunes.New(client, itunes.Config{DefaultCountry: cur.ITunes.DefaultCountry, RatePerMin: cur.ITunes.RatePerMin}, store.ParamsHash)
	qq := qqmusic.New(client, qqmusic.Config{RatePerMin: cur.QQMusic.RatePerMin}, store.ParamsHash)
	lyricSvc, err := buildLyricsService(client, qq)
	if err != nil {
		return nil, err
	}
	coverSources, err := buildCoverSources(client, mb, it, qq, svc)
	if err != nil {
		return nil, err
	}
	imageSources := []imageSource{}
	for _, source := range coverSources.List() {
		p, err := coverSources.Get(provider.ProviderID(source.ID))
		if err != nil {
			return nil, err
		}
		declaration, ok := p.(imageSource)
		if !ok {
			return nil, fmt.Errorf("封面来源 %q 未声明图片策略", source.ID)
		}
		imageSources = append(imageSources, declaration)
	}
	images, err := buildImagePolicies(imageSources...)
	if err != nil {
		return nil, err
	}
	registry := provider.NewRegistry()
	artwork := []provider.ArtworkStrategy{}
	for _, p := range []provider.MetadataProvider{mb, it, qq} {
		registry.Register(p)
		if s, ok := p.(provider.ArtworkStrategist); ok {
			artwork = append(artwork, s.ArtworkStrategy())
		}
	}
	priority := make([]provider.ProviderID, 0, len(cur.ProviderPriority))
	for _, id := range cur.ProviderPriority {
		priority = append(priority, provider.ProviderID(id))
	}
	registry.SetPriority(priority)
	return &upstreamServices{client: client, metadata: registry, lyrics: lyricSvc, covers: coverSources, images: images, artwork: artwork}, nil
}

// publishUpstreamsLocked is the only production publication point. Caller holds a.mu.
func (a *App) publishUpstreamsLocked(next *upstreamServices) {
	a.limiters.ConfigureFixedQuotas()
	for _, p := range next.metadata.List() {
		a.limiters.SetFromProvider(p)
	}
	a.coverSvc.ConfigureSources(next.artwork, next.images, func(ctx context.Context, ref provider.Ref) (*provider.Detail, error) {
		p, err := next.metadata.Get(ref.Provider)
		if err != nil {
			return nil, err
		}
		return p.FetchDetail(ctx, ref)
	})
	a.client, a.registry, a.lyricSvc, a.coverSources = next.client, next.metadata, next.lyrics, next.covers
	if a.library != nil {
		a.library.SetRegistry(next.metadata)
	}
}

// rebuildProviders is used during exclusive startup or while holding a.mu.
// Settings builds before any side effects and calls publishUpstreamsLocked directly.
func (a *App) rebuildProviders(cur config.Settings) error {
	next, err := assembleUpstreams(cur, a.limiters, a.store, a.coverSvc, a.userAgent(cur))
	if err != nil {
		return err
	}
	a.publishUpstreamsLocked(next)
	return nil
}

func (a *App) metadataRegistry() *provider.Registry {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.registry
}

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
