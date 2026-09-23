package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func coverAdoptionFixture(t *testing.T) (*App, int64, []store.CoverImage) {
	t.Helper()
	a, cleanup := newTestApp(t)
	t.Cleanup(cleanup)
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { a.coverSvc.Close() })
	id, err := a.store.FindOrCreateLocalRelease("album", nil)
	if err != nil {
		t.Fatal(err)
	}
	key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: "fixture"}
	if _, err := a.store.SaveCoverCatalog(key, []store.CoverImage{
		{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "1200", URL: "https://archive.org/front-1200"}, {Spec: "original", URL: "https://archive.org/front-original"}}},
		{SourceImageID: "back", PrimaryType: "Back", Variants: []store.CoverVariant{{Spec: "original", URL: "https://archive.org/back-original"}}},
	}); err != nil {
		t.Fatal(err)
	}
	catalog, err := a.store.GetCoverCatalog(key)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	allowed := map[int64]bool{}
	for _, image := range catalog.Images {
		for _, variant := range image.Variants {
			allowed[variant.ID] = true
		}
	}
	a.coverSessions = map[string]*coverSession{"test": {ctx: ctx, cancel: cancel, releaseID: id, variants: allowed}}
	return a, id, catalog.Images
}

func TestCoverAdoptionFailuresKeepSelectionAndNeverCrossImages(t *testing.T) {
	for _, status := range []int{404, 403, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a, id, images := coverAdoptionFixture(t)
			oldSelection, err := a.coverSvc.Library().ImportLocal(id, coverPNG(t, 800, 600), 0)
			if err != nil {
				t.Fatal(err)
			}
			old := http.DefaultTransport
			defer func() { http.DefaultTransport = old }()
			var calls atomic.Int32
			http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if strings.Contains(r.URL.Path, "back") {
					t.Error("跨图片退档")
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("not an image"))}, nil
			})
			if _, err := a.AdoptCoverCandidate("test", images[0].Variants[0].ID, 1); err == nil {
				t.Fatal("失败返回成功")
			}
			want := int32(1)
			if status == 404 {
				want = 2
			}
			if calls.Load() != want {
				t.Fatalf("只有确认不可用才允许同图退档: calls=%d want=%d", calls.Load(), want)
			}
			selection, err := a.store.GetCoverSelection(id)
			if err != nil || selection.VariantID != oldSelection.VariantID || selection.Revision != 1 {
				t.Fatalf("失败改变了选择: %+v %v", selection, err)
			}
			files, err := a.store.ListReleaseCoverFiles(id)
			if err != nil || len(files) != 1 {
				t.Fatal(files, err)
			}
			if _, err := a.coverSvc.Library().Read(files[0]); err != nil {
				t.Fatal("旧图不可读", err)
			}
		})
	}
}

func TestCoverAdoptionConcurrentRevisionAndDeletedTarget(t *testing.T) {
	a, id, images := coverAdoptionFixture(t)
	var variants []int64
	for _, image := range images {
		v := image.Variants[0]
		if _, _, err := a.coverSvc.Library().SaveVariant(0, v.ID, coverPNG(t, 600, 400), false, 0); err != nil {
			t.Fatal(err)
		}
		variants = append(variants, v.ID)
	}
	var successes, conflicts atomic.Int32
	var wg sync.WaitGroup
	for _, variant := range variants {
		wg.Add(1)
		go func(variant int64) {
			defer wg.Done()
			result, err := a.AdoptCoverCandidate("test", variant, 0)
			if err == nil {
				successes.Add(1)
				if result.Width != 600 || result.Height != 400 {
					t.Error("采用虚报请求规格", result)
				}
			} else if errors.Is(err, store.ErrCoverSelectionChanged) {
				conflicts.Add(1)
			} else {
				t.Error(err)
			}
		}(variant)
	}
	wg.Wait()
	if successes.Load() != 1 || conflicts.Load() != 1 {
		t.Fatal(successes.Load(), conflicts.Load())
	}
	if _, err := a.store.DB().Exec(`DELETE FROM release WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptCoverCandidate("test", variants[0], 1); err == nil || !strings.Contains(err.Error(), "已删除") {
		t.Fatal("目标删除后仍可采用", err)
	}
}

func TestCoverPreviewRejectsInvalidIDsAndUnsafeURLs(t *testing.T) {
	a, _, images := coverAdoptionFixture(t)
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var calls atomic.Int32
	http.DefaultTransport = appCoverTransport(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, fmt.Errorf("unexpected request")
	})
	for _, id := range []string{"-1", "abc", "99999999999", "0"} {
		r := httptest.NewRecorder()
		a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", "/cover?variantId="+id, nil))
		if r.Code != 400 && r.Code != 404 {
			t.Fatal(id, r.Code)
		}
	}
	variant := images[0].Variants[0].ID
	for _, raw := range []string{"http://archive.org/image", "https://127.0.0.1/image", "https://archive.org.evil.test/image", "file:///etc/passwd"} {
		if _, err := a.store.DB().Exec(`UPDATE cover_variant SET url=? WHERE id=?`, raw, variant); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRecorder()
		a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", fmt.Sprintf("/cover?variantId=%d", variant), nil))
		if r.Code != 502 {
			t.Fatal(raw, r.Code)
		}
		if _, err := a.AdoptCoverCandidate("test", variant, 0); err == nil {
			t.Fatal("采用接受了不可信 URL")
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("非法 ID/URL 触发了网络请求: %d", calls.Load())
	}
}
