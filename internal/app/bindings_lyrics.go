package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
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
	documents  map[string]*lyrics.Document
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
	session := &lyricSession{ctx: ctx, cancel: cancel, trackID: trackID, created: time.Now(), candidates: map[string]lyrics.Candidate{}, documents: map[string]*lyrics.Document{}}
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
func (a *App) FetchLyrics(id, candidateID string) (*lyrics.Document, error) {
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
	return a.saveLyrics(s.ctx, s.trackID, doc.Text, revision)
}
func (a *App) saveLyrics(ctx context.Context, trackID int64, text string, revision int64) (int64, error) {
	doc, err := lyrics.ParseText(text)
	if err != nil {
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

type LyricsPickResult struct {
	Cancelled bool  `json:"cancelled"`
	Revision  int64 `json:"revision"`
}

func (a *App) PickAndAdoptLyrics(trackID, revision int64) (*LyricsPickResult, error) {
	if _, err := a.LyricsRevision(trackID); err != nil {
		return nil, err
	}
	pick := a.pickLyricsFile
	if pick == nil {
		pick = wruntime.OpenFileDialog
	}
	path, err := pick(a.ctx, wruntime.OpenDialogOptions{Title: "选择歌词", Filters: []wruntime.FileFilter{{DisplayName: "UTF-8 歌词（TXT / LRC）", Pattern: "*.txt;*.lrc"}}})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return &LyricsPickResult{Cancelled: true, Revision: revision}, nil
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".txt" && ext != ".lrc" {
		return nil, fmt.Errorf("请选择 UTF-8 编码的 .txt 或 .lrc 文件")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > lyrics.MaxTextBytes {
		return nil, fmt.Errorf("歌词必须是 2 MiB 以内的普通文本文件")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err = f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > lyrics.MaxTextBytes {
		return nil, fmt.Errorf("歌词必须是 2 MiB 以内的普通文本文件")
	}
	body, err := io.ReadAll(io.LimitReader(f, lyrics.MaxTextBytes+1))
	if err != nil {
		return nil, err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	rev, err := a.saveLyrics(ctx, trackID, string(body), revision)
	if err != nil {
		return nil, err
	}
	return &LyricsPickResult{Revision: rev}, nil
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
