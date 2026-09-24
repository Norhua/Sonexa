package covers

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

const testReleaseMBID = "2659e6f6-ce21-4198-92e5-4878f5e7e3c0"

type resolverTransport func(*http.Request) (*http.Response, error)

func (f resolverTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func resolverService(t *testing.T) (*Service, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewDiskCache(filepath.Join(dir, "covers"), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(cache, 1, "test")
	if err := svc.AttachLibrary(st); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close(); st.Close() })
	return svc, st
}

func setResolverResponse(svc *Service, body string, indexStatus int, calls map[string]int, mu *sync.Mutex) {
	svc.pool.client.Transport = resolverTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		data, status := []byte(body), indexStatus
		if strings.HasSuffix(r.URL.Path, ".png") {
			status = 200
			size := 250
			if strings.Contains(r.URL.Path, "1200") || strings.Contains(r.URL.Path, "original") {
				size = 1200
			}
			data = pngBytes(size, size)
		}
		return &http.Response{StatusCode: status, Status: fmt.Sprintf("%d", status), Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
}

const twoSizeIndex = `{"images":[{"id":"1","front":true,"types":["Front"],"image":"https://archive.org/original.png","thumbnails":{"250":"https://archive.org/250.png","1200":"https://archive.org/1200.png"}}]}`

func TestResolverUsageSharesIndexNotQuality(t *testing.T) {
	svc, st := resolverService(t)
	var mu sync.Mutex
	calls := map[string]int{}
	setResolverResponse(svc, twoSizeIndex, 200, calls, &mu)
	target := Target{Provider: "musicbrainz", Ref: testReleaseMBID}
	var wg sync.WaitGroup
	for _, usage := range []Usage{Thumbnail, Detail} {
		wg.Add(1)
		go func(usage Usage) {
			defer wg.Done()
			got, err := svc.Resolve(context.Background(), target, usage)
			want := 250
			if usage == Detail {
				want = 1200
			}
			if err != nil || got.File == nil || got.File.Width != want {
				t.Errorf("用途 %s 使用了错误质量: %+v %v", usage, got, err)
			}
		}(usage)
	}
	wg.Wait()
	mu.Lock()
	if calls["/release/"+testReleaseMBID] != 1 || calls["/250.png"] != 1 || calls["/1200.png"] != 1 {
		t.Errorf("目录/图片重复请求: %v", calls)
	}
	mu.Unlock()
	catalog, err := st.GetCoverCatalog(store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID})
	if err != nil || len(catalog.Images[0].Variants) != 3 {
		t.Fatal("没有保存全部档位")
	}
}

func TestResolverNegativeMemoryAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, state string
		status            int
		wantError         bool
	}{{"empty", `{"images":[]}`, "empty", 200, false}, {"404", ``, "empty", 404, false}, {"invalid", `{}`, "error", 200, true}, {"503", `unavailable`, "error", 503, true}} {
		t.Run(tc.name, func(t *testing.T) {
			svc, st := resolverService(t)
			calls, mu := map[string]int{}, &sync.Mutex{}
			setResolverResponse(svc, tc.body, tc.status, calls, mu)
			for range 2 {
				got, err := svc.Resolve(context.Background(), Target{Provider: "musicbrainz", Ref: testReleaseMBID}, Detail)
				if (err != nil) != tc.wantError || (!tc.wantError && got.Outcome != OutcomeEmpty) {
					t.Fatalf("状态错误: %+v %v", got, err)
				}
			}
			catalog, _ := st.GetCoverCatalog(store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID})
			if catalog.State != tc.state || calls["/release/"+testReleaseMBID] != 1 {
				t.Fatalf("空结果或退避没有记忆: %+v %v", catalog, calls)
			}
		})
	}
}

func TestResolverNoMBAndSelectedOverride(t *testing.T) {
	svc, st := resolverService(t)
	svc.pool.client.Transport = resolverTransport(func(*http.Request) (*http.Response, error) {
		t.Error("没有 MB 绑定不应出网")
		return nil, fmt.Errorf("unexpected network")
	})
	for _, target := range []Target{{}, {Provider: "itunes", Ref: "https://is1.mzstatic.com/arbitrary.jpg"}, {Provider: "musicbrainz", Ref: "recording-only"}} {
		got, err := svc.Resolve(context.Background(), target, Thumbnail)
		if err != nil || got.Outcome != OutcomeEmpty {
			t.Fatalf("应为 nil: %+v %v", got, err)
		}
	}
	releaseID, err := st.FindOrCreateLocalRelease("本地专辑", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Library().ImportLocal(releaseID, pngBytes(800, 800), 0); err != nil {
		t.Fatal(err)
	}
	for _, usage := range []Usage{Thumbnail, Detail} {
		got, err := svc.Resolve(context.Background(), Target{ReleaseID: releaseID}, usage)
		if err != nil || got.File.Width != 800 {
			t.Fatalf("用户采用被忽略: %+v %v", got, err)
		}
	}
}

func TestImageURLAllowlist(t *testing.T) {
	for _, raw := range []string{"http://archive.org/image.jpg", "https://127.0.0.1/image", "https://archive.org.evil.test/image", "file:///secret", "https://user:pass@archive.org/image", "https://archive.org:8080/image"} {
		if err := validImageURL(raw); err == nil {
			t.Errorf("接受了不可信图片地址: %s", raw)
		}
	}
}
