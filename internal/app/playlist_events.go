package app

import "github.com/norhua/sonexa/internal/importer"

// Settings may publish a new Runner while an older batch is finishing. Keep
// cancellation and shutdown attached to every live generation, not just a.runner.
func (a *App) cancelInvalidatedImports() {
	a.runnerLifeMu.Lock()
	runners := append([]*importer.Runner{}, a.importRunners...)
	a.runnerLifeMu.Unlock()
	for _, runner := range runners {
		runner.CancelInvalidatedImports()
	}
}

// These are invalidation hints, never a replacement for an authoritative read.
// All also covers deleted bindings absent from the revisions list.
type SyncedPlaylistRevision struct {
	ID       int64 `json:"id"`
	Revision int64 `json:"revision"`
}
type SyncedPlaylistsChanged struct {
	All       bool                     `json:"all"`
	Playlists []SyncedPlaylistRevision `json:"playlists"`
}

func (a *App) notifySyncedPlaylistsChanged() {
	out := SyncedPlaylistsChanged{All: true, Playlists: []SyncedPlaylistRevision{}}
	if bindings, err := a.store.ListSyncedPlaylists(); err == nil {
		for _, b := range bindings {
			out.Playlists = append(out.Playlists, SyncedPlaylistRevision{ID: b.ID, Revision: b.Revision})
		}
	}
	a.emitEvent(EventSyncedPlaylistsChanged, out)
}

func (a *App) reconcileReviewDuplicates() error {
	changed, err := a.store.ReconcileReviewDuplicatesChanged()
	if err == nil && changed {
		a.notifyLibraryChanged()
	}
	return err
}
