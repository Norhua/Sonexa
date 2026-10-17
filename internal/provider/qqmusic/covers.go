package qqmusic

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func (p *Provider) SearchCovers(ctx context.Context, q sources.Query) ([]sources.Result, error) {
	term := sources.SearchTerm(q)
	if term == "" {
		return nil, fmt.Errorf("请输入封面搜索条件")
	}
	songs, err := p.searchSongs(ctx, term, 30, provider.NoStore)
	if err != nil {
		return nil, err
	}
	out := []sources.Result{}
	seen := map[string]bool{}
	for _, s := range songs {
		mid := s.Album.MID
		if mid == "" || seen[mid] {
			continue
		}
		for _, c := range mid {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
				return nil, fmt.Errorf("QQ 专辑身份无效")
			}
		}
		artist := creditOf(s.Singer)
		if !sources.Relevant(q, s.Name, artist, s.Album.Name) {
			continue
		}
		seen[mid] = true
		images := CoverImages(mid, s.Album.Name, artist)
		if images == nil {
			continue
		}
		out = append(out, sources.Result{Input: &domain.CoverCatalogInput{Key: domain.CoverCatalogKey{Source: "qq", ObjectType: "album", SourceRef: mid}, Images: images}})
		if len(out) >= sources.BoundedLimit(q.Limit) {
			break
		}
	}
	return out, nil
}

var _ sources.Provider = (*Provider)(nil)
