package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 是 Wails 的绑定层：只做参数转换与事件发射，业务逻辑在 internal/ 各包中。
type App struct {
	ctx     context.Context
	version string

	paths    config.Paths
	settings *config.Store
	store    *store.Store
	registry *provider.Registry
	limiters *provider.Limiters
	client   *provider.Client
	coverSvc *covers.Service
	runner   *importer.Runner

	mu sync.RWMutex
	// startupErr 记录启动失败的原因，供前端显示。
	startupErr string
}

// NewApp 构造绑定层实例。真正的初始化在 startup 中完成。
func NewApp(version string) *App {
	return &App{version: version, registry: provider.NewRegistry()}
}

// startup 在 Wails 启动时调用。
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	if err := a.initialize(); err != nil {
		log.Printf("启动失败: %v", err)
		a.mu.Lock()
		a.startupErr = err.Error()
		a.mu.Unlock()
		return
	}

	// 恢复上次未完成的导入：中断的条目退回排队，已完成的不重复处理
	if a.runner != nil {
		if err := a.runner.ResumeAll(ctx); err != nil {
			log.Printf("恢复导入任务失败: %v", err)
		}
	}
}

// initialize 建立数据目录、载入设置、打开数据库并装配各模块。
func (a *App) initialize() error {
	paths, err := config.DefaultPaths()
	if err != nil {
		return err
	}
	if err := paths.Ensure(); err != nil {
		return err
	}
	a.paths = paths

	settings, err := config.NewStore(paths.AppDir)
	if err != nil {
		return err
	}
	a.settings = settings

	cur := settings.Get()
	a.paths = paths.WithCoverDir(cur.Cache.CoverDir)
	if err := a.paths.Ensure(); err != nil {
		return err
	}

	st, err := store.Open(a.paths.DBPath)
	if err != nil {
		return err
	}
	a.store = st

	a.limiters = provider.NewLimiters()
	a.rebuildProviders(cur)

	cache, err := covers.NewDiskCache(
		a.paths.CoverDir,
		cur.Cache.CoverMaxBytes,
		coverTTL(cur),
	)
	if err != nil {
		return err
	}
	a.coverSvc = covers.NewService(cache, cur.Cache.CoverConcurrency, a.userAgent(cur))

	a.rebuildRunner(cur)
	return nil
}

// rebuildProviders 按当前设置重建上游注册表与令牌桶。
//
// 改动联系方式或速率后即时生效，无需重启应用。
func (a *App) rebuildProviders(cur config.Settings) {
	a.client = provider.NewClient(a.limiters, a.store.AsProviderCache(), a.userAgent(cur))

	mb := musicbrainz.New(a.client, musicbrainz.Config{
		Contact:    cur.MusicBrainz.Contact,
		ServerURL:  cur.MusicBrainz.ServerURL,
		RatePerSec: cur.MusicBrainz.RatePerSec,
	}, store.ParamsHash)

	it := itunes.New(a.client, itunes.Config{
		DefaultCountry: cur.ITunes.DefaultCountry,
		RatePerMin:     cur.ITunes.RatePerMin,
		TTLDays:        cur.Cache.ITunesTracklistTTLd,
	}, store.ParamsHash)

	a.registry = provider.NewRegistry()
	a.registry.Register(mb)
	a.registry.Register(it)

	priority := make([]provider.ProviderID, 0, len(cur.ProviderPriority))
	for _, id := range cur.ProviderPriority {
		priority = append(priority, provider.ProviderID(id))
	}
	a.registry.SetPriority(priority)

	for _, p := range a.registry.List() {
		a.limiters.SetFromProvider(p)
	}
}

// rebuildRunner 按当前设置重建队列驱动器。
func (a *App) rebuildRunner(cur config.Settings) {
	mb, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		return
	}
	engine := matching.NewEngine(cur.Matching)
	retry := provider.DefaultRetryPolicy()
	if cur.Matching.MaxRetries > 0 {
		retry.MaxAttempts = cur.Matching.MaxRetries
	}

	// 导入阶段只用 MusicBrainz
	a.runner = importer.NewRunner(a.store, engine, mb, retry, a.emitProgress)
}

// emitProgress 把匹配进度推送给前端。
//
// 进度由后端主动推送而非前端轮询——17 分钟的任务上轮询会产生
// 数千次无谓的 IPC 调用。
func (a *App) emitProgress(p importer.Progress) {
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, EventImportProgress, p)
}

// 前端订阅的事件名。
const (
	EventImportProgress = "import:progress"
	EventSettingsSaved  = "settings:saved"
	EventLibraryChanged = "library:changed"
	// EventProviderResults 在每个上游完成时单独推送，
	// 使先返回的先显示，不被慢的上游阻塞。
	EventProviderResults = "upstream:providerResults"
)

// shutdown 在应用退出时释放资源。
func (a *App) shutdown(ctx context.Context) {
	if a.store != nil {
		if err := a.store.Close(); err != nil {
			log.Printf("关闭数据库失败: %v", err)
		}
	}
}

// coverAssetHandler 返回供 Wails AssetServer 使用的封面 handler。
//
// 不导出：它返回 http.Handler，不是给前端调用的绑定方法。
func (a *App) coverAssetHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.RLock()
		svc := a.coverSvc
		a.mu.RUnlock()
		if svc == nil {
			http.NotFound(w, r)
			return
		}
		svc.Handler().ServeHTTP(w, r)
	})
}

// userAgent 构造 MusicBrainz 要求的规范 User-Agent。
func (a *App) userAgent(cur config.Settings) string {
	return provider.UserAgentFor(a.version, cur.MusicBrainz.Contact)
}

func coverTTL(cur config.Settings) time.Duration {
	return time.Duration(cur.Cache.CoverTTLDays) * 24 * time.Hour
}

// ready 报告后端是否初始化成功。
func (a *App) ready() error {
	if a.store == nil {
		a.mu.RLock()
		msg := a.startupErr
		a.mu.RUnlock()
		if msg == "" {
			msg = "应用尚未完成初始化"
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}
