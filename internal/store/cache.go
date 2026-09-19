package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ErrCacheMiss 表示缓存未命中或已过期。
var ErrCacheMiss = errors.New("缓存未命中")

// ParamsHash 由请求参数计算稳定的缓存键分量。
// 参数按键排序后编码，保证同一组参数无论构造顺序如何都得到同一个哈希。
func ParamsHash(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(url.QueryEscape(k))
		b.WriteByte('=')
		b.WriteString(url.QueryEscape(params[k]))
		b.WriteByte('&')
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// CacheEntry 是一条上游原始响应缓存。
type CacheEntry struct {
	Body      []byte
	FetchedAt time.Time
	TTL       time.Duration
}

// PutCache 写入（或覆盖）一条上游原始响应。
//
// 先落盘后解析：解析器是最容易出错的部分，而复现一次 bug 在 1 req/s 下代价高昂。
// 落盘后可离线反复重放。
func (s *Store) PutCache(provider, endpoint, paramsHash string, body []byte, ttl time.Duration) error {
	_, err := s.db.Exec(`
		INSERT INTO provider_cache (provider, endpoint, params_hash, body, fetched_at, ttl_seconds)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, endpoint, params_hash) DO UPDATE SET
			body = excluded.body,
			fetched_at = excluded.fetched_at,
			ttl_seconds = excluded.ttl_seconds`,
		provider, endpoint, paramsHash, body, time.Now().UTC(), int64(ttl.Seconds()))
	if err != nil {
		return fmt.Errorf("写入上游缓存失败: %w", err)
	}
	return nil
}

// GetCache 读取一条未过期的缓存；未命中或已过期时返回 ErrCacheMiss。
//
// TTL 为 0 表示不过期（MusicBrainz 数据为 CC0，可长期留存）。
func (s *Store) GetCache(provider, endpoint, paramsHash string) (*CacheEntry, error) {
	var (
		body       []byte
		fetchedAt  time.Time
		ttlSeconds sql.NullInt64
	)
	err := s.db.QueryRow(`
		SELECT body, fetched_at, ttl_seconds FROM provider_cache
		WHERE provider = ? AND endpoint = ? AND params_hash = ?`,
		provider, endpoint, paramsHash).Scan(&body, &fetchedAt, &ttlSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("读取上游缓存失败: %w", err)
	}

	ttl := time.Duration(ttlSeconds.Int64) * time.Second
	if ttl > 0 && time.Since(fetchedAt) > ttl {
		return nil, ErrCacheMiss
	}
	return &CacheEntry{Body: body, FetchedAt: fetchedAt, TTL: ttl}, nil
}

// GetCacheRaw 不判过期地读取原始响应，供离线重放解析器使用。
func (s *Store) GetCacheRaw(provider, endpoint, paramsHash string) (*CacheEntry, error) {
	var (
		body       []byte
		fetchedAt  time.Time
		ttlSeconds sql.NullInt64
	)
	err := s.db.QueryRow(`
		SELECT body, fetched_at, ttl_seconds FROM provider_cache
		WHERE provider = ? AND endpoint = ? AND params_hash = ?`,
		provider, endpoint, paramsHash).Scan(&body, &fetchedAt, &ttlSeconds)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrCacheMiss
	}
	if err != nil {
		return nil, err
	}
	return &CacheEntry{
		Body:      body,
		FetchedAt: fetchedAt,
		TTL:       time.Duration(ttlSeconds.Int64) * time.Second,
	}, nil
}

// ClearCache 清空上游响应缓存。曲库数据与已采用的封面不受影响。
func (s *Store) ClearCache() error {
	_, err := s.db.Exec(`DELETE FROM provider_cache`)
	return err
}
