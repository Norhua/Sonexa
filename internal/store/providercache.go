package store

import (
	"errors"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// ProviderCache 把 Store 适配为 provider.Cache。
//
// provider 包定义自己的最小缓存接口而不直接依赖 store，
// 因此需要这一层适配把两边的记录类型对上。
type ProviderCache struct {
	store *Store
}

// AsProviderCache 返回 Store 的 provider.Cache 视图。
func (s *Store) AsProviderCache() *ProviderCache { return &ProviderCache{store: s} }

// GetCache 实现 provider.Cache。
func (c *ProviderCache) GetCache(p, endpoint, hash string) (*provider.CacheEntry, error) {
	entry, err := c.store.GetCache(p, endpoint, hash)
	if errors.Is(err, ErrCacheMiss) {
		return nil, provider.ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return &provider.CacheEntry{
		Body:      entry.Body,
		FetchedAt: entry.FetchedAt,
		TTL:       entry.TTL,
	}, nil
}

// PutCache 实现 provider.Cache。
func (c *ProviderCache) PutCache(p, endpoint, hash string, body []byte, ttl time.Duration) error {
	return c.store.PutCache(p, endpoint, hash, body, ttl)
}

// 编译期断言：ProviderCache 必须满足 provider.Cache。
var _ provider.Cache = (*ProviderCache)(nil)
