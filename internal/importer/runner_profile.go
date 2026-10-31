package importer

import (
	"strconv"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

// inputFromItem is the production Runner boundary. Numeric values from an
// internal caller without raw profile have explicit canonical units, but no
// invented input column or default disc. File imports always carry a profile.
func inputFromItem(item *store.ImportItem) matching.Input {
	profile := item.Profile
	if profile == nil {
		p := matching.NewInputProfile(deref(item.InTitle), deref(item.InArtist), deref(item.InAlbum))
		if item.InYear != nil {
			p.Year = ParseYearEvidence(strconv.Itoa(*item.InYear))
		}
		if item.InDiscNo != nil {
			p.Disc = ParsePositionEvidence(strconv.Itoa(*item.InDiscNo), "disc")
		}
		if item.InTrackNo != nil {
			p.Track = ParsePositionEvidence(strconv.Itoa(*item.InTrackNo), "track")
		}
		if item.InDurationMS != nil {
			p.Duration = ParseDurationEvidence(strconv.Itoa(*item.InDurationMS)+"ms", "")
		}
		profile = &p
	}
	return matching.Input{Profile: profile, Title: profile.Title.Raw, Artist: profile.Artist.Text.Raw,
		Album: profile.Album.Raw, DurationMS: numberPointer(profile.Duration)}
}
