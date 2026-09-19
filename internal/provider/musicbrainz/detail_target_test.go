package musicbrainz

import "testing"

func TestDetailRequiresExactTrackTarget(t *testing.T) {
	// Disc/track positions repeat across media; only track MBID identifies the
	// target. The same recording on another track is not an identity fallback.
	body := []byte(`{"id":"release","title":"Album","artist-credit":[{"name":"Album Artist"}],"media":[
	 {"position":1,"format":"CD","track-count":2,"tracks":[
	  {"id":"first","position":1,"number":"1","title":"First","recording":{"id":"shared"}},
	  {"id":"second","position":2,"number":"2","title":"Second"}]},
	 {"position":2,"format":"CD","track-count":1,"tracks":[
	  {"id":"target","position":1,"number":"A1","title":"Target","recording":{"id":"shared"}}]}]}`)
	for _, ref := range []string{"first", "second", "target", ""} {
		t.Run("ref="+ref, func(t *testing.T) {
			d, err := ParseDetailJSON(body, ref)
			if err != nil {
				t.Fatal(err)
			}
			if d.TrackTotal == nil || *d.TrackTotal != 3 || d.DiscTotal == nil || *d.DiscTotal != 2 || len(d.Tracklist) != 3 {
				t.Fatalf("incomplete release totals/list: %+v", d)
			}
			if d.MatchRef != ref {
				t.Fatalf("matched %q, want %q", d.MatchRef, ref)
			}
			if ref == "" && (d.Title != nil || d.TrackCredit != nil || d.TrackNumber != nil || d.DiscNumber != nil || d.RecordingRef != nil || len(d.Artists) != 0) {
				t.Fatalf("release-only lookup leaked first-track fields: %+v", d)
			}
			if ref == "target" && (d.Title == nil || *d.Title != "Target" || *d.DiscNumber != 2 || *d.TrackNumber != 1 || *d.TrackNumberText != "A1") {
				t.Fatalf("wrong disc/track: %+v", d)
			}
		})
	}
	for _, ref := range []string{"missing", "shared", "shared@release"} {
		if _, err := ParseDetailJSON(body, ref); err == nil {
			t.Errorf("unresolved track %q should fail", ref)
		}
	}
}
