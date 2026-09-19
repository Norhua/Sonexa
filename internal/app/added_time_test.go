package app

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

func TestAddedTimeReachesEveryLibraryRow(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "Added time", Album: strPtrOrNil("Album"),
		MatchSource: strPtrOrNil("musicbrainz"), MatchRef: strPtrOrNil("owned"),
		Release: &store.ReleaseInput{Provider: strPtrOrNil("musicbrainz"), ProviderRef: strPtrOrNil("release")},
	})
	if err != nil {
		t.Fatal(err)
	}
	track, err := app.store.GetTrack(id)
	if err != nil {
		t.Fatal(err)
	}
	want := track.CreatedAt.UTC().Format(time.RFC3339)
	page, err := app.ListTracks(store.TrackFilter{})
	if err != nil || len(page.Tracks) != 1 || page.Tracks[0].AddedAt != want {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	detail, err := app.GetTrackDetail(id)
	if err != nil || detail.Track.AddedAt != want {
		t.Fatalf("detail=%+v err=%v", detail, err)
	}
	payload, err := json.Marshal(detail.Track)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["addedAt"] != want {
		t.Fatalf("missing frontend timestamp: %s", payload)
	}
	releaseID := *detail.Track.ReleaseID
	// 未完整的清单和完整清单都必须携带同一个首次入库时间。
	album, err := app.GetAlbumDetail(releaseID)
	if err != nil || len(album.Rows) != 1 || album.Rows[0].AddedAt == nil || *album.Rows[0].AddedAt != want {
		t.Fatalf("album=%+v err=%v", album, err)
	}
	if err := app.store.ReplaceTracklist(releaseID, []store.ReleaseTrack{
		{DiscNumber: 1, TrackNumber: 1, Title: "owned", TrackRef: strPtrOrNil("owned")},
		{DiscNumber: 1, TrackNumber: 2, Title: "missing", TrackRef: strPtrOrNil("missing")},
	}, store.TracklistComplete); err != nil {
		t.Fatal(err)
	}
	album, err = app.GetAlbumDetail(releaseID)
	if err != nil || len(album.Rows) != 2 {
		t.Fatalf("album=%+v err=%v", album, err)
	}
	if album.Rows[0].AddedAt == nil || *album.Rows[0].AddedAt != want || album.Rows[1].AddedAt != nil {
		t.Fatalf("rows=%+v", album.Rows)
	}
	if err := app.store.UpdateBasicOnMatch(id, strPtrOrNil("Updated"), nil); err != nil {
		t.Fatal(err)
	}
	updated, err := app.store.GetTrackView(id)
	if err != nil || updated.AddedAt != want {
		t.Fatalf("refresh must not change added time: %+v %v", updated, err)
	}

	localID, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "Local", Album: strPtrOrNil("Local album")})
	if err != nil {
		t.Fatal(err)
	}
	local, err := app.store.GetTrackView(localID)
	if err != nil {
		t.Fatal(err)
	}
	album, err = app.GetAlbumDetail(*local.ReleaseID)
	if err != nil || album.Rows[0].AddedAt == nil || *album.Rows[0].AddedAt != local.AddedAt {
		t.Fatalf("local=%+v err=%v", album, err)
	}
}
