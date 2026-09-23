package covers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// 封面图片来自 CAA/archive.org 等不限并发的静态来源；瓶颈是每次重定向和 TLS 握手的延迟，
// 并发只限制本机同时打开的连接数，因此默认值取得较高。
const DefaultConcurrency = 32
const MaxConcurrency = 64

type Fetcher struct {
	Key  string   `json:"key"`
	URLs []string `json:"urls"`
}

type IndexParser func(url string, body []byte) (urls []string, handled bool, err error)

type sharedFetch struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	value   any
	err     error
}

// Pool 在目录/图片身份上合并消费者，在每次 HTTP 传输上获取共享并发槽。
// 等待另一任务的目录解析不持有槽位，单消费者取消不杀死其他消费者。
type Pool struct {
	cache  *DiskCache
	client *http.Client

	mu          sync.Mutex
	limit       int
	active      int
	changed     chan struct{}
	jobs        map[string]*sharedFetch
	workers     sync.WaitGroup
	closed      bool
	userAgent   string
	indexParser IndexParser
	gate        func() error
	retry       provider.RetryPolicy
}

func boundedConcurrency(n int) int {
	if n <= 0 {
		return DefaultConcurrency
	}
	return min(n, MaxConcurrency)
}

func NewPool(cache *DiskCache, concurrency int, userAgent string) *Pool {
	return &Pool{cache: cache, client: &http.Client{Timeout: 60 * time.Second}, limit: boundedConcurrency(concurrency),
		changed: make(chan struct{}), jobs: map[string]*sharedFetch{}, userAgent: userAgent, retry: provider.RetryPolicy{MaxAttempts: 1}}
}

func (p *Pool) SetIndexParser(parser IndexParser) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.indexParser = parser
}

func (p *Pool) SetGate(gate func() error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gate = gate
}

func (p *Pool) checkGate() error {
	p.mu.Lock()
	gate := p.gate
	p.mu.Unlock()
	if gate != nil {
		return gate()
	}
	return nil
}

func (p *Pool) SetConcurrency(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.limit = boundedConcurrency(n)
	p.signalLocked()
}

func (p *Pool) signalLocked() { close(p.changed); p.changed = make(chan struct{}) }

func (p *Pool) Concurrency() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.limit
}

func (p *Pool) acquire(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		if p.closed {
			return context.Canceled
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p.active < p.limit {
			p.active++
			return nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
		}
		p.mu.Lock()
	}
}

func (p *Pool) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active--
	p.signalLocked()
}

// Shared 按键合并工作，不占用 HTTP 槽位；调用方必须确保键代表同一个操作。
func (p *Pool) Shared(ctx context.Context, key string, work func(context.Context) (any, error)) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, context.Canceled
	}
	job := p.jobs[key]
	if job == nil {
		jobCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		job = &sharedFetch{ctx: jobCtx, cancel: cancel, done: make(chan struct{})}
		p.jobs[key] = job
		p.workers.Add(1)
		go func() {
			defer p.workers.Done()
			value, err := work(job.ctx)
			p.mu.Lock()
			job.value, job.err = value, err
			if p.jobs[key] == job {
				delete(p.jobs, key)
			}
			close(job.done)
			job.cancel()
			p.mu.Unlock()
		}()
	}
	job.waiters++
	p.mu.Unlock()
	select {
	case <-job.done:
		return job.value, job.err
	case <-ctx.Done():
		p.mu.Lock()
		job.waiters--
		if job.waiters == 0 {
			job.cancel()
			if p.jobs[key] == job {
				delete(p.jobs, key)
			}
		}
		p.mu.Unlock()
		return nil, ctx.Err()
	}
}

var ErrNotFound = errors.New("封面不可得")

func (p *Pool) Fetch(ctx context.Context, f Fetcher) ([]byte, error) {
	if data := p.cache.Get(f.Key); data != nil {
		return data, nil
	}
	value, err := p.Shared(ctx, "legacy:"+f.Key, func(ctx context.Context) (any, error) {
		if data := p.cache.Get(f.Key); data != nil {
			return data, nil
		}
		data, err := p.fetchLadder(ctx, f.URLs)
		if err != nil {
			return nil, err
		}
		_ = p.cache.Put(f.Key, data)
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]byte), nil
}

func (p *Pool) fetchLadder(ctx context.Context, urls []string) ([]byte, error) {
	p.mu.Lock()
	parser := p.indexParser
	p.mu.Unlock()
	return p.fetchURLs(ctx, urls, parser)
}

func (p *Pool) fetchURLs(ctx context.Context, urls []string, parser IndexParser) ([]byte, error) {
	var failure error
	for _, url := range urls {
		data, err := p.fetchOne(ctx, url)
		if err == nil && parser != nil {
			images, handled, parseErr := parser(url, data)
			if handled {
				err = parseErr
				if err == nil {
					data, err = p.fetchURLs(ctx, images, nil)
				}
			}
		}
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !errors.Is(err, ErrNotFound) {
			failure = err
		}
	}
	if failure != nil {
		return nil, failure
	}
	return nil, ErrNotFound
}

func (p *Pool) fetchOne(ctx context.Context, url string) ([]byte, error) {
	p.mu.Lock()
	retry := p.retry
	p.mu.Unlock()
	var last error
	for attempt := 1; attempt <= max(retry.MaxAttempts, 1); attempt++ {
		data, err := p.fetchOnce(ctx, url)
		if err == nil {
			return data, nil
		}
		last = err
		if !provider.IsRetryable(err) || attempt >= retry.MaxAttempts {
			break
		}
		timer := time.NewTimer(retry.JitteredBackoff(attempt, err))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, last
}

func (p *Pool) fetchOnce(ctx context.Context, url string) ([]byte, error) {
	if err := p.acquire(ctx); err != nil {
		return nil, err
	}
	defer p.release()
	if err := p.checkGate(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	ua := p.userAgent
	p.mu.Unlock()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		delay := time.Duration(seconds) * time.Second
		if when, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && time.Until(when) > delay {
			delay = time.Until(when)
		}
		return nil, &provider.HTTPError{StatusCode: resp.StatusCode, Status: fmt.Sprintf("HTTP %d", resp.StatusCode), RetryAfter: delay}
	}
	return ReadImageBytes(resp.Body)
}

func (p *Pool) Cancel(key string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range []string{key, "legacy:" + key} {
		if job := p.jobs[k]; job != nil {
			job.cancel()
		}
	}
}

func (p *Pool) CancelAllExcept(keep map[string]bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for key, job := range p.jobs {
		if !keep[key] && !(len(key) > 7 && key[:7] == "legacy:" && keep[key[7:]]) {
			job.cancel()
		}
	}
}

func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	jobs := make([]*sharedFetch, 0, len(p.jobs))
	for _, job := range p.jobs {
		job.cancel()
		jobs = append(jobs, job)
	}
	p.signalLocked()
	p.mu.Unlock()
	for _, job := range jobs {
		<-job.done
	}
	p.workers.Wait()
	p.client.CloseIdleConnections()
}

func (p *Pool) InflightCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.jobs)
}

func (p *Pool) Cache() *DiskCache { return p.cache }
