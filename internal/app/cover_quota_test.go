package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func TestCoverSearchSharesMetadataQuotaButCAADirectoryDoesNot(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var calls atomic.Int32
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		body := `{"images":[]}`
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	for _, source := range []struct {
		cover    string
		metadata provider.ProviderID
	}{{"caa", provider.MusicBrainz}, {"itunes", provider.ITunes}} {
		// 模拟元数据请求耗尽同一进程桶；封面搜索不能另建桶绕过。
		a.limiters.Configure(source.metadata, 1, time.Hour)
		if err := a.limiters.Wait(context.Background(), source.metadata); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		adapter, err := a.coverSources.Get(provider.ProviderID(source.cover))
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.searchCoverSource(ctx, adapter, sources.Query{Title: "song", Force: true})
		cancel()
		if err == nil || calls.Load() != 0 {
			t.Fatalf("%s 搜索绕过了元数据配额: %v calls=%d", source.cover, err, calls.Load())
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := a.coverSvc.Catalog(ctx, store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: uuid.NewString()}, true); err != nil || calls.Load() != 1 {
		t.Fatalf("CAA 目录错误消费 MB 元数据配额: %v calls=%d", err, calls.Load())
	}
}

func TestCoverPreviewAdoptionAndAutomaticImagesSharePool(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	defer a.coverSvc.Close()
	a.coverSvc.Pool().SetConcurrency(1)
	id, err := a.store.FindOrCreateLocalRelease("target", nil)
	if err != nil {
		t.Fatal(err)
	}
	variants := []store.CoverVariant{}
	for i, source := range []string{"caa", "itunes", "qq", "163"} {
		host := []string{"archive.org", "is1.mzstatic.com", "y.qq.com", "p1.music.126.net"}[i]
		key := store.CoverCatalogKey{Source: source, ObjectType: "album", SourceRef: "test"}
		if _, err := a.store.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250", URL: "https://" + host + "/image.png"}}}}); err != nil {
			t.Fatal(err)
		}
		catalog, err := a.store.GetCoverCatalog(key)
		if err != nil {
			t.Fatal(err)
		}
		variants = append(variants, catalog.Images[0].Variants[0])
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	a.coverSessions = map[string]*coverSession{"test": {ctx: ctx, cancel: cancel, releaseID: id, variants: map[int64]bool{variants[3].ID: true}}}
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	var active, peak, total atomic.Int32
	data := coverPNG(t, 250, 250)
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		n := active.Add(1)
		defer active.Add(-1)
		total.Add(1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		time.Sleep(10 * time.Millisecond)
		body := data
		if strings.HasPrefix(r.URL.Path, "/release/") {
			body = []byte(`{"images":[]}`)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(body)))}, nil
	})
	var wg sync.WaitGroup
	for _, variant := range variants[:3] {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			r := httptest.NewRecorder()
			a.coverAssetHandler().ServeHTTP(r, httptest.NewRequest("GET", fmt.Sprintf("/cover?variantId=%d", id), nil).WithContext(ctx))
			if r.Code != 200 {
				t.Errorf("预览失败: %d %s", r.Code, r.Body.String())
			}
		}(variant.ID)
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := a.AdoptCoverCandidate("test", variants[3].ID, 0); err != nil {
			t.Error(err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := a.coverSvc.Resolve(ctx, covers.Target{Provider: "musicbrainz", Ref: uuid.NewString()}, covers.Thumbnail); err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
	if peak.Load() != 1 || total.Load() != 5 {
		t.Fatalf("共享封面并发失效: peak=%d requests=%d", peak.Load(), total.Load())
	}
}
