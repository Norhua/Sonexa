package itunes

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

type recordingCache struct {
	entries map[string]*provider.CacheEntry
}

func (c *recordingCache) GetCache(source, endpoint, hash string) (*provider.CacheEntry, error) {
	entry := c.entries[source+endpoint+hash]
	if entry == nil {
		return nil, provider.ErrCacheMiss
	}
	return entry, nil
}
func (c *recordingCache) PutCache(source, endpoint, hash string, body []byte, ttl time.Duration) error {
	c.entries[source+endpoint+hash] = &provider.CacheEntry{Body: body, TTL: ttl, FetchedAt: time.Now()}
	return nil
}

func TestResponsesHaveNoExpiryAndExplicitRefreshBypassesCache(t *testing.T) {
	cache := &recordingCache{entries: map[string]*provider.CacheEntry{}}
	client := provider.NewClient(nil, cache, "test")
	requests := 0
	client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"resultCount":0,"results":[]}`))}, nil
	})
	p := New(client, Config{DefaultCountry: "jp"}, func(map[string]string) string { return "hash" })
	for _, force := range []bool{false, false, true} {
		if _, err := p.SearchTracks(context.Background(), provider.Query{Title: "song", SkipCache: force}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.FetchDetail(context.Background(), provider.Ref{ID: "jp:1", SkipCache: force}); err != nil {
			t.Fatal(err)
		}
	}
	if requests != 4 {
		t.Fatalf("普通命中不出网/显式刷新应出网: %d", requests)
	}
	for key, entry := range cache.entries {
		if entry.TTL != 0 {
			t.Errorf("响应有日期期限: %s %v", key, entry.TTL)
		}
	}
}
