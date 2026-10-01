package itunes

import (
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"testing"
)

func TestCollectionFactsAndPartialSnapshot(t *testing.T) {
	resp := &lookupResponse{Results: []result{{WrapperType: "collection", CollectionID: 1, ArtistName: "Various Artists", TrackCount: 300, Country: "USA"}, {WrapperType: "track", CollectionID: 1, TrackID: 2, ArtistName: "Creator", DiscNumber: 2, TrackNumber: 1, TrackCount: 10}}}
	s := provider.DetailSnapshot(ParseLookup(resp, "jp", "jp:2"))
	if s.Release.Credit.Value.Text != "Various Artists" || s.Track.Credit.Value.Text != "Creator" || s.Release.ReleaseTrackTotal.Value != 300 {
		t.Fatal("collection facts conflated")
	}
	if s.Release.TracklistCompleteness != domain.Partial || s.Release.Country.IsKnown() || s.Release.Media.IsKnown() || s.Track.Roles["vocal"].IsKnown() {
		t.Fatalf("guessed unknown facts: %+v", s)
	}
	if s.Track.Identity.Ref != "jp:2" || s.Release.Identity.Ref != "jp:1" {
		t.Fatal("storefront lost")
	}
	c := candidateFrom(resp.Results[1], "jp")
	if c.AlbumCredit != nil || c.ReleaseTrackTotal != nil {
		t.Fatal("track used as collection facts")
	}
	// A song-only response cannot masquerade as collection metadata.
	s = provider.DetailSnapshot(ParseLookup(&lookupResponse{Results: resp.Results[1:]}, "jp", "jp:2"))
	if s.Release != nil {
		t.Fatal("song treated as collection")
	}
}
