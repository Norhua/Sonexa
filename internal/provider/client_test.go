package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// memCache 是测试用的内存缓存。
type memCache struct {
	mu      sync.Mutex
	entries map[string]*CacheEntry
	puts    int
	gets    int
}

func newMemCache() *memCache {
	return &memCache{entries: map[string]*CacheEntry{}}
}

func (m *memCache) key(p, e, h string) string { return p + "|" + e + "|" + h }

func (m *memCache) GetCache(p, e, h string) (*CacheEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets++
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

// TestRefreshCacheForcesRequest 覆盖用户手动「重新获取」。
func TestRefreshCacheForcesRequest(t *testing.T) {
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
	req.CacheMode = RefreshCache
	c.Get(context.Background(), req)

	if requests != 2 {
		t.Errorf("出网 %d 次, 期望 2 次（RefreshCache 应强制重取）", requests)
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
	// 这里测的是错误字段被带出，不是重试行为——留着重试只会白等 42 秒
	c.Retry.MaxAttempts = 1

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
	c.Retry.MaxAttempts = 1

	c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h",
	})
	if cache.puts != 0 {
		t.Error("失败响应不应落盘")
	}
}

// TestGetRetriesTransient503 覆盖「上游瞬时 503 不应上抛给调用方」：
// MusicBrainz 即便在完全遵守 1 req/s 的前提下也会频繁返回
// `503 … currently busy`，不在传输层吸收掉会让整批导入变成整批失败。
func TestGetRetriesTransient503(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&hits, 1) < 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte(`{"error": "currently busy"}`))
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "ua")
	c.Limiters.Configure("p", 1000, time.Second)
	c.Retry = RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	body, err := c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h1",
	})
	if err != nil {
		t.Fatalf("瞬时 503 应被重试吸收，却失败了: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q，期望重试后的成功响应", body)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("请求次数 = %d，期望 3（两次 503 + 一次成功）", got)
	}
}

// TestGetGivesUpAfterMaxAttempts 覆盖「持续失败仍要上抛」：
// 传输层重试只吸收抖动，不能把一直不可用的上游伪装成可用。
func TestGetGivesUpAfterMaxAttempts(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "ua")
	c.Limiters.Configure("p", 1000, time.Second)
	c.Retry = RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	_, err := c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h1",
	})
	if err == nil {
		t.Fatal("上游持续 503 时应返回错误")
	}
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("err = %v，期望 503 的 HTTPError", err)
	}
	if got := atomic.LoadInt32(&hits); got != 3 {
		t.Errorf("请求次数 = %d，期望恰好 3 次（MaxAttempts）", got)
	}
}

// TestGetDoesNotRetryClientError 覆盖「4xx 不重试」：
// 查询语法错误重试多少次结果都一样，重试只会白白消耗令牌。
func TestGetDoesNotRetryClientError(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "ua")
	c.Limiters.Configure("p", 1000, time.Second)
	c.Retry = RetryPolicy{MaxAttempts: 4, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}

	if _, err := c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h1",
	}); err == nil {
		t.Fatal("400 应作为错误返回")
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("请求次数 = %d，期望 1（4xx 不重试）", got)
	}
}

// TestRetryTakesTokenEachAttempt 覆盖「重试不绕过限流」：
// 重试是抖动的补偿，不是超过上游速率上限的后门。
func TestRetryTakesTokenEachAttempt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "ua")
	// 每 40ms 一个令牌：3 次尝试至少要等掉 2 个令牌
	c.Limiters.Configure("p", 1, 40*time.Millisecond)
	c.Retry = RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond}

	start := time.Now()
	_, _ = c.Get(context.Background(), GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h1",
	})
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Errorf("耗时 %v，重试未取令牌——限流被绕过了", elapsed)
	}
}

// TestGetHonoursContextCancelDuringBackoff 覆盖「退避期间要响应取消」：
// 用户暂停导入时不该被一串退避拖住。
func TestGetHonoursContextCancelDuringBackoff(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := NewClient(NewLimiters(), newMemCache(), "ua")
	c.Limiters.Configure("p", 1000, time.Second)
	c.Retry = RetryPolicy{MaxAttempts: 5, BaseDelay: 10 * time.Second, MaxDelay: time.Minute}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	if _, err := c.Get(ctx, GetRequest{
		Provider: "p", Endpoint: "/x", BaseURL: srv.URL, ParamsHash: "h1",
	}); err == nil {
		t.Fatal("取消后应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("耗时 %v，退避期间没有响应 ctx 取消", elapsed)
	}
}
