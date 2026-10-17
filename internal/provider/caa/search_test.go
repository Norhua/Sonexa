package caa

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

type recallFunc func(context.Context, provider.Query) ([]provider.Candidate, error)

func (f recallFunc) SearchTracks(ctx context.Context, q provider.Query) ([]provider.Candidate, error) {
	return f(ctx, q)
}
func TestSearchKeepsBoundReleaseAndPartialResults(t *testing.T) {
	failed := errors.New("recall failed")
	other := "other"
	p := New(recallFunc(func(context.Context, provider.Query) ([]provider.Candidate, error) {
		return []provider.Candidate{{ReleaseRef: &other}}, failed
	}), func(_ context.Context, key domain.CoverCatalogKey, force bool) (domain.CoverCatalogKey, error) {
		if !force {
			t.Error("refresh lost")
		}
		if key.SourceRef == other {
			return domain.CoverCatalogKey{}, errors.New("catalog failed")
		}
		return key, nil
	})
	rows, err := p.SearchCovers(context.Background(), sources.Query{Force: true, Release: &domain.ReleaseIdentity{Provider: provider.MusicBrainz, Ref: "bound"}})
	if !errors.Is(err, failed) || len(rows) != 1 || rows[0].Existing == nil || rows[0].Existing.SourceRef != "bound" || rows[0].Input != nil {
		t.Fatal(rows, err)
	}
}
func TestCAAOnlyCatalogCapabilityNeedsNoMetadata(t *testing.T) {
	p := New(nil, func(_ context.Context, key domain.CoverCatalogKey, _ bool) (domain.CoverCatalogKey, error) {
		return key, nil
	})
	rows, err := p.SearchCovers(context.Background(), sources.Query{Release: &domain.ReleaseIdentity{Provider: provider.MusicBrainz, Ref: "bound"}})
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
}
