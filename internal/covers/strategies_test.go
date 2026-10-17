package covers

import (
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/caa"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/provider/netease"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

func newTestService(cache *DiskCache, concurrency int, agent string) *Service {
	s := NewService(cache, concurrency, agent)
	s.RegisterArtwork((&musicbrainz.Provider{}).ArtworkStrategy())
	s.RegisterArtwork((&itunes.Provider{}).ArtworkStrategy())
	s.RegisterArtwork((&qqmusic.Provider{}).ArtworkStrategy())
	policies := map[string]*provider.ImagePolicy{}
	for _, p := range []interface {
		provider.Upstream
		provider.ImagePolicySource
	}{&caa.Provider{}, &itunes.Provider{}, &qqmusic.Provider{}, &netease.Provider{}} {
		policy, err := p.ImagePolicy()
		if err != nil {
			panic(err)
		}
		policies[string(p.ID())] = policy
	}
	compiled, err := provider.NewImagePolicies(policies)
	if err != nil {
		panic(err)
	}
	s.SetImagePolicies(compiled)
	return s
}
