package provider

import (
	"context"
	"sync"
)

type requestLimitsKey struct{}

// RequestLimits is opt-in and shared across retries/pages in one operation.
// Zero fields preserve existing behavior. Configuration is copied on attach.
type RequestLimits struct {
	MaxAttempts        int
	ResponseBytes      int64
	TotalResponseBytes int64
}

type requestLimits struct {
	RequestLimits
	mu    sync.Mutex
	bytes int64
}

func WithRequestLimits(ctx context.Context, limits RequestLimits) context.Context {
	return context.WithValue(ctx, requestLimitsKey{}, &requestLimits{RequestLimits: limits})
}
func limitsFor(ctx context.Context) *requestLimits {
	limits, _ := ctx.Value(requestLimitsKey{}).(*requestLimits)
	return limits
}
func (l *requestLimits) consume(n int64) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.bytes += n
	if l.TotalResponseBytes > 0 && l.bytes > l.TotalResponseBytes {
		return ErrResponseTooLarge
	}
	return nil
}
