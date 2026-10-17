package app

import (
	"context"
	"encoding/json"
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
	"github.com/norhua/sonexa/internal/lyrics"
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
	// CAA and images must remain usable while both MB and QQ API budgets are exhausted.
	a.limiters.Configure(provider.QQQuota, 1, time.Hour)
	if err := a.limiters.Wait(context.Background(), provider.QQQuota); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := a.coverSvc.Catalog(ctx, store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: uuid.NewString()}, true); err != nil || calls.Load() != 1 {
		t.Fatalf("CAA 目录错误消费 MB 元数据配额: %v calls=%d", err, calls.Load())
	}
	data := coverPNG(t, 250, 250)
	http.DefaultTransport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
	})
	for _, source := range []string{"caa", "qq"} {
		host := "archive.org"
		if source == "qq" {
			host = "y.qq.com"
		}
		key := store.CoverCatalogKey{Source: source, ObjectType: "album", SourceRef: "image"}
		if _, err := a.store.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "front", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250", URL: "https://" + host + "/image.png"}}}}); err != nil {
			t.Fatal(err)
		}
		catalog, err := a.store.GetCoverCatalog(key)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.coverSvc.DownloadVariant(ctx, catalog.Images[0].Variants[0]); err != nil {
			t.Fatal("图片占用了 API 配额", err)
		}
	}
	if calls.Load() != 3 {
		t.Fatal("图片未实际请求", calls.Load())
	}
}

type quotaArrival struct {
	kind string
	at   time.Time
}

func TestQQThreeCapabilitiesShareHTTPQuotaIncludingRetryAndFallback(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	defer a.coverSvc.Close()
	// Shorten wall time only: exercise the real shared bucket and production adapters.
	const interval = 80 * time.Millisecond
	a.limiters.Configure(provider.QQQuota, 1, interval)
	a.client.Retry = provider.RetryPolicy{MaxAttempts: 2, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}
	var mu sync.Mutex
	arrivals := []quotaArrival{}
	failed := false
	transport := appCoverTransport(func(r *http.Request) (*http.Response, error) {
		kind := "fallback"
		if r.Body != nil {
			var payload struct {
				Req struct {
					Method string
					Param  struct {
						Num int `json:"num_per_page"`
					}
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				return nil, err
			}
			kind = fmt.Sprint(payload.Req.Param.Num)
			if payload.Req.Method == "GetPlayLyricInfo" {
				kind = "lyrics-fetch"
			}
		}
		mu.Lock()
		arrivals = append(arrivals, quotaArrival{kind, time.Now()})
		retry := kind == "30" && !failed
		if retry {
			failed = true
		}
		mu.Unlock()
		status, body := 200, `{"code":0,"req":{"code":0,"data":{"body":{"song":{"list":[]}}}}}`
		if retry {
			status = 503
			body = "temporary"
		} else if kind == "lyrics-fetch" {
			body = `{"code":0,"req":{"code":0,"data":{"lyric":""}},"plain":{"code":0,"data":{"lyric":""}}}`
		} else if kind == "fallback" {
			body = `{"code":0,"lyric":"[00:01]歌词回退"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	a.client.HTTP.Transport = transport
	// A fresh composition must not reconfigure or refill the existing quota.
	fresh, err := assembleUpstreams(a.settings.Get(), a.limiters, a.store, a.coverSvc, "test")
	if err != nil {
		t.Fatal(err)
	}
	fresh.client.HTTP.Transport = transport
	fresh.client.Retry = a.client.Retry
	metadata, _ := a.registry.Get(provider.QQMusic)
	cover, _ := fresh.covers.Get(provider.QQMusic)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, work := range []func() error{
		func() error {
			_, err := metadata.SearchTracks(ctx, provider.Query{Title: "song", SkipCache: true})
			return err
		},
		func() error { _, err := cover.SearchCovers(ctx, sources.Query{Title: "song"}); return err },
		func() error { _, err := a.lyricSvc.Search(ctx, "qq", lyrics.Query{Title: "song"}); return err },
		func() error {
			doc, err := fresh.lyrics.Fetch(ctx, lyrics.Candidate{Source: "qq", Ref: "0039MnYb0qxYhV"})
			if err == nil && doc.Text != "[00:01]歌词回退" {
				return fmt.Errorf("回退歌词丢失")
			}
			return err
		},
	} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if err := work(); err != nil {
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(arrivals) != 6 {
		t.Fatalf("请求数=%d，预期三搜索、重试、正文与回退: %v", len(arrivals), arrivals)
	}
	counts := map[string]int{}
	for i, arrival := range arrivals {
		counts[arrival.kind]++
		if i > 0 && arrival.at.Sub(arrivals[i-1].at) < interval-15*time.Millisecond {
			t.Fatalf("合计 HTTP 速率绕过共享桶: %v", arrivals)
		}
	}
	for kind, want := range map[string]int{"25": 1, "30": 2, "20": 1, "lyrics-fetch": 1, "fallback": 1} {
		if counts[kind] != want {
			t.Fatalf("%s 请求数=%d want=%d", kind, counts[kind], want)
		}
	}
	// Another newly constructed service cannot receive an immediate extra token.
	next, err := assembleUpstreams(a.settings.Get(), a.limiters, a.store, a.coverSvc, "test")
	if err != nil {
		t.Fatal(err)
	}
	next.client.HTTP.Transport = transport
	short, stop := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer stop()
	if _, err := next.lyrics.Search(short, "qq", lyrics.Query{Title: "new"}); err == nil || len(arrivals) != 6 {
		t.Fatal("新实例取得额外突发额度", err, arrivals)
	}
}

func TestPlatformQuotasIndependentAndNetEaseCapabilitiesShared(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	defer a.coverSvc.Close()
	a.limiters.Configure(provider.QQQuota, 1, time.Hour)
	if err := a.limiters.Wait(context.Background(), provider.QQQuota); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	arrivals := []quotaArrival{}
	a.client.HTTP.Transport = appCoverTransport(func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		arrivals = append(arrivals, quotaArrival{r.URL.Host, time.Now()})
		mu.Unlock()
		body := `{"code":200,"result":{"songCount":0,"songs":[]}}`
		if r.URL.Host == "mobileservice.kugou.com" {
			body = `{"status":1,"data":{"info":[]}}`
		}
		if r.URL.Host == "search.kuwo.cn" {
			body = `{"TOTAL":"0","abslist":[]}`
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	cover, _ := a.coverSources.Get("163")
	if _, err := cover.SearchCovers(ctx, sources.Query{Title: "song"}); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"kugou", "kuwo", "163"} {
		if _, err := a.lyricSvc.Search(ctx, source, lyrics.Query{Title: "song"}); err != nil {
			t.Fatal(source, err)
		}
	}
	if len(arrivals) != 4 || arrivals[3].at.Sub(arrivals[0].at) < 950*time.Millisecond {
		t.Fatal("网易云两能力未共用每秒一次配额", arrivals)
	}
	if arrivals[2].at.Sub(arrivals[0].at) > 750*time.Millisecond {
		t.Fatal("其他平台被 QQ/网易云配额阻塞", arrivals)
	}
	short, stop := context.WithTimeout(ctx, time.Millisecond)
	defer stop()
	if _, err := a.lyricSvc.Search(short, "qq", lyrics.Query{Title: "song"}); err == nil {
		t.Fatal("QQ 配额未保持耗尽", err)
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
