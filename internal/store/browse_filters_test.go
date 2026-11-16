package store

import (
	"reflect"
	"testing"
)

func TestBrowseMultiSelectScopesAndCounts(t *testing.T) {
	s := openTestStore(t)
	makeTrack := func(title string, member bool) int64 {
		t.Helper()
		id, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: member, Title: title, Credit: ptr(title + " Artist"), Album: ptr(title + " Album")})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b, c := makeTrack("A Needle", true), makeTrack("B Needle", true), makeTrack("C Needle", false)
	playlist, err := s.CreateCollection("P", KindPlaylist, nil)
	if err != nil {
		t.Fatal(err)
	}
	tag, err := s.CreateCollection("T", KindTag, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, pair := range []struct {
		id     int64
		tracks []int64
	}{{playlist, []int64{a, b}}, {tag, []int64{b}}} {
		if err := s.AddToCollection(pair.id, pair.tracks); err != nil {
			t.Fatal(err)
		}
	}
	sync, _, _ := saveSyncFixture(t, s, syncTracks("one", "two", "two"), false)
	for position, id := range []int64{b, c, c} {
		if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET local_state='imported',track_id=? WHERE playlist_id=? AND position=?`, id, sync, position+1); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`UPDATE synced_playlist_entry SET upstream_present=0 WHERE playlist_id=? AND position=3`, sync); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		filter TrackFilter
		want   []int64
	}{
		{"default", TrackFilter{}, []int64{a, b}},
		{"all includes untagged", TrackFilter{IncludeSynced: true}, []int64{a, b, c}},
		{"empty tag", TrackFilter{IncludeSynced: true, TagIDs: []int64{}}, nil},
		{"tag OR", TrackFilter{TagIDs: []int64{tag, 999}}, []int64{b}},
		{"categories AND", TrackFilter{IncludeSynced: true, PlaylistIDs: []int64{playlist}, TagIDs: []int64{tag}, SyncedPlaylistIDs: []int64{sync}}, []int64{b}},
		{"sync OR deduplicated", TrackFilter{IncludeSynced: true, SyncedPlaylistIDs: []int64{sync, 999}}, []int64{b, c}},
		{"sync library intersection", TrackFilter{SyncedPlaylistIDs: []int64{sync}}, []int64{b}},
		{"wrong collection kind", TrackFilter{TagIDs: []int64{playlist}}, nil},
		{"fts before limit", TrackFilter{IncludeSynced: true, SyncedPlaylistIDs: []int64{sync}, Query: "C Needle", Limit: 1}, []int64{c}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tracks, err := s.ListTracks(tc.filter)
			if err != nil {
				t.Fatal(err)
			}
			var got []int64
			for _, track := range tracks {
				got = append(got, track.ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			count, err := s.CountTracks(tc.filter)
			if err != nil || count != len(tc.want) {
				t.Fatal(count, err)
			}
		})
	}
	f := TrackFilter{IncludeSynced: true, SyncedPlaylistIDs: []int64{sync}}
	artists, err := s.ListArtistsFiltered(f)
	if err != nil || len(artists) != 2 {
		t.Fatal(artists, err)
	}
	albums, err := s.ListAlbumsFiltered(f)
	if err != nil || len(albums) != 2 {
		t.Fatal(albums, err)
	}
	for _, album := range albums {
		if album.Title == "C Needle Album" && (album.OwnedCount != 0 || album.SavedCount != 1 || album.MemberCount != 1) {
			t.Fatal(album)
		}
	}
	for _, artist := range artists {
		facets, err := s.RoleFacetsFiltered(artist.ID, f)
		if err != nil || facets[0].Count != 1 || facets[1].Count != 1 {
			t.Fatal(facets, err)
		}
	}
	entries, err := s.ListSyncedTracksFiltered(sync, TrackFilter{IncludeSynced: true})
	if err != nil || len(entries) != 3 || entries[2].UpstreamPresent {
		t.Fatal(entries, err)
	}
	entries, err = s.ListSyncedTracksFiltered(sync, TrackFilter{TagIDs: []int64{tag}})
	if err != nil || len(entries) != 1 || entries[0].TrackID != b {
		t.Fatal(entries, err)
	}
	entries, err = s.ListSyncedTracksFiltered(sync, TrackFilter{IncludeSynced: true, Query: "C"})
	if err != nil || len(entries) != 2 || entries[0].EntryID == entries[1].EntryID {
		t.Fatal(entries, err)
	}
	for _, collection := range []int64{playlist, tag} {
		if err := s.AddToCollection(collection, []int64{a, c}); err == nil {
			t.Fatal("uncollected mixed write accepted")
		}
	}
}

func TestRecentAllUsesSavedTimeWithoutInventingMembership(t *testing.T) {
	s := openTestStore(t)
	a, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: true, Title: "A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE track SET created_at='2026-01-01 00:00:00',library_added_at='2026-03-01 00:00:00' WHERE id=?`, a); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE track SET created_at='2026-04-01 00:00:00' WHERE id=?`, b); err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListTracks(TrackFilter{IncludeSynced: true, RecentOnly: true})
	if err != nil || len(rows) != 2 || rows[0].ID != b || rows[0].AddedAt != "" || rows[0].InLibrary {
		t.Fatal(rows, err)
	}
	rows, err = s.ListTracks(TrackFilter{RecentOnly: true})
	if err != nil || len(rows) != 1 || rows[0].ID != a {
		t.Fatal(rows, err)
	}
}
