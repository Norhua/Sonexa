package provider

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func policyResponse(code int, location, body string) *http.Response {
	h := make(http.Header)
	if location != "" {
		h.Set("Location", location)
	}
	return &http.Response{StatusCode: code, Status: fmt.Sprint(code), Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestRequestRedirectPolicy(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		limit        int
		wantCalls    int
		ok           bool
	}{
		{"allowed", "https://allowed.test/end", 1, 2, true},
		{"other-host", "https://other.test/end", 1, 1, false},
		{"subdomain", "https://sub.allowed.test/end", 1, 1, false},
		{"downgrade", "http://allowed.test/end", 1, 1, false},
		{"userinfo", "https://user@allowed.test/end", 1, 1, false},
		{"port", "https://allowed.test:8443/end", 1, 1, false},
		{"disabled", "https://allowed.test/end", -1, 1, false},
		{"loop", "https://allowed.test/start", 2, 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(nil, nil, "test")
			calls := 0
			c.HTTP.Transport = policyTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path == "/end" {
					return policyResponse(200, "", "ok"), nil
				}
				return policyResponse(302, tc.target, ""), nil
			})
			_, err := c.Get(context.Background(), GetRequest{BaseURL: "https://allowed.test", Endpoint: "/start", CacheMode: NoStore, Redirects: &RedirectPolicy{Hosts: []string{"allowed.test"}, MaxRedirects: tc.limit}})
			if (err == nil) != tc.ok || calls != tc.wantCalls {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestRedirectRechecksGateAndCancellation(t *testing.T) {
	for _, cancelInstead := range []bool{false, true} {
		c := NewClient(nil, nil, "test")
		// Replacing HTTP must not remove the dynamic gate.
		c.HTTP = &http.Client{}
		var disabled atomic.Bool
		denied := errors.New("disabled")
		ctx, cancel := context.WithCancel(WithRequestGate(context.Background(), func() error {
			if disabled.Load() {
				return denied
			}
			return nil
		}))
		calls := 0
		c.HTTP.Transport = policyTransport(func(*http.Request) (*http.Response, error) {
			calls++
			if cancelInstead {
				cancel()
			} else {
				disabled.Store(true)
			}
			return policyResponse(302, "https://allowed.test/end", ""), nil
		})
		_, err := c.Get(ctx, GetRequest{BaseURL: "https://allowed.test", Redirects: &RedirectPolicy{Hosts: []string{"allowed.test"}}, CacheMode: NoStore})
		cancel()
		want := denied
		if cancelInstead {
			want = context.Canceled
		}
		if !errors.Is(err, want) || calls != 1 {
			t.Fatalf("calls=%d err=%v want=%v", calls, err, want)
		}
	}
}

func TestConcurrentRedirectPoliciesDoNotMutateSharedClient(t *testing.T) {
	c := NewClient(nil, nil, "test")
	var callbacks atomic.Int32
	c.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { callbacks.Add(1); return nil }
	c.HTTP.Transport = policyTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/start" {
			return policyResponse(302, "https://"+r.URL.Host+"/end", ""), nil
		}
		return policyResponse(200, "", "ok"), nil
	})
	var wg sync.WaitGroup
	for _, host := range []string{"a.test", "b.test"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_, err := c.Get(context.Background(), GetRequest{BaseURL: "https://" + host, Endpoint: "/start", Redirects: &RedirectPolicy{Hosts: []string{host}}, CacheMode: NoStore})
				if err != nil {
					t.Error(err)
				}
			}
		}()
	}
	wg.Wait()
	if callbacks.Load() != 20 {
		t.Fatal(callbacks.Load())
	}
	// Original callback still accepts an otherwise unrelated target.
	req, _ := http.NewRequest("GET", "http://unrelated.test", nil)
	if err := c.HTTP.CheckRedirect(req, nil); err != nil {
		t.Fatal("shared callback was replaced", err)
	}
}

func TestResponseLimitAndTLSVerification(t *testing.T) {
	c := NewClient(nil, nil, "test")
	c.HTTP.Transport = policyTransport(func(*http.Request) (*http.Response, error) {
		return policyResponse(200, "", strings.Repeat("x", (4<<20)+1)), nil
	})
	if _, err := c.Get(context.Background(), GetRequest{BaseURL: "https://allowed.test", MaxResponseBytes: 4 << 20, CacheMode: NoStore}); err == nil {
		t.Fatal("超过 4 MiB 未拒绝")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) }))
	defer srv.Close()
	c = NewClient(nil, nil, "test")
	c.Retry.MaxAttempts = 1
	if _, err := c.Get(context.Background(), GetRequest{BaseURL: srv.URL, CacheMode: NoStore}); err == nil {
		t.Fatal("不可信 TLS 证书被接受")
	}
}
