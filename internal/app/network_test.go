package app

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
)

// Ordinary App tests may use httptest or explicit replay transports, never
// production endpoints. Installing this before assembly also covers clients
// rebuilt by SaveSettings and the cover pool. Live suites remain opt-in.
func TestMain(m *testing.M) {
	for _, key := range []string{"SONEXA_LIVE_COVERS", "SONEXA_LIVE_ITUNES", "SONEXA_LYRICS_LIVE", "SONEXA_QQ_METADATA_LIVE", "SONEXA_QQ_FALLBACK_DISCOVERY", "SONEXA_QQ_RATE_LIVE"} {
		if os.Getenv(key) == "1" {
			os.Exit(m.Run())
		}
	}
	guard := &offlineTestTransport{base: http.DefaultTransport}
	http.DefaultTransport = guard
	code := m.Run()
	guard.mu.Lock()
	for _, endpoint := range guard.blocked {
		fmt.Fprintln(os.Stderr, "unexpected external request blocked:", endpoint)
		code = 1
	}
	guard.mu.Unlock()
	os.Exit(code)
}

type offlineTestTransport struct {
	base    http.RoundTripper
	mu      sync.Mutex
	blocked []string
}

func (t *offlineTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	// No hostname resolution: even localhost-like names must not escape to DNS.
	ip := net.ParseIP(r.URL.Hostname())
	if ip != nil && ip.IsLoopback() {
		return t.base.RoundTrip(r)
	}
	endpoint := r.URL.Scheme + "://" + r.URL.Host + r.URL.Path
	t.mu.Lock()
	t.blocked = append(t.blocked, endpoint)
	t.mu.Unlock()
	return nil, fmt.Errorf("offline App test blocked external request: %s", endpoint)
}

func TestOfflineTransportRejectsExternalBeforeSending(t *testing.T) {
	calls := 0
	guard := &offlineTestTransport{base: roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK}, nil
	})}
	for _, host := range []string{"musicbrainz.org", "localhost.example.com", "192.168.1.1"} {
		r, _ := http.NewRequest(http.MethodGet, "https://"+host+"/probe", nil)
		if _, err := guard.RoundTrip(r); err == nil {
			t.Fatal("external request allowed", host)
		}
	}
	if calls != 0 || len(guard.blocked) != 3 {
		t.Fatal("reached network", calls, guard.blocked)
	}
	for _, host := range []string{"127.0.0.1:1234", "[::1]:1234"} {
		r, _ := http.NewRequest(http.MethodGet, "http://"+host+"/probe", nil)
		if _, err := guard.RoundTrip(r); err != nil {
			t.Fatal("httptest blocked", err)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
}
