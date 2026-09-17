package musicbrainz

import (
	"context"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

// 精简自 2026-09-17 的 CAA 响应；Tetra. 所在发行只有 Medium，没有 Front。
func TestCAAThumbnailIndexMediumFallback(t *testing.T) {
	body, err := os.ReadFile("../../../testdata/golden/responses/musicbrainz/caa_lines.json")
	if err != nil {
		t.Fatal(err)
	}
	urls, handled, err := ParseCAAThumbnailIndex(CoverArtArchiveBaseURL+"/release/2659e6f6-ce21-4198-92e5-4878f5e7e3c0", body)
	if err != nil || !handled || len(urls) != 4 {
		t.Fatalf("urls=%v handled=%v err=%v", urls, handled, err)
	}
	if urls[0] != "https://coverartarchive.org/release/2659e6f6-ce21-4198-92e5-4878f5e7e3c0/37269419555-250.jpg" {
		t.Fatal(urls)
	}
	for _, u := range urls {
		if !strings.HasPrefix(u, "https://") {
			t.Fatal(u)
		}
	}
}

func TestCAAThumbnailIndexPrefersFrontAndRecognizesOnlyIndexes(t *testing.T) {
	body := []byte(`{"images":[{"front":false,"image":"medium"},{"front":true,"image":"front","thumbnails":{"250":"thumb","500":"thumb"}}]}`)
	urls, handled, err := ParseCAAThumbnailIndex(CoverArtArchiveBaseURL+"/release-group/rg", body)
	if !handled || err != nil || !reflect.DeepEqual(urls, []string{"thumb", "front", "medium"}) {
		t.Fatalf("%v %v %v", urls, handled, err)
	}
	for _, u := range []string{CoverArtArchiveBaseURL + "/release/rel/front-250", "https://example.com/release/rel"} {
		if _, handled, _ := ParseCAAThumbnailIndex(u, body); handled {
			t.Fatalf("not an index: %s", u)
		}
	}
	for _, body := range []string{"broken", `{}`, `{"images":null}`} {
		if _, handled, err := ParseCAAThumbnailIndex(CoverArtArchiveBaseURL+"/release/rel", []byte(body)); !handled || err == nil {
			t.Fatalf("invalid index must fail: %s", body)
		}
	}
}

type coverTransport func(*http.Request) (*http.Response, error)

func (f coverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchCoversReadsRealIndexWithoutArtworkFlag(t *testing.T) {
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = coverTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"recordings":[{"id":"rec","title":"Tetra.","releases":[{"id":"rel","title":"LINES","media":[{"position":1,"tracks":[{"id":"track","number":"1","title":"Tetra."}]}]}]}]}`
		if r.URL.Host == "coverartarchive.org" {
			body = `{"images":[{"front":false,"types":["Medium"],"image":"http://coverartarchive.org/release/rel/disc.png","thumbnails":{"250":"http://coverartarchive.org/release/rel/disc-250.jpg"}}]}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	p := New(client, Config{Contact: "test@example.com"}, nil)
	groups, err := p.SearchCovers(context.Background(), provider.Query{Title: "Tetra."})
	if err != nil || len(groups) != 1 {
		t.Fatalf("%+v %v", groups, err)
	}
	if groups[0].Constructed || len(groups[0].Images) != 2 || groups[0].Images[0].Type != "Medium" || strings.Contains(groups[0].Images[0].URL, "front-") {
		t.Fatalf("not actual index: %+v", groups)
	}
}
