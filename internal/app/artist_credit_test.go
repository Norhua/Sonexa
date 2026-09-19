package app

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

func TestStructuredCreditsAndRolesStaySeparate(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	// 真实 JSON 解析入口：曲目与录音的署名不同，带合作连接词与实际角色关系。
	body := []byte(`{"id":"rel","title":"By the Seaside","media":[{"position":1,"tracks":[{
		"id":"trk","position":1,"title":"By the Seaside",
		"artist-credit":[
			{"name":"Kirara Magic","joinphrase":" feat. ","artist":{"id":"kirara","name":"Kirara Magic","type":"Person"}},
			{"name":"Kitsui Akira","artist":{"id":"kitsui","name":"Kitsui Akira","type":"Person"}}],
		"recording":{"id":"rec","artist-credit":[{"name":"incorrect recording credit","artist":{"name":"incorrect recording credit"}}],
		"relations":[
			{"type":"vocal","artist":{"id":"kirara","name":"Kirara Magic","type":"Person"}},
			{"type":"vocal","artist":{"id":"kitsui","name":"Kitsui Akira","type":"Person"}},
			{"type":"performance","work":{"id":"work","relations":[{"type":"composer","artist":{"id":"kirara","name":"Kirara Magic"}}]}}
		]}}]}]}`)
	detail, err := musicbrainz.ParseDetailJSON(body, "trk")
	if err != nil {
		t.Fatal(err)
	}
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "By the Seaside"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
		t.Fatal(err)
	}
	artists, err := app.ListArtists()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range artists {
		names = append(names, a.Name)
		tracks, err := app.store.ListTracks(store.TrackFilter{ArtistID: a.ID})
		if err != nil || len(tracks) != 1 || tracks[0].ID != id {
			t.Fatalf("artist tracks = %+v, %v", tracks, err)
		}
	}
	if !reflect.DeepEqual(names, []string{"Kirara Magic", "Kitsui Akira"}) {
		t.Fatalf("artists = %v", names)
	}
	d, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.CreditText != "Kirara Magic feat. Kitsui Akira" || len(d.CreditSpans) != 2 {
		t.Fatalf("credit = %+v", d)
	}
	if len(d.Participants) != 2 || d.Participants[0].Role != "vocal" || len(d.Participants[0].Artists) != 2 || d.Participants[1].Role != "composer" {
		t.Fatalf("participants = %+v", d.Participants)
	}
	// 刷新后没有角色的响应应移除旧 vocal，不能因空切片而保留旧关联。
	detail.Artists = nil
	if err := app.ApplyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
		t.Fatal(err)
	}
	d, err = app.GetTrackDetail(id)
	if err != nil || len(d.Participants) != 0 {
		t.Fatalf("stale roles = %+v, %v", d, err)
	}
	artists, _ = app.ListArtists()
	if len(artists) != 2 {
		t.Fatalf("credits must remain without vocal: %+v", artists)
	}
}

func TestCVDetailUsesOneArtistWithoutVoiceLookup(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	body, err := os.ReadFile("../../testdata/golden/responses/musicbrainz/release_detail.json")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := musicbrainz.ParseDetailJSON(body, "trk-cd-jp-02")
	if err != nil {
		t.Fatal(err)
	}
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "God knows..."})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
		t.Fatal(err)
	}
	artists, err := app.ListArtists()
	const credit = "涼宮ハルヒ(CV:平野綾)"
	if err != nil || len(artists) != 1 || artists[0].Name != credit {
		t.Fatalf("CV artists = %+v, %v", artists, err)
	}
	d, err := app.GetTrackDetail(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Participants[0].Role != "vocal" || len(d.Participants[0].Artists) != 1 || d.Participants[0].Artists[0].Name != credit {
		t.Fatalf("vocal must target combined CV identity: %+v", d.Participants)
	}
	var count int
	if err := app.store.DB().QueryRow("SELECT COUNT(*) FROM artist_link").Scan(&count); err != nil || count != 0 {
		t.Fatalf("must not derive voice links: %d, %v", count, err)
	}
	for _, name := range []string{"涼宮ハルヒ", "平野綾"} {
		hits, err := app.store.SearchTrackIDs(name, 10)
		if err != nil || len(hits) != 1 || hits[0] != id {
			t.Fatalf("search %s: %v, %v", name, hits, err)
		}
	}
}

func TestCVSplitAcrossUpstreamCreditPartsStaysWhole(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	body := []byte(`{"id":"rel","media":[{"tracks":[{"id":"trk","title":"song",
		"artist-credit":[
			{"name":"角色","joinphrase":" (CV: ","artist":{"id":"character","name":"角色","type":"Character"}},
			{"name":"声优","joinphrase":") feat. ","artist":{"id":"actor","name":"声优","type":"Person"}},
			{"name":"Guest","artist":{"id":"guest","name":"Guest","type":"Person"}}],
		"recording":{"relations":[
			{"type":"vocal","artist":{"id":"actor","name":"声优"}},
			{"type":"vocal","artist":{"id":"guest","name":"Guest"}}]}
	}]}]}`)
	detail, err := musicbrainz.ParseDetailJSON(body, "trk")
	if err != nil {
		t.Fatal(err)
	}
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "song"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
		t.Fatal(err)
	}
	entries, err := app.store.GetTrackCredits(id)
	if err != nil || len(entries) != 2 || deref(entries[0].CreditedName) != "角色 (CV: 声优)" || deref(entries[1].CreditedName) != "Guest" {
		t.Fatalf("split CV identity: %+v, %v", entries, err)
	}
	d, err := app.GetTrackDetail(id)
	if err != nil || len(d.Participants) != 1 || len(d.Participants[0].Artists) != 2 || d.Participants[0].Artists[0].Name != "角色 (CV: 声优)" {
		t.Fatalf("wrong CV participation: %+v, %v", d, err)
	}
}

func TestRecordingCreditDoesNotInventVocal(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	body := []byte(`{"id":"rel","media":[{"tracks":[{"id":"trk","title":"愛言葉","recording":{
		"artist-credit":[{"name":"DECO*27","artist":{"id":"deco","name":"DECO*27"}}],
		"relations":[{"type":"vocal","artist":{"id":"miku","name":"初音ミク"}}]
	}}]}]}`)
	detail, err := musicbrainz.ParseDetailJSON(body, "trk")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Artists) != 1 || detail.Artists[0].Name != "初音ミク" {
		t.Fatalf("invented roles: %+v", detail.Artists)
	}
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "愛言葉"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
		t.Fatal(err)
	}
	artists, _ := app.ListArtists()
	if len(artists) != 2 {
		t.Fatal(artists)
	}
	for _, artist := range artists {
		if artist.Name != "DECO*27" {
			continue
		}
		facets, err := app.store.RoleFacets(artist.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, facet := range facets {
			if facet.Role == "vocal" && facet.Count != 0 {
				t.Fatalf("composer inferred as vocalist: %+v", facets)
			}
		}
	}
}

func TestStructuredBandAndRepeatedCredit(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	for _, name := range []string{"Earth, Wind & Fire", "A feat. B"} {
		response := map[string]any{"id": "rel", "media": []any{map[string]any{"tracks": []any{map[string]any{
			"id": "trk", "title": "song", "artist-credit": []any{map[string]any{"name": name, "artist": map[string]string{"id": "band-" + name, "name": name, "type": "Group"}}},
		}}}}}
		body, _ := json.Marshal(response)
		detail, err := musicbrainz.ParseDetailJSON(body, "trk")
		if err != nil {
			t.Fatal(err)
		}
		id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "song"})
		if err != nil {
			t.Fatal(err)
		}
		if err := app.ApplyDetailToTrack(context.Background(), id, "musicbrainz", detail); err != nil {
			t.Fatal(err)
		}
		entries, err := app.store.GetTrackCredits(id)
		if err != nil || len(entries) != 1 || deref(entries[0].CreditedName) != name {
			t.Fatalf("split explicit band: %+v, %v", entries, err)
		}
	}
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{Title: "repeat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.ApplyDetailToTrack(context.Background(), id, "itunes", &provider.Detail{TrackCredit: strPtrOrNil("A feat. B & A")}); err != nil {
		t.Fatal(err)
	}
	entries, err := app.store.GetTrackCredits(id)
	if err != nil || len(entries) != 3 || entries[0].ArtistID != entries[2].ArtistID {
		t.Fatalf("repeated credit = %+v, %v", entries, err)
	}
}
