package provider

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestTokenBucketEnforcesRate 覆盖 5.4：
// 并发 5 个请求时平均速率不超过声明值。
func TestTokenBucketEnforcesRate(t *testing.T) {
	l := NewLimiters()
	// 10 req/s，使测试在约 0.4s 内完成而非 5s
	l.Configure("test", 10, time.Second)

	const n = 5
	start := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := l.Wait(context.Background(), "test"); err != nil {
				t.Errorf("取令牌失败: %v", err)
			}
		}()
	}
	wg.Wait()
	elapsed := time.Since(start)

	// 首个令牌立即可得，其余 4 个各需 100ms
	minExpected := time.Duration(n-1) * 100 * time.Millisecond
	if elapsed < minExpected-20*time.Millisecond {
		t.Errorf("%d 个请求耗时 %v, 期望至少 %v——速率未被限制", n, elapsed, minExpected)
	}
}

// TestBucketIsSharedAcrossCallSites 覆盖 5.4 与 provider-registry spec：
// 同一上游的搜索、导入、详情共用一个桶，不各自为政。
func TestBucketIsSharedAcrossCallSites(t *testing.T) {
	l := NewLimiters()
	l.Configure("mb", 10, time.Second)

	start := time.Now()
	// 模拟三个不同调用点各发一次请求
	for _, site := range []string{"搜索", "导入匹配", "详情拉取"} {
		if err := l.Wait(context.Background(), "mb"); err != nil {
			t.Fatalf("%s 取令牌失败: %v", site, err)
		}
	}
	elapsed := time.Since(start)

	// 若各自一个桶，三次都会立即返回
	if elapsed < 180*time.Millisecond {
		t.Errorf("三个调用点共耗时 %v, 期望至少 200ms——它们应共用同一个桶", elapsed)
	}
}

// TestSeparateProvidersHaveSeparateBuckets 确保不同上游互不挤占。
func TestSeparateProvidersHaveSeparateBuckets(t *testing.T) {
	l := NewLimiters()
	l.Configure("slow", 1, time.Second)
	l.Configure("fast", 100, time.Second)

	// 先耗掉 slow 的令牌
	l.Wait(context.Background(), "slow")

	start := time.Now()
	if err := l.Wait(context.Background(), "fast"); err != nil {
		t.Fatalf("取令牌失败: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("fast 上游等待 %v——不同上游应有各自的桶", elapsed)
	}
}

// TestReconfigureAdjustsExistingBucket 确保调整速率是就地生效，
// 而不是换一个新桶（换桶会让正在排队的请求逃出限制）。
func TestReconfigureAdjustsExistingBucket(t *testing.T) {
	l := NewLimiters()
	l.Configure("p", 1, time.Second)
	before := l.buckets["p"]

	l.Configure("p", 20, time.Second)
	after := l.buckets["p"]

	if before != after {
		t.Error("调整速率应就地修改既有桶，而不是换一个新桶")
	}
}

// TestWaitRespectsContext 确保取令牌可被取消。
func TestWaitRespectsContext(t *testing.T) {
	l := NewLimiters()
	l.Configure("p", 1, time.Minute) // 极慢，保证会排队
	l.Wait(context.Background(), "p")

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if err := l.Wait(ctx, "p"); err == nil {
		t.Error("ctx 超时后取令牌应返回错误")
	}
}

type rateOnly struct{}

func (rateOnly) ID() ProviderID                  { return "mb" }
func (rateOnly) RateLimit() (int, time.Duration) { return 1, time.Second }

// TestSetFromProvider 确保仅速率声明即可配置桶，不要求元数据或显示信息。
func TestSetFromProvider(t *testing.T) {
	l := NewLimiters()
	l.SetFromProvider(rateOnly{})

	if _, ok := l.buckets["mb"]; !ok {
		t.Fatal("应按上游声明建立令牌桶")
	}
	if got := l.buckets["mb"].Limit(); float64(got) != 1.0 {
		t.Errorf("速率 = %v, 期望 1.0/s", got)
	}
}

// TestInteractiveJumpsAheadOfBackground 覆盖优先级闸门：
// 后台请求已排成一队时，用户触发的请求插到它们之前。
//
// 没有这道闸的话，用户在后台补全任务跑着时点一次搜索，要排在几百个后台请求
// 之后——在 1 req/s 之下就是几分钟。
func TestInteractiveJumpsAheadOfBackground(t *testing.T) {
	l := NewLimiters()
	// 5 req/s：闸门必须真的被占住，队列才排得起来（每个令牌相隔 200ms）
	l.Configure("test", 5, time.Second)

	// 先占住闸门，让后续请求必须排队
	if err := l.Wait(context.Background(), "test"); err != nil {
		t.Fatalf("首个令牌应立即可得: %v", err)
	}

	var mu sync.Mutex
	var order []string
	var wg sync.WaitGroup

	record := func(name string, p Priority) {
		defer wg.Done()
		ctx := WithPriority(context.Background(), p)
		if err := l.Wait(ctx, "test"); err != nil {
			t.Errorf("取令牌失败: %v", err)
			return
		}
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
	}

	// 三个后台请求先排进队列
	for _, name := range []string{"bg1", "bg2", "bg3"} {
		wg.Add(1)
		go record(name, PriorityBackground)
		// 让这一个确实进到队列里再排下一个：断言的是队列次序，不是调度巧合
		time.Sleep(10 * time.Millisecond)
	}
	// 交互式请求最后到达
	wg.Add(1)
	go record("user", PriorityInteractive)
	time.Sleep(10 * time.Millisecond)

	wg.Wait()

	if len(order) != 4 {
		t.Fatalf("放行 %d 个, 期望 4 个: %v", len(order), order)
	}
	// 期望 [bg1 user bg2 bg3]：已经握着闸门的那一个不被抢占（它已经在等自己的令牌了），
	// 但**还在排队**的后台请求全部让位。抢占在途请求只会浪费一个已经预约的令牌。
	if order[0] != "bg1" {
		t.Errorf("放行次序 = %v, 期望已在途的后台请求不被抢占", order)
	}
	if order[1] != "user" {
		t.Errorf("放行次序 = %v, 期望交互式请求插到排队中的后台请求之前", order)
	}
}

// TestPriorityDefaultsToBackground 钉住默认值：
// 忘记标注优先级的代价应当是「慢一点」，而不是「悄悄抢在用户前面」。
func TestPriorityDefaultsToBackground(t *testing.T) {
	if got := PriorityOf(context.Background()); got != PriorityBackground {
		t.Errorf("未标注的 ctx 优先级 = %v, 期望后台", got)
	}
	ctx := WithPriority(context.Background(), PriorityInteractive)
	if got := PriorityOf(ctx); got != PriorityInteractive {
		t.Errorf("标注后的优先级 = %v, 期望交互式", got)
	}
}
