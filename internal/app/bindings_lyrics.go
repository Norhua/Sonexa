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
	ctx        context.Context
	cancel     context.CancelFunc
	trackID    int64
	created    time.Time
	service    *lyrics.Service
	mu         sync.Mutex
	searched   bool
	candidates map[string]lyrics.Candidate
	documents  map[string]*lyricdoc.Document
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
	for id, s := range a.lyricSessions {
		if time.Since(s.created) > 30*time.Minute {
			s.cancel()
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
		s.cancel()
		delete(a.lyricSessions, id)
	}
}

// Registered work is drained before the store closes, just like cover searches.
func (a *App) lyricWork(id string) (*lyricSession, *lyrics.Service, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.lyricSessions[id]
	if a.coverClosing || s == nil || s.ctx.Err() != nil || time.Since(s.created) > 30*time.Minute {
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
	if s.searched {
		s.mu.Unlock()
		return nil, fmt.Errorf("请为新搜索创建新会话")
	}
	s.searched = true
	s.mu.Unlock()
	defer a.beginInteractive()()
	candidates, err := service.Search(s.ctx, source, q)
	if err != nil {
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
	}
	return candidates, nil
}
func (a *App) FetchLyrics(id, candidateID string) (*lyricdoc.Document, error) {
	s, service, err := a.lyricWork(id)
	if err != nil {
		return nil, err
	}
	defer a.lyricWorkers.Done()
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.documents[candidateID]; d != nil {
		return d, nil
	}
	c, ok := s.candidates[candidateID]
	if !ok {
		return nil, fmt.Errorf("所选歌词不属于当前搜索")
	}
	defer a.beginInteractive()()
	doc, err := service.Fetch(s.ctx, c)
	if err != nil {
		return nil, err
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	s.documents[candidateID] = doc
	return doc, nil
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
	if doc == nil {
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
