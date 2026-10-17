package netease

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
)

func (p *Provider) SearchCovers(ctx context.Context, q sources.Query) ([]sources.Result, error) {
	term := sources.SearchTerm(q)
	if term == "" {
		return nil, fmt.Errorf("请输入封面搜索条件")
	}
	songs, err := p.searchSongs(ctx, term, 30)
	if err != nil {
		return nil, err
	}
	out := []sources.Result{}
	seen := map[string]bool{}
	for _, s := range songs {
		album := s.Album
		if album.PicURL == "" {
			continue
		}
		artists := []string{}
		for _, a := range s.Artists {
			artists = append(artists, a.Name)
		}
		artist := strings.Join(artists, " / ")
		if !sources.Relevant(q, s.Name, artist, album.Name) {
			continue
		}
		u, err := url.Parse(album.PicURL)
		if err != nil || u.User != nil || (!strings.HasSuffix(u.Hostname(), ".music.126.net") && u.Hostname() != "music.126.net") {
			continue
		}
		ref := album.ID.String()
		if ref == "" || ref == "0" {
			ref = "pic-" + strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
		}
		if seen[ref] {
			continue
		}
		seen[ref] = true
		u.Scheme = "https"
		u.RawQuery = ""
		image := domain.CoverImage{SourceImageID: ref, Title: album.Name, Artist: artist, PrimaryType: "Front", Types: []string{"Front"}, Front: true, OriginalURL: u.String()}
		image.Variants = append(image.Variants, domain.CoverVariant{Spec: "original", URL: u.String()})
		for _, size := range []int{250, 500} {
			resized := *u
			resized.RawQuery = url.Values{"param": {fmt.Sprintf("%dy%d", size, size)}}.Encode()
			image.Variants = append(image.Variants, domain.CoverVariant{Spec: strconv.Itoa(size), URL: resized.String(), Constructed: true})
		}
		out = append(out, sources.Result{Input: &domain.CoverCatalogInput{Key: domain.CoverCatalogKey{Source: "163", ObjectType: "album", SourceRef: ref}, Images: []domain.CoverImage{image}}})
		if len(out) >= sources.BoundedLimit(q.Limit) {
			break
		}
	}
	return out, nil
}

var _ sources.Provider = (*Provider)(nil)
