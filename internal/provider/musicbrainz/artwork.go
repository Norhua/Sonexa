package musicbrainz

import (
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/caa"
)

// ArtworkStrategy only associates MusicBrainz metadata with CAA's image protocol.
func (*Provider) ArtworkStrategy() provider.ArtworkStrategy {
	strategy := caa.ArtworkStrategy()
	strategy.Provider = provider.MusicBrainz
	return strategy
}
