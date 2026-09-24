package covers

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/provider/qqmusic"
	"github.com/norhua/sonexa/internal/store"
)

// QQCatalog 按明确的专辑 MID 登记 QQ 图片目录，不做文字搜索。
//
// 图片地址由 MID 直接构造，登记目录本身无需出网；真实下载仍走封面池、
// 出网检查与逐档回退（并非每张专辑都有 1200 档）。
func (s *Service) QQCatalog(ctx context.Context, albumMID string) (*store.CoverCatalog, error) {
	ctx, done, err := s.beginOperation(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if !qqmusic.ValidMID(albumMID) {
		return nil, fmt.Errorf("无效 QQ 专辑身份")
	}
	key := store.CoverCatalogKey{Source: "qq", ObjectType: "album", SourceRef: albumMID}
	value, err := s.pool.Shared(ctx, "catalog:qq:"+albumMID, func(ctx context.Context) (any, error) {
		cached, err := s.library.store.GetCoverCatalog(key)
		if err != nil {
			return nil, err
		}
		if cached != nil && (cached.State == "found" || cached.State == "empty") {
			return cached, nil
		}
		if _, err := s.library.store.SaveCoverCatalog(key, store.QQCoverImages(albumMID, "", "")); err != nil {
			return nil, err
		}
		return s.library.store.GetCoverCatalog(key)
	})
	if err != nil {
		return nil, err
	}
	return value.(*store.CoverCatalog), nil
}
