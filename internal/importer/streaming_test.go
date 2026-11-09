package importer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type importPlaylist struct{}

func (importPlaylist) ID() provider.ProviderID { return "playlist-only" }
func (importPlaylist) DisplayName() string     { return "同步目标" }
func (importPlaylist) PlaylistAvailability() playlists.Availability {
	return playlists.Availability{Available: true}
}
func (importPlaylist) ParsePlaylist(input string) (playlists.Ref, error) {
	return playlists.Ref{Provider: "playlist-only", ID: input}, nil
}
func (importPlaylist) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	return &playlists.Snapshot{Ref: ref, Name: "目标歌单", FetchedAt: time.Now().UTC(), Entries: []playlists.Entry{{EntryKey: "one:1", TrackRef: "one", Position: 1, Title: sptr("Song"), Artists: []string{"Artist"}, Album: sptr("Album")}}, Completeness: playlists.Completeness{Complete: true, DeclaredTotal: 1, RawSeen: 1, TrackCount: 1, Pages: 1}}, nil
}
func importSyncFixture(t *testing.T, st *store.Store) (*playlists.Service, *playlists.CommitResult) {
	t.Helper()
	registry, err := playlists.NewRegistry(importPlaylist{})
	if err != nil {
		t.Fatal(err)
	}
	fetcher := playlists.NewFetcher()
	svc, err := playlists.NewService(registry, st, fetcher, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close(); fetcher.Close() })
	preview, err := svc.PrepareBinding(context.Background(), "bind", "playlist-only", "list", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil)
	if err != nil {
		t.Fatal(err)
	}
	return svc, result
}
func TestRunnerSyncTargetsCreateOrReuseWithoutImplicitMembership(t *testing.T) {
	for _, mode := range []string{"sync", "mixed", "existing"} {
		t.Run(mode, func(t *testing.T) {
			st := openStore(t)
			_, binding := importSyncFixture(t, st)
			if mode == "mixed" {
				enqueueOne(t, st, "Song", "Artist", "Album")
			}
			if mode == "existing" {
				if _, err := st.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Song", Credit: sptr("Artist"), Album: sptr("Album")}); err != nil {
					t.Fatal(err)
				}
			}
			searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{"Song": {mbCandidate("track", "Song", "Artist", "Album")}}}
			r := newRunner(st, searcher)
			notified := 0
			r.SetLibraryChanged(func() {
				notified++
				entries, err := st.SyncedPlaylistState(binding.PlaylistID)
				if err != nil || entries[0].LocalState != playlists.Imported {
					t.Error("event preceded target delivery", entries, err)
				}
			})
			events := runToCompletion(t, r, binding.BatchID)
			entries, err := st.SyncedPlaylistState(binding.PlaylistID)
			if err != nil || entries[0].TrackID == nil {
				t.Fatal(entries, err)
			}
			track, err := st.GetTrack(*entries[0].TrackID)
			if err != nil || track.InLibrary != (mode == "mixed") {
				t.Fatal(track, err)
			}
			if notified != 1 {
				t.Fatal("missing/duplicate terminal event", notified)
			}
			if mode == "existing" && searcher.queryCount() != 0 {
				t.Fatal("duplicate made network requests")
			}
			found := false
			for _, event := range events {
				if strings.Contains(event.Line, "完成导入") || strings.Contains(event.Line, "复用本地") {
					found = true
				}
				if strings.Contains(event.Line, "直接入库") {
					t.Fatal(event.Line)
				}
			}
			if !found {
				t.Fatal(events)
			}
			queries := searcher.queryCount()
			runToCompletion(t, r, binding.BatchID)
			if notified != 1 || searcher.queryCount() != queries {
				t.Fatal("terminal reprocessed")
			}
		})
	}
}
