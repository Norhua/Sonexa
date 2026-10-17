package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRequestGateKeepsCacheAndChecksQueuedRequests(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cache := newMemCache()
	client := NewClient(NewLimiters(), cache, "test")
	client.Limiters.Configure("p", 1, 30*time.Millisecond)
	disabled := errors.New("outbound disabled")
	ctx := WithRequestGate(context.Background(), func() error { return disabled })
	req := GetRequest{Provider: "p", Endpoint: "/search", BaseURL: srv.URL, ParamsHash: "same"}
	if err := cache.PutCache("p", "/search", "same", []byte(`{"cached":true}`), 0); err != nil {
		t.Fatal(err)
	}
	if body, err := client.Get(ctx, req); err != nil || string(body) != `{"cached":true}` {
		t.Fatalf("gate 拒绝了缓存: %s %v", body, err)
	}
	req.CacheMode = RefreshCache
	if _, err := client.Get(ctx, req); !errors.Is(err, disabled) {
		t.Fatal(err)
	}
	if calls.Load() != 0 {
		t.Fatal("禁用后仍出网")
	}

	// Get 在配额等待后才检查 gate，不能仅在调用入口检查一次。
	var allowed atomic.Bool
	allowed.Store(true)
	queued := WithRequestGate(context.Background(), func() error {
		if !allowed.Load() {
			return disabled
		}
		return nil
	})
	allowed.Store(false)
	if _, err := client.Get(queued, req); !errors.Is(err, disabled) || calls.Load() != 0 {
		t.Fatalf("排队后的请求绕过 gate: %v", err)
	}
}
