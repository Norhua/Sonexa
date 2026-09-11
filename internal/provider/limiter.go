package provider

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiters 为每个上游维护一个进程级共享令牌桶。
//
// 同一上游的搜索、导入匹配、详情拉取共用同一个桶——它们打的是同一个受限 API，
// 各自一个桶会让各功能互相把对方打到限流。
//
// 图片服务不在桶内：封面档案与图片 CDN 都不是被限流的那个端点，
// 改由封面工作池的并发上限约束。因此「MB 1 req/s」与「封面 8 并发」并不矛盾。
type Limiters struct {
	mu      sync.Mutex
	buckets map[ProviderID]*rate.Limiter
}

// NewLimiters 建立一个空的令牌桶集合。
func NewLimiters() *Limiters {
	return &Limiters{buckets: map[ProviderID]*rate.Limiter{}}
}

// Configure 设置某上游的速率：每 per 时间内至多 n 次。
//
// 重复调用会就地调整既有桶的速率，而不是换一个新桶——
// 换桶会让此时正在排队等待的请求逃出限制。
func (l *Limiters) Configure(id ProviderID, n int, per time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	limit := rate.Limit(float64(n) / per.Seconds())
	if bucket, ok := l.buckets[id]; ok {
		bucket.SetLimit(limit)
		bucket.SetBurst(1)
		return
	}
	// burst 固定为 1：突发放行会瞬间超出上游的平均速率要求。
	l.buckets[id] = rate.NewLimiter(limit, 1)
}

// Wait 阻塞直到取得该上游的一个令牌，或 ctx 被取消。
func (l *Limiters) Wait(ctx context.Context, id ProviderID) error {
	l.mu.Lock()
	bucket, ok := l.buckets[id]
	if !ok {
		// 未配置过的上游默认按 1 req/s 保守放行
		bucket = rate.NewLimiter(rate.Limit(1), 1)
		l.buckets[id] = bucket
	}
	l.mu.Unlock()

	return bucket.Wait(ctx)
}

// SetFromProvider 按上游自己声明的速率配置其令牌桶。
func (l *Limiters) SetFromProvider(p Provider) {
	n, per := p.RateLimit()
	l.Configure(p.ID(), n, per)
}
