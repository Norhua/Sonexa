package app

import (
	"github.com/norhua/sonexa/internal/store"
	"testing"
)

func TestAlbumVersionsKeepEveryCollectionAndOwnFacts(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	group, err := a.store.UpsertReleaseGroup(&store.ReleaseGroup{Provider: strPtrOrNil("musicbrainz"), ProviderRef: strPtrOrNil("group"), Title: "合集"})
	if err != nil {
		t.Fatal(err)
	}
	makeRelease := func(ref, format, date string) int64 {
		id, err := a.store.UpsertRelease(&store.Release{ReleaseGroupID: &group, Provider: strPtrOrNil("musicbrainz"), ProviderRef: strPtrOrNil(ref), Title: ref, MediumFormat: strPtrOrNil(format), ReleaseDate: strPtrOrNil(date)})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	empty := makeRelease("empty", "CD", "2012")
	digital := makeRelease("digital", "Digital Media", "2014")
	sacd := makeRelease("sacd", "SACD", "2015")
	add := func(ref, title, release string) int64 {
		id, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{Title: title, MatchSource: strPtrOrNil("musicbrainz"), MatchRef: strPtrOrNil(ref), Release: &store.ReleaseInput{Provider: strPtrOrNil("musicbrainz"), ProviderRef: strPtrOrNil(release)}, Metadata: &store.MetadataInput{RecordingRef: strPtrOrNil("same-recording")}})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	one := add("one", "用户曲名", "digital")
	two := add("two", "White Love", "sacd")
	outside := add("outside", "清单外收藏", "sacd")
	if err := a.store.ReplaceTracklist(sacd, []store.ReleaseTrack{{DiscNumber: 1, TrackNumber: 6, TrackRef: strPtrOrNil("two"), Title: "上游旧名"}, {DiscNumber: 1, TrackNumber: 7, TrackRef: strPtrOrNil("one"), Title: "相同标识但不同发行"}}, store.TracklistComplete); err != nil {
		t.Fatal(err)
	}
	albums, err := a.ListAlbums()
	if err != nil || len(albums) != 1 {
		t.Fatalf("%+v %v", albums, err)
	}
	if albums[0].ReleaseID != empty || albums[0].OwnedCount != 3 || albums[0].MemberCount != 3 || albums[0].OwnedReleaseCount != 2 {
		t.Fatalf("summary: %+v", albums[0])
	}
	detail, err := a.GetAlbumGroupDetail(group)
	if err != nil {
		t.Fatal(err)
	}
	if detail.OwnedCount != 3 || len(detail.Releases) != 3 || detail.RepresentativeID != empty {
		t.Fatalf("%+v", detail)
	}
	seen := map[int64]bool{}
	for _, r := range detail.Releases {
		if r.ReleaseID == digital && (!r.Incomplete || len(r.Rows) != 1 || r.Rows[0].TrackID != one || *r.Year != 2014) {
			t.Fatalf("digital: %+v", r)
		}
		if r.ReleaseID == sacd {
			if r.OwnedCount != 2 || len(r.Rows) != 3 || *r.Year != 2015 {
				t.Fatalf("sacd: %+v", r)
			}
			for _, row := range r.Rows {
				if row.TrackID == two && (row.Title != "White Love" || row.OutsideTracklist) {
					t.Fatalf("must use current value: %+v", row)
				}
				if row.TrackID == outside && !row.OutsideTracklist {
					t.Fatalf("outside: %+v", row)
				}
				if row.TrackRef != nil && *row.TrackRef == "one" && !row.Ghost {
					t.Fatalf("cross-release ownership: %+v", row)
				}
			}
		}
		for _, row := range r.Rows {
			if !row.Ghost {
				if seen[row.TrackID] {
					t.Fatal("duplicate collection")
				}
				seen[row.TrackID] = true
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("lost collections: %v", seen)
	}
}
