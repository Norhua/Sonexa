package covers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestResolverLocalThresholds(t *testing.T) {
	for _, tc := range []struct {
		sizes []int
		usage Usage
		want  int
	}{
		{[]int{250, 500, 1200}, Thumbnail, 250}, {[]int{500, 600, 800, 801, 1200}, Detail, 1200},
		{[]int{600, 800}, Thumbnail, 600}, {[]int{600, 800}, Detail, 800}, {[]int{801}, Detail, 801},
	} {
		t.Run(fmt.Sprint(tc.sizes, tc.usage), func(t *testing.T) {
			svc, st := resolverService(t)
			id, _ := st.FindOrCreateLocalRelease("local", nil)
			key := store.CoverCatalogKey{Source: "unknown", ObjectType: "local", SourceRef: "r"}
			var variants []store.CoverVariant
			for _, size := range tc.sizes {
				variants = append(variants, store.CoverVariant{Spec: fmt.Sprint(size)})
			}
			if _, err := st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "one", PrimaryType: "Front", Variants: variants}}); err != nil {
				t.Fatal(err)
			}
			catalog, _ := st.GetCoverCatalog(key)
			for i, v := range catalog.Images[0].Variants {
				if _, _, err := svc.Library().SaveVariant(id, v.ID, pngBytes(tc.sizes[i], tc.sizes[i]), false, 0); err != nil {
					t.Fatal(err)
				}
			}
			svc.pool.client.Transport = resolverTransport(func(*http.Request) (*http.Response, error) {
				t.Error("本地图不应出网")
				return nil, fmt.Errorf("network")
			})
			got, err := svc.Resolve(context.Background(), Target{ReleaseID: id}, tc.usage)
			if err != nil || got.File.Width != tc.want {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestResolverMissingVariantsAndMedium(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		usage      Usage
		want       int
	}{
		{"original", `{"images":[{"id":"1","image":"https://archive.org/original.png"}]}`, Detail, 1200},
		{"no250", `{"images":[{"id":"1","thumbnails":{"500":"https://archive.org/500.png"}}]}`, Thumbnail, 500},
		{"medium", `{"images":[{"id":"1","types":["Medium"],"thumbnails":{"1200":"https://archive.org/1200.png"}}]}`, Detail, 1200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := resolverService(t)
			svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
				body := []byte(tc.body)
				if strings.HasSuffix(r.URL.Path, ".png") {
					body = pngBytes(tc.want, tc.want)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
			})
			got, err := svc.Resolve(context.Background(), Target{Provider: "musicbrainz", Ref: testReleaseMBID}, tc.usage)
			if err != nil || got.File.Width != tc.want {
				t.Fatalf("%+v %v", got, err)
			}
		})
	}
}

func TestResolverCatalogReopensAndRefreshFailurePreservesSnapshot(t *testing.T) {
	svc, st := resolverService(t)
	calls, mu := map[string]int{}, &sync.Mutex{}
	setResolverResponse(svc, twoSizeIndex, 200, calls, mu)
	if _, err := svc.Catalog(context.Background(), store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}, false); err != nil {
		t.Fatal(err)
	}
	cache, err := NewDiskCache(svc.pool.cache.Dir(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	second := newTestService(cache, 1, "test")
	if err := second.AttachLibrary(st); err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	setResolverResponse(second, `failed`, 503, calls, mu)
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}
	catalog, err := second.Catalog(context.Background(), key, false)
	if err != nil || catalog.State != "found" || calls["/release/"+testReleaseMBID] != 1 {
		t.Fatal("新服务未复用目录")
	}
	if _, err := second.Catalog(context.Background(), key, true); err == nil {
		t.Fatal("刷新故障应上报")
	}
	catalog, err = second.Catalog(context.Background(), key, false)
	if err != nil || len(catalog.Images) != 1 {
		t.Fatal("故障抹掉目录")
	}
}

func TestResolverAdopted800WinsOverAutomatic1200AndMissingFails(t *testing.T) {
	svc, st := resolverService(t)
	source, ref := "musicbrainz", testReleaseMBID
	id, err := st.UpsertRelease(&store.Release{Title: "album", Provider: &source, ProviderRef: &ref, TracklistState: store.TracklistNone})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Library().ImportLocal(id, pngBytes(800, 800), 0); err != nil {
		t.Fatal(err)
	}
	calls, mu := map[string]int{}, &sync.Mutex{}
	setResolverResponse(svc, twoSizeIndex, 200, calls, mu)
	catalog, err := svc.Catalog(context.Background(), store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.Library().SaveVariant(id, catalog.Images[0].Variants[1].ID, pngBytes(1200, 1200), false, 0); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Resolve(context.Background(), Target{ReleaseID: id}, Detail)
	if err != nil || got.File.Width != 800 {
		t.Fatal("用户选择被自动高清图覆盖")
	}
	if err := svc.Library().files.Remove(got.File.RelativePath); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Resolve(context.Background(), Target{ReleaseID: id}, Detail); err == nil {
		t.Fatal("已采用文件丢失应报告故障")
	}
}

func TestEditUpstreamPreviewDoesNotUseOrChangeAdoptedCover(t *testing.T) {
	svc, st := resolverService(t)
	source, ref := "musicbrainz", testReleaseMBID
	id, err := st.UpsertRelease(&store.Release{Title: "album", Provider: &source, ProviderRef: &ref, TracklistState: store.TracklistNone})
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := svc.Library().ImportLocal(id, pngBytes(800, 800), 0)
	if err != nil {
		t.Fatal(err)
	}
	calls, mu := map[string]int{}, &sync.Mutex{}
	setResolverResponse(svc, twoSizeIndex, 200, calls, mu)
	preview, err := svc.Resolve(context.Background(), Target{Provider: source, Ref: ref, Preview: true}, Detail)
	if err != nil || preview.File == nil || preview.File.Width != 1200 {
		t.Fatalf("upstream preview: %+v %v", preview, err)
	}
	selection, err := st.GetCoverSelection(id)
	if err != nil || selection.VariantID != adopted.VariantID || selection.Revision != adopted.Revision {
		t.Fatal("preview changed adoption")
	}
}
