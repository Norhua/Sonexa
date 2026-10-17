package itunes

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

func (p *Provider) SearchCovers(ctx context.Context, q sources.Query) ([]sources.Result, error) {
	limit := sources.BoundedLimit(q.Limit)
	rows, err := p.SearchTracks(provider.WithPriority(ctx, provider.PriorityInteractive), provider.Query{Title: q.Title, Artist: q.Artist, Album: q.Album, Country: q.Country, Limit: limit, SkipCache: q.Force})
	if err != nil {
		return nil, err
	}
	out := []sources.Result{}
	albums := map[string]int{}
	seen := map[string]bool{}
	count := 0
	for _, c := range rows {
		if c.ReleaseRef == nil || c.ThumbURL == nil {
			continue
		}
		if !sources.Relevant(q, coverValue(c.Title), coverValue(c.ArtistName), coverValue(c.AlbumTitle)) {
			continue
		}
		identity := fmt.Sprintf("%s|%s", *c.ReleaseRef, *c.ThumbURL)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		image := CoverImages(*c.ThumbURL, coverValue(c.AlbumTitle), coverValue(c.ArtistName))[0]
		index, exists := albums[*c.ReleaseRef]
		if !exists {
			index = len(out)
			albums[*c.ReleaseRef] = index
			out = append(out, sources.Result{Input: &domain.CoverCatalogInput{Key: domain.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: *c.ReleaseRef}}})
		}
		out[index].Input.Images = append(out[index].Input.Images, image)
		count++
		if count >= limit {
			break
		}
	}
	return out, nil
}
func coverValue(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

var _ sources.Provider = (*Provider)(nil)
