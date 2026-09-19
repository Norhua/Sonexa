package app

import (
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestViewTagFilterScopesAndAlbumRepresentative(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	s := app.store
	mustID := func(id int64, err error) int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	tag := mustID(app.CreateCollection("Selected", "tag", nil))
	playlist := mustID(app.CreateCollection("Playlist", "playlist", nil))
	artist := mustID(s.UpsertArtist(&store.Artist{Name: "Artist"}))
	vocal := mustID(s.UpsertArtist(&store.Artist{Name: "Vocal"}))
	composer := mustID(s.UpsertArtist(&store.Artist{Name: "Composer"}))
	create := func(ref, release, format string) int64 {
		return mustID(s.CreateTrackWithProvenance(store.NewTrackInput{
			Title: ref, Album: strPtrOrNil("Album"), MatchSource: strPtrOrNil("musicbrainz"), MatchRef: &ref,
			Release: &store.ReleaseInput{Provider: strPtrOrNil("musicbrainz"), ProviderRef: &release,
				ReleaseGroupRef: strPtrOrNil("group"), MediumFormat: &format},
		}))
	}
	cd := create("cd track", "cd", "CD")
	digital := create("digital track", "digital", "Digital Media")
	must(s.SetTrackCredits(cd, []store.CreditEntry{{ArtistID: artist}}))
	must(s.SetTrackCredits(digital, []store.CreditEntry{{ArtistID: artist}}))
	must(s.SetTrackArtists(digital, []store.TrackArtist{{ArtistID: vocal, Role: store.RoleVocal}, {ArtistID: composer, Role: store.RoleComposer}}))
	must(app.AddToCollection(tag, []int64{digital}))
	must(app.AddToCollection(playlist, []int64{cd, digital}))
	artists, err := app.ListArtistsForTag(tag)
	if err != nil || len(artists) != 2 {
		t.Fatalf("artists=%+v err=%v", artists, err)
	}
	for _, item := range artists {
		if item.ID == composer || (item.ID == artist && item.TrackCount != 2) {
			t.Fatalf("unexpected member/count: %+v", item)
		}
	}
	albums, err := app.ListAlbumsForTag(tag)
	if err != nil || len(albums) != 1 || albums[0].OwnedCount != 2 || albums[0].RepresentativeRef == nil || *albums[0].RepresentativeRef != "cd" {
		t.Fatalf("representative/count changed: %+v %v", albums, err)
	}
	for _, f := range []store.TrackFilter{
		{TagID: tag}, {TagID: tag, CollectionID: playlist}, {TagID: tag, ArtistID: artist},
		{TagID: tag, ReleaseGroupID: albums[0].GroupID}, {TagID: tag, Query: "digital"}, {TagID: tag, RecentOnly: true},
	} {
		page, err := app.ListTracks(f)
		if err != nil || page.Total != 1 || len(page.Tracks) != 1 || page.Tracks[0].ID != digital {
			t.Fatalf("filter=%+v page=%+v err=%v", f, page, err)
		}
	}
	for _, check := range []func() (int, error){
		func() (int, error) { rows, err := app.ListArtistsForTag(tag + 999); return len(rows), err },
		func() (int, error) { rows, err := app.ListAlbumsForTag(tag + 999); return len(rows), err },
	} {
		if count, err := check(); err != nil || count != 0 {
			t.Fatalf("unknown tag: %d %v", count, err)
		}
	}
}
