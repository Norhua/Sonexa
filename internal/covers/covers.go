package covers

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// Ref 是一条待预取的封面引用。
type Ref struct {
	// Key 是缓存键，前端以它构造 <img src>。
	Key string `json:"key"`
	// SourceRef 是上游对象标识（release MBID / iTunes collection ref）。
	SourceRef string `json:"sourceRef"`
	// Size 是目标尺寸，列表缩略图用 250。
	Size int `json:"size"`
	// URLs 仅兼容旧工作池调用，专辑服务忽略客户端 URL。
	URLs   []string `json:"urls"`
	Target Target   `json:"target"`
	Usage  Usage    `json:"usage"`
}

// LadderBuilder 由来源标识构造降级链。
//
// 降级链是 provider 特有逻辑，由绑定层注入，封面包不认识任何具体上游。
type LadderBuilder func(providerID, ref, releaseGroupRef string) []string

// Outcome 是一次取图的结果，供界面区分「上游没有这张封面」与「取图出错」。
type Outcome string

const (
	// OutcomeUnknown 表示这张图还没被取过。
	OutcomeUnknown Outcome = ""
	// OutcomeOK 表示取到了。
	OutcomeOK Outcome = "ok"
	// OutcomeEmpty 表示降级链走完每一档都是 404——上游确实没有这张封面。
	OutcomeEmpty Outcome = "empty"
	// OutcomeError 表示取图出错（网络、超时、上游 5xx）。
	OutcomeError Outcome = "error"
)

// maxOutcomes 是结果记录的条数上限。
//
// 它只是给界面看的提示，丢了不影响取图，因此超限直接整体丢弃，
// 不引入 LRU——为一个提示维护淘汰顺序不值得。
const maxOutcomes = 4096

// Service 把工作池、缓存与 handler 组装为一个整体。
type Service struct {
	prefetchMu sync.Mutex
	prefetches map[string]context.CancelFunc
	life       context.Context
	stop       context.CancelFunc
	operations sync.WaitGroup
	closing    bool
	pool       *Pool
	library    *LibraryImages
	// ladder 在 <img> 请求到达时按来源构造降级链。
	//
	// 早期实现要求前端先调 PrefetchCovers 把降级链登记进来，handler 只认登记过的键。
	// 那条路径有三个地方走不通：资料库表格与详情抽屉从来不调预取；复核队列也不调；
	// 而搜索页虽然调了，预取却是在行渲染**之后**才完成的，<img> 的请求先到，
	// 拿不到降级链就 404，再被前端记进「坏图」集合，此后永不重试。
	// 改为按请求参数就地构造后，登记与否不再影响取图，预取只是提前暖机。
	ladder LadderBuilder
	// 来源策略与详情加载由应用注入，复用元数据客户端的缓存、配额与动态配置。
	artwork       map[string]provider.ArtworkStrategy
	imagePolicies *provider.ImagePolicies
	detailLoader  func(context.Context, provider.Ref) (*provider.Detail, error)

	mu sync.Mutex
	// outcomes 记录各缓存键最近一次取图的结果，供 Outcomes 查询。
	outcomes map[string]Outcome
}

// NewService 建立封面服务。
func NewService(cache *DiskCache, concurrency int, userAgent string) *Service {
	life, stop := context.WithCancel(context.Background())
	return &Service{
		life: life, stop: stop, prefetches: map[string]context.CancelFunc{},
		pool:     NewPool(cache, concurrency, userAgent),
		outcomes: map[string]Outcome{},
		artwork:  map[string]provider.ArtworkStrategy{},
	}
}

// SetLadderBuilder 注入降级链构造器。
func (s *Service) SetLadderBuilder(fn LadderBuilder) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ladder = fn
}

// Outcomes 返回若干缓存键最近一次取图的结果。
func (s *Service) Outcomes(keys []string) []Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Outcome, len(keys))
	for i, k := range keys {
		out[i] = s.outcomes[k]
	}
	return out
}

func (s *Service) recordOutcome(key string, o Outcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.outcomes) >= maxOutcomes {
		s.outcomes = map[string]Outcome{}
	}
	s.outcomes[key] = o
}

func (s *Service) buildLadder(providerID, ref, rgRef string) []string {
	s.mu.Lock()
	fn := s.ladder
	s.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(providerID, ref, rgRef)
}

// Pool 暴露工作池。
func (s *Service) Pool() *Pool { return s.pool }

// Prefetch 批量预取一页列表的封面。
//
// 前端一页只调一次（整批一次，不是每行一次），
// 这样并发度由 Go 的池决定，不受 webview 每主机连接数的限制。
func (s *Service) Prefetch(ctx context.Context, refs []Ref) {
	for _, r := range refs {
		if s.library != nil {
			s.prefetchMu.Lock()
			if s.closing {
				s.prefetchMu.Unlock()
				return
			}
			if _, exists := s.prefetches[r.Key]; exists {
				s.prefetchMu.Unlock()
				continue
			}
			child, cancel := context.WithCancel(ctx)
			s.prefetches[r.Key] = cancel
			s.prefetchMu.Unlock()
			go func(ref Ref) {
				defer cancel()
				usage := ref.Usage
				if usage == "" {
					usage = Thumbnail
				}
				resolved, err := s.Resolve(child, ref.Target, usage)
				if child.Err() == nil {
					outcome := OutcomeError
					var disabled *OutboundError
					if errors.As(err, &disabled) {
						outcome = Outcome("disabled")
					} else if err == nil {
						outcome = resolved.Outcome
					}
					s.recordOutcome(ref.Key, outcome)
				}
				s.prefetchMu.Lock()
				delete(s.prefetches, ref.Key)
				s.prefetchMu.Unlock()
			}(r)
			continue
		}
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
	if s.library != nil {
		s.prefetchMu.Lock()
		defer s.prefetchMu.Unlock()
		for key, cancel := range s.prefetches {
			if !keep[key] {
				cancel()
			}
		}
		return
	}
	s.pool.CancelAllExcept(keep)
}

// Handler 返回处理 /cover 请求的 handler。
//
//	<img src="/cover?provider=musicbrainz&ref=<releaseMBID>&rg=<rgMBID>&size=250" loading="lazy">
//	<img src="/cover?adopted=<落盘路径>">
//
// 地址里带的是**来源**而不是缓存键：键是来源的哈希，不可逆，handler 拿着键
// 构造不出降级链，只能依赖别处先把链登记进来——那正是封面全都加载不出来的成因。
//
// 请求到达时若图仍在下载则等同一个结果（singleflight），已在盘则直接命中。
// 这样 <img> 的懒加载、滚出视口自动取消、解码不占主线程等原生行为全部保留。
func (s *Service) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.library != nil && r.URL.Query().Get("adopted") == "" {
			s.serveLibrary(w, r)
			return
		}
		// 已采用的封面按落盘路径直接读取；临时缩略图按来源标识取。
		// 两条路径都要能走通，否则用户采用过的封面显示不出来。
		if adoptedPath := r.URL.Query().Get("adopted"); adoptedPath != "" {
			if s.library != nil {
				registered, err := s.library.store.IsLegacyCoverPath(adoptedPath)
				if err != nil || !registered {
					http.NotFound(w, r)
					return
				}
				relative, err := filepath.Rel(s.library.files.root.Name(), adoptedPath)
				if err != nil {
					http.NotFound(w, r)
					return
				}
				data, err := s.library.files.Read(filepath.ToSlash(relative))
				if err != nil {
					http.NotFound(w, r)
					return
				}
				writeImage(w, data)
				return
			}
			data := s.pool.cache.GetAdopted(adoptedPath)
			if data == nil {
				http.NotFound(w, r)
				return
			}
			writeImage(w, data)
			return
		}

		q := r.URL.Query()
		providerID, ref := q.Get("provider"), q.Get("ref")
		if providerID == "" || ref == "" {
			http.Error(w, "缺少封面来源", http.StatusBadRequest)
			return
		}
		size, err := strconv.Atoi(q.Get("size"))
		if err != nil || size <= 0 {
			size = DefaultThumbSize
		}

		key := CacheKey(providerID+":"+ref, size)
		urls := s.buildLadder(providerID, ref, q.Get("rg"))

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()

		data, err := s.pool.Fetch(ctx, Fetcher{Key: key, URLs: urls})
		if err != nil {
			// 单张失败只影响该行。两种失败必须可区分：
			// 降级链走完都是 404 是「上游没有这张封面」（界面显示 nil），
			// 其余是「取图出错」（界面显示失败图标）——两者对用户的含义不同。
			if errors.Is(err, ErrNotFound) || len(urls) == 0 {
				s.recordOutcome(key, OutcomeEmpty)
				http.NotFound(w, r)
				return
			}
			s.recordOutcome(key, OutcomeError)
			http.Error(w, "取图失败", http.StatusBadGateway)
			return
		}

		s.recordOutcome(key, OutcomeOK)
		writeImage(w, data)
	})
}

// DefaultThumbSize 是列表缩略图的尺寸，也是 size 参数缺失时的取值。
const DefaultThumbSize = 250

func writeImage(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", contentTypeOf(data))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	// 由 Go 侧统一管控缓存，webview 自身不再缓存
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
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
