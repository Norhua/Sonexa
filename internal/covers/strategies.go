package covers

import (
	"context"
	"errors"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
	"time"
)

// ConfigureSources publishes automatic artwork, image safety and detail lookup
// under one lock. Operations pin this snapshot in beginOperation.
func (s *Service) ConfigureSources(artwork []provider.ArtworkStrategy, images *provider.ImagePolicies, detail func(context.Context, provider.Ref) (*provider.Detail, error)) {
	entries := make(map[string]provider.ArtworkStrategy, len(artwork))
	for _, strategy := range artwork {
		entries[strategy.Source] = strategy
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artwork, s.imagePolicies, s.detailLoader = entries, images, detail
}

func (s *Service) RegisterArtwork(policy provider.ArtworkStrategy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.artwork[policy.Source] = policy
}
func (s *Service) SetDetailLoader(load func(context.Context, provider.Ref) (*provider.Detail, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.detailLoader = load
}
func (s *Service) artworkForProvider(id string) (provider.ArtworkStrategy, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.artwork {
		if string(p.Provider) == id {
			return p, true
		}
	}
	return provider.ArtworkStrategy{}, false
}
func (s *Service) imageURLs(ctx context.Context, source, raw string) []string {
	if p, ok := artworkIn(ctx, source); ok && p.ImageURLs != nil {
		return p.ImageURLs(raw)
	}
	return []string{raw}
}

// Catalog 只执行通用缓存、同键合并和错误语义，来源负责身份及响应转换。
func (s *Service) Catalog(ctx context.Context, key store.CoverCatalogKey, force bool) (*store.CoverCatalog, error) {
	ctx, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	policy, ok := artworkIn(ctx, key.Source)
	if !ok {
		return nil, fmt.Errorf("图片来源 %s 未装配", key.Source)
	}
	if err := policy.Validate(key.ObjectType, key.SourceRef); err != nil {
		return nil, err
	}
	ctx = imageContext(ctx, key.Source)
	value, err := s.pool.Shared(ctx, "catalog:"+key.Source+":"+key.ObjectType+":"+key.SourceRef+":"+imagePolicy(ctx).Key(), func(ctx context.Context) (any, error) {
		cached, err := s.library.store.GetCoverCatalog(key)
		if err != nil {
			return nil, err
		}
		if !force && cached != nil {
			if cached.State == "found" || cached.State == "empty" {
				return cached, nil
			}
			if cached.RetryAfter != nil && time.Now().Before(*cached.RetryAfter) {
				return nil, fmt.Errorf("图片来源暂时不可用: %s", cached.LastError)
			}
		}
		snapshot, _ := ctx.Value(imagePoliciesKey{}).(*sourceSnapshot)
		load := snapshot.detail
		io := provider.ArtworkIO{Fetch: s.pool.fetchOne, Detail: func(ctx context.Context, ref provider.Ref) (*provider.Detail, error) {
			if load == nil {
				return nil, fmt.Errorf("来源详情查询未装配")
			}
			ref.SkipCache = force
			return load(provider.WithRequestGate(ctx, s.pool.checkGate), ref)
		}}
		input, err := policy.Load(ctx, key, io)
		if errors.Is(err, ErrNotFound) {
			input = &domain.CoverCatalogInput{Key: key}
			err = nil
		}
		if err != nil {
			var disabled *OutboundError
			if ctx.Err() == nil && !errors.As(err, &disabled) {
				_ = s.library.store.RecordCoverCatalogError(key, err.Error(), time.Now().Add(time.Minute))
			}
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if input == nil || input.Key != key {
			return nil, fmt.Errorf("封面目录身份不匹配")
		}
		id, err := s.library.store.SaveCoverCatalog(key, input.Images)
		if err != nil {
			return nil, err
		}
		if err := s.library.store.SetCoverCatalogRelease(id, input.ActualReleaseURL); err != nil {
			return nil, err
		}
		return s.library.store.GetCoverCatalog(key)
	})
	if err != nil {
		return nil, err
	}
	return value.(*store.CoverCatalog), nil
}
