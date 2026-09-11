package provider

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

// DefaultMaxAttempts 是同一条目累计失败的上限；达到后标记失败状态，不再自动重排。
const DefaultMaxAttempts = 5

// TransportMaxAttempts 是单次 HTTP 请求内部的尝试次数上限。
//
// 与条目级的 DefaultMaxAttempts 是两层不同的重试：这一层吸收上游的瞬时抖动，
// 条目级那层处理「这条目前确实查不到」。两层都省掉的话，
// MusicBrainz 的 503 会直接变成条目失败——实测在完全遵守 1 req/s 的前提下，
// MB 仍有过半的请求返回 `503 … currently busy`，不吸收掉这层抖动导入必然大面积失败。
const TransportMaxAttempts = 4

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

// TransportRetryPolicy 返回单次 HTTP 请求内部使用的重试策略。
//
// 退避远比条目级的短：用户正盯着搜索结果等，而 503 多半下一秒就好了；
// 真的连续几次都不行，再交给条目级那层用分钟量级的退避处理。
func TransportRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxAttempts: TransportMaxAttempts,
		BaseDelay:   time.Second,
		MaxDelay:    10 * time.Second,
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

// JitteredBackoff 在 Backoff 之上加入 ±25% 的抖动。
//
// 一批条目往往在同一秒撞上同一次上游抖动；不加抖动它们此后会一直同步重试，
// 把一次瞬时拥塞变成周期性拥塞。
func (p RetryPolicy) JitteredBackoff(attempt int, err error) time.Duration {
	d := p.Backoff(attempt, err)
	if d <= 0 {
		return 0
	}
	spread := float64(d) * 0.25
	return time.Duration(float64(d) - spread + rand.Float64()*2*spread)
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
