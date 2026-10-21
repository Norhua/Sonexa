// Package lyricfetch provides error adaptation shared by lyric providers.
// It is not part of the provider transport parent package; only adapters call it.
package lyricfetch

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

// Normalize retains errors.Is/As provenance while presenting common failure
// categories. Undocumented business failures stop conservatively; no message
// matching or source-ID switch guesses whether an error permits more requests.
func Normalize(err error) error {
	if err == nil {
		return nil
	}
	var already *lyrics.FetchError
	if errors.As(err, &already) {
		return err
	}
	kind, stop := "transport", true
	var business *provider.BusinessError
	var httpErr *provider.HTTPError
	var cert *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var syntax *json.SyntaxError
	var typeError *json.UnmarshalTypeError
	switch {
	case errors.Is(err, context.Canceled):
		kind = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "timeout"
	case errors.Is(err, provider.ErrSendBudget):
		kind = "budget"
	case errors.Is(err, provider.ErrResponseTooLarge), errors.Is(err, lyricdoc.ErrTooLarge):
		kind = "too_large"
		if !errors.Is(err, lyricdoc.ErrTooLarge) {
			err = fmt.Errorf("%w：%w", lyricdoc.ErrTooLarge, err)
		}
	case errors.As(err, &cert), errors.As(err, &authority), errors.As(err, &hostname), errors.As(err, &invalid):
		kind = "tls"
	case errors.As(err, &business):
		kind = "upstream"
	case errors.As(err, &httpErr):
		switch httpErr.StatusCode {
		case 401, 403:
			kind = "auth"
		case 429:
			kind = "rate_limit"
		case 404, 410:
			kind, stop = "unsupported_endpoint", false
		}
	case errors.Is(err, lyricdoc.ErrContract):
		kind, stop = "contract", false
	case errors.Is(err, lyricdoc.ErrFormat), errors.As(err, &syntax), errors.As(err, &typeError):
		kind, stop = "format", false
	case errors.Is(err, lyricdoc.ErrNoLyrics):
		kind, stop = "no_lyrics", false
	}
	return &lyrics.FetchError{Kind: kind, StopSource: stop, Cause: err}
}

// CanFallback is an adapter-only allow-list, never a business repair path.
func CanFallback(err error) bool {
	if err == nil {
		return false
	}
	var failure *lyrics.FetchError
	if !errors.As(Normalize(err), &failure) || failure.StopSource {
		return false
	}
	return failure.Kind == "no_lyrics" || failure.Kind == "format" || failure.Kind == "unsupported_endpoint"
}
