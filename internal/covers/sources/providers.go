package sources

import (
	"context"
	"fmt"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"sync"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
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
	Provider   provider.MetadataProvider
	ReleaseRef string
	Catalog    CatalogLoader
}

func (s *CAASource) Search(ctx context.Context, q provider.Query) ([]Result, error) {
	q.Limit = boundedLimit(q.Limit)
	// 固定目标与 MusicBrainz 召回的发行一起并发读取目录；CAA 不限制并发，
	// 串行读取十个目录（每个都要经 archive.org 重定向）是封面搜索慢的主因。
	refs := []string{}
	seen := map[string]bool{}
	add := func(ref string) {
		if ref != "" && !seen[ref] && len(refs) < MaxCAAReleases {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	add(s.ReleaseRef)
	candidates, failure := s.Provider.SearchTracks(provider.WithPriority(ctx, provider.PriorityInteractive), q)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if candidate.ReleaseRef != nil {
			add(*candidate.ReleaseRef)
		}
	}
	catalogs := make([]*store.CoverCatalog, len(refs))
	errs := make([]error, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			catalogs[i], errs[i] = s.Catalog(ctx, "release", ref, q.SkipCache)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	results := []Result{}
	for i, catalog := range catalogs {
		if errs[i] != nil {
			failure = errs[i]
			continue
		}
		results = append(results, Result{Existing: &catalog.CoverCatalogKey})
	}
	return results, failure
}

type ITunesSource struct{ Provider provider.MetadataProvider }

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
		// iTunes 对词项宽松召回，会返回同艺人的无关专辑。
		if !Relevant(q, deref(c.Title), deref(c.ArtistName), deref(c.AlbumTitle)) {
			continue
		}
		identity := fmt.Sprintf("%s|%s", *c.ReleaseRef, *c.ThumbURL)
		if seen[identity] {
			continue
		}
		seen[identity] = true
		image := itunes.CoverImages(*c.ThumbURL, deref(c.AlbumTitle), deref(c.ArtistName))[0]
		index, exists := albums[*c.ReleaseRef]
		if !exists {
			index = len(results)
			albums[*c.ReleaseRef] = index
			results = append(results, Result{Input: &domain.CoverCatalogInput{Key: store.CoverCatalogKey{Source: "itunes", ObjectType: "album", SourceRef: *c.ReleaseRef}}})
		}
		results[index].Input.Images = append(results[index].Input.Images, image)
		count++
		if count >= q.Limit {
			break
		}
	}
	return results, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
