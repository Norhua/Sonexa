package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"sync"
)

var ErrSendBudget = errors.New("本轮请求次数已达上限")

type sendBudgetKey struct{}

// SendBudget is opt-in transport accounting. It knows no lyric business or
// provider protocol. One shared budget follows retries, redirects and callers'
// fallback requests via context; unrelated contexts retain existing behavior.
type SendBudget struct {
	mu          sync.Mutex
	limit, sent int
	stopped     error
}

func NewSendBudget(limit int) *SendBudget { return &SendBudget{limit: limit} }
func WithSendBudget(ctx context.Context, budget *SendBudget) context.Context {
	return context.WithValue(ctx, sendBudgetKey{}, budget)
}

// StopSendBudget lets a protocol adapter stop its operation after a business
// restriction, including a partial-success response. No protocol is interpreted
// here, and unrelated operations without an attached budget are unchanged.
func StopSendBudget(ctx context.Context, err error) {
	if b := sendBudget(ctx); b != nil {
		b.Stop(err)
	}
}
func sendBudget(ctx context.Context) *SendBudget {
	b, _ := ctx.Value(sendBudgetKey{}).(*SendBudget)
	return b
}
func (b *SendBudget) Sent() int  { b.mu.Lock(); defer b.mu.Unlock(); return b.sent }
func (b *SendBudget) Err() error { b.mu.Lock(); defer b.mu.Unlock(); return b.stopped }
func (b *SendBudget) Stop(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped == nil {
		b.stopped = err
	}
}
func (b *SendBudget) take() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stopped != nil {
		return b.stopped
	}
	if b.sent >= b.limit {
		b.stopped = ErrSendBudget
		return b.stopped
	}
	b.sent++
	return nil
}

func restrictedTransportError(err error) bool {
	var h *HTTPError
	var cert *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	return errors.Is(err, ErrResponseTooLarge) || errors.Is(err, ErrSendBudget) ||
		errors.As(err, &h) && (h.StatusCode == 401 || h.StatusCode == 403 || h.StatusCode == 429) ||
		errors.As(err, &cert) || errors.As(err, &authority) || errors.As(err, &hostname) || errors.As(err, &invalid)
}

type budgetTransport struct {
	base   http.RoundTripper
	client *Client
	quota  ProviderID
	budget *SendBudget
}

func (t budgetTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := t.budget.Err(); err != nil {
		return nil, err
	}
	if err := checkRequestGate(ctx); err != nil {
		t.budget.Stop(err)
		return nil, err
	}
	if t.client.Limiters != nil {
		if err := t.client.Limiters.Wait(ctx, t.quota); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Settings may change while quota is queued. Recheck immediately before
	// accounting/sending, just as the unbudgeted client does after Wait.
	if err := checkRequestGate(ctx); err != nil {
		t.budget.Stop(err)
		return nil, err
	}
	if err := t.budget.take(); err != nil {
		return nil, err
	}
	resp, err := t.base.RoundTrip(req)
	if restrictedTransportError(err) {
		t.budget.Stop(err)
	}
	if resp != nil && (resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429) {
		t.budget.Stop(&HTTPError{StatusCode: resp.StatusCode, Status: resp.Status, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))})
	}
	return resp, err
}
