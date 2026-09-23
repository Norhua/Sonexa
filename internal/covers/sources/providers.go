package sources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/store"
)

const MaxResults = 20
const MaxCAAReleases = 10

// Info 属于封面来源列表，不扩展音乐元数据注册表。
type Info struct {
	ID           string
	Name         string
	NeedsCountry bool
}

func List() []Info {
	return []Info{{"caa", "CAA", false}, {"itunes", "iTunes", true}, {"qq", "QQ", false}, {"163", "网易云", false}}
}

// CatalogLoader 复用封面服务的持久化目录、共享并发和出网检查。
type CatalogLoader func(context.Context, string, string, bool) (*store.CoverCatalog, error)

type CAASource struct {
	Provider   provider.Provider
	ReleaseRef string
	Catalog    CatalogLoader
}

func (s *CAASource) Search(ctx context.Context, q provider.Query) ([]Result, error) {
	q.Limit = boundedLimit(q.Limit)
	results := []Result{}
	seen := map[string]bool{}
	var failure error
	load := func(ref string) {
		if ref == "" || seen[ref] || len(seen) >= MaxCAAReleases || ctx.Err() != nil {
			return
		}
		seen[ref] = true
		catalog, err := s.Catalog(ctx, "release", ref, q.SkipCache)
		if err != nil {
			failure = err
			return
		}
		results = append(results, Result{Key: catalog.CoverCatalogKey, Images: catalog.Images})
	}
	// 固定目标先查；之后用原元数据 provider 的共享配额召回其他发行。
	load(s.ReleaseRef)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	candidates, err := s.Provider.SearchTracks(provider.WithPriority(ctx, provider.PriorityInteractive), q)
	if err != nil {
		failure = err
	}
	for _, candidate := range candidates {
		if candidate.ReleaseRef != nil {
			load(*candidate.ReleaseRef)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, failure
}

type ITunesSource struct{ Provider provider.Provider }

func (s *ITunesSource) Search(ctx context.Context, q provider.Query) ([]Result, error) {
	q.Limit = boundedLimit(q.Limit)
	candidates, err := s.Provider.SearchTracks(provider.WithPriority(ctx, provider.PriorityInteractive), q)
	if err != nil {
		return nil, err
	}
	results := []Result{}
	albums := map[string]int{}
	seen := map[string]bool{}
	count := 0
	for _, c := range candidates {
		if c.ReleaseRef == nil || c.ThumbURL == nil {
			continue
		}
		identity := fmt.Sprintf("%s|%s", *c.ReleaseRef, *c.ThumbURL)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		image := store.CoverImage{SourceImageID: *c.ThumbURL, PrimaryType: "Front", Types: []string{"Front"}, Front: true}
		if c.AlbumTitle != nil {
			image.Title = *c.AlbumTitle
		}
		if c.ArtistName != nil {
			image.Artist = *c.ArtistName
		}
		for _, size := range itunes.ArtworkSizes {
			image.Variants = append(image.Variants, store.CoverVariant{Spec: strconv.Itoa(size), URL: itunes.ArtworkURL(*c.ThumbURL, size), Constructed: true})
		}
		index, exists := albums[*c.ReleaseRef]
		if !exists {
			index = len(results)
			albums[*c.ReleaseRef] = index
			results = append(results, Result{Key: store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: *c.ReleaseRef}})
		}
		results[index].Images = append(results[index].Images, image)
		count++
		if count >= q.Limit {
			break
		}
	}
	return results, nil
}

func boundedLimit(limit int) int {
	if limit <= 0 || limit > MaxResults {
		return MaxResults
	}
	return limit
}

var _ Searcher = (*CAASource)(nil)
var _ Searcher = (*ITunesSource)(nil)
var _ Searcher = (*WebSource)(nil)
