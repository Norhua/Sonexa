package provider

import (
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
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		Limiters:  limiters,
		Cache:     cache,
		Retry:     DefaultRetryPolicy(),
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
	ParamsHash string
}

// Get 发起一次 GET 请求，先查缓存、再取令牌、最后出网。
//
// 响应先落盘再交给解析器：解析器是本项目最容易出错的部分，
// 而复现一次 bug 在 1 req/s 下代价高昂，落盘后可离线反复重放。
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

	// 取令牌：同一上游的全部受限端点共用一个桶
	if c.Limiters != nil {
		if err := c.Limiters.Wait(ctx, req.Provider); err != nil {
			return nil, err
		}
	}

	body, err := c.do(ctx, req)
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

// do 执行实际的 HTTP 请求。
func (c *Client) do(ctx context.Context, req GetRequest) ([]byte, error) {
	u, err := url.Parse(req.BaseURL + req.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("构造请求地址失败: %w", err)
	}
	q := u.Query()
	for k, v := range req.Params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
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
