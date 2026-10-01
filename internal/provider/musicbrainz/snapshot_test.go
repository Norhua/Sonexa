package musicbrainz

import (
	"encoding/json"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"os"
	"testing"
)

func TestResearchReleaseSnapshots(t *testing.T) {
	for _, tc := range []struct {
		ref       string
		total     int
		exclusive string
		position  int
	}{
		{"0bcd51de-dabe-4106-9118-716f67fef4db", 14, "", 0},
		{"8c4d9698-bcac-456a-a5ac-1bd90d64c4ff", 15, "White Love", 6},
		{"bb08c318-b5c2-426e-9c43-58c520ae116c", 14, "", 0},
		{"d642286a-4ff9-476a-8708-116176151f97", 15, "深愛", 13},
	} {
		t.Run(tc.ref, func(t *testing.T) {
			b, err := os.ReadFile("testdata/" + tc.ref + ".json")
			if err != nil {
				t.Fatal(err)
			}
			d, err := ParseDetailJSON(b, "")
			if err != nil {
				t.Fatal(err)
			}
			s := provider.DetailSnapshot(d)
			if s.Track != nil || s.Release.ReleaseTrackTotal.Value != tc.total || s.Release.TracklistCompleteness != domain.Complete || s.Release.Media.Value[0].TrackTotal.Value != tc.total {
				t.Fatalf("wrong snapshot: %+v", s.Release)
			}
			if tc.exclusive != "" && s.Release.Tracklist.Value[tc.position-1].Title.Value != tc.exclusive {
				t.Fatalf("exclusive song position: %+v", s.Release.Tracklist.Value[tc.position-1])
			}
			if !s.Release.Credit.IsKnown() {
				t.Fatal("missing release credit")
			}
		})
	}
}

func TestSnapshotCompleteFactsAndVocalAttributes(t *testing.T) {
	raw := []byte(`{"id":"release","title":"Album","barcode":"0012345678905","artist-credit":[{"name":"Album Artist"}],"release-group":{"id":"group","title":"Group","artist-credit":[{"name":"Group Artist"}]},"label-info":[{"catalog-number":"A","label":{"name":"One"}},{"catalog-number":"B","label":{"name":"Two"}}],"media":[{"position":1,"track-count":1,"tracks":[{"id":"track","position":1,"title":"Song","artist-credit":[{"name":"DECO*27"}],"recording":{"id":"recording","isrcs":["ISRC1","ISRC2"],"relations":[{"type":"vocal","attributes":["guest","background vocals"],"artist":{"id":"miku","name":"初音ミク"}},{"type":"vocal","attributes":["lead vocals"],"artist":{"id":"miku","name":"初音ミク"}},{"type":"performance","work":{"id":"work","iswcs":["ISWC1","ISWC2"]}}]}}]}]}`)
	d, err := ParseDetailJSON(raw, "track")
	if err != nil {
		t.Fatal(err)
	}
	s := provider.DetailSnapshot(d)
	if s.Group.Credit.Value.Text != "Group Artist" || s.Release.Credit.Value.Text != "Album Artist" || s.Track.Credit.Value.Text != "DECO*27" {
		t.Fatal("credits conflated")
	}
	if len(s.Release.Labels.Value) != 2 || len(s.Track.Identifiers.Value) != 4 || s.Release.Identifiers.Value[0].Value != "0012345678905" {
		t.Fatal("facts lost")
	}
	vocals := s.Track.Roles["vocal"].Value
	if len(vocals) != 2 || len(vocals[0].Attributes) != 2 || vocals[0].Artist.Identity.Ref != "miku" {
		t.Fatalf("vocal attributes lost: %+v", vocals)
	}
	var rel release
	if err := json.Unmarshal(raw, &rel); err != nil {
		t.Fatal(err)
	}
	rel.Media[0].Tracks[0].Recording.Relations = nil
	rel.Media[0].Tracks[0].ArtistCredit = []artistCredit{{Name: "Creator feat. Singer"}}
	s = provider.DetailSnapshot(ParseDetail(&rel, "track"))
	if s.Track.Roles["vocal"].IsKnown() {
		t.Fatal("credit inferred as vocals")
	}
	voice := ParseVoiceActor(&artist{Relations: []relation{{Type: "voice", Artist: &artist{Name: "Actor"}}}})
	if voice == nil || voice.Role != "voice" {
		t.Fatal("voice actor is not vocal evidence")
	}
}

func TestSearchDoesNotConfuseMediumTotalWithReleaseTotal(t *testing.T) {
	c := buildCandidate(recording{}, release{MediumCount: 2, Media: []medium{{Position: 2, TrackCount: 10, SearchTracks: []track{{ID: "track"}}}}}, medium{Position: 2, TrackCount: 10}, track{ID: "track"}, "")
	if c.ReleaseTrackTotal != nil || c.MediumTrackTotal == nil || *c.MediumTrackTotal != 10 {
		t.Fatalf("wrong totals: %+v", c)
	}
}
