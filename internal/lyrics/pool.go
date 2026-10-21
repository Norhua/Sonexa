package lyrics

import (
	"context"
	"sync"
)

// FetchPool bounds complete candidate operations (not individual HTTP sends).
// Different service snapshots share one process pool; source keys are generic
// identities, not a provider-specific dispatch mechanism.
type FetchPool struct {
	mu        sync.Mutex
	limit     int
	perSource int
	active    int
	sources   map[string]int
	queue     []*fetchWaiter
}

type fetchWaiter struct {
	source  string
	ready   chan struct{}
	granted bool
}

func NewFetchPool(limit, perSource int) *FetchPool {
	if limit < 1 || perSource < 1 {
		panic("positive lyric fetch concurrency required")
	}
	return &FetchPool{limit: limit, perSource: perSource, sources: map[string]int{}}
}

func (p *FetchPool) grantLocked() {
	pending := p.queue[:0]
	for _, w := range p.queue {
		if p.active < p.limit && p.sources[w.source] < p.perSource {
			p.active++
			p.sources[w.source]++
			w.granted = true
			close(w.ready)
		} else {
			pending = append(pending, w)
		}
	}
	// Don't retain cancelled/completed waiters in spare queue capacity.
	for i := len(pending); i < len(p.queue); i++ {
		p.queue[i] = nil
	}
	p.queue = pending
}

func (p *FetchPool) Acquire(ctx context.Context, source string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &fetchWaiter{source: source, ready: make(chan struct{})}
	p.mu.Lock()
	p.queue = append(p.queue, w)
	p.grantLocked()
	p.mu.Unlock()
	select {
	case <-w.ready:
	case <-ctx.Done():
	}
	p.mu.Lock()
	if err := ctx.Err(); err != nil {
		if w.granted {
			p.releaseLocked(source)
		} else {
			for i, item := range p.queue {
				if item == w {
					copy(p.queue[i:], p.queue[i+1:])
					p.queue[len(p.queue)-1] = nil
					p.queue = p.queue[:len(p.queue)-1]
					break
				}
			}
		}
		p.mu.Unlock()
		return nil, err
	}
	p.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { p.mu.Lock(); p.releaseLocked(source); p.mu.Unlock() }) }, nil
}
func (p *FetchPool) releaseLocked(source string) {
	p.active--
	p.sources[source]--
	if p.sources[source] == 0 {
		delete(p.sources, source)
	}
	p.grantLocked()
}
