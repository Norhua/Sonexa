package covers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Ref 是一条待预取的封面引用。
type Ref struct {
	// Key 是缓存键，前端以它构造 <img src>。
	Key string `json:"key"`
	// SourceRef 是上游对象标识（release MBID / iTunes collection ref）。
	SourceRef string `json:"sourceRef"`
	// Size 是目标尺寸，列表缩略图用 250。
	Size int `json:"size"`
	// URLs 是降级链，由 Go 侧的 provider 逻辑生成。
	URLs []string `json:"urls"`
}

// Service 把工作池、缓存与 handler 组装为一个整体。
type Service struct {
	pool *Pool

	mu sync.RWMutex
	// ladders 记录各缓存键的降级链，供 handler 在 <img> 请求到达时使用。
	ladders map[string][]string
}

// NewService 建立封面服务。
func NewService(cache *DiskCache, concurrency int, userAgent string) *Service {
	return &Service{
		pool:    NewPool(cache, concurrency, userAgent),
		ladders: map[string][]string{},
	}
}

// Pool 暴露工作池。
func (s *Service) Pool() *Pool { return s.pool }

// Prefetch 批量预取一页列表的封面。
//
// 前端一页只调一次（整批一次，不是每行一次），
// 这样并发度由 Go 的池决定，不受 webview 每主机连接数的限制。
func (s *Service) Prefetch(ctx context.Context, refs []Ref) {
	s.mu.Lock()
	for _, r := range refs {
		s.ladders[r.Key] = r.URLs
	}
	s.mu.Unlock()

	for _, r := range refs {
		go func(ref Ref) {
			// 失败只影响该行，不向上传播
			_, _ = s.pool.Fetch(ctx, Fetcher{Key: ref.Key, URLs: ref.URLs})
		}(r)
	}
}

// CancelOutOfView 取消已滚出视口且尚未开始的任务，
// 让工作池转而服务当前视口内的行。
func (s *Service) CancelOutOfView(visibleKeys []string) {
	keep := make(map[string]bool, len(visibleKeys))
	for _, k := range visibleKeys {
		keep[k] = true
	}
	s.pool.CancelAllExcept(keep)
}

// Handler 返回处理 /cover 请求的 handler。
//
//	<img src="/cover?key=...&size=250" loading="lazy">
//
// 请求到达时若图仍在下载则等同一个结果（singleflight），已在盘则直接命中。
// 这样 <img> 的懒加载、滚出视口自动取消、解码不占主线程等原生行为全部保留。
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "缺少 key", http.StatusBadRequest)
			return
		}

		s.mu.RLock()
		urls := s.ladders[key]
		s.mu.RUnlock()

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		data, err := s.pool.Fetch(ctx, Fetcher{Key: key, URLs: urls})
		if err != nil {
			// 单张失败只影响该行：返回 404，前端显示占位图
			if errors.Is(err, ErrNotFound) || len(urls) == 0 {
				http.NotFound(w, r)
				return
			}
			http.Error(w, "取图失败", http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", contentTypeOf(data))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		// 由 Go 侧统一管控缓存，webview 自身不再缓存
		w.Header().Set("Cache-Control", "no-store")
		w.Write(data)
	})
}

// contentTypeOf 按字节前缀判断图片类型。
func contentTypeOf(data []byte) string {
	switch {
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8:
		return "image/jpeg"
	case len(data) > 8 && data[0] == 0x89 && data[1] == 'P':
		return "image/png"
	case len(data) > 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// AssetHandler 返回一个不做任何事的 handler，供尚未初始化服务时使用。
func AssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}
