package covers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestCache(t *testing.T) *DiskCache {
	t.Helper()
	c, err := NewDiskCache(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("建立缓存失败: %v", err)
	}
	return c
}

// TestPoolRespectsConcurrency 覆盖 9.1：并发度受配置约束。
func TestPoolRespectsConcurrency(t *testing.T) {
	var active, maxActive int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt32(&active, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if cur <= old || atomic.CompareAndSwapInt32(&maxActive, old, cur) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		atomic.AddInt32(&active, -1)
		w.Write(jpegBytes())
	}))
	defer srv.Close()

	pool := NewPool(newTestCache(t), 4, "test")

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			pool.Fetch(context.Background(), Fetcher{
				Key:  fmt.Sprintf("key-%d", n),
				URLs: []string{fmt.Sprintf("%s/img-%d", srv.URL, n)},
			})
		}(i)
	}
	wg.Wait()

	if maxActive > 4 {
		t.Errorf("最大并发 = %d, 期望不超过 4", maxActive)
	}
}

// TestConcurrencyCappedAtMax 覆盖 9.1：并发上限被截断为 16。
func TestConcurrencyCappedAtMax(t *testing.T) {
	pool := NewPool(newTestCache(t), 100, "test")
	if pool.Concurrency() != MaxConcurrency {
		t.Errorf("并发 = %d, 期望截断为 %d", pool.Concurrency(), MaxConcurrency)
	}

	pool.SetConcurrency(8)
	if pool.Concurrency() != 8 {
		t.Errorf("调整后并发 = %d, 期望 8", pool.Concurrency())
	}
}

// TestSingleflightCoalesces 覆盖 9.2：
// 同一 release 出现在 3 行时只发出一次获取。
func TestSingleflightCoalesces(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		time.Sleep(50 * time.Millisecond) // 制造重叠窗口
		w.Write(jpegBytes())
	}))
	defer srv.Close()

	pool := NewPool(newTestCache(t), 8, "test")
	key := CacheKey("rel-1", 250)

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool.Fetch(context.Background(), Fetcher{Key: key, URLs: []string{srv.URL + "/front"}})
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Errorf("实际获取 %d 次, 期望 1 次（同一 release 应合并）", got)
	}
}

// TestFallbackLadder 覆盖 9.4：404 优雅降级。
func TestFallbackLadder(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 只有 front-500 存在，front-1200 返回 404
		if r.URL.Path == "/release/rel/front-500" {
			w.Write(jpegBytes())
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	pool := NewPool(newTestCache(t), 8, "test")
	data, err := pool.Fetch(context.Background(), Fetcher{
		Key: "k",
		URLs: []string{
			srv.URL + "/release/rel/front-1200", // 404
			srv.URL + "/release/rel/front-500",  // 命中
		},
	})
	if err != nil {
		t.Fatalf("降级链应命中 front-500: %v", err)
	}
	if len(data) == 0 {
		t.Error("应取得 front-500 的字节")
	}
}

// TestAllMissingReturnsNotFound 覆盖 9.4：全部不可得时优雅降级到占位图。
func TestAllMissingReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	pool := NewPool(newTestCache(t), 8, "test")
	_, err := pool.Fetch(context.Background(), Fetcher{
		Key:  "k",
		URLs: []string{srv.URL + "/a", srv.URL + "/b"},
	})
	if err != ErrNotFound {
		t.Errorf("全部 404 时 = %v, 期望 ErrNotFound", err)
	}
}

// TestSingleFailureIsolated 覆盖 9.4 与「单张失败只影响该行」。
func TestSingleFailureIsolated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			http.NotFound(w, r)
			return
		}
		w.Write(jpegBytes())
	}))
	defer srv.Close()

	pool := NewPool(newTestCache(t), 8, "test")

	_, badErr := pool.Fetch(context.Background(), Fetcher{Key: "bad", URLs: []string{srv.URL + "/bad"}})
	goodData, goodErr := pool.Fetch(context.Background(), Fetcher{Key: "good", URLs: []string{srv.URL + "/good"}})

	if badErr != ErrNotFound {
		t.Errorf("失败行 = %v, 期望 ErrNotFound", badErr)
	}
	if goodErr != nil || len(goodData) == 0 {
		t.Error("失败行不应影响其他行的获取")
	}
}

// TestDiskCacheLRUEviction 覆盖 9.7：超出容量上限时按 LRU 淘汰。
func TestDiskCacheLRUEviction(t *testing.T) {
	// 容量上限设为约 2.5 个文件大小
	img := jpegBytes()
	cache, err := NewDiskCache(t.TempDir(), int64(len(img)*2+len(img)/2), 0)
	if err != nil {
		t.Fatalf("建立缓存失败: %v", err)
	}

	cache.Put("a", img)
	time.Sleep(5 * time.Millisecond)
	cache.Put("b", img)
	time.Sleep(5 * time.Millisecond)
	// 访问 a，使其成为最近使用
	cache.Get("a")
	time.Sleep(5 * time.Millisecond)
	cache.Put("c", img) // 触发淘汰

	// b 最久未使用，应被淘汰；a 与 c 保留
	if cache.Get("b") != nil {
		t.Error("最久未使用的 b 应被淘汰")
	}
	if cache.Get("a") == nil {
		t.Error("最近访问的 a 应保留")
	}
	if cache.Get("c") == nil {
		t.Error("刚写入的 c 应保留")
	}
}

// TestClearKeepsAdopted 覆盖 9.7：
// 清除缓存后临时缩略图消失而已采用的封面保留。
func TestClearKeepsAdopted(t *testing.T) {
	cache := newTestCache(t)
	img := jpegBytes()

	cache.Put("thumb-1", img)
	adoptedPath, err := cache.Adopt(img, ".jpg")
	if err != nil {
		t.Fatalf("采用封面失败: %v", err)
	}

	if err := cache.Clear(); err != nil {
		t.Fatalf("清除缓存失败: %v", err)
	}

	// 临时缩略图消失
	if cache.Get("thumb-1") != nil {
		t.Error("清除后临时缩略图应消失")
	}
	// 已采用的封面保留
	if !fileExists(adoptedPath) {
		t.Error("已采用的封面不应被清除")
	}
}

// TestAdoptDedupesByContent 覆盖「按内容哈希落盘」：内容相同即同一张图。
func TestAdoptDedupesByContent(t *testing.T) {
	cache := newTestCache(t)
	img := jpegBytes()

	path1, _ := cache.Adopt(img, ".jpg")
	path2, _ := cache.Adopt(img, ".jpg")
	if path1 != path2 {
		t.Errorf("内容相同应落到同一路径: %q vs %q", path1, path2)
	}
}

// TestHandlerServesBytes 覆盖 9.8：
// <img src="/cover?key=..."> 可加载且字节不经 IPC 桥。
func TestHandlerServesBytes(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(jpegBytes())
	}))
	defer origin.Close()

	svc := NewService(newTestCache(t), 8, "test")
	svc.Prefetch(context.Background(), []Ref{
		{Key: "k1", SourceRef: "rel-1", Size: 250, URLs: []string{origin.URL + "/front"}},
	})
	// 等预取完成
	time.Sleep(100 * time.Millisecond)

	handler := httptest.NewServer(svc.Handler())
	defer handler.Close()

	resp, err := http.Get(handler.URL + "/cover?key=k1&size=250")
	if err != nil {
		t.Fatalf("请求封面失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("状态码 = %d, 期望 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, 期望 image/jpeg", ct)
	}
}

// TestHandlerMissingReturns404 确保缺图时返回 404，前端显示占位图。
func TestHandlerMissingReturns404(t *testing.T) {
	svc := NewService(newTestCache(t), 8, "test")
	handler := httptest.NewServer(svc.Handler())
	defer handler.Close()

	resp, err := http.Get(handler.URL + "/cover?key=unknown&size=250")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("未知 key 状态码 = %d, 期望 404", resp.StatusCode)
	}
}

// TestPrefetchIsOneCall 覆盖 9.9：一页列表只发起一次批量预取。
func TestPrefetchIsOneCall(t *testing.T) {
	var requests int32
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Write(jpegBytes())
	}))
	defer origin.Close()

	svc := NewService(newTestCache(t), 8, "test")
	// 25 行不同封面，一次批量预取
	refs := make([]Ref, 25)
	for i := range refs {
		refs[i] = Ref{
			Key:  fmt.Sprintf("k-%d", i),
			URLs: []string{fmt.Sprintf("%s/img-%d", origin.URL, i)},
		}
	}
	svc.Prefetch(context.Background(), refs)
	time.Sleep(200 * time.Millisecond)

	// 每张不同的图各取一次，共 25 次（合并只发生在相同 key）
	if got := atomic.LoadInt32(&requests); got != 25 {
		t.Errorf("获取 %d 次, 期望 25 次（25 张不同封面）", got)
	}
}

// --- helpers ---

func jpegBytes() []byte {
	// 最小的合法 JPEG 前缀 + 填充
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	b = append(b, make([]byte, 200)...)
	return b
}

func fileExists(path string) bool {
	_, err := statFile(path)
	return err == nil
}
