package lyricfetch

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"testing"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func TestNormalizedErrorsAndFallbackPermission(t *testing.T) {
	for _, tt := range []struct {
		name           string
		err            error
		kind           string
		stop, fallback bool
	}{
		{"response size", provider.ErrResponseTooLarge, "too_large", true, false},
		{"decoded size", lyricdoc.ErrTooLarge, "too_large", true, false},
		{"cancel", context.Canceled, "cancelled", true, false},
		{"timeout", context.DeadlineExceeded, "timeout", true, false},
		{"TLS", x509.UnknownAuthorityError{}, "tls", true, false},
		{"business", &provider.BusinessError{Code: 2001}, "upstream", true, false},
		{"auth", &provider.HTTPError{StatusCode: 403}, "auth", true, false},
		{"limited", &provider.HTTPError{StatusCode: 429}, "rate_limit", true, false},
		{"missing endpoint", &provider.HTTPError{StatusCode: 404}, "unsupported_endpoint", false, true},
		{"format", lyricdoc.ErrFormat, "format", false, true},
		{"no lyrics", lyricdoc.ErrNoLyrics, "no_lyrics", false, true},
		{"contract", lyricdoc.ErrContract, "contract", false, false},
		{"unknown", errors.New("unclassified"), "transport", true, false},
		{"restricted fallback cause", fmt.Errorf("%w: %w", lyricdoc.ErrFormat, &provider.HTTPError{StatusCode: 429}), "rate_limit", true, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := Normalize(tt.err)
			var failure *lyrics.FetchError
			if !errors.As(got, &failure) || failure.Kind != tt.kind || failure.StopSource != tt.stop || CanFallback(got) != tt.fallback {
				t.Fatalf("%+v", failure)
			}
			if !errors.Is(got, tt.err) || Normalize(got) != got {
				t.Fatal("lost error provenance or double wrapped")
			}
			if tt.kind == "too_large" && !errors.Is(got, lyricdoc.ErrTooLarge) {
				t.Fatal("size errors disagree")
			}
		})
	}
	if Normalize(nil) != nil || CanFallback(nil) {
		t.Fatal("nil error is not failure")
	}
}
