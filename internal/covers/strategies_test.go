package covers

import (
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

func newTestService(cache *DiskCache, concurrency int, agent string) *Service {
	s := NewService(cache, concurrency, agent)
	s.RegisterArtwork((&musicbrainz.Provider{}).ArtworkStrategy())
	s.RegisterArtwork((&itunes.Provider{}).ArtworkStrategy())
	s.RegisterArtwork((&qqmusic.Provider{}).ArtworkStrategy())
	return s
}
