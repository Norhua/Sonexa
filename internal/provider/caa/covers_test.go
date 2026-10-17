package caa

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
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

func TestArtworkReadsRealIndexWithoutArtworkFlag(t *testing.T) {
	strategy := ArtworkStrategy()
	key := domain.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "rel"}
	input, err := strategy.Load(context.Background(), key, provider.ArtworkIO{Fetch: func(context.Context, string) ([]byte, error) {
		return []byte(`{"images":[{"id":1,"front":false,"types":["Medium"],"image":"http://coverartarchive.org/release/rel/disc.png","thumbnails":{"250":"http://coverartarchive.org/release/rel/disc-250.jpg"}}]}`), nil
	}})
	if err != nil || len(input.Images) != 1 || input.Images[0].PrimaryType != "Medium" || len(input.Images[0].Variants) != 2 || strings.Contains(input.Images[0].OriginalURL, "front-") {
		t.Fatal(input, err)
	}
}

func TestCAALadder(t *testing.T) {
	got := CAAFrontLadder("rel-1", "rg-1")
	want := []string{"https://coverartarchive.org/release/rel-1/front-1200", "https://coverartarchive.org/release/rel-1/front-500", "https://coverartarchive.org/release-group/rg-1/front-1200"}
	if len(got) != len(want) {
		t.Fatal(got)
	}
	for i, w := range want {
		if got[i].URL != w {
			t.Fatal(got)
		}
	}
	if CAAThumbnailURL("rel-1") != "https://coverartarchive.org/release/rel-1/front-250" {
		t.Fatal("thumbnail changed")
	}
}
