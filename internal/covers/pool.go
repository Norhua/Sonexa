package covers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultConcurrency 是封面工作池的默认并发数。
const DefaultConcurrency = 8

// MaxConcurrency 是建议的并发上限。
const MaxConcurrency = 16

// Fetcher 是一个封面获取任务的来源描述。
type Fetcher struct {
	// Key 是缓存键；同一张图在多行出现时键相同，请求被合并。
	Key string
	// URLs 是降级链；图片不可得时可由注入的解析器展开链尾索引。
	URLs []string
}

// IndexParser 识别 provider 的图片索引并提取实际图片地址。
// 非索引地址返回 handled=false；仅由 Go 侧注入，不接受前端提供解析逻辑。
type IndexParser func(url string, body []byte) (urls []string, handled bool, err error)

// Pool 是进程级封面工作池。
//
// 取缩略图是延迟受限而非带宽受限的：单张常需数秒且 404 是常态，
// 串行取图会让列表长时间空白，因此必须并发。
//
// 该池的请求不占用任何上游的 API 令牌桶——封面档案与图片 CDN
// 都不是被限流的那个端点。因此「MB 1 req/s」与「封面 8 并发」并不矛盾。
type Pool struct {
	cache  *DiskCache
	client *http.Client
	group  singleflight.Group

	mu          sync.Mutex
	sem         chan struct{}
	inflight    map[string]context.CancelFunc
	userAgent   string
	indexParser IndexParser
}

// NewPool 建立工作池。concurrency 超出上限时被截断。
func NewPool(cache *DiskCache, concurrency int, userAgent string) *Pool {
	if concurrency <= 0 {
		concurrency = DefaultConcurrency
	}
	if concurrency > MaxConcurrency {
		concurrency = MaxConcurrency
	}
	return &Pool{
		cache:     cache,
		client:    &http.Client{Timeout: 30 * time.Second},
		sem:       make(chan struct{}, concurrency),
		inflight:  map[string]context.CancelFunc{},
		userAgent: userAgent,
	}
}

// SetIndexParser 在启动时注入索引解析器。索引与图片共用并发槽位和 singleflight。
func (p *Pool) SetIndexParser(parser IndexParser) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.indexParser = parser
}

// SetConcurrency 调整并发上限。
func (p *Pool) SetConcurrency(n int) {
	if n <= 0 {
		n = DefaultConcurrency
	}
	if n > MaxConcurrency {
		n = MaxConcurrency
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sem = make(chan struct{}, n)
}

// Concurrency 返回当前并发上限。
func (p *Pool) Concurrency() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return cap(p.sem)
}

// ErrNotFound 表示降级链确认无图；网络/服务错误不能伪装成无图。
var ErrNotFound = errors.New("封面不可得")

// Fetch 取一张封面。
//
// 按缓存键合并并发请求：同一张图在多行出现时只获取一次，
// 后到的请求等待同一个结果。
func (p *Pool) Fetch(ctx context.Context, f Fetcher) ([]byte, error) {
	// 已在盘则直接命中
	if data := p.cache.Get(f.Key); data != nil {
		return data, nil
	}

	// singleflight：同一键的并发请求共享一次获取
	v, err, _ := p.group.Do(f.Key, func() (any, error) {
		// 进入临界区后再查一次：等待期间可能已被别的请求写入
		if data := p.cache.Get(f.Key); data != nil {
			return data, nil
		}

		fetchCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		p.mu.Lock()
		p.inflight[f.Key] = cancel
		sem := p.sem
		p.mu.Unlock()

		defer func() {
			cancel()
			p.mu.Lock()
			delete(p.inflight, f.Key)
			p.mu.Unlock()
		}()

		// 取并发槽位
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-fetchCtx.Done():
			return nil, fetchCtx.Err()
		}

		data, err := p.fetchLadder(fetchCtx, f.URLs)
		if err != nil {
			return nil, err
		}
		if err := p.cache.Put(f.Key, data); err != nil {
			// 缓存写入失败不影响本次结果
			return data, nil
		}
		return data, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// fetchLadder 先尝试直接图片，必要时解析链尾索引。
// 只有全为 404 / 空索引才返回 ErrNotFound；保留其余错误供 UI 区分。
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
					// 索引只展开一层，不递归跟随任意 JSON。
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if p.userAgent != "" {
		req.Header.Set("User-Agent", p.userAgent)
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
		return nil, fmt.Errorf("取图返回 %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// Cancel 取消某个尚未完成的获取任务。
//
// 滚出视口且尚未开始的任务被取消，工作池转而服务当前视口内的行。
func (p *Pool) Cancel(key string) {
	p.mu.Lock()
	cancel, ok := p.inflight[key]
	p.mu.Unlock()
	if ok {
		cancel()
	}
}

// CancelAllExcept 取消除指定键之外的全部进行中任务。
// 用于快速滚动时把工作池让给当前视口。
func (p *Pool) CancelAllExcept(keep map[string]bool) {
	p.mu.Lock()
	var toCancel []context.CancelFunc
	for key, cancel := range p.inflight {
		if !keep[key] {
			toCancel = append(toCancel, cancel)
		}
	}
	p.mu.Unlock()

	for _, cancel := range toCancel {
		cancel()
	}
}

// InflightCount 返回进行中的任务数，供测试与诊断使用。
func (p *Pool) InflightCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.inflight)
}

// Cache 暴露磁盘缓存，供 handler 与设置页使用。
func (p *Pool) Cache() *DiskCache { return p.cache }
