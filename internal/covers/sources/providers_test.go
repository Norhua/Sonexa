package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

func TestCAASourceBoundsReleasesAndPreservesImages(t *testing.T) {
	client := provider.NewClient(nil, nil, "test")
	client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("limit") != "20" {
			t.Error("搜索缺少有界限制")
		}
		rows := []string{}
		for i := range 15 {
			rows = append(rows, fmt.Sprintf(`{"id":"recording-%d","title":"song","releases":[{"id":"release-%d","title":"album"},{"id":"release-%d","title":"album"}]}`, i, i, i))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"recordings":[` + strings.Join(rows, ",") + `]}`))}, nil
	})
	mb := musicbrainz.New(client, musicbrainz.Config{Contact: "test@example.com"}, nil)
	refs := []string{}
	catalogs := map[string]*store.CoverCatalog{}
	var mu sync.Mutex
	source := &CAASource{Provider: mb, ReleaseRef: "release-0", Catalog: func(_ context.Context, kind, ref string, force bool) (*store.CoverCatalog, error) {
		if kind != "release" || !force {
			t.Error("目录类型或强制刷新丢失")
		}
		mu.Lock()
		defer mu.Unlock()
		refs = append(refs, ref)
		catalog := &store.CoverCatalog{CoverCatalogKey: store.CoverCatalogKey{Source: "caa", ObjectType: kind, SourceRef: ref}, Images: []store.CoverImage{
			{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250"}, {Spec: "1200"}}},
			{SourceImageID: "back", PrimaryType: "Back", Variants: []store.CoverVariant{{Spec: "original"}}},
		}, ActualReleaseURL: "https://musicbrainz.org/release/actual", Version: 7}
		catalogs[ref] = catalog
		return catalog, nil
	}}
	rows, err := source.Search(context.Background(), provider.Query{Title: "song", SkipCache: true})
	if err != nil || len(refs) != 10 || len(rows) != 10 || !slices.Contains(refs, "release-0") {
		t.Fatalf("发行查询应去重并含固定目标: %v %v", refs, err)
	}
	unique := map[string]bool{}
	for _, ref := range refs {
		if unique[ref] {
			t.Fatal("重复目录请求", ref)
		}
		unique[ref] = true
	}
	for _, row := range rows {
		if row.Validate() != nil || row.Input != nil || row.Existing == nil {
			t.Fatal("CAA 必须返回只读目录引用", row)
		}
		catalog := catalogs[row.Key().SourceRef]
		if catalog.Version != 7 || catalog.ActualReleaseURL != "https://musicbrainz.org/release/actual" || len(catalog.Images) != 2 || len(catalog.Images[0].Variants) != 2 || catalog.Images[1].PrimaryType != "Back" {
			t.Fatal("已登记的图片或目录信息改变", catalog)
		}
	}
}

func TestITunesSourceCountryVariantsAndImageIdentity(t *testing.T) {
	for _, country := range []string{"", "us"} {
		t.Run(country, func(t *testing.T) {
			client := provider.NewClient(nil, nil, "test")
			client.HTTP.Transport = transport(func(r *http.Request) (*http.Response, error) {
				want := country
				if want == "" {
					want = "jp"
				}
				if r.URL.Query().Get("country") != want || r.URL.Query().Get("limit") != "20" {
					t.Error(r.URL)
				}
				body := `{"results":[
					{"trackId":1,"collectionId":12,"collectionName":"album","artworkUrl100":"https://is1.mzstatic.com/first/100x100bb.jpg"},
					{"trackId":2,"collectionId":12,"collectionName":"album","artworkUrl100":"https://is1.mzstatic.com/first/100x100bb.jpg"},
					{"trackId":3,"collectionId":12,"collectionName":"album","artworkUrl100":"https://is1.mzstatic.com/second/100x100bb.jpg"},
					{"trackId":4,"trackName":"other","collectionId":13,"collectionName":"unrelated","artworkUrl100":"https://is1.mzstatic.com/third/100x100bb.jpg"}
				]}`
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			source := &ITunesSource{Provider: itunes.New(client, itunes.Config{DefaultCountry: "jp"}, nil)}
			rows, err := source.Search(context.Background(), provider.Query{Title: "song", Album: "album", Country: country, SkipCache: true})
			if err != nil || len(rows) != 1 || len(rows[0].Input.Images) != 2 {
				t.Fatalf("同图未去重或异图被合并: %+v %v", rows, err)
			}
			for _, image := range rows[0].Input.Images {
				specs := []string{}
				for _, v := range image.Variants {
					specs = append(specs, v.Spec)
					if !strings.Contains(v.URL, "/"+v.Spec+"x"+v.Spec+"bb.jpg") {
						t.Fatalf("档位 %s 仍指向缩略图: %s", v.Spec, v.URL)
					}
					if !v.Constructed || v.Width != nil || v.Height != nil {
						t.Fatal("构造档位被当成实际尺寸")
					}
				}
				if !reflect.DeepEqual(specs, []string{"3000", "1200", "600", "250", "100"}) {
					t.Fatal(specs)
				}
			}
		})
	}
}
