package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// memCache 是测试用的内存缓存。
type memCache struct {
	mu      sync.Mutex
	entries map[string]*CacheEntry
	puts    int
}

func newMemCache() *memCache {
	return &memCache{entries: map[string]*CacheEntry{}}
}

func (m *memCache) key(p, e, h string) string { return p + "|" + e + "|" + h }

func (m *memCache) GetCache(p, e, h string) (*CacheEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, ok := m.entries[m.key(p, e, h)]
	if !ok {
		return nil, ErrCacheMiss
	}
	if entry.TTL > 0 && time.Since(entry.FetchedAt) > entry.TTL {
		return nil, ErrCacheMiss
	}
	return entry, nil
}

func (m *memCache) PutCache(p, e, h string, body []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.puts++
	m.entries[m.key(p, e, h)] = &CacheEntry{Body: body, FetchedAt: time.Now(), TTL: ttl}
	return nil
}

// TestClientInjectsUserAgent 覆盖 6.1：
// 请求头携带 Sonexa/<版本> ( <联系方式> ) 格式的标识。
func TestClientInjectsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	ua := UserAgentFor("0.1.0", "test@example.com")
	c := NewClient(NewLimiters(), newMemCache(), ua)
	c.Limiters.Configure("p", 100, time.Second)

	_, err := c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h1",
	})
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}

	if gotUA != "Sonexa/0.1.0 ( test@example.com )" {
		t.Errorf("User-Agent = %q, 期望 Sonexa/0.1.0 ( test@example.com )", gotUA)
	}
	if !strings.HasPrefix(gotUA, "Sonexa/") {
		t.Error("User-Agent 应以应用名与版本开头")
	}
	if !strings.Contains(gotUA, "test@example.com") {
		t.Error("User-Agent 应包含联系方式")
	}
}

// TestCacheHitSkipsRequestAndToken 覆盖 5.7：
// 同参数请求在 TTL 内命中缓存，且不消耗令牌。
func TestCacheHitSkipsRequestAndToken(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{"n":1}`))
	}))
	defer srv.Close()

	cache := newMemCache()
	c := NewClient(NewLimiters(), cache, "test")
	// 极慢的桶：若缓存命中仍取令牌，第二次调用会明显变慢
	c.Limiters.Configure("p", 1, 10*time.Second)

	req := GetRequest{Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "same", TTL: time.Hour}

	body1, err := c.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("首次请求失败: %v", err)
	}

	start := time.Now()
	body2, err := c.Get(context.Background(), req)
	if err != nil {
		t.Fatalf("二次请求失败: %v", err)
	}
	elapsed := time.Since(start)

	if requests != 1 {
		t.Errorf("实际出网 %d 次, 期望 1 次（第二次应命中缓存）", requests)
	}
	if string(body1) != string(body2) {
		t.Error("两次返回内容应相同")
	}
	if elapsed > time.Second {
		t.Errorf("缓存命中耗时 %v——不应消耗令牌", elapsed)
	}
}

// TestCacheStoresBeforeParse 覆盖 D18：原始响应先落盘。
func TestCacheStoresBeforeParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"raw":"response"}`))
	}))
	defer srv.Close()

	cache := newMemCache()
	c := NewClient(NewLimiters(), cache, "test")
	c.Limiters.Configure("p", 100, time.Second)

	if _, err := c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/e", BaseURL: srv.URL, ParamsHash: "h",
	}); err != nil {
		t.Fatalf("请求失败: %v", err)
	}

	if cache.puts != 1 {
		t.Errorf("落盘 %d 次, 期望 1 次", cache.puts)
	}
	entry, err := cache.GetCache("p", "/e", "h")
	if err != nil {
		t.Fatalf("读取缓存失败: %v", err)
	}
	if string(entry.Body) != `{"raw":"response"}` {
		t.Errorf("落盘内容 = %q, 应为原始响应", entry.Body)
	}
}

// TestSkipCacheForcesRequest 覆盖用户手动「重新获取」。
func TestSkipCacheForcesRequest(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "test")
	c.Limiters.Configure("p", 100, time.Second)
	req := GetRequest{Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h", TTL: time.Hour}

	c.Get(context.Background(), req)
	req.SkipCache = true
	c.Get(context.Background(), req)

	if requests != 2 {
		t.Errorf("出网 %d 次, 期望 2 次（SkipCache 应强制重取）", requests)
	}
}

// TestHTTPErrorCarriesRetryAfter 确保 429 的重试提示被带出。
func TestHTTPErrorCarriesRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "test")
	c.Limiters.Configure("p", 100, time.Second)

	_, err := c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h",
	})
	if err == nil {
		t.Fatal("429 应返回错误")
	}

	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("错误类型 = %T, 期望 *HTTPError", err)
	}
	if httpErr.StatusCode != http.StatusTooManyRequests {
		t.Errorf("状态码 = %d", httpErr.StatusCode)
	}
	if httpErr.RetryAfter != 42*time.Second {
		t.Errorf("RetryAfter = %v, 期望 42s", httpErr.RetryAfter)
	}
	if !IsRetryable(err) {
		t.Error("429 应可重试")
	}
}

// TestErrorResponseNotCached 确保失败响应不被当作有效缓存。
func TestErrorResponseNotCached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cache := newMemCache()
	c := NewClient(NewLimiters(), cache, "test")
	c.Limiters.Configure("p", 100, time.Second)

	c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h",
	})
	if cache.puts != 0 {
		t.Error("失败响应不应落盘")
	}
}
