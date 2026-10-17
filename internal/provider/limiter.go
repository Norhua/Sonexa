package provider

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Limiters 为每个平台 API 维护一个进程级共享令牌桶。
//
// 同一平台的元数据、歌词与封面查询共用配额；只有应用装配/设置入口配置桶，
// 能力构造不能配置或替换桶。图片和 CAA 索引继续由封面工作池控制。
//
// 桶前面还有一道**优先级闸门**：交互式请求整体排在后台请求之前。
// 没有它的话，用户在后台补全任务跑着的时候点一次搜索，要排在几百个
// 后台请求之后——在 1 req/s 下就是几分钟。
//
// 图片服务不在桶内：封面档案与图片 CDN 都不是被限流的那个端点，
// 改由封面工作池的并发上限约束。因此「MB 1 req/s」与「封面 8 并发」并不矛盾。
type Limiters struct {
	mu      sync.Mutex
	buckets map[QuotaID]*rate.Limiter
	gates   map[QuotaID]*priorityGate
}

// NewLimiters 建立一个空的令牌桶集合。
func NewLimiters() *Limiters {
	return &Limiters{
		buckets: map[QuotaID]*rate.Limiter{},
		gates:   map[QuotaID]*priorityGate{},
	}
}

// Configure 设置某上游的速率：每 per 时间内至多 n 次。
//
// 重复调用会就地调整既有桶的速率，而不是换一个新桶——
// 换桶会让此时正在排队等待的请求逃出限制。
func (l *Limiters) Configure(id QuotaID, n int, per time.Duration) {
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
//
// 两段：先过优先级闸门（交互式插队），再取令牌。闸门在**取到令牌后**即释放，
// 而不是握到请求返回——握到返回等于把每次请求的网络耗时叠加到限流间隔上，
// 实际速率会莫名其妙地低于配置值。次序仍有保证：闸门一次只放行一个，
// 被放行者立刻去桶里预约下一个令牌，因此「谁先被放行」就等于「谁先发出」。
func (l *Limiters) Wait(ctx context.Context, id QuotaID) error {
	gate := l.gateFor(id)
	priority := PriorityOf(ctx)
	if err := gate.acquire(ctx, priority); err != nil {
		return err
	}
	defer gate.release()

	return l.bucketFor(id).Wait(ctx)
}

// bucketFor 取（或建）某上游的令牌桶。
func (l *Limiters) bucketFor(id QuotaID) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()
	bucket, ok := l.buckets[id]
	if !ok {
		// 未配置过的上游默认按 1 req/s 保守放行
		bucket = rate.NewLimiter(rate.Limit(1), 1)
		l.buckets[id] = bucket
	}
	return bucket
}

// gateFor 取（或建）某上游的优先级闸门。
func (l *Limiters) gateFor(id QuotaID) *priorityGate {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.gates == nil {
		l.gates = map[QuotaID]*priorityGate{}
	}
	gate, ok := l.gates[id]
	if !ok {
		gate = &priorityGate{}
		l.gates[id] = gate
	}
	return gate
}

// SetFromProvider 仅由装配入口按速率声明配置桶，不要求元数据能力。
func (l *Limiters) SetFromProvider(p RateSource) {
	n, per := p.RateLimit()
	l.Configure(p.ID(), n, per)
}

// priorityGate 串行化令牌申请，并让交互式请求插到后台请求之前。
//
// 用两条显式的等待队列而不是一个信号量：信号量的唤醒顺序不确定，
// 「用户请求插队」这件事就无从保证。闸门一次只放行一个，
// 因此「谁先被唤醒」等于「谁先取到令牌」。
type priorityGate struct {
	mu   sync.Mutex
	busy bool
	// 两条队列各自 FIFO，交互式整体优先于后台。
	interactive []chan struct{}
	background  []chan struct{}
}

func (g *priorityGate) acquire(ctx context.Context, p Priority) error {
	g.mu.Lock()
	if !g.busy {
		g.busy = true
		g.mu.Unlock()
		return nil
	}
	ch := make(chan struct{})
	if p == PriorityInteractive {
		g.interactive = append(g.interactive, ch)
	} else {
		g.background = append(g.background, ch)
	}
	g.mu.Unlock()

	select {
	case <-ch:
		return nil
	case <-ctx.Done():
		// 取消与「被唤醒」可能同时发生。若还在队列里就是没轮到我们，直接走人；
		// 若已经不在队列里，说明闸门已经交到我们手上，必须传下去，
		// 否则整条队列就此卡死。
		g.mu.Lock()
		removed := g.removeLocked(ch, p)
		g.mu.Unlock()
		if !removed {
			g.release()
		}
		return ctx.Err()
	}
}

func (g *priorityGate) removeLocked(ch chan struct{}, p Priority) bool {
	queue := &g.background
	if p == PriorityInteractive {
		queue = &g.interactive
	}
	for i, waiter := range *queue {
		if waiter == ch {
			*queue = append((*queue)[:i], (*queue)[i+1:]...)
			return true
		}
	}
	return false
}

func (g *priorityGate) release() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if len(g.interactive) > 0 {
		next := g.interactive[0]
		g.interactive = g.interactive[1:]
		close(next)
		return
	}
	if len(g.background) > 0 {
		next := g.background[0]
		g.background = g.background[1:]
		close(next)
		return
	}
	g.busy = false
}
