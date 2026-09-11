package provider

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"
)

// DefaultMaxAttempts 是同一条目累计失败的上限；达到后标记失败状态，不再自动重排。
const DefaultMaxAttempts = 5

// RetryPolicy 是统一的重试策略。
type RetryPolicy struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
}

// DefaultRetryPolicy 返回默认重试策略。
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: DefaultMaxAttempts,
		BaseDelay:   2 * time.Second,
		MaxDelay:    5 * time.Minute,
	}
}

// HTTPError 携带上游返回的状态码与重试等待提示。
type HTTPError struct {
	StatusCode int
	Status     string
	// RetryAfter 来自响应头；上游未给出时为 0。
	RetryAfter time.Duration
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("上游返回 %s", e.Status)
}

// Retryable 报告该错误是否值得重试：429、5xx 与超时可重试。
func (e *HTTPError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// IsRetryable 报告任意错误是否值得重试。
// 超时与临时网络错误同样可重试。
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Retryable()
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return true
	}
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return true
	}
	return false
}

// Backoff 返回第 attempt 次失败后应等待的时长（attempt 从 1 起）。
//
// 尊重上游给出的重试等待时间：若 Retry-After 比指数退避更长，以它为准，
// 而不是按自己的节奏立即重试。
func (p RetryPolicy) Backoff(attempt int, err error) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(float64(p.BaseDelay) * math.Pow(2, float64(attempt-1)))
	if delay > p.MaxDelay {
		delay = p.MaxDelay
	}

	var httpErr *HTTPError
	if errors.As(err, &httpErr) && httpErr.RetryAfter > delay {
		return httpErr.RetryAfter
	}
	return delay
}

// ShouldGiveUp 报告累计失败 attempts 次后是否应停止自动重排。
func (p RetryPolicy) ShouldGiveUp(attempts int) bool {
	max := p.MaxAttempts
	if max <= 0 {
		max = DefaultMaxAttempts
	}
	return attempts >= max
}

// parseRetryAfter 解析 Retry-After 响应头，支持秒数与 HTTP 日期两种形态。
func parseRetryAfter(header string) time.Duration {
	if header == "" {
		return 0
	}
	if secs, err := strconv.Atoi(header); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}
