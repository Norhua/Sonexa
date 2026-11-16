package app

import (
	"github.com/norhua/sonexa/internal/store"
	"testing"
)

func TestAlbumBrowseScopeDistinguishesSavedUncollectedFromGhost(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Saved", Credit: strPtrOrNil("Artist"), Album: strPtrOrNil("Album"), MatchSource: strPtrOrNil("musicbrainz"), MatchRef: strPtrOrNil("track-1"), Release: &store.ReleaseInput{Provider: strPtrOrNil("musicbrainz"), ProviderRef: strPtrOrNil("release"), ReleaseGroupRef: strPtrOrNil("group")}})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.store.GetTrackView(id)
	if err != nil {
		t.Fatal(err)
	}
	rel, err := app.store.GetRelease(*track.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	err = app.store.ReplaceTracklist(rel.ID, []store.ReleaseTrack{
		{DiscNumber: 1, TrackNumber: 1, Title: "Saved", TrackRef: strPtrOrNil("track-1")},
		{DiscNumber: 1, TrackNumber: 2, Title: "Missing", TrackRef: strPtrOrNil("track-2")},
	}, store.TracklistComplete)
	if err != nil {
		t.Fatal(err)
	}
	defaultDetail, err := app.GetAlbumGroupDetail(*rel.ReleaseGroupID)
	if err != nil || len(defaultDetail.Releases) != 0 {
		t.Fatal(defaultDetail, err)
	}
	detail, err := app.GetAlbumGroupDetailForScope(*rel.ReleaseGroupID, true)
	if err != nil || len(detail.Releases) != 1 || detail.OwnedCount != 0 || detail.SavedCount != 1 || detail.RepresentativeID != rel.ID {
		t.Fatal(detail, err)
	}
	rows := detail.Releases[0].Rows
	if len(rows) != 2 || rows[0].TrackID != id || rows[0].Ghost || rows[0].InLibrary || *rows[0].AddedAt != "" || !rows[1].Ghost || rows[1].TrackID != 0 {
		t.Fatal(rows)
	}
	albums, err := app.ListAlbumsFiltered(store.TrackFilter{IncludeSynced: true})
	if err != nil || len(albums) != 1 || albums[0].ReleaseID != rel.ID || albums[0].SavedCount != 1 || albums[0].OwnedCount != 0 {
		t.Fatal(albums, err)
	}
	artists, err := app.ListArtistsFiltered(store.TrackFilter{IncludeSynced: true})
	if err != nil || len(artists) != 1 {
		t.Fatal(artists, err)
	}
	header, err := app.GetArtistHeaderFiltered(artists[0].ID, store.TrackFilter{IncludeSynced: true})
	if err != nil || header.Facets[0].Count != 1 {
		t.Fatal(header, err)
	}
}
