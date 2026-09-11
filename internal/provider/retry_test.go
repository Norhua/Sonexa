package provider

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestRetryableStatuses 覆盖 5.6：429 / 5xx / 超时可重试。
func TestRetryableStatuses(t *testing.T) {
	cases := []struct {
		status int
		want   bool
	}{
		{http.StatusTooManyRequests, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusNotFound, false},
		{http.StatusBadRequest, false},
		{http.StatusForbidden, false},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.status), func(t *testing.T) {
			err := &HTTPError{StatusCode: c.status, Status: fmt.Sprint(c.status)}
			if got := IsRetryable(err); got != c.want {
				t.Errorf("状态 %d 可重试 = %v, 期望 %v", c.status, got, c.want)
			}
		})
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string { return "超时" }
func (timeoutErr) Timeout() bool { return true }

func TestTimeoutIsRetryable(t *testing.T) {
	if !IsRetryable(timeoutErr{}) {
		t.Error("超时应可重试")
	}
	if IsRetryable(errors.New("解析失败")) {
		t.Error("普通错误不应重试")
	}
	if IsRetryable(nil) {
		t.Error("nil 不应重试")
	}
}

// TestBackoffIsExponential 覆盖指数退避。
func TestBackoffIsExponential(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Hour}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i, w := range want {
		if got := p.Backoff(i+1, nil); got != w {
			t.Errorf("第 %d 次退避 = %v, 期望 %v", i+1, got, w)
		}
	}
}

func TestBackoffRespectsMaxDelay(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 10, BaseDelay: time.Second, MaxDelay: 5 * time.Second}
	if got := p.Backoff(10, nil); got != 5*time.Second {
		t.Errorf("退避 = %v, 期望封顶 5s", got)
	}
}

// TestBackoffHonoursRetryAfter 覆盖「尊重重试等待提示」：
// 上游给出的等待时间比指数退避更长时以它为准。
func TestBackoffHonoursRetryAfter(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: time.Hour}

	err := &HTTPError{StatusCode: 429, RetryAfter: 30 * time.Second}
	if got := p.Backoff(1, err); got != 30*time.Second {
		t.Errorf("退避 = %v, 期望 30s（尊重 Retry-After）", got)
	}
	// Retry-After 比退避短时以退避为准，不抢跑
	short := &HTTPError{StatusCode: 429, RetryAfter: time.Millisecond}
	if got := p.Backoff(3, short); got != 4*time.Second {
		t.Errorf("退避 = %v, 期望 4s", got)
	}
}

// TestGiveUpAtLimit 覆盖 5.6：累计 5 次失败后标记失败，不再自动重排。
func TestGiveUpAtLimit(t *testing.T) {
	p := DefaultRetryPolicy()
	for attempts := 1; attempts <= 4; attempts++ {
		if p.ShouldGiveUp(attempts) {
			t.Errorf("第 %d 次失败后不应放弃", attempts)
		}
	}
	if !p.ShouldGiveUp(5) {
		t.Error("第 5 次失败后应停止自动重排")
	}
	if !p.ShouldGiveUp(6) {
		t.Error("超过上限后应继续保持放弃")
	}
}

// TestParseRetryAfter 覆盖秒数与 HTTP 日期两种形态。
func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("120"); got != 2*time.Minute {
		t.Errorf("秒数形态 = %v, 期望 2m", got)
	}
	if got := parseRetryAfter(""); got != 0 {
		t.Errorf("空值 = %v, 期望 0", got)
	}
	if got := parseRetryAfter("garbage"); got != 0 {
		t.Errorf("无法解析时 = %v, 期望 0", got)
	}
	future := time.Now().Add(time.Minute).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 0 {
		t.Errorf("日期形态 = %v, 期望正值", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 0 {
		t.Errorf("过去的时间 = %v, 期望 0", got)
	}
}

// TestJitteredBackoffNeverUndercutsRetryAfter 覆盖「抖动不得削短 Retry-After」：
// 上游明确说「等 N 秒」时向下抖动就是抢跑，只会招来继续限流。
func TestJitteredBackoffNeverUndercutsRetryAfter(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: time.Second, MaxDelay: 20 * time.Second}
	err := &HTTPError{StatusCode: 503, RetryAfter: 30 * time.Second}

	for i := 0; i < 200; i++ {
		if got := p.JitteredBackoff(1, err); got < 30*time.Second {
			t.Fatalf("JitteredBackoff = %v, 不得短于 Retry-After 的 30s", got)
		}
	}
}

// TestJitteredBackoffSpreadsExponentialDelay 确保指数退避那一路仍然被抖开——
// 否则一批同时失败的条目会永远同步重试，把瞬时拥塞变成周期性拥塞。
func TestJitteredBackoffSpreadsExponentialDelay(t *testing.T) {
	p := RetryPolicy{MaxAttempts: 5, BaseDelay: 4 * time.Second, MaxDelay: time.Minute}

	seen := map[time.Duration]bool{}
	for i := 0; i < 50; i++ {
		d := p.JitteredBackoff(1, nil)
		if d < 3*time.Second || d > 5*time.Second {
			t.Fatalf("抖动后 = %v, 期望落在基数 4s 的 ±25%% 内", d)
		}
		seen[d] = true
	}
	if len(seen) < 10 {
		t.Errorf("50 次只出现 %d 个不同取值，抖动没有生效", len(seen))
	}
}
