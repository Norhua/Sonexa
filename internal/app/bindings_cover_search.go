package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"sync"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/covers/sources"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

const EventCoverResults = "cover:results"
const EventCoverChanged = "cover:changed"

type CoverChangedEvent struct {
	ReleaseID int64  `json:"releaseId"`
	Revision  int64  `json:"revision"`
	Provider  string `json:"provider"`
	Ref       string `json:"ref"`
}

func (a *App) notifyCoverChanged(selection *store.CoverSelection) {
	event := CoverChangedEvent{ReleaseID: selection.ReleaseID, Revision: selection.Revision}
	if release, err := a.store.GetRelease(selection.ReleaseID); err == nil && release != nil {
		if release.Provider != nil {
			event.Provider = *release.Provider
		}
		if release.ProviderRef != nil {
			event.Ref = *release.ProviderRef
		}
	}
	a.emitEvent(EventCoverChanged, event)
	a.notifyLibraryChanged()
}

type CoverSearchRequest struct {
	SearchID   string   `json:"searchId"`
	Generation int64    `json:"generation"`
	ReleaseID  int64    `json:"releaseId"`
	Title      string   `json:"title"`
	Artist     string   `json:"artist"`
	Album      string   `json:"album"`
	Country    string   `json:"country"`
	Sources    []string `json:"sources"`
}

type CoverSearchCard struct {
	Source    string           `json:"source"`
	SourceRef string           `json:"sourceRef"`
	Image     store.CoverImage `json:"image"`
}

type CoverSearchEvent struct {
	SearchID   string            `json:"searchId"`
	Generation int64             `json:"generation"`
	ReleaseID  int64             `json:"releaseId"`
	Source     string            `json:"source"`
	State      string            `json:"state"`
	Cards      []CoverSearchCard `json:"cards"`
	Error      string            `json:"error"`
}

type coverSession struct {
	ctx       context.Context
	cancel    context.CancelFunc
	releaseID int64
	mu        sync.Mutex
	variants  map[int64]bool
	results   map[string]CoverSearchEvent
}

type CoverSourceInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	NeedsCountry bool   `json:"needsCountry"`
}

func (a *App) ListCoverSources() []CoverSourceInfo {
	out := make([]CoverSourceInfo, 0, len(sources.List()))
	for _, source := range sources.List() {
		out = append(out, CoverSourceInfo{source.ID, source.Name, source.NeedsCountry})
	}
	return out
}

func (a *App) StartCoverSearch(request CoverSearchRequest) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.checkCoverOutbound(); err != nil {
		return err
	}
	if a.coverSvc == nil || a.coverSvc.Library() == nil {
		return fmt.Errorf("封面服务未就绪")
	}
	if _, err := uuid.Parse(request.SearchID); err != nil {
		return fmt.Errorf("搜索会话标识无效")
	}
	release, err := a.store.GetRelease(request.ReleaseID)
	if err != nil {
		return err
	}
	if release == nil {
		return fmt.Errorf("没有所属歌集")
	}
	if len(request.Sources) == 0 {
		return fmt.Errorf("请选择封面来源")
	}
	seen := map[string]bool{}
	for _, source := range request.Sources {
		if source != "caa" && source != "itunes" && source != "qq" && source != "163" {
			return fmt.Errorf("未知封面来源")
		}
		if seen[source] {
			return fmt.Errorf("重复封面来源")
		}
		seen[source] = true
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	ctx = provider.WithRequestGate(ctx, a.checkCoverOutbound)
	session := &coverSession{ctx: ctx, cancel: cancel, releaseID: request.ReleaseID, variants: map[int64]bool{}, results: map[string]CoverSearchEvent{}}
	a.mu.Lock()
	if a.coverClosing {
		a.mu.Unlock()
		cancel()
		return fmt.Errorf("应用正在关闭")
	}
	if len(a.coverSessions) >= 32 {
		a.mu.Unlock()
		cancel()
		return fmt.Errorf("封面搜索会话过多，请关闭旧搜索")
	}
	if a.coverSessions == nil {
		a.coverSessions = map[string]*coverSession{}
	}
	if a.coverSessions[request.SearchID] != nil {
		a.mu.Unlock()
		cancel()
		return fmt.Errorf("搜索会话标识已使用，请新建会话")
	}
	a.coverSessions[request.SearchID] = session
	registry, svc := a.registry, a.coverSvc
	a.coverWorkers.Add(len(request.Sources))
	a.mu.Unlock()
	for _, source := range request.Sources {
		go func(source string) {
			defer a.coverWorkers.Done()
			defer a.beginInteractive()()
			q := provider.Query{Title: request.Title, Artist: request.Artist, Album: request.Album, Country: request.Country, Limit: 20, SkipCache: true}
			cards, err := a.searchCoverSource(ctx, registry, svc, source, release, q)
			if ctx.Err() != nil {
				return
			}
			event := CoverSearchEvent{SearchID: request.SearchID, Generation: request.Generation, ReleaseID: request.ReleaseID, Source: source, State: "ok", Cards: cards}
			if err != nil {
				event.State = "error"
				event.Error = err.Error()
			} else if len(cards) == 0 {
				event.State = "empty"
			}
			session.mu.Lock()
			session.results[source] = event
			for _, card := range cards {
				for _, v := range card.Image.Variants {
					session.variants[v.ID] = true
				}
			}
			session.mu.Unlock()
			a.mu.RLock()
			current := a.coverSessions[request.SearchID] == session
			a.mu.RUnlock()
			if current && ctx.Err() == nil {
				a.emitEvent(EventCoverResults, event)
			}
		}(source)
	}
	return nil
}

// CoverSearchResults 同时提供事件丢失后的快照恢复，并让绑定生成器看到事件模型。
func (a *App) CoverSearchResults(searchID string) []CoverSearchEvent {
	a.mu.RLock()
	session := a.coverSessions[searchID]
	a.mu.RUnlock()
	if session == nil {
		return []CoverSearchEvent{}
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	out := make([]CoverSearchEvent, 0, len(session.results))
	for _, result := range session.results {
		out = append(out, result)
	}
	return out
}

func (a *App) CancelCoverSearch(searchID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if session := a.coverSessions[searchID]; session != nil {
		session.cancel()
		delete(a.coverSessions, searchID)
	}
}

func (a *App) searchCoverSource(ctx context.Context, registry *provider.Registry, svc *covers.Service, source string, release *store.Release, q provider.Query) ([]CoverSearchCard, error) {
	var adapter sources.Searcher
	switch source {
	case "caa":
		p, err := registry.Get(provider.MusicBrainz)
		if err != nil {
			return nil, err
		}
		ref := ""
		if release.Provider != nil && *release.Provider == "musicbrainz" && release.ProviderRef != nil {
			ref = *release.ProviderRef
		}
		adapter = &sources.CAASource{Provider: p, ReleaseRef: ref, Catalog: svc.CAACatalog}
	case "itunes":
		p, err := registry.Get(provider.ITunes)
		if err != nil {
			return nil, err
		}
		adapter = &sources.ITunesSource{Provider: p}
	default:
		adapter = sources.NewWebSource(source, a.userAgent(a.settings.Get()), a.limiters, a.checkCoverOutbound)
	}
	results, searchErr := adapter.Search(ctx, q)
	cards := []CoverSearchCard{}
	for _, result := range results {
		if err := result.Validate(); err != nil {
			return cards, err
		}
		key := result.Key()
		if input := result.Input; input != nil {
			id, err := a.store.SaveCoverCatalog(key, input.Images)
			if err != nil {
				return cards, err
			}
			if err := a.store.SetCoverCatalogRelease(id, input.ActualReleaseURL); err != nil {
				return cards, err
			}
		}
		catalog, err := a.store.GetCoverCatalog(key)
		if err != nil {
			return cards, err
		}
		if catalog == nil {
			continue
		}
		for _, image := range catalog.Images {
			cards = append(cards, CoverSearchCard{Source: source, SourceRef: key.SourceRef, Image: image})
			if len(cards) >= sources.MaxResults {
				return cards, searchErr
			}
		}
	}
	return cards, searchErr
}

type CoverAdoptionResult struct {
	Cancelled bool  `json:"cancelled"`
	ReleaseID int64 `json:"releaseId"`
	Revision  int64 `json:"revision"`
	Width     int   `json:"width"`
	Height    int   `json:"height"`
	VariantID int64 `json:"variantId"`
}

func (a *App) AdoptCoverCandidate(searchID string, variantID, revision int64) (*CoverAdoptionResult, error) {
	a.mu.RLock()
	session := a.coverSessions[searchID]
	svc := a.coverSvc
	a.mu.RUnlock()
	if session == nil || session.ctx.Err() != nil {
		return nil, fmt.Errorf("封面搜索会话已结束")
	}
	if svc == nil || svc.Library() == nil {
		return nil, fmt.Errorf("封面服务未就绪")
	}
	release, err := a.store.GetRelease(session.releaseID)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, fmt.Errorf("目标歌集已删除")
	}
	selection, err := a.store.GetCoverSelection(session.releaseID)
	if err != nil {
		return nil, err
	}
	currentRevision := int64(0)
	if selection != nil {
		currentRevision = selection.Revision
	}
	if revision != currentRevision {
		return nil, store.ErrCoverSelectionChanged
	}
	session.mu.Lock()
	allowed := session.variants[variantID]
	session.mu.Unlock()
	if !allowed {
		return nil, fmt.Errorf("图片不属于当前搜索")
	}
	variant, err := a.store.GetCoverVariant(variantID)
	if err != nil {
		return nil, err
	}
	if variant == nil || !variant.Active {
		return nil, fmt.Errorf("图片规格不存在或已失效")
	}
	image, err := a.store.GetCoverImage(variant.ImageID)
	if err != nil {
		return nil, err
	}
	if image == nil {
		return nil, fmt.Errorf("图片不存在")
	}
	variants := append([]store.CoverVariant(nil), image.Variants...)
	sort.SliceStable(variants, func(i, j int) bool {
		if variants[i].ID == variants[j].ID {
			return false
		}
		if variants[i].ID == variantID {
			return true
		}
		if variants[j].ID == variantID {
			return false
		}
		a, _ := strconv.Atoi(variants[i].Spec)
		b, _ := strconv.Atoi(variants[j].Spec)
		return a > b
	})
	var failure error
	for _, v := range variants {
		if !v.Active {
			continue
		}
		data, err := svc.DownloadVariant(session.ctx, v)
		if err != nil {
			failure = err
			if session.ctx.Err() != nil || !errors.Is(err, covers.ErrNotFound) {
				return nil, err
			}
			continue
		}
		if session.ctx.Err() != nil {
			return nil, session.ctx.Err()
		}
		file, selection, err := svc.Library().SaveVariant(session.releaseID, v.ID, data, true, revision)
		if err != nil {
			return nil, err
		}
		a.notifyCoverChanged(selection)
		return &CoverAdoptionResult{ReleaseID: selection.ReleaseID, Revision: selection.Revision, Width: file.Width, Height: file.Height, VariantID: v.ID}, nil
	}
	return nil, fmt.Errorf("所选图片全部规格均不可用: %w", failure)
}

func (a *App) PickAndAdoptCover(releaseID, revision int64) (*CoverAdoptionResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if a.ctx == nil {
		return nil, fmt.Errorf("原生窗口未就绪")
	}
	if a.coverSvc == nil || a.coverSvc.Library() == nil {
		return nil, fmt.Errorf("封面服务未就绪")
	}
	release, err := a.store.GetRelease(releaseID)
	if err != nil {
		return nil, err
	}
	if release == nil {
		return nil, fmt.Errorf("没有所属歌集")
	}
	pick := a.pickCoverFile
	if pick == nil {
		pick = wruntime.OpenFileDialog
	}
	path, err := pick(a.ctx, wruntime.OpenDialogOptions{Title: "选择专辑封面", Filters: []wruntime.FileFilter{{DisplayName: "封面图片 (*.jpg;*.jpeg;*.png;*.webp)", Pattern: "*.jpg;*.jpeg;*.png;*.webp"}}})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return &CoverAdoptionResult{Cancelled: true}, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("封面必须是普通图片文件")
	}
	data, err := covers.ReadImageBytes(file)
	if err != nil {
		return nil, err
	}
	selection, err := a.coverSvc.Library().ImportLocal(releaseID, data, revision)
	if err != nil {
		return nil, err
	}
	a.notifyCoverChanged(selection)
	return &CoverAdoptionResult{ReleaseID: releaseID, Revision: selection.Revision}, nil
}

func (a *App) CoverRevision(releaseID int64) int64 {
	if a.store == nil {
		return 0
	}
	selection, err := a.store.GetCoverSelection(releaseID)
	if err != nil || selection == nil {
		return 0
	}
	return selection.Revision
}

// ClearCover 清除歌集封面并删除已保存的封面文件；revision 防止覆盖并发的新采用。
func (a *App) ClearCover(releaseID, revision int64) (*CoverAdoptionResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if a.coverSvc == nil || a.coverSvc.Library() == nil {
		return nil, fmt.Errorf("封面服务未就绪")
	}
	selection, err := a.coverSvc.Library().ClearRelease(releaseID, revision)
	if selection != nil {
		a.notifyCoverChanged(selection)
		return &CoverAdoptionResult{ReleaseID: releaseID, Revision: selection.Revision}, err
	}
	return nil, err
}
