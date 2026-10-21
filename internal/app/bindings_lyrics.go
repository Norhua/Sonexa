package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

type lyricSession struct {
	ctx             context.Context
	cancel          context.CancelFunc
	trackID         int64
	created         time.Time
	service         *lyrics.Service
	mu              sync.Mutex
	searched        bool
	source          string
	query           lyrics.Query
	roundCtx        context.Context
	roundCancel     context.CancelFunc
	roundBudget     *provider.SendBudget
	restricted      error
	phase           string
	sequence        uint64
	prefetchStarted bool
	top             []string
	order           []string
	summaries       map[string]lyrics.CandidateSummary
	inflight        map[string]*lyricFlight
	versions        map[string]uint64
	previewed       map[string]uint64
	candidates      map[string]lyrics.Candidate
	documents       map[string]*lyricdoc.Document
	retention       *lyrics.RetentionBudget
	retainedBytes   int
}

var processLyricsRetention = lyrics.NewRetentionBudget(32<<20, 128<<20)

// close never waits for network work; cancelled workers finish their flights
// but cannot commit new documents. App lock may precede the session lock.
func (s *lyricSession) close() {
	s.cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.roundCancel != nil {
		s.roundCancel()
	}
	s.retention.Release(s.retainedBytes)
	s.retainedBytes = 0
	s.documents = map[string]*lyricdoc.Document{}
	s.previewed = nil
}

func (a *App) ListLyricsSources() []lyrics.Source {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.lyricSvc == nil {
		return []lyrics.Source{}
	}
	return a.lyricSvc.Sources.List()
}
func (a *App) LyricsRevision(trackID int64) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	return a.store.TrackRevision(trackID)
}
func (a *App) CreateLyricsSearch(trackID int64) (string, error) {
	if _, err := a.LyricsRevision(trackID); err != nil {
		return "", err
	}
	if err := a.checkCoverOutbound(); err != nil {
		return "", err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	ctx = provider.WithRequestGate(provider.WithPriority(ctx, provider.PriorityInteractive), a.checkCoverOutbound)
	session := &lyricSession{ctx: ctx, cancel: cancel, trackID: trackID, created: time.Now(), candidates: map[string]lyrics.Candidate{}, documents: map[string]*lyricdoc.Document{}}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lyricSvc == nil {
		cancel()
		return "", fmt.Errorf("歌词服务未就绪")
	}
	session.service = a.lyricSvc
	if a.lyricRetention == nil {
		a.lyricRetention = processLyricsRetention
	}
	session.retention = a.lyricRetention
	for id, s := range a.lyricSessions {
		if time.Since(s.created) > 30*time.Minute {
			s.close()
			delete(a.lyricSessions, id)
		}
	}
	if a.coverClosing || len(a.lyricSessions) >= 32 {
		cancel()
		return "", fmt.Errorf("歌词搜索会话不可用，请关闭旧搜索后重试")
	}
	if a.lyricSessions == nil {
		a.lyricSessions = map[string]*lyricSession{}
	}
	id := uuid.NewString()
	a.lyricSessions[id] = session
	return id, nil
}
func (a *App) CancelLyricsSearch(id string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.lyricSessions[id]; s != nil {
		s.close()
		delete(a.lyricSessions, id)
	}
}

// Registered work is drained before the store closes, just like cover searches.
func (a *App) lyricWork(id string) (*lyricSession, *lyrics.Service, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.lyricSessions[id]
	if s != nil && time.Since(s.created) > 30*time.Minute {
		s.close()
		delete(a.lyricSessions, id)
		s = nil
	}
	if a.coverClosing || s == nil || s.ctx.Err() != nil {
		return nil, nil, fmt.Errorf("歌词搜索已失效，请重新搜索")
	}
	a.lyricWorkers.Add(1)
	return s, s.service, nil
}
func (a *App) SearchLyrics(id, source string, q lyrics.Query) ([]lyrics.Candidate, error) {
	s, service, err := a.lyricWork(id)
	if err != nil {
		return nil, err
	}
	defer a.lyricWorkers.Done()
	if _, err := service.Sources.Get(provider.ProviderID(source)); err != nil {
		return nil, err
	}
	s.mu.Lock()
	if err := s.ctx.Err(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	if s.searched {
		s.mu.Unlock()
		return nil, fmt.Errorf("请为新搜索创建新会话")
	}
	s.searched = true
	s.source, s.query, s.phase = source, q, "searching"
	s.roundBudget = provider.NewSendBudget(16)
	s.roundCtx, s.roundCancel = context.WithDeadline(provider.WithSendBudget(s.ctx, s.roundBudget), time.Now().Add(60*time.Second))
	s.summaries = map[string]lyrics.CandidateSummary{}
	s.inflight = map[string]*lyricFlight{}
	s.versions = map[string]uint64{}
	s.previewed = map[string]uint64{}
	s.sequence++
	s.mu.Unlock()
	defer a.beginInteractive()()
	candidates, err := service.Search(s.roundCtx, source, q)
	if err != nil {
		s.mu.Lock()
		s.phase = "error"
		s.roundCancel()
		s.sequence++
		snapshot := s.snapshotLocked(id)
		s.mu.Unlock()
		a.emitEvent(EventLyricsProgress, snapshot)
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	for i := range candidates {
		candidates[i].ID = uuid.NewString()
		s.candidates[candidates[i].ID] = candidates[i]
		s.order = append(s.order, candidates[i].ID)
		summary := lyrics.CandidateSummary{Candidate: candidates[i], FetchState: "pending", WordTiming: "pending", Translation: pendingLyricChannel("pending"), Romanization: pendingLyricChannel("pending")}
		if i < 3 {
			s.top = append(s.top, candidates[i].ID)
			summary.Prefetch = true
		}
		s.summaries[candidates[i].ID] = summary
	}
	s.phase = "ready"
	if len(candidates) == 0 {
		s.phase = "done"
	}
	s.sequence++
	return candidates, nil
}

type LyricsPreview struct {
	Document *lyricdoc.Document `json:"document"`
	Version  uint64             `json:"version"`
}

func (a *App) FetchLyrics(id, candidateID string) (*LyricsPreview, error) {
	return a.lyricsPreview(id, candidateID, false)
}

// RefreshLyrics is an explicit single-candidate operation, never a new batch.
func (a *App) RefreshLyrics(id, candidateID string) (*LyricsPreview, error) {
	return a.lyricsPreview(id, candidateID, true)
}

func (a *App) lyricsPreview(id, candidateID string, refresh bool) (*LyricsPreview, error) {
	s, _, err := a.lyricWork(id)
	if err != nil {
		return nil, err
	}
	defer a.lyricWorkers.Done()
	doc, err := a.fetchLyricDocument(id, s, candidateID, false, refresh)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || s.documents[candidateID] != doc || s.inflight[candidateID] != nil {
		return nil, fmt.Errorf("歌词版本已变化，请重新预览")
	}
	return &LyricsPreview{Document: doc, Version: s.versions[candidateID]}, nil
}

// AcknowledgeLyricsPreview records the bridge's successful display of exactly
// this version. Fetch and prefetch alone never grant adoption permission.
func (a *App) AcknowledgeLyricsPreview(id, candidateID string, version uint64) error {
	s, _, err := a.lyricWork(id)
	if err != nil {
		return err
	}
	defer a.lyricWorkers.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil || version == 0 || s.documents[candidateID] == nil || s.versions[candidateID] != version || s.inflight[candidateID] != nil {
		return fmt.Errorf("歌词版本已变化，请重新预览")
	}
	if err := lyricdoc.Validate(s.documents[candidateID]); err != nil {
		return err
	}
	s.previewed[candidateID] = version
	return nil
}
func (a *App) AdoptLyrics(id, candidateID string, revision int64) (int64, error) {
	s, _, err := a.lyricWork(id)
	if err != nil {
		return 0, err
	}
	defer a.lyricWorkers.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.documents[candidateID]
	if doc == nil || s.ctx.Err() != nil || s.inflight[candidateID] != nil || s.previewed[candidateID] == 0 || s.previewed[candidateID] != s.versions[candidateID] {
		return 0, fmt.Errorf("请先获取并预览歌词")
	}
	return a.saveLyrics(s.ctx, s.trackID, doc, revision)
}
func (a *App) saveLyrics(ctx context.Context, trackID int64, doc *lyricdoc.Document, revision int64) (int64, error) {
	if err := lyricdoc.Validate(doc); err != nil {
		return 0, err
	}
	draft, err := a.store.StageLyrics(ctx, doc.Text)
	if err != nil {
		return 0, err
	}
	defer a.store.DiscardLyrics(draft)
	if err := a.store.SaveLyrics(ctx, trackID, &draft, revision); err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return revision + 1, nil
}

func (a *App) ClearLyrics(trackID, revision int64) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.store.SaveLyrics(ctx, trackID, nil, revision); err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return revision + 1, nil
}
