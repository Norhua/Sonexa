package domain

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestFieldWireStates(t *testing.T) {
	for _, tc := range []struct {
		f    Field[string]
		want string
	}{
		{Field[string]{}, `{"state":"unknown"}`},
		{Cleared[string](), `{"state":"empty"}`},
		{Known(""), `{"state":"value","value":""}`},
		{Known("[00:01]歌词"), `{"state":"value","value":"[00:01]歌词"}`},
	} {
		b, err := json.Marshal(tc.f)
		if err != nil || string(b) != tc.want {
			t.Fatalf("%s %v, want %s", b, err, tc.want)
		}
		var got Field[string]
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if got.IsKnown() != tc.f.IsKnown() || got.Value != tc.f.Value {
			t.Fatalf("round trip: %#v", got)
		}
	}
	for _, raw := range []string{`{}`, `{"state":"value"}`, `{"state":"value","value":null}`, `{"state":"empty","value":"x"}`, `{"state":"invalid"}`} {
		var f Field[string]
		if json.Unmarshal([]byte(raw), &f) == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestSnapshotFieldsKeepSeparateMeanings(t *testing.T) {
	s := Snapshot{
		Group:   &GroupSnapshot{FirstReleaseDate: Known("1999")},
		Release: &ReleaseSnapshot{Date: Known("2014"), ReleaseTrackTotal: Known(20), Media: Known([]Medium{{Number: 1, TrackTotal: Known(10)}, {Number: 2, TrackTotal: Known(10)}}), Tracklist: Known([]TracklistEntry{{DiscNumber: 2, TrackNumber: 1}}), TracklistCompleteness: Partial},
		Track:   &TrackSnapshot{Year: Known(2012), DiscNumber: Known(2), TrackNumber: Known(1)},
	}
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var got Snapshot
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Track.Year.Value != 2012 || got.Group.FirstReleaseDate.Value != "1999" || got.Release.ReleaseTrackTotal.Value != 20 || got.Release.Media.Value[1].TrackTotal.Value != 10 || got.Release.TracklistCompleteness != Partial {
		t.Fatalf("semantics changed: %s", b)
	}
	if !reflect.DeepEqual(got.Release.Summary().ReleaseTrackTotal, got.Release.ReleaseTrackTotal) {
		t.Fatal("summary changed total")
	}
}

func TestUnknownAndUserProtection(t *testing.T) {
	fact := Known([]Role{{Artist: Artist{Name: "初音ミク"}, Role: "vocal", Attributes: []string{"background vocals"}}})
	if !reflect.DeepEqual(MergeField(fact, Missing[[]Role]()), fact) {
		t.Fatal("unknown erased known fact")
	}
	user := Current[[]Role]{Field: fact, Mode: User, Source: "user"}
	if !reflect.DeepEqual(user.Sync(Cleared[[]Role](), "musicbrainz"), user) {
		t.Fatal("sync changed user roles")
	}
	follow := Current[[]Role]{Field: fact, Mode: Follow}
	if follow.Sync(Cleared[[]Role](), "musicbrainz").Field.State != Empty {
		t.Fatal("explicit empty did not clear follow")
	}
}

func TestDetailTarget(t *testing.T) {
	for _, target := range []DetailTarget{{}, {Release: Identity{Provider: "qq", Ref: "album"}, Track: &Identity{Provider: "itunes", Ref: "track"}}} {
		if target.Validate() == nil {
			t.Fatal("accepted invalid target")
		}
	}
	if err := (DetailTarget{Release: Identity{Provider: "qq", Ref: "album"}}).Validate(); err != nil {
		t.Fatal(err)
	}
}
