package lyrics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestFetchPoolLimitsCancellationAndFairSourceProgress(t *testing.T) {
	pool := NewFetchPool(4, 1)
	var mu sync.Mutex
	active, maxActive := 0, 0
	bySource := map[string]int{}
	started := make(chan string, 8)
	releaseAll := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for _, source := range []string{"a", "a", "a", "b", "c", "d", "e", "f"} {
		wg.Add(1)
		go func(source string) {
			defer wg.Done()
			release, err := pool.Acquire(ctx, source)
			if err != nil {
				return
			}
			defer release()
			mu.Lock()
			active++
			bySource[source]++
			maxActive = max(maxActive, active)
			if active > 4 || bySource[source] > 1 {
				t.Error("fetch pool limit exceeded")
			}
			mu.Unlock()
			started <- source
			select {
			case <-releaseAll:
			case <-ctx.Done():
			}
			mu.Lock()
			active--
			bySource[source]--
			mu.Unlock()
		}(source)
	}
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		select {
		case source := <-started:
			if seen[source] {
				t.Fatal("same source concurrent")
			}
			seen[source] = true
		case <-time.After(time.Second):
			t.Fatal("head-of-line source blocked other sources")
		}
	}
	cancel()
	close(releaseAll)
	wg.Wait()
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if pool.active != 0 || len(pool.queue) != 0 || len(pool.sources) != 0 || maxActive != 4 {
		t.Fatal("pool not drained", pool.active, len(pool.queue), maxActive)
	}
}

func TestFetchPoolCancelledWaitDoesNotConsumeSlot(t *testing.T) {
	pool := NewFetchPool(1, 1)
	release, err := pool.Acquire(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := pool.Acquire(ctx, "source"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	release()
	next, err := pool.Acquire(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	next()
}
