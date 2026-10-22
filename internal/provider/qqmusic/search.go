package qqmusic

import (
	"context"

	"github.com/norhua/sonexa/internal/provider"
)

// songSearchBlock 是 QQ 歌曲搜索唯一请求定义；歌词能力可在同一请求中批量查询。
func songSearchBlock(term string, limit int) map[string]any {
	return map[string]any{
		"module": "music.search.SearchCgiService",
		"method": "DoSearchForQQMusicDesktop",
		"param":  map[string]any{"query": term, "search_type": 0, "num_per_page": limit, "page_num": 1},
	}
}

// searchSongs 供元数据/封面使用；调用方选择关键词、数量和缓存策略。
func (p *Provider) searchSongs(ctx context.Context, term string, limit int, mode provider.CacheMode) ([]song, error) {
	body, err := p.musicu(ctx, mode, map[string]any{"req": songSearchBlock(term, limit)})
	if err != nil {
		return nil, err
	}
	return parseSongs(body)
}
