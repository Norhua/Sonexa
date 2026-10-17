// Package caa owns Cover Art Archive protocols. Release recall and persisted
// catalog access are injected, so CAA never imports MusicBrainz or writes store.
package caa

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

const MaxReleases = 10

type ReleaseSearcher interface {
	SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error)
}

// CatalogLoader returns only an already registered key, never writable images.
type CatalogLoader func(context.Context, domain.CoverCatalogKey, bool) (domain.CoverCatalogKey, error)
type Provider struct {
	recall  ReleaseSearcher
	catalog CatalogLoader
}

func New(recall ReleaseSearcher, catalog CatalogLoader) *Provider {
	return &Provider{recall: recall, catalog: catalog}
}
func (*Provider) ID() provider.ProviderID { return "caa" }
func (*Provider) DisplayName() string     { return "CAA" }
func (p *Provider) SearchCovers(ctx context.Context, q sources.Query) ([]sources.Result, error) {
	if p.catalog == nil {
		return nil, fmt.Errorf("CAA 目录服务未装配")
	}
	refs := []string{}
	seen := map[string]bool{}
	add := func(ref string) {
		if ref != "" && !seen[ref] && len(refs) < MaxReleases {
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	if q.Release != nil && q.Release.Provider == provider.MusicBrainz {
		add(q.Release.Ref)
	}
	var failure error
	if p.recall != nil {
		rows, err := p.recall.SearchTracks(provider.WithPriority(ctx, provider.PriorityInteractive), provider.Query{Title: q.Title, Artist: q.Artist, Album: q.Album, Country: q.Country, Limit: sources.BoundedLimit(q.Limit), SkipCache: q.Force})
		failure = err
		for _, c := range rows {
			if c.ReleaseRef != nil {
				add(*c.ReleaseRef)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keys := make([]domain.CoverCatalogKey, len(refs))
	errs := make([]error, len(refs))
	var wg sync.WaitGroup
	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys[i], errs[i] = p.catalog(ctx, domain.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: ref}, q.Force)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []sources.Result{}
	for i, key := range keys {
		if errs[i] != nil {
			failure = errors.Join(failure, errs[i])
			continue
		}
		out = append(out, sources.Result{Existing: &key})
	}
	return out, failure
}

var _ sources.Provider = (*Provider)(nil)
