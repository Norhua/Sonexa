package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCacheModesAccountForReadsAndWrites(t *testing.T) {
	for _, tc := range []struct {
		mode                    CacheMode
		reads, writes, requests int
		body                    string
	}{
		{UseCache, 1, 0, 0, "cached"},
		{RefreshCache, 0, 1, 1, "fresh"},
		{NoStore, 0, 0, 1, "fresh"},
	} {
		t.Run(tc.body+string(rune('0'+tc.mode)), func(t *testing.T) {
			requests := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++; _, _ = w.Write([]byte("fresh")) }))
			defer srv.Close()
			cache := newMemCache()
			if err := cache.PutCache("qq", "/search", "body-hash", []byte("cached"), 0); err != nil {
				t.Fatal(err)
			}
			cache.puts = 0
			c := NewClient(nil, cache, "test")
			body, err := c.Get(context.Background(), GetRequest{Provider: "qq", Endpoint: "/search", BaseURL: srv.URL, ParamsHash: "body-hash", Body: []byte(`{}`), CacheMode: tc.mode})
			if err != nil || string(body) != tc.body || requests != tc.requests || cache.gets != tc.reads || cache.puts != tc.writes {
				t.Fatalf("body=%s err=%v requests=%d reads=%d writes=%d", body, err, requests, cache.gets, cache.puts)
			}
			if tc.mode == NoStore && string(cache.entries[cache.key("qq", "/search", "body-hash")].Body) != "cached" {
				t.Fatal("NoStore 改写既有缓存")
			}
		})
	}
}

func TestRefreshMappingAndInvalidCacheMode(t *testing.T) {
	if CacheModeForRefresh(false) != UseCache || CacheModeForRefresh(true) != RefreshCache {
		t.Fatal("公开刷新含义改变")
	}
	cache := newMemCache()
	client := NewClient(NewLimiters(), cache, "test")
	client.Limiters.Configure("qq", 1, time.Hour)
	if _, err := client.Get(context.Background(), GetRequest{CacheMode: CacheMode(255), ParamsHash: "hash"}); err == nil {
		t.Fatal("未知缓存模式被接受")
	}
	if cache.gets != 0 || cache.puts != 0 {
		t.Fatal("非法模式触碰缓存")
	}
}
