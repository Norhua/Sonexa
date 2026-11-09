package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOptInRequestLimitsAndResponseInfo(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("12345"))
	}))
	defer server.Close()
	client := NewClient(nil, nil, "test")
	info := &ResponseInfo{}
	req := GetRequest{BaseURL: server.URL, CacheMode: NoStore, ResponseInfo: info}
	ctx := WithRequestLimits(context.Background(), RequestLimits{MaxAttempts: 3, ResponseBytes: 5, TotalResponseBytes: 8})
	if body, err := client.Get(ctx, req); err != nil || string(body) != "12345" {
		t.Fatalf("%s %v", body, err)
	}
	if info.StatusCode != 200 || info.ContentType != "text/plain" {
		t.Fatal(info)
	}
	if _, err := client.Get(ctx, req); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	if _, err := client.Get(context.Background(), req); err != nil {
		t.Fatal("unrelated request inherited limits", err)
	}
}

func TestOptInAttemptLimitPreservesOldRetryPolicy(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(503) }))
	defer server.Close()
	client := NewClient(nil, nil, "test")
	client.Retry.BaseDelay, client.Retry.MaxDelay = 0, 0
	req := GetRequest{BaseURL: server.URL, CacheMode: NoStore}
	_, _ = client.Get(WithRequestLimits(context.Background(), RequestLimits{MaxAttempts: 3}), req)
	if calls != 3 {
		t.Fatalf("new policy calls=%d", calls)
	}
	calls = 0
	_, _ = client.Get(context.Background(), req)
	if calls != TransportMaxAttempts {
		t.Fatalf("old policy changed: %d", calls)
	}
}

func TestResponseLimitIncludesErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte(strings.Repeat("x", 20)))
	}))
	defer server.Close()
	client := NewClient(nil, nil, "test")
	ctx := WithRequestLimits(context.Background(), RequestLimits{ResponseBytes: 8, TotalResponseBytes: 16})
	_, err := client.Get(ctx, GetRequest{BaseURL: server.URL, CacheMode: NoStore})
	if !errors.Is(err, ErrResponseTooLarge) {
		t.Fatal(err)
	}
}
