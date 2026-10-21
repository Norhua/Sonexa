package provider

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type sendTransport func(*http.Request) (*http.Response, error)

func (f sendTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func budgetRequest() GetRequest {
	return GetRequest{Provider: QQQuota, BaseURL: "https://test.example", Endpoint: "/start", CacheMode: NoStore, Redirects: &RedirectPolicy{Hosts: []string{"test.example"}, MaxRedirects: 20}}
}
func sendResponse(code int, body string) *http.Response {
	return &http.Response{StatusCode: code, Status: http.StatusText(code), Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
}

func TestBudgetRechecksGateAfterQuotaWait(t *testing.T) {
	limiters := NewLimiters()
	gate := limiters.gateFor(QQQuota)
	if err := gate.acquire(context.Background(), PriorityInteractive); err != nil {
		t.Fatal(err)
	}
	var disabled atomic.Bool
	checked := make(chan struct{}, 1)
	denied := errors.New("outbound disabled")
	budget := NewSendBudget(16)
	ctx := WithRequestGate(WithSendBudget(context.Background(), budget), func() error {
		if disabled.Load() {
			return denied
		}
		checked <- struct{}{}
		return nil
	})
	client := NewClient(limiters, nil, "test")
	calls := 0
	transport := budgetTransport{client: client, quota: QQQuota, budget: budget, base: sendTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return sendResponse(200, "ok"), nil
	})}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://test.example", nil)
	finished := make(chan error, 1)
	go func() { _, err := transport.RoundTrip(req); finished <- err }()
	<-checked
	disabled.Store(true)
	gate.release()
	if err := <-finished; !errors.Is(err, denied) {
		t.Fatal("queued request missed updated gate", err)
	}
	if calls != 0 || budget.Sent() != 0 || !errors.Is(budget.Err(), denied) {
		t.Fatal("disabled request consumed send budget", calls, budget.Sent(), budget.Err())
	}
}

func TestSendBudgetCountsRedirectsRetriesAndFallbackRequests(t *testing.T) {
	client := NewClient(nil, nil, "test")
	client.Retry = RetryPolicy{MaxAttempts: 3}
	calls := 0
	client.HTTP.Transport = sendTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return sendResponse(503, "busy"), nil
		}
		if r.URL.Path == "/start" {
			resp := sendResponse(302, "")
			resp.Header.Set("Location", "https://test.example/end")
			return resp, nil
		}
		return sendResponse(200, "ok"), nil
	})
	budget := NewSendBudget(16)
	ctx := WithSendBudget(context.Background(), budget)
	if _, err := client.Get(ctx, budgetRequest()); err != nil {
		t.Fatal(err)
	}
	if budget.Sent() != 3 || calls != 3 {
		t.Fatal(calls, budget.Sent())
	}
	req := budgetRequest()
	req.Endpoint = "/fallback"
	for i := 0; i < 13; i++ {
		if _, err := client.Get(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.Get(ctx, req); !errors.Is(err, ErrSendBudget) {
		t.Fatal(err)
	}
	if calls != 16 || budget.Sent() != 16 {
		t.Fatal("17th send escaped", calls, budget.Sent())
	}
	// Different source/round contexts are not stopped by another budget.
	if _, err := client.Get(WithSendBudget(context.Background(), NewSendBudget(1)), req); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetRestrictedErrorsStopWithoutRetries(t *testing.T) {
	for _, kind := range []string{"auth", "rate", "TLS", "size"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			client := NewClient(nil, nil, "test")
			client.Retry = RetryPolicy{MaxAttempts: 3}
			client.HTTP.Transport = sendTransport(func(*http.Request) (*http.Response, error) {
				calls++
				switch kind {
				case "auth":
					return sendResponse(403, "bad"), nil
				case "rate":
					return sendResponse(429, "bad"), nil
				case "TLS":
					return nil, x509.UnknownAuthorityError{}
				default:
					return sendResponse(200, "too large"), nil
				}
			})
			budget := NewSendBudget(16)
			ctx := WithSendBudget(context.Background(), budget)
			req := budgetRequest()
			req.MaxResponseBytes = 3
			if _, err := client.Get(ctx, req); err == nil {
				t.Fatal("restriction hidden")
			}
			if _, err := client.Get(ctx, req); err == nil {
				t.Fatal("stopped context reused")
			}
			if calls != 1 || budget.Sent() != 1 {
				t.Fatal(calls)
			}
		})
	}
	client := NewClient(nil, nil, "test")
	client.Retry = RetryPolicy{MaxAttempts: 2}
	calls := 0
	client.HTTP.Transport = sendTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return sendResponse(429, "limited"), nil
		}
		return sendResponse(200, "ok"), nil
	})
	if _, err := client.Get(context.Background(), budgetRequest()); err != nil || calls != 2 {
		t.Fatal("unbudgeted behavior changed", err, calls)
	}
}

func TestBudgetRedirectConsumesQuotaAndRejectedRedirectDoesNotSend(t *testing.T) {
	limiters := NewLimiters()
	limiters.Configure(QQQuota, 1, time.Hour)
	client := NewClient(limiters, nil, "test")
	calls := 0
	client.HTTP.Transport = sendTransport(func(*http.Request) (*http.Response, error) {
		calls++
		r := sendResponse(302, "")
		r.Header.Set("Location", "https://test.example/end")
		return r, nil
	})
	budget := NewSendBudget(16)
	ctx, cancel := context.WithTimeout(WithSendBudget(context.Background(), budget), 20*time.Millisecond)
	defer cancel()
	if _, err := client.Get(ctx, budgetRequest()); err == nil {
		t.Fatal("redirect bypassed quota")
	}
	if calls != 1 || budget.Sent() != 1 {
		t.Fatal(calls, budget.Sent())
	}
	client = NewClient(nil, nil, "test")
	calls = 0
	client.HTTP.Transport = sendTransport(func(*http.Request) (*http.Response, error) {
		calls++
		r := sendResponse(302, "")
		r.Header.Set("Location", "https://other.example/end")
		return r, nil
	})
	budget = NewSendBudget(16)
	if _, err := client.Get(WithSendBudget(context.Background(), budget), budgetRequest()); err == nil {
		t.Fatal("unsafe redirect")
	}
	if calls != 1 || budget.Sent() != 1 {
		t.Fatal("rejected redirect counted", calls)
	}
}
