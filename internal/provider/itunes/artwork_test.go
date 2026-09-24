package itunes

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

func TestArtworkURLResizesConstructedPathsOnly(t *testing.T) {
	for _, original := range []string{"30x30", "60x60", "100x100", "250x250", "600x600", "1200x1200", "3000x3000"} {
		base := "https://is1-ssl.mzstatic.com/image/100x100/a%2Fb/" + original + "bb.jpg?token=100x100#image"
		want := "https://is1-ssl.mzstatic.com/image/100x100/a%2Fb/1200x1200bb.jpg?token=100x100#image"
		if got := ArtworkURL(base, 1200); got != want {
			t.Errorf("%s => %s, want %s", original, got, want)
		}
	}
	for _, raw := range []string{"https://is1.mzstatic.com/100x100/photo.jpg", "https://is1.mzstatic.com/photo.jpg?size=100x100", ":invalid"} {
		if got := ArtworkURL(raw, 1200); got != raw {
			t.Errorf("unexpected rewrite: %s => %s", raw, got)
		}
	}
}

func TestLookupPreservesArtworkWithoutInventedRoles(t *testing.T) {
	var resp lookupResponse
	loadFixture(t, "lookup_side_c.json", &resp)
	d := ParseLookup(&resp, "jp", "jp:1475065334")
	if d.ArtworkURL == nil || !strings.HasSuffix(*d.ArtworkURL, "/100x100bb.jpg") || d.MatchRef != "jp:1475065334" || len(d.Tracklist) != 4 {
		t.Fatalf("lost artwork or target: %+v", d)
	}
	if len(d.Artists) != 0 || d.Country != nil || d.Label != nil || d.PrimaryType != nil {
		t.Fatalf("invented unsupported metadata: %+v", d)
	}
	resp.Results[0].ArtworkURL60, resp.Results[0].ArtworkURL100 = "https://is1.mzstatic.com/60x60bb.jpg", ""
	if d := ParseLookup(&resp, "jp", ""); d.ArtworkURL == nil || *d.ArtworkURL != resp.Results[0].ArtworkURL60 {
		t.Fatal("lost provided 60px artwork")
	}
	resp.Results[0].ArtworkURL60 = ""
	resp.Results[1].ArtworkURL100 = "https://is1.mzstatic.com/track/100x100bb.jpg"
	if d := ParseLookup(&resp, "jp", ""); d.ArtworkURL == nil || *d.ArtworkURL != resp.Results[1].ArtworkURL100 {
		t.Fatal("lost same-album track artwork")
	}
	resp.Results[1].CollectionID = 42
	if d := ParseLookup(&resp, "jp", ""); d.ArtworkURL != nil {
		t.Fatal("used artwork from another album")
	}
}

func TestSearchFallsBackToProvidedArtwork60(t *testing.T) {
	c := candidateFrom(result{TrackID: 1, CollectionID: 2, ArtworkURL60: "https://is1.mzstatic.com/60x60bb.jpg"}, "jp")
	if !c.HasCover || c.ThumbURL == nil || *c.ThumbURL != "https://is1.mzstatic.com/250x250bb.jpg" {
		t.Fatalf("lost provided artwork: %+v", c)
	}
}

func TestLookupEmptyAndMalformedAreDifferent(t *testing.T) {
	for _, body := range []string{
		`{"results":[]}`, `{}`, `{"results":null}`,
		`{"results":[{"wrapperType":"track","collectionId":1475065332,"trackId":1475065334}]}`,
		`{"results":[{"wrapperType":"collection","collectionId":42}]}`,
	} {
		p := New(testClient(""), Config{}, nil)
		p.client.HTTP.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		d, err := p.FetchDetail(context.Background(), provider.Ref{ID: "jp:1475065332"})
		if body == `{"results":[]}` {
			if err != nil || d.ReleaseRef == nil || *d.ReleaseRef != "jp:1475065332" || d.ArtworkURL != nil {
				t.Fatalf("valid empty: %+v %v", d, err)
			}
		} else if err == nil {
			t.Fatalf("malformed response accepted: %s", body)
		}
	}
}
