package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/provider/qqmusic"
	"github.com/norhua/sonexa/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App 协调应用用例并提供 Wails 绑定；存储、匹配与上游原语在各领域包中。
type App struct {
	ctx     context.Context
	version string

	paths          config.Paths
	settings       *config.Store
	store          *store.Store
	library        *library.Service
	registry       *provider.Registry
	limiters       *provider.Limiters
	client         *provider.Client
	lyricSvc       *lyrics.Service
	coverSvc       *covers.Service
	coverSessions  map[string]*coverSession
	coverWorkers   sync.WaitGroup
	coverClosing   bool
	pickCoverFile  func(context.Context, runtime.OpenDialogOptions) (string, error)
	pickLyricsFile func(context.Context, runtime.OpenDialogOptions) (string, error)
	lyricSessions  map[string]*lyricSession
	lyricWorkers   sync.WaitGroup
	runner         *importer.Runner
	// enricher 是后台的「补全信息匹配」任务：入库不再同步补详情，
	// 详情由它在空闲时按条补上（见 enrichWorker）。
	enricher *enrichWorker

	// interactive 记录此刻有几个**用户触发**的出网请求在飞。
	// 后台补全据它让路——见 App.outboundBusy。
	interactive atomic.Int32

	// emit 是事件推送的唯一出口，为空时用 Wails 的运行时。
	//
	// 留这个接缝是因为 Wails 的 EventsEmit 在**非生命周期** context 上
	// 会直接结束进程（不是返回错误），于是任何走到推送的测试都会整包崩掉。
	// 测试把它换成记账函数。
	emit func(name string, data ...any)

	settingsMu sync.Mutex
	mu         sync.RWMutex
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

	// 上次退出时没补完的曲目在这里接着补。空闲判断在任务内部做：
	// 导入正在恢复时它会等，不与导入抢同一个 1 req/s。
	a.autoStartEnrich()
}

// autoStartEnrich 在空闲时自动开始补全信息匹配。
//
// 调用点是「刚出现新的待补条目」或「刚空闲下来」：启动、导入跑完、
// 接受一条候选、加入曲库。用户主动按过暂停时它不生效。
func (a *App) autoStartEnrich() {
	if a.enricher == nil {
		return
	}
	// 出网未启用（联系方式没填）时一条也补不了。此时不该起任务：
	// 每条都会失败一次，重试计数被白白烧掉，最后整个任务转 stalled，
	// 而真正的原因只是设置里少填了一个字段。
	if gate := a.CheckOutbound(); !gate.Allowed {
		return
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	a.enricher.AutoStart(ctx)
}

// outboundBusy 报告此刻后台补全该不该让路。
//
// 两种情形：导入匹配在跑，或用户触发的检索/详情请求在飞。
// 让的是**整个配额**而不只是次序——优先级闸门解决的是谁先发，
// 而用户等的那一次搜索仍会因为后台占用了这一秒的名额而多等一轮。
func (a *App) outboundBusy() (bool, string) {
	// 出网被关掉时整体等着，而不是逐条失败：设置里补上联系方式之后
	// 任务会自己接着跑，不需要用户再去点一次「开始」。
	if gate := a.CheckOutbound(); !gate.Allowed {
		return true, gate.Reason
	}
	if a.interactive.Load() > 0 {
		return true, "用户检索进行中"
	}
	if a.runner != nil && a.runner.AnyRunning() {
		return true, "导入匹配进行中"
	}
	return false, ""
}

// beginInteractive 标记一次用户触发的出网请求开始，返回结束时要调用的函数。
//
// 成对使用（`defer a.beginInteractive()()`）：漏掉结束会让后台补全永久让路。
func (a *App) beginInteractive() func() {
	a.interactive.Add(1)
	return func() {
		if a.interactive.Add(-1) == 0 {
			// 刚空闲下来：让后台补全接着跑
			a.autoStartEnrich()
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
	// 专辑视图选代表 release 与匹配阶段共用同一份阶梯配置——
	// 函数共用了而配置各走一套，漂移照样发生。
	a.store.SetLadderConfig(a.ladderConfig())

	a.limiters = provider.NewLimiters()
	a.rebuildProviders(cur)

	svc, err := newCoverService(a.store, a.paths, cur, a.version)
	if err != nil {
		return err
	}
	a.coverSvc = a.attachLadderBuilder(svc)
	if _, err := a.libraryService(); err != nil {
		return err
	}
	svc.EnableNetworkPolicy(a.checkCoverOutbound)

	a.rebuildRunner(cur)
	a.attachEnrichWorker()
	return nil
}

// attachEnrichWorker 建立后台补全任务并接好它的四条外部依赖。
//
// 抽成方法是有意的：测试辅助也走这一条接线。测试里另建一份接了一半的任务，
// 正是「某条依赖忘了接」这类 bug 能在测试全绿的情况下发布的原因
// （封面的降级链构造器就是这么漏掉过一次，见 attachLadderBuilder）。
func (a *App) attachEnrichWorker() {
	a.enricher = newEnrichWorker(a.store)
	a.enricher.enrich = a.enrichOne
	a.enricher.busy = a.outboundBusy
	a.enricher.emit = a.emitEnrichStatus
	// 整表重载只在一轮结束时做一次：逐条刷新会让 200 条的补全
	// 触发 200 次全表查询与 200 次重渲染。
	a.enricher.onLibraryChanged = a.notifyLibraryChanged
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
		// 上游响应缓存的有效期也是设置项。
		// MB 数据为 CC0 不受 TTL 约束，但用户可以在缓存分组里为自己
		// 保留的响应设置一个上限；为 0 时按不过期处理。
		CacheTTL: time.Duration(cur.Cache.ProviderCacheTTLh) * time.Hour,
	}, store.ParamsHash)

	it := itunes.New(a.client, itunes.Config{
		DefaultCountry: cur.ITunes.DefaultCountry,
		RatePerMin:     cur.ITunes.RatePerMin,
	}, store.ParamsHash)

	qq := qqmusic.New(a.client, qqmusic.Config{RatePerMin: cur.QQMusic.RatePerMin}, store.ParamsHash)
	lyricSvc, err := buildLyricsService(a.client, qq)
	if err != nil {
		panic(err) // Built-in IDs are fixed; atomic fallible assembly is task 6.1.
	}
	a.lyricSvc = lyricSvc

	a.registry = provider.NewRegistry()
	a.registry.Register(mb)
	a.registry.Register(it)
	a.registry.Register(qq)

	priority := make([]provider.ProviderID, 0, len(cur.ProviderPriority))
	for _, id := range cur.ProviderPriority {
		priority = append(priority, provider.ProviderID(id))
	}
	a.registry.SetPriority(priority)

	a.limiters.ConfigureFixedQuotas()
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
	// 最大重试次数与退避基数都是设置项，改动即时生效
	if cur.Matching.MaxRetries > 0 {
		retry.MaxAttempts = cur.Matching.MaxRetries
	}
	if cur.Matching.BackoffBaseSeconds > 0 {
		retry.BaseDelay = time.Duration(cur.Matching.BackoffBaseSeconds * float64(time.Second))
	}

	// 清单拉取时机是设置项：默认「首次打开专辑时」懒加载，
	// 改为「匹配时」则在导入入库后立即补详情与清单。
	fetchOnMatch := cur.Matching.TracklistTiming == config.TracklistOnMatch

	// 导入阶段只用 MusicBrainz
	service, err := a.libraryServiceLocked()
	if err != nil {
		return
	}
	a.runner = importer.NewRunner(a.store, engine, mb, retry, fetchOnMatch, a.emitProgress, service)
	a.runner.SetLibraryChanged(a.notifyLibraryChanged)
	if fetchOnMatch {
		// 走后台优先级与同一套补全状态机：这是导入循环内部的一次取数，
		// 不是用户在等的请求，标成交互式会让它抢在用户的搜索之前。
		a.runner.SetEnricher(func(trackID int64, providerID, releaseRef string) {
			target := store.EnrichTarget{TrackID: trackID, Provider: providerID, ReleaseRef: releaseRef}
			if err := a.enrichOne(context.Background(), target); err != nil {
				_ = a.store.RecordEnrichFailure(trackID, err.Error())
				return
			}
			_ = a.store.SetEnrichState(trackID, store.EnrichFetched)
			a.notifyLibraryChanged()
		})
	}
}

// emitEvent 把一个事件推给前端。全部推送都经由它。
func (a *App) emitEvent(name string, data ...any) {
	if a.emit != nil {
		a.emit(name, data...)
		return
	}
	if a.ctx == nil {
		return
	}
	runtime.EventsEmit(a.ctx, name, data...)
}

// emitEnrichStatus 把补全信息匹配的状态推送给前端。
func (a *App) emitEnrichStatus(s EnrichStatus) {
	a.emitEvent(EventEnrichProgress, s)
}

// emitProgress 把匹配进度推送给前端。
//
// 进度由后端主动推送而非前端轮询——17 分钟的任务上轮询会产生
// 数千次无谓的 IPC 调用。
func (a *App) emitProgress(p importer.Progress) {
	a.emitEvent(EventImportProgress, p)
	// 导入跑完就该轮到补全：这一批入库的曲目全都还等着补详情
	if p.Done {
		a.autoStartEnrich()
	}
}

// 前端订阅的事件名。
const (
	EventImportProgress = "import:progress"
	EventSettingsSaved  = "settings:saved"
	EventLibraryChanged = "library:changed"
	// EventEnrichProgress 是后台「补全信息匹配」的状态。
	EventEnrichProgress = "enrich:progress"
	// EventProviderResults 在每个上游完成时单独推送，
	// 使先返回的先显示，不被慢的上游阻塞。
	EventProviderResults = "upstream:providerResults"
)

// shutdown 在应用退出时释放资源。
func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	a.coverClosing = true
	for _, session := range a.coverSessions {
		session.cancel()
	}
	for _, session := range a.lyricSessions {
		session.cancel()
	}
	a.mu.Unlock()
	a.coverWorkers.Wait()
	a.lyricWorkers.Wait()
	if a.enricher != nil {
		a.enricher.Stop()
	}
	if a.coverSvc != nil {
		_ = a.coverSvc.Close()
	}
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

// newCoverService 按给定路径与设置建立封面缓存与工作池。
//
// 抽成函数是有意的：改动封面缓存目录时要重建一套新的，
// 与启动时用的是同一条构造路径，避免两处参数不一致。
func newCoverService(st *store.Store, paths config.Paths, cur config.Settings, version string) (*covers.Service, error) {
	cache, err := covers.NewDiskCache(paths.CoverDir, cur.Cache.CoverMaxBytes, coverTTL(cur))
	if err != nil {
		return nil, err
	}
	svc := covers.NewService(cache, cur.Cache.CoverConcurrency, provider.UserAgentFor(version, cur.MusicBrainz.Contact))
	svc.RegisterArtwork((&musicbrainz.Provider{}).ArtworkStrategy())
	svc.RegisterArtwork((&itunes.Provider{}).ArtworkStrategy())
	svc.RegisterArtwork((&qqmusic.Provider{}).ArtworkStrategy())
	if err := svc.AttachLibrary(st); err != nil {
		return nil, err
	}

	return svc, nil
}

// attachLadderBuilder 把降级链构造器接到封面服务上。
//
// 封面包不认识任何具体上游，降级链是 provider 特有逻辑，由这一层注入。
// 建服务的地方有两处（启动、改缓存目录后重建），因此接线必须走同一个函数，
// 否则重建出来的那份会少一条腿——封面在改过设置之后突然全都加载不出来。
func (a *App) attachLadderBuilder(svc *covers.Service) *covers.Service {
	if svc == nil {
		return nil
	}
	svc.SetDetailLoader(func(ctx context.Context, ref provider.Ref) (*provider.Detail, error) {
		a.mu.RLock()
		p, err := a.registry.Get(ref.Provider)
		a.mu.RUnlock()
		if err != nil {
			return nil, err
		}
		return p.FetchDetail(ctx, ref)
	})

	for _, p := range a.registry.List() {
		if source, ok := p.(provider.ArtworkStrategist); ok {
			svc.RegisterArtwork(source.ArtworkStrategy())
		}
	}
	svc.Pool().SetIndexParser(musicbrainz.ParseCAAThumbnailIndex)
	svc.SetLadderBuilder(func(providerID, ref, releaseGroupRef string) []string {
		var rg *string
		if releaseGroupRef != "" {
			rg = &releaseGroupRef
		}
		// MusicBrainz 用 ref 作 release MBID，iTunes 用它作基准图地址，
		// 两边各取所需——coverLadder 本就按来源分支。
		return a.coverLadder(providerID, ref, ref, rg)
	})
	return svc
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
