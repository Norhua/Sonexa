package qqmusic

import (
	"context"

	"github.com/norhua/sonexa/internal/provider"
)

// searchSongs 是 QQ 歌曲搜索唯一协议入口。调用方选择业务关键词、数量和
// 缓存模式；这里保留原生歌曲/专辑字段，不借用元数据候选裁剪其他业务结果。
func (p *Provider) searchSongs(ctx context.Context, term string, limit int, mode provider.CacheMode) ([]song, error) {
	body, err := p.musicu(ctx, mode, map[string]any{
		"req": map[string]any{
			"module": "music.search.SearchCgiService",
			"method": "DoSearchForQQMusicDesktop",
			"param":  map[string]any{"query": term, "search_type": 0, "num_per_page": limit, "page_num": 1},
		},
	})
	if err != nil {
		return nil, err
	}
	return parseSongs(body)
}
