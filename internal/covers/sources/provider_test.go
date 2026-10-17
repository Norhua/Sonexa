package sources

import (
	"context"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
)

type coversOnly struct {
	id    provider.ProviderID
	query Query
	calls int
}

func (p *coversOnly) ID() provider.ProviderID { return p.id }
func (*coversOnly) DisplayName() string       { return "封面来源" }
func (p *coversOnly) SearchCovers(_ context.Context, q Query) ([]Result, error) {
	p.calls++
	p.query = q
	return []Result{{Input: &domain.CoverCatalogInput{Key: domain.CoverCatalogKey{Source: string(p.id), ObjectType: "album", SourceRef: "native"}}}}, nil
}

var _ Provider = (*coversOnly)(nil)
var _ provider.Upstream = (*coversOnly)(nil)

func TestCoverOnlyQueryContract(t *testing.T) {
	p := &coversOnly{id: "only"}
	entry := Entry{Provider: p, Name: "独立封面", NeedsCountry: true}
	for _, release := range []*domain.ReleaseIdentity{nil, {Provider: "musicbrainz", Ref: "release"}} {
		q := Query{Title: "song", Artist: "artist", Album: "album", Country: "jp", Limit: 13, Force: true, Release: release}
		rows, err := entry.Provider.SearchCovers(context.Background(), q)
		if err != nil || !reflect.DeepEqual(p.query, q) || len(rows) != 1 || rows[0].Validate() != nil {
			t.Fatalf("query=%+v rows=%+v err=%v", p.query, rows, err)
		}
	}
	if _, ok := entry.Provider.(provider.MetadataProvider); ok {
		t.Fatal("封面来源不需要元数据能力")
	}
}

func TestResultExclusiveForms(t *testing.T) {
	key := domain.CoverCatalogKey{Source: "only", ObjectType: "album", SourceRef: "native"}
	input := &domain.CoverCatalogInput{Key: key, ActualReleaseURL: "https://example.org/actual", Images: []domain.CoverImage{{SourceImageID: "front"}}}
	for _, r := range []Result{{}, {Input: input, Existing: &key}, {Input: &domain.CoverCatalogInput{}}, {Existing: &domain.CoverCatalogKey{}}, {Existing: &domain.CoverCatalogKey{Source: " ", ObjectType: "album", SourceRef: "native"}}} {
		if r.Validate() == nil {
			t.Fatalf("invalid result accepted: %+v", r)
		}
	}
	for _, r := range []Result{{Input: input}, {Existing: &key}} {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		if r.Key() != key {
			t.Fatal("identity changed")
		}
	}
	if input.ActualReleaseURL != "https://example.org/actual" || len(input.Images) != 1 {
		t.Fatal("输入信息丢失")
	}
}
