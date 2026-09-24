package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// Cache 是上游原始响应缓存的最小接口，由 store 实现。
//
// 定义在此而非直接依赖 store：provider 不应被存储层的其余部分绑住，
// 测试时也便于替换为内存实现。
type Cache interface {
	GetCache(provider, endpoint, paramsHash string) (*CacheEntry, error)
	PutCache(provider, endpoint, paramsHash string, body []byte, ttl time.Duration) error
}

// CacheEntry 是一条缓存记录。
type CacheEntry struct {
	Body      []byte
	FetchedAt time.Time
	TTL       time.Duration
}

// ErrCacheMiss 由 Cache 实现在未命中时返回。
var ErrCacheMiss = errors.New("缓存未命中")

// Client 是所有上游共用的 HTTP 客户端。
//
// 出网路径单一且可管控：User-Agent、限流、缓存、TTL 全部只有这一处实现。
type Client struct {
	HTTP      *http.Client
	Limiters  *Limiters
	Cache     Cache
	Retry     RetryPolicy
	UserAgent string
}

// NewClient 建立一个客户端。
func NewClient(limiters *Limiters, cache Cache, userAgent string) *Client {
	return &Client{
		HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("上游重定向次数过多")
			}
			return checkRequestGate(req.Context())
		}},
		Limiters:  limiters,
		Cache:     cache,
		Retry:     TransportRetryPolicy(),
		UserAgent: userAgent,
	}
}

// GetRequest 描述一次 GET 请求。
type GetRequest struct {
	Provider ProviderID
	// Endpoint 是缓存键的一部分，也是日志中的可读标识。
	Endpoint string
	// BaseURL + Endpoint + Params 构成完整地址。
	BaseURL string
	Params  map[string]string
	// TTL 是缓存有效期；0 表示不过期（MB 数据为 CC0 可长期留存）。
	TTL time.Duration
	// SkipCache 为真时绕过缓存直接请求（如用户手动「重新获取」）。
	SkipCache bool
	// ParamsHash 由调用方提供，通常为 store.ParamsHash(Params)。
	// 带 Body 的请求须把请求体也纳入哈希，否则不同请求会命中同一缓存。
	ParamsHash string
	// Body 非空时改发 POST JSON（QQ 音乐的 musicu.fcg 只接受 POST）。
	Body []byte
	// Headers 是附加请求头，如 Referer。
	Headers map[string]string
}

// Get 发起一次 GET 请求，先查缓存、再取令牌、最后出网。
//
// 响应先落盘再交给解析器：解析器是本项目最容易出错的部分，
// 而复现一次 bug 在 1 req/s 下代价高昂，落盘后可离线反复重放。
//
// 瞬时错误在这里就地重试，不上抛给调用方——MusicBrainz 即便在完全遵守
// 1 req/s 的前提下也会频繁返回 `503 … currently busy`，把这种抖动一路
// 传上去会让整批导入变成整批失败。
func (c *Client) Get(ctx context.Context, req GetRequest) ([]byte, error) {
	// 缓存命中时不消耗令牌
	if !req.SkipCache && c.Cache != nil && req.ParamsHash != "" {
		entry, err := c.Cache.GetCache(string(req.Provider), req.Endpoint, req.ParamsHash)
		if err == nil && entry != nil {
			return entry.Body, nil
		}
		if err != nil && !errors.Is(err, ErrCacheMiss) {
			return nil, err
		}
	}

	body, err := c.getWithRetry(ctx, req)
	if err != nil {
		return nil, err
	}

	if c.Cache != nil && req.ParamsHash != "" {
		if err := c.Cache.PutCache(string(req.Provider), req.Endpoint, req.ParamsHash, body, req.TTL); err != nil {
			// 缓存写入失败不影响本次结果，但要让调用方知道
			return body, nil
		}
	}
	return body, nil
}

// getWithRetry 取令牌、出网，并对可重试错误按策略退避重试。
//
// 每次尝试都重新取令牌：重试不是绕过限流的后门，退避等待与令牌等待相加，
// 实际速率只会比配置的更慢。
func (c *Client) getWithRetry(ctx context.Context, req GetRequest) ([]byte, error) {
	attempts := c.Retry.MaxAttempts
	if attempts <= 0 {
		attempts = TransportMaxAttempts
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		if c.Limiters != nil {
			if err := c.Limiters.Wait(ctx, req.Provider); err != nil {
				return nil, err
			}
		}

		body, err := c.do(ctx, req)
		if err == nil {
			return body, nil
		}
		lastErr = err

		// 不可重试的错误立刻上抛：400 的查询语法错误重试多少次都一样
		if !IsRetryable(err) || attempt == attempts {
			return nil, err
		}
		// ctx 已取消时不必再等退避
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(c.Retry.JitteredBackoff(attempt, err)):
		}
	}
	return nil, lastErr
}

// do 执行实际的 HTTP 请求。
func (c *Client) do(ctx context.Context, req GetRequest) ([]byte, error) {
	if err := checkRequestGate(ctx); err != nil {
		return nil, err
	}
	u, err := url.Parse(req.BaseURL + req.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("构造请求地址失败: %w", err)
	}
	q := u.Query()
	for k, v := range req.Params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	method, payload := http.MethodGet, io.Reader(nil)
	if req.Body != nil {
		method, payload = http.MethodPost, bytes.NewReader(req.Body)
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), payload)
	if err != nil {
		return nil, err
	}
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	// User-Agent 是 MusicBrainz 的硬性要求，统一在这里注入。
	if c.UserAgent != "" {
		httpReq.Header.Set("User-Agent", c.UserAgent)
	}
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 500 {
			snippet = snippet[:500]
		}
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After")),
			Body:       snippet,
		}
	}
	if readErr != nil {
		return nil, readErr
	}
	return body, nil
}

// Head 发起一次 HEAD 请求，用于验证构造出的图片地址是否可用。
// 图片服务不占用上游令牌桶，因此这里不取令牌。
func (c *Client) Head(ctx context.Context, rawURL string) error {
	if err := checkRequestGate(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HTTPError{StatusCode: resp.StatusCode, Status: resp.Status}
	}
	return nil
}

// UserAgentFor 构造 MusicBrainz 要求的规范 User-Agent：
//
//	Sonexa/<版本> ( <联系方式> )
//
// 这是 HTTP 头，不是查询参数；缺它可能被限流或 403。
func UserAgentFor(version, contact string) string {
	return fmt.Sprintf("Sonexa/%s ( %s )", version, contact)
}
