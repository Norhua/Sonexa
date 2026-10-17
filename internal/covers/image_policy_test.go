package covers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

type policyTransport func(*http.Request) (*http.Response, error)

func (f policyTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPoolSourcePolicyChecksFirstRequestAndEveryRedirect(t *testing.T) {
	for _, tc := range []struct {
		name, target string
		allowed      bool
	}{
		{"allowed", "https://img.one.test/end", true},
		{"another source", "https://two.test/end", false},
		{"spoof", "https://one.test.evil.test/end", false},
		{"http", "http://one.test/end", false},
		{"credentials", "https://user@one.test/end", false},
		{"port", "https://one.test:444/end", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPool(newTestCache(t), 1, "test")
			defer p.Close()
			p.requireImagePolicy = true
			one, _ := provider.NewImagePolicy([]provider.ImageHost{{Host: "one.test", Subdomains: true}}, 2)
			two, _ := provider.NewImagePolicy([]provider.ImageHost{{Host: "two.test"}}, 2)
			policies, _ := provider.NewImagePolicies(map[string]*provider.ImagePolicy{"one": one, "two": two})
			ctx := imageContext(context.WithValue(context.Background(), imagePoliciesKey{}, &sourceSnapshot{images: policies}), "one")
			calls := 0
			p.client.Transport = policyTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				h := make(http.Header)
				status := 200
				if r.URL.Path != "/end" {
					h.Set("Location", tc.target)
					status = 302
				}
				return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("image"))}, nil
			})
			_, err := p.fetchOne(ctx, "https://one.test/start")
			if (err == nil) != tc.allowed || (!tc.allowed && calls != 1) {
				t.Fatalf("calls=%d err=%v", calls, err)
			}
			before := calls
			if _, err := p.fetchOne(ctx, "https://two.test/start"); err == nil || calls != before {
				t.Fatal("first request borrowed other policy")
			}
			if _, err := p.fetchOne(context.Background(), "https://one.test/start"); err == nil || calls != before {
				t.Fatal("unregistered policy sent request")
			}
		})
	}
}

func TestPoolPolicyRedirectGateCancelAndLimit(t *testing.T) {
	for _, mode := range []string{"gate", "cancel", "limit"} {
		t.Run(mode, func(t *testing.T) {
			p := NewPool(newTestCache(t), 1, "test")
			defer p.Close()
			p.requireImagePolicy = true
			policy, _ := provider.NewImagePolicy([]provider.ImageHost{{Host: "one.test"}}, 1)
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), imagePolicyKey{}, policy))
			defer cancel()
			var disabled atomic.Bool
			denied := errors.New("disabled")
			p.SetGate(func() error {
				if disabled.Load() {
					return denied
				}
				return nil
			})
			calls := 0
			p.client.Transport = policyTransport(func(*http.Request) (*http.Response, error) {
				calls++
				if mode == "gate" {
					disabled.Store(true)
				}
				if mode == "cancel" {
					cancel()
				}
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"https://one.test/again"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			_, err := p.fetchOne(ctx, "https://one.test/start")
			if err == nil {
				t.Fatal("redirect should stop")
			}
			want := 1
			if mode == "limit" {
				want = 2
			}
			if calls != want {
				t.Fatal(calls, err)
			}
		})
	}
}
