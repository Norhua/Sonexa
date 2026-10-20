package provider

import (
	"context"
	"github.com/norhua/sonexa/internal/domain"
)

// ArtworkStrategy 留在来源适配器，调度层只处理稳定身份、缓存及工作池。
type ArtworkStrategy struct {
	Provider     ProviderID
	Source       string
	ObjectType   string
	Validate     func(kind, ref string) error
	Load         func(context.Context, domain.CoverCatalogKey, ArtworkIO) (*domain.CoverCatalogInput, error)
	Candidate    func(Candidate) *domain.CoverCatalogInput
	Specs        func(detail bool) []string
	ImageURLs    func(string) []string
	LegacyLadder func(releaseRef, artworkURL, groupRef string) []string
	// CatalogKeys returns ordered valid targets: first the primary catalog, then
	// fallbacks to try only after an empty catalog. Local preview reuse uses the
	// same identities. Adapters own object types, native refs and ordering.
	CatalogKeys func(releaseRef, groupRef string) []domain.CoverCatalogKey
}
type ArtworkIO struct {
	Fetch  func(context.Context, string) ([]byte, error)
	Detail func(context.Context, Ref) (*Detail, error)
}
type ArtworkStrategist interface{ ArtworkStrategy() ArtworkStrategy }
