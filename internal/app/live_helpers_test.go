package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
)

var errLiveStopped = errors.New("在线样本已停止：请求上限、传输失败或来源限制；不继续重试或提速")

type liveArrival struct {
	path   string
	at     time.Time
	status int
}

// Test-only observer. It never logs query/body/lyrics or changes a successful
// upstream response. Every wire request (including redirects) consumes budget.
type liveAudit struct {
	mu       sync.Mutex
	max      int
	stopped  bool
	arrivals []liveArrival
	log      func(string, ...any)
}

func (a *liveAudit) gate() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stopped || len(a.arrivals) >= a.max {
		return errLiveStopped
	}
	return nil
}
func (a *liveAudit) snapshot() []liveArrival {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]liveArrival(nil), a.arrivals...)
}
func (a *liveAudit) stop() { a.mu.Lock(); a.stopped = true; a.mu.Unlock() }

func liveResponseRestricted(host string, status int, body []byte) bool {
	if status != http.StatusOK {
		return true
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		if host == "c.y.qq.com" {
			_, err := qqmusic.ParseLyric(body)
			return err != nil
		}
		return true
	}
	check := func(raw json.RawMessage, success int) bool {
		if raw == nil {
			return false
		}
		var code int
		return json.Unmarshal(raw, &code) != nil || code != success
	}
	switch host {
	case "u.y.qq.com", "c.y.qq.com":
		if root["code"] == nil || check(root["code"], 0) || check(root["retcode"], 0) || check(root["subcode"], 0) {
			return true
		}
		for _, raw := range root {
			var block map[string]json.RawMessage
			if json.Unmarshal(raw, &block) == nil && check(block["code"], 0) {
				return true
			}
		}
	case "music.163.com":
		return check(root["code"], 200)
	case "mobileservice.kugou.com":
		return check(root["status"], 1) || check(root["errcode"], 0)
	case "lyrics.kugou.com":
		return check(root["status"], 200) || check(root["error_code"], 0)
	case "m.kuwo.cn":
		return check(root["status"], 200)
	case "www.kuwo.cn":
		return check(root["code"], 200) || check(root["status"], 200)
	}
	return false
}

func liveCodeSummary(body []byte) []string {
	var root map[string]json.RawMessage
	if json.Unmarshal(body, &root) != nil {
		return []string{"non_json"}
	}
	codes := []string{}
	for key, value := range root {
		if key == "code" || key == "status" || key == "retcode" || key == "subcode" || key == "errcode" || key == "error_code" {
			var n int
			if json.Unmarshal(value, &n) == nil {
				codes = append(codes, fmt.Sprintf("%s=%d", key, n))
			}
		}
		var block struct{ Code *int }
		if json.Unmarshal(value, &block) == nil && block.Code != nil {
			codes = append(codes, fmt.Sprintf("block_code=%d", *block.Code))
		}
	}
	sort.Strings(codes)
	return codes
}

func (a *liveAudit) transport(base http.RoundTripper, api bool) http.RoundTripper {
	return roundTrip(func(r *http.Request) (*http.Response, error) {
		if err := r.Context().Err(); err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.stopped || len(a.arrivals) >= a.max {
			a.mu.Unlock()
			return nil, errLiveStopped
		}
		now := time.Now()
		delta := time.Duration(0)
		if len(a.arrivals) > 0 {
			delta = now.Sub(a.arrivals[len(a.arrivals)-1].at)
		}
		index := len(a.arrivals)
		a.arrivals = append(a.arrivals, liveArrival{path: r.URL.Path, at: now})
		a.mu.Unlock()
		resp, err := base.RoundTrip(r)
		if err != nil {
			a.stop()
			a.log("request=%d host=%s path=%s interval=%s transport_error=%T", index+1, r.URL.Host, r.URL.Path, delta, err)
			return nil, errLiveStopped
		}
		a.mu.Lock()
		a.arrivals[index].status = resp.StatusCode
		a.mu.Unlock()
		a.log("request=%d host=%s path=%s interval=%s status=%d retry_after=%q", index+1, r.URL.Host, r.URL.Path, delta, resp.StatusCode, resp.Header.Get("Retry-After"))
		// Normal redirects and missing image sizes are not rate-limit evidence.
		if resp.StatusCode >= 300 && resp.StatusCode < 400 || !api && resp.StatusCode == 404 {
			return resp, nil
		}
		if api {
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, (4<<20)+1))
			resp.Body.Close()
			a.log("request=%d business_status=%v response_bytes=%d", index+1, liveCodeSummary(body), len(body))
			if readErr != nil || len(body) > 4<<20 || liveResponseRestricted(r.URL.Hostname(), resp.StatusCode, body) {
				a.stop()
				a.log("request=%d stopped=HTTP_or_business_error_or_invalid_response", index+1)
				return nil, errLiveStopped
			}
			resp.Body = io.NopCloser(bytes.NewReader(body))
		} else if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			a.stop()
			return nil, errLiveStopped
		}
		return resp, nil
	})
}

func requireLive(t *testing.T, env, value string) {
	t.Helper()
	if value != "1" {
		t.Skipf("set %s=1 for bounded real-source verification", env)
	}
}

// Uses the same production assembly and settings publication as the app. All
// storage is temporary; no account, user settings or adopted files are touched.
func newLiveApp(t *testing.T, budget int) (*App, *liveAudit) {
	t.Helper()
	a, cleanup := newTestApp(t)
	a.enricher.Pause()
	t.Cleanup(cleanup)
	if err := a.coverSvc.AttachLibrary(a.store); err != nil {
		t.Fatal(err)
	}
	a.coverSvc.EnableNetworkPolicy(a.checkCoverOutbound)
	next := a.settings.Get()
	next.MusicBrainz.Contact = "https://github.com/norhua/sonexa"
	next.QQMusic.RatePerMin = 30 // Every live entry starts conservatively.
	if err := a.SaveSettings(next); err != nil {
		t.Fatal(err)
	}
	audit := &liveAudit{max: budget, log: t.Logf}
	a.client.HTTP.Transport = audit.transport(http.DefaultTransport, true)
	t.Cleanup(func() { a.shutdown(context.Background()) })
	return a, audit
}

func liveContext(a *App, audit *liveAudit, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	return provider.WithRequestGate(ctx, func() error {
		if err := audit.gate(); err != nil {
			return err
		}
		return a.checkCoverOutbound()
	}), cancel
}

func TestLiveAuditStopsBeforeAnotherRequest(t *testing.T) {
	for _, tc := range []struct {
		host, body string
		status     int
	}{
		{"u.y.qq.com", `{"code":0,"req":{"code":2001}}`, 200},
		{"u.y.qq.com", `{}`, 429}, {"u.y.qq.com", `{}`, 403},
		{"music.163.com", `{"code":301}`, 200}, {"m.kuwo.cn", `{"status":301}`, 200},
		{"www.kuwo.cn", `{"code":301}`, 200}, {"www.kuwo.cn", `{"status":301}`, 200},
	} {
		t.Run(fmt.Sprint(tc.host, tc.status), func(t *testing.T) {
			calls := 0
			audit := &liveAudit{max: 3, log: t.Logf}
			transport := audit.transport(roundTrip(func(*http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Retry-After": {"60"}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			}), true)
			r, _ := http.NewRequest("GET", "https://"+tc.host+"/probe", nil)
			for range 2 {
				if _, err := transport.RoundTrip(r); !errors.Is(err, errLiveStopped) {
					t.Fatal(err)
				}
			}
			if calls != 1 {
				t.Fatal("continued after restriction", calls)
			}
		})
	}
	audit := &liveAudit{max: 1, log: t.Logf}
	transport := audit.transport(roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0}`))}, nil
	}), true)
	r, _ := http.NewRequest("GET", "https://u.y.qq.com/probe", nil)
	resp, err := transport.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, err := transport.RoundTrip(r); !errors.Is(err, errLiveStopped) || len(audit.snapshot()) != 1 {
		t.Fatal("budget bypass", err)
	}
	logs := ""
	var sent int
	private := &liveAudit{max: 2, log: func(format string, values ...any) { logs += fmt.Sprintf(format, values...) }}
	wire := private.transport(roundTrip(func(*http.Request) (*http.Response, error) {
		sent++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"code":0,"lyric":"private-lyric-sentinel"}`))}, nil
	}), true)
	resp, err = wire.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := wire.RoundTrip(r.WithContext(cancelled)); !errors.Is(err, context.Canceled) || sent != 1 {
		t.Fatal("cancelled request sent", err)
	}
	if strings.Contains(logs, "private-lyric-sentinel") {
		t.Fatal("lyrics leaked to log")
	}
}

func TestLiveEntriesDefaultToSkipped(t *testing.T) {
	for _, value := range []string{"", "0", "true"} {
		reached := false
		t.Run("value="+value, func(t *testing.T) { requireLive(t, "SONEXA_TEST_LIVE", value); reached = true })
		if reached {
			t.Fatal("live entry enabled implicitly")
		}
	}
}
