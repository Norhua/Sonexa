package app

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

// fixture 摘自 2026-10-14 缓存的实际响应：署名有两个上游部件，歌曲没有 vocal
// 关系，只有编曲及作品的作曲/作词。省略其他曲目，因此仍是 partial 清单。
func TestVocalYouCVCreditFallbackAndArtistViews(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	body, err := os.ReadFile("testdata/vocal_you.json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := musicbrainz.ParseDetailJSON(body, "f0a33d1a-628a-4a53-bc4e-6af077505956")
	if err != nil {
		t.Fatal(err)
	}
	for _, artist := range detail.Artists {
		if artist.Role == "vocal" {
			t.Fatal("provider must not invent vocal evidence")
		}
	}
	id, err := admitDetailForTest(app, detail)
	if err != nil {
		t.Fatal(err)
	}
	// 二次补全也不能累积派生声优或关系。
	for i := 0; i < 2; i++ {
		if err := app.applyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
			t.Fatal(err)
		}
	}
	d, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	var roles []string
	for _, group := range d.Participants {
		roles = append(roles, group.Role)
	}
	if !reflect.DeepEqual(roles, []string{"vocal", "composer", "lyricist", "arranger"}) {
		t.Fatalf("participant order = %v", roles)
	}
	vocal := d.Participants[0].Artists
	if len(vocal) != 1 || vocal[0].Name != "茅野愛衣" || deref(vocal[0].Source) != store.CVCreditSource || vocal[0].Identity == nil || vocal[0].Identity.Ref != "f74f5468-91f9-443b-aedc-c969a75d1678" {
		t.Fatalf("CV vocal/source identity = %+v", vocal)
	}
	if d.CreditText != "霞ヶ丘詩羽(CV.茅野愛衣)" || len(d.CreditSpans) != 1 || d.CreditSpans[0].Text != d.CreditText {
		t.Fatalf("whole CV credit/link lost: %+v", d.CreditSpans)
	}
	artists, err := app.ListArtists()
	if err != nil || len(artists) != 2 {
		t.Fatalf("artist list = %+v, %v", artists, err)
	}
	for _, artist := range artists {
		if artist.Name != d.CreditText && artist.Name != "茅野愛衣" {
			t.Fatalf("unexpected artist: %+v", artist)
		}
		if artist.TrackCount != 1 {
			t.Fatalf("duplicate count: %+v", artist)
		}
		for _, role := range []string{"", store.RoleCredited} {
			tracks, err := app.ListTracks(store.TrackFilter{ArtistID: artist.ID, Role: role})
			if err != nil || len(tracks.Tracks) != 1 || tracks.Tracks[0].ID != id {
				t.Fatalf("artist tracks = %+v, %v", tracks, err)
			}
		}
		header, err := app.GetArtistHeader(artist.ID)
		if err != nil || header.Title != artist.Name {
			t.Fatalf("artist header = %+v, %v", header, err)
		}
	}
	tag, err := app.CreateCollection("CV", "tag", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.AddToCollection(tag, []int64{id}); err != nil {
		t.Fatal(err)
	}
	filtered, err := app.ListArtistsForTag(tag)
	if err != nil || len(filtered) != 2 {
		t.Fatalf("tag artists = %+v, %v", filtered, err)
	}
	var snapshotJSON string
	if err := app.store.DB().QueryRow("SELECT body FROM track_snapshot WHERE track_id=?", id).Scan(&snapshotJSON); err != nil {
		t.Fatal(err)
	}
	var snapshot domain.TrackSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil || snapshot.Roles["vocal"].IsKnown() {
		t.Fatalf("fallback leaked into upstream snapshot: %s, %v", snapshotJSON, err)
	}
}
