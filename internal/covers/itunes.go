package covers

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

var itunesAlbumRef = regexp.MustCompile(`^[a-z]{2}:[1-9][0-9]*$`)

func (s *Service) SetITunesDetailLoader(load func(context.Context, string) (*provider.Detail, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.itunesDetail = load
}

// ITunesCatalog 只按明确的 storefront:collectionId 恢复目录，不做文字搜索。
// 候选的已知 artwork 已登记时无需 Lookup；无目录的既有歌集同样可恢复。
func (s *Service) ITunesCatalog(ctx context.Context, ref string) (*store.CoverCatalog, error) {
	ctx, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !itunesAlbumRef.MatchString(ref) {
		return nil, fmt.Errorf("无效 iTunes 专辑身份")
	}
	key := store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: ref}
	value, err := s.pool.Shared(ctx, "catalog:itunes:"+ref, func(ctx context.Context) (any, error) {
		cached, err := s.library.store.GetCoverCatalog(key)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			if cached.State == "found" || cached.State == "empty" {
				return cached, nil
			}
			if cached.RetryAfter != nil && time.Now().Before(*cached.RetryAfter) {
				return nil, fmt.Errorf("iTunes 封面暂时不可用: %s", cached.LastError)
			}
		}
		s.mu.Lock()
		load := s.itunesDetail
		s.mu.Unlock()
		if load == nil {
			return nil, fmt.Errorf("iTunes 封面查询未装配")
		}
		// 缓存读取不受 gate 限制，真实 HTTP（含重定向）才检查出网条件。
		d, err := load(provider.WithRequestGate(ctx, s.pool.checkGate), ref)
		if err == nil && (d == nil || d.Provider != provider.ITunes || d.ReleaseRef == nil || *d.ReleaseRef != ref) {
			err = fmt.Errorf("iTunes 响应未匹配目标专辑")
		}
		if err != nil {
			var disabled *OutboundError
			if ctx.Err() == nil && !errors.As(err, &disabled) {
				_ = s.library.store.RecordCoverCatalogError(key, err.Error(), time.Now().Add(time.Minute))
			}
			return nil, err
		}
		text := func(p *string) string {
			if p == nil {
				return ""
			}
			return *p
		}
		if _, err := s.library.store.SaveCoverCatalog(key, store.ITunesCoverImages(text(d.ArtworkURL), text(d.AlbumTitle), text(d.AlbumCredit))); err != nil {
			return nil, err
		}
		return s.library.store.GetCoverCatalog(key)
	})
	if err != nil {
		return nil, err
	}
	return value.(*store.CoverCatalog), nil
}
