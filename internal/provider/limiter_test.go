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

// TestSetFromProvider 确保按上游自己声明的速率配置桶。
func TestSetFromProvider(t *testing.T) {
	l := NewLimiters()
	l.SetFromProvider(&stubProvider{id: "mb", rateN: 1, ratePer: time.Second})

	if _, ok := l.buckets["mb"]; !ok {
		t.Fatal("应按上游声明建立令牌桶")
	}
	if got := l.buckets["mb"].Limit(); float64(got) != 1.0 {
		t.Errorf("速率 = %v, 期望 1.0/s", got)
	}
}
