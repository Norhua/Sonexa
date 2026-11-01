package covers

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// 跨来源搜索和同图规格回归已迁至 sources/providers_test.go 与 app/cover_search_test.go。

// TestAdoptRecordsProvenance 覆盖 9.11：
// 确认采用后按内容哈希落盘并记录来源/地址/尺寸/时间。
func TestAdoptRecordsProvenance(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(pngBytes(600, 600))
	}))
	defer srv.Close()

	svc := newTestService(newTestCache(t), 8, "test")
	url := srv.URL + "/front-1200.jpg"

	got, err := svc.Adopt(context.Background(), provider.MusicBrainz, url)
	if err != nil {
		t.Fatalf("采用封面失败: %v", err)
	}

	if got.Path == "" {
		t.Error("应返回落盘路径")
	}
	if !fileExists(got.Path) {
		t.Error("封面应已落盘")
	}
	if got.Source != "musicbrainz" {
		t.Errorf("来源 = %q", got.Source)
	}
	if got.SourceURL != url {
		t.Errorf("原始地址 = %q, 期望 %q", got.SourceURL, url)
	}
	if got.Width == nil || *got.Width != 600 {
		t.Errorf("宽度 = %v, 期望 600", got.Width)
	}
	if got.Height == nil || *got.Height != 600 {
		t.Errorf("高度 = %v, 期望 600", got.Height)
	}
	if got.FetchedAt.IsZero() {
		t.Error("应记录获取时间")
	}
}

// TestAdoptFromLadderFallsBack 覆盖 9.6：
// 构造型来源选中的档位不可得时自动退到下一档。
func TestAdoptFromLadderFallsBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 3000 档不存在（未文档化行为可能失效），1200 可用
		if r.URL.Path == "/1200x1200bb.jpg" {
			w.Write(pngBytes(1200, 1200))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	svc := newTestService(newTestCache(t), 8, "test")
	got, err := svc.AdoptFromLadder(context.Background(), provider.ITunes, []string{
		srv.URL + "/3000x3000bb.jpg", // 404
		srv.URL + "/1200x1200bb.jpg", // 命中
	})
	if err != nil {
		t.Fatalf("应退档到 1200: %v", err)
	}
	if got.Width == nil || *got.Width != 1200 {
		t.Errorf("采用的尺寸 = %v, 期望退到 1200 档", got.Width)
	}
}

// TestCancelOutOfView 覆盖 9.3：
// 滚出视口且尚未开始的任务被取消。
func TestCancelOutOfView(t *testing.T) {
	release := make(chan struct{})
	visibleStarted := make(chan struct{}, 1)
	var started atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Add(1)
		if r.URL.Path == "/a" {
			visibleStarted <- struct{}{}
		}
		<-release // 阻塞住，模拟慢请求
		w.Write(jpegBytes())
	}))
	defer srv.Close()

	// 并发 1：第一个任务占住槽位，其余排队。
	pool := NewPool(newTestCache(t), 1, "test")
	defer pool.Close() // 等待所有工作结束后，testing 才清理缓存目录。
	visibleDone := make(chan error, 1)
	go func() {
		_, err := pool.Fetch(context.Background(), Fetcher{Key: "visible", URLs: []string{srv.URL + "/a"}})
		visibleDone <- err
	}()
	defer func() {
		close(release)
		select {
		case err := <-visibleDone:
			if err != nil {
				t.Errorf("仍在视口内的任务应成功写盘: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("仍在视口内的任务未完成")
		}
	}()
	select {
	case <-visibleStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("可见任务未开始")
	}

	done := make(chan error, 1)
	go func() {
		_, err := pool.Fetch(context.Background(), Fetcher{Key: "scrolled-away", URLs: []string{srv.URL + "/b"}})
		done <- err
	}()
	// 等待排队登记，不靠固定 sleep 猜测调度顺序。
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for pool.InflightCount() != 2 {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("第二个任务未进入等待队列")
		}
	}

	pool.CancelAllExcept(map[string]bool{"visible": true})
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("滚出视口的任务应被取消: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("滚出视口的任务未被取消")
	}
	if started.Load() != 1 {
		t.Errorf("被取消的排队任务不应发出请求: %d", started.Load())
	}
}

// TestImageDimensions 校准尺寸解析。
func TestImageDimensions(t *testing.T) {
	w, h := imageDimensions(pngBytes(1200, 800))
	if w == nil || h == nil || *w != 1200 || *h != 800 {
		t.Errorf("PNG 尺寸 = %v×%v, 期望 1200×800", w, h)
	}

	// 无法判定时返回 nil，而不是伪造 0
	w2, h2 := imageDimensions([]byte{0x00, 0x01})
	if w2 != nil || h2 != nil {
		t.Errorf("无法判定时应返回 nil, 得到 %v×%v", w2, h2)
	}
}

// pngBytes 返回可被标准解码器验证的 PNG，而不是只有 IHDR 的伪造前缀。
func pngBytes(w, h int) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		panic(err)
	}
	return b.Bytes()
}
