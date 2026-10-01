package covers

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
	"net/http"
	"time"
)

// AttachLibrary 在开始服务请求前装配歌集文件服务，旧构造器仍支持隔离的工作池测试。
func (s *Service) AttachLibrary(st *store.Store) error {
	library, err := NewLibraryImages(st, s.pool.cache.Dir())
	if err != nil {
		return err
	}
	s.library = library
	library.catalogKeys = s.catalogsForRelease
	if err := library.Configure(s.pool.cache.maxBytes, s.pool.cache.ttl); err != nil {
		library.Close()
		return err
	}
	s.pool.client.Transport = &imageTransport{}
	s.pool.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("封面重定向次数过多")
		}
		if err := validImageURL(req.URL.String()); err != nil {
			return err
		}
		return s.pool.checkGate()
	}
	return nil
}

func (s *Service) EnableNetworkPolicy(gate func() error) {
	s.pool.SetGate(gate)
	s.pool.mu.Lock()
	s.pool.retry = provider.TransportRetryPolicy()
	s.pool.mu.Unlock()
}

func (s *Service) ConfigureCache(maxBytes int64, ttl time.Duration) error {
	if s.library != nil {
		return s.library.Configure(maxBytes, ttl)
	}
	return nil
}

func (s *Service) ClearCache() error {
	if s.library != nil {
		if err := s.library.Clear(); err != nil {
			return err
		}
	}
	return s.pool.cache.Clear()
}

func (s *Service) MoveRoot(dir string, commit func() error) error {
	if s.library == nil {
		return fmt.Errorf("封面服务未装配")
	}
	if _, err := NewDiskCache(dir, s.pool.cache.maxBytes, s.pool.cache.ttl); err != nil {
		return err
	}
	if err := s.library.MoveRoot(dir, commit); err != nil {
		return err
	}
	return nil
}

func (s *Service) ConfigureNetwork(gate func() error, userAgent string) {
	s.pool.SetGate(gate)
	s.pool.mu.Lock()
	s.pool.userAgent = userAgent
	s.pool.mu.Unlock()
}

func (s *Service) Library() *LibraryImages { return s.library }

func (s *Service) beginOperation(ctx context.Context) (context.Context, func(), error) {
	s.prefetchMu.Lock()
	if s.closing {
		s.prefetchMu.Unlock()
		return nil, nil, context.Canceled
	}
	s.operations.Add(1)
	s.prefetchMu.Unlock()
	child, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.life, cancel)
	return child, func() { stop(); cancel(); s.operations.Done() }, nil
}

func (s *Service) Close() error {
	s.prefetchMu.Lock()
	s.closing = true
	s.stop()
	for _, cancel := range s.prefetches {
		cancel()
	}
	s.prefetchMu.Unlock()
	s.pool.Close()
	s.operations.Wait()
	if s.library != nil {
		return s.library.Close()
	}
	return nil
}

func (s *Service) catalogsForRelease(release *store.Release) []store.CoverCatalogKey {
	if release.Provider == nil || release.ProviderRef == nil {
		return nil
	}
	policy, ok := s.artworkForProvider(*release.Provider)
	if !ok || policy.CatalogKeys == nil {
		return nil
	}
	group := ""
	if release.ReleaseGroupRef != nil {
		group = *release.ReleaseGroupRef
	}
	return policy.CatalogKeys(*release.ProviderRef, group)
}
