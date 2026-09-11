// Package covers 实现封面获取：进程级工作池、singleflight 请求合并、
// 磁盘 LRU 缓存与 Wails AssetServer handler。
//
// 封面字节由后端取得并写入磁盘缓存，再以本地地址供 <img> 加载——
// 不经 IPC 桥回传（JSON 序列化会把 []byte 变成 base64，体积 +33%，
// 且丢掉 <img> 自带的懒加载与滚出视口自动取消），
// 也不让前端直接出网（降级链是 provider 特有逻辑，且前端写不了文件、
// 设不了 User-Agent、缓存不可控）。
package covers

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DiskCache 是封面的磁盘 LRU 缓存。
//
// 分两类存放：
//   - 临时缩略图（tmp/）：列表用，带 TTL，受容量上限约束，可被「清除缓存」清空
//   - 已采用的封面（adopted/）：用户在详情抽屉确认采用的，按内容哈希落盘，
//     不受 TTL 与清除缓存影响
type DiskCache struct {
	mu       sync.Mutex
	dir      string
	maxBytes int64
	ttl      time.Duration
	// lastUsed 记录临时文件的最近访问时间，用于 LRU 淘汰。
	lastUsed map[string]time.Time
}

// NewDiskCache 建立磁盘缓存。
func NewDiskCache(dir string, maxBytes int64, ttl time.Duration) (*DiskCache, error) {
	c := &DiskCache{
		dir:      dir,
		maxBytes: maxBytes,
		ttl:      ttl,
		lastUsed: map[string]time.Time{},
	}
	for _, sub := range []string{c.tmpDir(), c.adoptedDir()} {
		if err := os.MkdirAll(sub, 0o755); err != nil {
			return nil, fmt.Errorf("创建封面缓存目录失败: %w", err)
		}
	}
	return c, nil
}

func (c *DiskCache) tmpDir() string     { return filepath.Join(c.dir, "tmp") }
func (c *DiskCache) adoptedDir() string { return filepath.Join(c.dir, "adopted") }

// CacheKey 由来源与尺寸构成缓存键。
//
// 同一张图在多行出现时共享同一个键，使并发请求可被合并。
func CacheKey(sourceRef string, size int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", sourceRef, size)))
	return hex.EncodeToString(sum[:16])
}

// Get 读取临时缓存中的图片；未命中或已过期时返回 nil。
func (c *DiskCache) Get(key string) []byte {
	path := c.tmpPath(key)
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if c.ttl > 0 && time.Since(info.ModTime()) > c.ttl {
		os.Remove(path)
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	c.mu.Lock()
	c.lastUsed[key] = time.Now()
	c.mu.Unlock()
	return data
}

// Put 写入临时缓存，并在超出容量上限时按 LRU 淘汰。
func (c *DiskCache) Put(key string, data []byte) error {
	if err := os.WriteFile(c.tmpPath(key), data, 0o644); err != nil {
		return fmt.Errorf("写入封面缓存失败: %w", err)
	}
	c.mu.Lock()
	c.lastUsed[key] = time.Now()
	c.mu.Unlock()

	return c.evictIfNeeded()
}

// Adopt 把一张图按内容哈希落盘到正式区。
//
// 用户在详情抽屉确认采用后调用；这些文件不受 TTL 与「清除缓存」影响。
// 返回落盘路径。
func (c *DiskCache) Adopt(data []byte, ext string) (string, error) {
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + normalizeExt(ext)
	path := filepath.Join(c.adoptedDir(), name)

	// 内容哈希相同即同一张图，已存在时不重复写
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("落盘封面失败: %w", err)
	}
	return path, nil
}

// Clear 清空临时缩略图缓存。
//
// 已确认采用并落盘的封面不受影响——它们是曲目数据的一部分。
func (c *DiskCache) Clear() error {
	c.mu.Lock()
	c.lastUsed = map[string]time.Time{}
	c.mu.Unlock()

	entries, err := os.ReadDir(c.tmpDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if err := os.Remove(filepath.Join(c.tmpDir(), e.Name())); err != nil {
			return err
		}
	}
	return nil
}

// evictIfNeeded 在超出容量上限时按最近最少使用淘汰临时文件。
func (c *DiskCache) evictIfNeeded() error {
	if c.maxBytes <= 0 {
		return nil
	}

	entries, err := os.ReadDir(c.tmpDir())
	if err != nil {
		return err
	}

	type fileInfo struct {
		name string
		size int64
		used time.Time
	}
	var files []fileInfo
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		key := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		c.mu.Lock()
		used, ok := c.lastUsed[key]
		c.mu.Unlock()
		if !ok {
			used = info.ModTime()
		}
		files = append(files, fileInfo{name: e.Name(), size: info.Size(), used: used})
		total += info.Size()
	}
	if total <= c.maxBytes {
		return nil
	}

	// 最久未使用的先淘汰
	sort.Slice(files, func(i, j int) bool { return files[i].used.Before(files[j].used) })
	for _, f := range files {
		if total <= c.maxBytes {
			break
		}
		if err := os.Remove(filepath.Join(c.tmpDir(), f.name)); err != nil {
			continue
		}
		total -= f.size
		c.mu.Lock()
		delete(c.lastUsed, strings.TrimSuffix(f.name, filepath.Ext(f.name)))
		c.mu.Unlock()
	}
	return nil
}

func (c *DiskCache) tmpPath(key string) string {
	return filepath.Join(c.tmpDir(), key+".img")
}

// Dir 返回缓存根目录。
func (c *DiskCache) Dir() string { return c.dir }

func normalizeExt(ext string) string {
	if ext == "" {
		return ".jpg"
	}
	if !strings.HasPrefix(ext, ".") {
		return "." + ext
	}
	return ext
}
