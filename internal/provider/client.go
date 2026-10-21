package provider

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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

// Client 为上游 API 提供共享传输设施。
//
// API 共用门禁、请求级重定向、缓存模式、有限重试及平台配额。
// CAA 索引和图片使用封面工作池及专用传输，不消费这些 API 桶。
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

// CacheMode 明确区分正常缓存、刷新写回与完全不缓存。
type CacheMode uint8

const (
	UseCache     CacheMode = iota // 可读可写；命中不取令牌。
	RefreshCache                  // 跳过读取，成功响应仍写回。
	NoStore                       // 零缓存读取、零缓存写入，与哈希是否存在无关。
)

// CacheModeForRefresh 保留公开 Query/Ref.SkipCache 的刷新含义。
func CacheModeForRefresh(refresh bool) CacheMode {
	if refresh {
		return RefreshCache
	}
	return UseCache
}

// RedirectPolicy 将一次 API 请求的重定向限制在指定 HTTPS 主机内。
// Hosts 使用完整 URL Host（默认端口省略）；不接受用户信息或隐式子域名。
// MaxRedirects 为零时最多跟随 5 次；负值禁止跟随。
type RedirectPolicy struct {
	Hosts        []string
	MaxRedirects int
}

// GetRequest 描述一次 GET 或带请求体的 POST 请求。
type GetRequest struct {
	Provider ProviderID
	// Endpoint 是缓存键的一部分，也是日志中的可读标识。
	Endpoint string
	// BaseURL + Endpoint + Params 构成完整地址。
	BaseURL string
	Params  map[string]string
	// TTL 是缓存有效期；0 表示不过期（MB 数据为 CC0 可长期留存）。
	TTL       time.Duration
	CacheMode CacheMode
	// ParamsHash 由调用方提供，通常为 store.ParamsHash(Params)。
	// 带 Body 的请求须把请求体也纳入哈希，否则不同请求会命中同一缓存。
	ParamsHash string
	// Body 非空时改发 POST JSON（QQ 音乐的 musicu.fcg 只接受 POST）。
	Body []byte
	// Headers 是附加请求头，如 Referer。
	Headers map[string]string
	// MaxResponseBytes bounds opt-in text endpoints; zero preserves existing callers.
	MaxResponseBytes int64
	Redirects        *RedirectPolicy
}

// Get 按缓存模式处理响应，再经共享配额发起 GET/POST 请求。
//
// 允许缓存的响应先落盘再交给解析器，方便离线重放；NoStore 完全不触碰缓存。
//
// 瞬时错误在这里就地重试，不上抛给调用方——MusicBrainz 即便在完全遵守
// 1 req/s 的前提下也会频繁返回 `503 … currently busy`，把这种抖动一路
// 传上去会让整批导入变成整批失败。
func (c *Client) Get(ctx context.Context, req GetRequest) ([]byte, error) {
	if req.CacheMode > NoStore {
		return nil, fmt.Errorf("未知响应缓存模式")
	}
	// 缓存命中时不消耗令牌
	if req.CacheMode == UseCache && c.Cache != nil && req.ParamsHash != "" {
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

	if req.CacheMode != NoStore && c.Cache != nil && req.ParamsHash != "" {
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
		// Budgeted operations take quota at each actual send, including
		// redirects. Unbudgeted callers retain their existing retry semantics.
		if c.Limiters != nil && sendBudget(ctx) == nil {
			if err := c.Limiters.Wait(ctx, req.Provider); err != nil {
				return nil, err
			}
		}

		body, err := c.do(ctx, req)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if budget := sendBudget(ctx); budget != nil && restrictedTransportError(err) {
			budget.Stop(err)
			return nil, err
		}

		// 不可重试的错误立刻上抛：400 的查询语法错误重试多少次都一样
		if stopRestrictedRetry(ctx, err) || !IsRetryable(err) || attempt == attempts {
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

	client := c.httpForRequest(req.Redirects)
	if budget := sendBudget(ctx); budget != nil {
		base := client.Transport
		if base == nil {
			base = http.DefaultTransport
		}
		client.Transport = budgetTransport{base: base, client: c, quota: req.Provider, budget: budget}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := io.Reader(resp.Body)
	if req.MaxResponseBytes > 0 {
		reader = io.LimitReader(resp.Body, req.MaxResponseBytes+1)
	}
	body, readErr := io.ReadAll(reader)
	if req.MaxResponseBytes > 0 && int64(len(body)) > req.MaxResponseBytes {
		return nil, ErrResponseTooLarge
	}
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

// httpForRequest 只复制 Client，复用 Transport；从不修改共享重定向回调。
// 即使调用方替换了 HTTP Client，每次重定向仍需检查公共门禁。
func (c *Client) httpForRequest(policy *RedirectPolicy) *http.Client {
	client := *c.HTTP
	previous := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := req.Context().Err(); err != nil {
			return err
		}
		if err := checkRequestGate(req.Context()); err != nil {
			return err
		}
		if policy != nil {
			limit := policy.MaxRedirects
			if limit == 0 {
				limit = 5
			}
			allowed := false
			for _, host := range policy.Hosts {
				if strings.EqualFold(req.URL.Host, host) {
					allowed = true
					break
				}
			}
			if len(via) > limit || req.URL.Scheme != "https" || req.URL.User != nil || !allowed {
				return fmt.Errorf("上游重定向超出允许范围")
			}
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("上游重定向次数过多")
		}
		return nil
	}
	return &client
}

// UserAgentFor 构造 MusicBrainz 要求的规范 User-Agent：
//
//	Sonexa/<版本> ( <联系方式> )
//
// 这是 HTTP 头，不是查询参数；缺它可能被限流或 403。
func UserAgentFor(version, contact string) string {
	return fmt.Sprintf("Sonexa/%s ( %s )", version, contact)
}
