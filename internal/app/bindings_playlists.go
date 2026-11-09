package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/norhua/sonexa/internal/library"
	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// playlistWork registers the complete binding call, not just network reading.
// Shutdown must wait for projection, commit and notifications before Store.Close.
func (a *App) playlistWork() (*playlists.Service, context.Context, func(), error) {
	if err := a.ready(); err != nil {
		return nil, nil, nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coverClosing {
		return nil, nil, nil, fmt.Errorf("应用正在关闭")
	}
	if a.playlistService == nil {
		service, err := playlists.NewService(a.playlistSources, a.store, a.playlistFetcher, nil)
		if err != nil {
			return nil, nil, nil, err
		}
		a.playlistService = service
	}
	if a.playlistLife == nil {
		ctx := a.ctx
		if ctx == nil {
			ctx = context.Background()
		}
		a.playlistLife, a.cancelPlaylistWork = context.WithCancel(ctx)
	}
	a.playlistWorkers.Add(1)
	return a.playlistService, a.playlistLife, a.playlistWorkers.Done, nil
}

func (a *App) ListPlaylistSources() ([]playlists.Source, error) {
	svc, _, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	return svc.Sources(), nil
}

type SyncedPlaylistView struct {
	ID            int64  `json:"id"`
	Provider      string `json:"provider"`
	PlaylistRef   string `json:"playlistRef"`
	Name          string `json:"name"`
	UpstreamName  string `json:"upstreamName"`
	Alias         string `json:"alias"`
	Revision      int64  `json:"revision"`
	ImportedCount int    `json:"importedCount"`
	LastSyncedAt  string `json:"lastSyncedAt"`
	FetchedAt     string `json:"fetchedAt"`
}

func (a *App) ListSyncedPlaylists() ([]SyncedPlaylistView, error) {
	_, _, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	bindings, err := a.store.ListSyncedPlaylists()
	if err != nil {
		return nil, err
	}
	counts, err := a.store.SyncedImportedCounts()
	if err != nil {
		return nil, err
	}
	out := make([]SyncedPlaylistView, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, SyncedPlaylistView{ID: b.ID, Provider: string(b.Ref.Provider), PlaylistRef: b.Ref.ID, Name: b.DisplayName(), UpstreamName: b.UpstreamName, Alias: b.Alias, Revision: b.Revision, ImportedCount: counts[b.ID], LastSyncedAt: b.LastSyncedAt.UTC().Format(time.RFC3339), FetchedAt: b.FetchedAt.UTC().Format(time.RFC3339)})
	}
	return out, nil
}

func (a *App) GetSyncedPlaylistEntries(playlistID int64, query string) ([]store.SyncedTrackRow, error) {
	_, _, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	if playlistID <= 0 {
		return nil, playlists.ErrPlanStale
	}
	b, err := a.store.GetSyncedPlaylist(playlistID)
	if err != nil {
		return nil, err
	}
	if b == nil {
		return nil, playlists.ErrPlanStale
	}
	return a.store.ListSyncedTracks(playlistID, query)
}

// Duplicate binding is data, since Wails serializes errors as messages only.
// It lets the UI locate an existing playlist without parsing an error string.
type PlaylistBindingPreview struct {
	Preview            *playlists.Preview `json:"preview"`
	ExistingPlaylistID int64              `json:"existingPlaylistId"`
}

func (a *App) PreparePlaylistBinding(operationID, source, input, alias string) (*PlaylistBindingPreview, error) {
	svc, ctx, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	if err := a.checkCoverOutbound(); err != nil {
		return nil, err
	}
	defer a.beginInteractive()()
	ctx = provider.WithRequestGate(provider.WithPriority(ctx, provider.PriorityInteractive), a.checkCoverOutbound)
	preview, err := svc.PrepareBinding(ctx, operationID, source, input, alias)
	var duplicate *playlists.AlreadyBoundError
	if errors.As(err, &duplicate) {
		return &PlaylistBindingPreview{ExistingPlaylistID: duplicate.PlaylistID}, nil
	}
	if err != nil {
		return nil, err
	}
	return &PlaylistBindingPreview{Preview: preview}, nil
}
func (a *App) PreparePlaylistSync(operationID string, playlistID int64) (*playlists.Preview, error) {
	svc, ctx, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	if playlistID <= 0 {
		return nil, playlists.ErrPlanStale
	}
	if err := a.checkCoverOutbound(); err != nil {
		return nil, err
	}
	defer a.beginInteractive()()
	ctx = provider.WithRequestGate(provider.WithPriority(ctx, provider.PriorityInteractive), a.checkCoverOutbound)
	return svc.PrepareSync(ctx, operationID, playlistID)
}
func (a *App) CommitPlaylistBinding(planID, hash string) (*playlists.CommitResult, error) {
	return a.CommitPlaylistSync(planID, hash, nil)
}
func (a *App) CommitPlaylistSync(planID, hash string, restoreEntryIDs []int64) (*playlists.CommitResult, error) {
	svc, ctx, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	result, err := svc.Commit(ctx, planID, hash, restoreEntryIDs)
	if err != nil {
		return nil, err
	}
	a.notifyLibraryChanged()
	// The transaction is already durable. Runner startup failure cannot turn a
	// committed plan into a reported rollback; the persisted batch is resumable.
	a.mu.RLock()
	runner := a.runner
	a.mu.RUnlock()
	if result.BatchID != 0 && runner != nil {
		if batch, err := a.store.GetBatch(result.BatchID); err == nil && batch != nil && batch.Status == store.BatchRunning {
			if err := runner.Start(ctx, result.BatchID); err != nil {
				log.Printf("同步已提交，导入任务等待恢复: %v", err)
			}
		}
	}
	return result, nil
}
func (a *App) CancelPlaylistOperation(id string) error {
	svc, _, done, err := a.playlistWork()
	if err != nil {
		return err
	}
	defer done()
	svc.Cancel(id)
	return nil
}
func (a *App) DeleteSyncedPlaylist(playlistID int64) error {
	svc, ctx, done, err := a.playlistWork()
	if err != nil {
		return err
	}
	defer done()
	if playlistID <= 0 {
		return playlists.ErrPlanStale
	}
	if err := svc.Delete(ctx, playlistID); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// The client receives classifications for display, but commits only a token and
// hash. Neither a playlist snapshot nor a replacement target list is accepted.
func (a *App) PrepareLibraryAddition(trackIDs []int64) (*library.LibraryAdditionPlan, error) {
	_, ctx, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	svc, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return svc.PrepareLibraryAddition(ctx, trackIDs)
}
func (a *App) CommitLibraryAddition(planID, hash string) (*store.LibraryAddition, error) {
	_, ctx, done, err := a.playlistWork()
	if err != nil {
		return nil, err
	}
	defer done()
	svc, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	result, err := svc.CommitLibraryAddition(ctx, planID, hash)
	if err == nil {
		a.notifyLibraryChanged()
	}
	return result, err
}
func (a *App) CancelLibraryAddition(planID string) error {
	_, _, done, err := a.playlistWork()
	if err != nil {
		return err
	}
	defer done()
	svc, err := a.libraryService()
	if err != nil {
		return err
	}
	svc.CancelLibraryAddition(planID)
	return nil
}
