package covers

import (
	"fmt"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
	"net/http"
)

// AttachLibrary 在开始服务请求前装配歌集文件服务，旧构造器仍支持隔离的工作池测试。
func (s *Service) AttachLibrary(st *store.Store) error {
	library, err := NewLibraryImages(st, s.pool.cache.Dir())
	if err != nil {
		return err
	}
	s.library = library
	s.pool.client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("封面重定向次数过多")
		}
		return validImageURL(req.URL.String())
	}
	return nil
}

func (s *Service) EnableNetworkPolicy(gate func() error) {
	s.pool.SetGate(gate)
	s.pool.retry = provider.TransportRetryPolicy()
}

func (s *Service) Library() *LibraryImages { return s.library }

func (s *Service) Close() error {
	s.pool.Close()
	if s.library != nil {
		return s.library.Close()
	}
	return nil
}
