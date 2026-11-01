package importer

import (
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

// inputFromItem is the production Runner boundary. Numeric values from an
// internal caller without raw profile have explicit canonical units, but no
// invented input column or default disc. File imports always carry a profile.
func inputFromItem(item *store.ImportItem) matching.Input {
	p := store.ItemProfile(item)
	profile := &p
	return matching.Input{Profile: profile, Title: profile.Title.Raw, Artist: profile.Artist.Text.Raw,
		Album: profile.Album.Raw, DurationMS: numberPointer(profile.Duration)}
}
