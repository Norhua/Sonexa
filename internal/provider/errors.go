package provider

import (
	"context"
	"errors"
	"fmt"
)

var ErrResponseTooLarge = errors.New("上游响应过大")

// BusinessError preserves codes without guessing whether an undocumented
// platform failure is authentication or rate limiting. Consumers may stop
// conservatively; never infer permissions from the error's localized text.
type BusinessError struct {
	Source string
	Block  string
	Code   int
}

func (e *BusinessError) Error() string {
	return fmt.Sprintf("%s %s 返回错误码 %d", e.Source, e.Block, e.Code)
}

type stopRestrictedKey struct{}

func WithStopRestrictedRetries(ctx context.Context) context.Context {
	return context.WithValue(ctx, stopRestrictedKey{}, true)
}
func stopRestrictedRetry(ctx context.Context, err error) bool {
	if enabled, _ := ctx.Value(stopRestrictedKey{}).(bool); !enabled {
		return false
	}
	var h *HTTPError
	return errors.As(err, &h) && (h.StatusCode == 401 || h.StatusCode == 403 || h.StatusCode == 429)
}
