package lyrics

import (
	"context"
	"errors"

	"github.com/norhua/sonexa/internal/provider"
)

// CanFallback is allow-listed. Unclassified transport and business failures
// cannot be silently treated as missing lyrics or an unsupported endpoint.
func CanFallback(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrTooLarge) {
		return false
	}
	var business *provider.BusinessError
	if errors.As(err, &business) {
		return false
	}
	var httpErr *provider.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 404 || httpErr.StatusCode == 410
	}
	return errors.Is(err, ErrNoLyrics) || errors.Is(err, ErrFormat)
}
