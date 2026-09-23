package covers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/norhua/sonexa/internal/store"
)

func TestCoverRedirectRechecksOutboundGate(t *testing.T) {
	for _, kind := range []string{"catalog", "image"} {
		t.Run(kind, func(t *testing.T) {
			svc, st := resolverService(t)
			var allowed atomic.Bool
			allowed.Store(true)
			svc.EnableNetworkPolicy(func() error {
				if !allowed.Load() {
					return &OutboundError{Reason: "disabled"}
				}
				return nil
			})
			var calls atomic.Int32
			svc.pool.client.Transport = resolverTransport(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				allowed.Store(false)
				header := make(http.Header)
				header.Set("Location", "https://archive.org/redirected")
				return &http.Response{StatusCode: 302, Header: header, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			var err error
			if kind == "catalog" {
				_, err = svc.CAACatalog(context.Background(), "release", testReleaseMBID, false)
			} else {
				key := store.CoverCatalogKey{Source: "caa", ObjectType: "release", SourceRef: testReleaseMBID}
				if _, err := st.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: "1", PrimaryType: "Front", Variants: []store.CoverVariant{{Spec: "250", URL: "https://archive.org/image"}}}}); err != nil {
					t.Fatal(err)
				}
				catalog, readErr := st.GetCoverCatalog(key)
				if readErr != nil {
					t.Fatal(readErr)
				}
				_, err = svc.DownloadVariant(context.Background(), catalog.Images[0].Variants[0])
			}
			var disabled *OutboundError
			if !errors.As(err, &disabled) || calls.Load() != 1 {
				t.Fatalf("重定向没有重新检查 gate: %v calls=%d", err, calls.Load())
			}
		})
	}
}
