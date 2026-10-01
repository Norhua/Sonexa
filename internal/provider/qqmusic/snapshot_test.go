package qqmusic

import (
	"encoding/json"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"testing"
)

func TestIndependentRolesAndSnapshot(t *testing.T) {
	d := detailFromFixture(t, "detail_world.json", "000BDJvT2aWQYZ", "003RcjkV1XjQQy")
	s := provider.DetailSnapshot(d)
	if s.Release.Credit.Value.Text != "supercell" || s.Release.ReleaseTrackTotal.Value != 12 || len(s.Track.Roles["album_artist"].Value) > 0 {
		t.Fatalf("album artist leaked: %+v", s)
	}
	var info []infoEntry
	if err := json.Unmarshal([]byte(`[{"title":"演唱者","type":"irrelevant","content":[{"value":"初音未来","mid":"miku-id"}]},{"title":"合声","content":[{"value":"初音未来","mid":"miku-id"}]}]`), &info); err != nil {
		t.Fatal(err)
	}
	d.Artists = nil
	d.TrackCredit = strPtr("创作者")
	applyInfo(d, info)
	s = provider.DetailSnapshot(d)
	v := s.Track.Roles["vocal"].Value
	if len(v) != 2 || v[1].Attributes[0] != "合声" || v[1].Artist.Identity.Ref != "miku-id" || s.Track.Credit.Value.Text != "创作者" {
		t.Fatalf("roles: %+v", v)
	}
	d.Artists = nil
	s = provider.DetailSnapshot(d)
	if s.Track.Roles["vocal"].IsKnown() {
		t.Fatal("empty response inferred empty vocals")
	}
	if s.Release.TracklistCompleteness != domain.Complete {
		t.Fatal("known complete fixture is partial")
	}
}
