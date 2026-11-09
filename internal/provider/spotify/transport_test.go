package spotify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
)

func TestBridgeRetriesRedirectsAndGateShareBudget(t *testing.T) {
	for _, mode := range []string{"retry", "redirect", "hostile-redirect", "gate", "budget"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			client := provider.NewClient(nil, nil, "fixture")
			client.Retry.BaseDelay, client.Retry.MaxDelay = 0, 0
			address := "https://open.spotify.com/embed/playlist/" + testPlaylist
			client.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				status := 200
				headers := http.Header{}
				if calls == 1 {
					switch mode {
					case "retry":
						status = 503
					case "redirect", "gate", "budget":
						status = 302
						headers.Set("Location", address)
					case "hostile-redirect":
						status = 302
						headers.Set("Location", "https://evil.test/")
					}
				}
				return &http.Response{StatusCode: status, Header: headers, Body: io.NopCloser(strings.NewReader("fixture")), Request: r}, nil
			})
			limit := 10
			if mode == "budget" {
				limit = 1
			}
			budget := provider.NewSendBudget(limit)
			ctx := provider.WithSendBudget(context.Background(), budget)
			ctx = provider.WithRequestLimits(ctx, provider.RequestLimits{MaxAttempts: 3, ResponseBytes: 32, TotalResponseBytes: 64})
			if mode == "gate" {
				ctx = provider.WithRequestGate(ctx, func() error {
					if calls > 0 {
						return playlists.Failure(playlists.Access, "disabled")
					}
					return nil
				})
			}
			bridge := &transportBridge{client: client, ref: testPlaylist, tracks: map[string]bool{}}
			raw, _ := json.Marshal(httpMessage{Method: "GET", URL: address})
			_, err := bridge.exchange(ctx, raw)
			if mode == "retry" || mode == "redirect" {
				if err != nil || calls != 2 || budget.Sent() != 2 {
					t.Fatalf("%v calls=%d sent=%d", err, calls, budget.Sent())
				}
			} else if err == nil || calls != 1 || budget.Sent() != 1 {
				t.Fatalf("unsafe continuation %v calls=%d sent=%d", err, calls, budget.Sent())
			}
		})
	}
}
