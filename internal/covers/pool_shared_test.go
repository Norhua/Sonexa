package covers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedConsumerCancellationDoesNotCancelOther(t *testing.T) {
	pool := NewPool(newTestCache(t), 1, "test")
	defer pool.Close()
	started, finish := make(chan struct{}), make(chan struct{})
	work := func(ctx context.Context) (any, error) {
		close(started)
		select {
		case <-finish:
			return "ok", nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	first, cancel := context.WithCancel(context.Background())
	firstDone, secondDone := make(chan error, 1), make(chan error, 1)
	go func() { _, err := pool.Shared(first, "image", work); firstDone <- err }()
	<-started
	go func() { _, err := pool.Shared(context.Background(), "image", work); secondDone <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		pool.mu.Lock()
		waiters := pool.jobs["image"].waiters
		pool.mu.Unlock()
		if waiters == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("第二个消费者未加入")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-firstDone; err != context.Canceled {
		t.Fatal(err)
	}
	close(finish)
	if err := <-secondDone; err != nil {
		t.Fatalf("取消一个消费者影响了其他人: %v", err)
	}
}

func TestResizeTracksRunningTransfers(t *testing.T) {
	pool := NewPool(newTestCache(t), 3, "test")
	defer pool.Close()
	var active atomic.Int32
	started, finish := make(chan struct{}, 4), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		active.Add(1)
		started <- struct{}{}
		<-finish
		active.Add(-1)
		w.Write([]byte("image"))
	}))
	defer srv.Close()
	var wg sync.WaitGroup
	for range 3 {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = pool.fetchOne(context.Background(), srv.URL) }()
	}
	for range 3 {
		<-started
	}
	pool.SetConcurrency(1)
	wg.Add(1)
	go func() { defer wg.Done(); _, _ = pool.fetchOne(context.Background(), srv.URL) }()
	select {
	case <-started:
		t.Fatal("缩小并发后仍启动超额任务")
	case <-time.After(30 * time.Millisecond):
	}
	close(finish)
	wg.Wait()
}

func TestNestedDirectoryDoesNotHoldTransferSlot(t *testing.T) {
	pool := NewPool(newTestCache(t), 1, "test")
	defer pool.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := pool.Shared(ctx, "catalog", func(ctx context.Context) (any, error) {
		if _, err := pool.fetchOne(ctx, srv.URL); err != nil {
			return nil, err
		}
		return pool.Shared(ctx, "variant", func(ctx context.Context) (any, error) { return pool.fetchOne(ctx, srv.URL) })
	})
	if err != nil {
		t.Fatalf("并发1发生嵌套死锁: %v", err)
	}
}

func TestPoolCloseCancelsAndDrains(t *testing.T) {
	pool := NewPool(newTestCache(t), 1, "test")
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := pool.Shared(context.Background(), "task", func(ctx context.Context) (any, error) {
			close(started)
			<-ctx.Done()
			return nil, ctx.Err()
		})
		done <- err
	}()
	<-started
	pool.Close()
	if err := <-done; err != context.Canceled {
		t.Fatal(err)
	}
	if pool.InflightCount() != 0 {
		t.Fatal("关闭后仍有未结束任务")
	}
	if _, err := pool.Shared(context.Background(), "new", func(context.Context) (any, error) { return nil, nil }); err != context.Canceled {
		t.Fatal("关闭后仍接受任务")
	}
}
