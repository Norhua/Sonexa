package provider

import (
	"context"
	"github.com/norhua/sonexa/internal/domain"
)

// ArtworkStrategy 留在来源适配器，调度层只处理稳定身份、缓存及工作池。
type ArtworkStrategy struct {
	Provider      ProviderID
	Source        string
	ObjectType    string
	Validate      func(kind, ref string) error
	Load          func(context.Context, domain.CoverCatalogKey, ArtworkIO) (*domain.CoverCatalogInput, error)
	Candidate     func(Candidate) *domain.CoverCatalogInput
	Specs         func(detail bool) []string
	ImageURLs     func(string) []string
	LegacyLadder  func(releaseRef, artworkURL, groupRef string) []string
	GroupFallback bool
}
type ArtworkIO struct {
	Fetch  func(context.Context, string) ([]byte, error)
	Detail func(context.Context, Ref) (*Detail, error)
}
type ArtworkStrategist interface{ ArtworkStrategy() ArtworkStrategy }
