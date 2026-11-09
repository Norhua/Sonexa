package playlists

import (
	"context"
	"sync"

	"github.com/norhua/sonexa/internal/provider"
)

// FetchPool is App-owned and survives registry/settings publication. It bounds
// all playlist fetches to two total and one per platform, including quota waits.
type FetchPool struct {
	global  chan struct{}
	mu      sync.Mutex
	sources map[provider.ProviderID]chan struct{}
}

func NewFetchPool() *FetchPool {
	return &FetchPool{global: make(chan struct{}, 2), sources: map[provider.ProviderID]chan struct{}{}}
}
func (p *FetchPool) acquire(ctx context.Context, id provider.ProviderID) (func(), error) {
	p.mu.Lock()
	lane := p.sources[id]
	if lane == nil {
		lane = make(chan struct{}, 1)
		p.sources[id] = lane
	}
	p.mu.Unlock()
	select {
	case lane <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case p.global <- struct{}{}:
		return func() { <-p.global; <-lane }, nil
	case <-ctx.Done():
		<-lane
		return nil, ctx.Err()
	}
}

// Fetcher owns cancellation/waiting separately from immutable source registries.
// Close must complete before closing any store used by its eventual consumers.
type Fetcher struct {
	pool   *FetchPool
	life   context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	work   sync.WaitGroup
}

func NewFetcher() *Fetcher {
	ctx, cancel := context.WithCancel(context.Background())
	return &Fetcher{pool: NewFetchPool(), life: ctx, cancel: cancel}
}
func (f *Fetcher) Fetch(ctx context.Context, p Provider, ref Ref) (*Snapshot, error) {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil, Failure(Cancelled, "歌单服务已关闭")
	}
	f.work.Add(1)
	f.mu.Unlock()
	defer f.work.Done()
	ctx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()
	stop := context.AfterFunc(f.life, cancel)
	defer stop()
	release, err := f.pool.acquire(ctx, p.ID())
	if err != nil {
		return nil, Failure(Cancelled, "歌单读取已取消")
	}
	defer release()
	if ctx.Err() != nil {
		return nil, Failure(Cancelled, "歌单读取已取消")
	}
	s, err := p.FetchPlaylist(ctx, ref)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, Failure(Cancelled, "歌单读取已取消")
	}
	if err = s.Validate(); err != nil {
		return nil, err
	}
	if s.Ref != ref {
		return nil, Failure(Incomplete, "歌单来源返回了不同身份")
	}
	return s, nil
}
func (f *Fetcher) Close() {
	f.mu.Lock()
	f.closed = true
	f.cancel()
	f.mu.Unlock()
	f.work.Wait()
}
