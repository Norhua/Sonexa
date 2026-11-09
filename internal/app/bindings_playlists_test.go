package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/playlists"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type bindingPlaylist struct {
	reviewPlaylist
	fetch        func(context.Context, playlists.Ref) (*playlists.Snapshot, error)
	availability playlists.Availability
}

func (p *bindingPlaylist) PlaylistAvailability() playlists.Availability { return p.availability }
func (p *bindingPlaylist) FetchPlaylist(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
	if p.fetch != nil {
		return p.fetch(ctx, ref)
	}
	return p.reviewPlaylist.FetchPlaylist(ctx, ref)
}
func playlistApp(t *testing.T) (*App, *bindingPlaylist) {
	t.Helper()
	a, cleanup := newTestApp(t)
	a.enricher.Pause()
	// Binding/transaction tests advance the queue explicitly; separate tests
	// below exercise automatic Runner startup through production wiring.
	a.runner = nil
	p := &bindingPlaylist{availability: playlists.Availability{Available: true}}
	r, err := playlists.NewRegistry(p)
	if err != nil {
		t.Fatal(err)
	}
	a.playlistSources = r
	t.Cleanup(func() { a.shutdown(context.Background()); cleanup() })
	return a, p
}
func bindPlaylist(t *testing.T, a *App, id string) *playlists.CommitResult {
	t.Helper()
	preview, err := a.PreparePlaylistBinding("bind-"+id, "playlist-only", id, "别名 "+id)
	if err != nil || preview.Preview == nil {
		t.Fatal(preview, err)
	}
	result, err := a.CommitPlaylistBinding(preview.Preview.ID, preview.Preview.Hash)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func reviewSyncedItem(t *testing.T, a *App, batch int64) int64 {
	t.Helper()
	item, err := a.store.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err := a.store.MarkReview(item.ID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	return item.ID
}

func TestPlaylistBindingsValidationCancellationAndWireDTO(t *testing.T) {
	a, p := playlistApp(t)
	for _, input := range []struct{ op, source, id string }{{"", "playlist-only", "a"}, {"op", "unknown", "a"}, {"op", "playlist-only", ""}} {
		if _, err := a.PreparePlaylistBinding(input.op, input.source, input.id, ""); err == nil {
			t.Fatal("invalid accepted", input)
		}
	}
	p.availability = playlists.Availability{Reason: "运行环境缺失"}
	sources, err := a.ListPlaylistSources()
	if err != nil || len(sources) != 1 || sources[0].Available || sources[0].Reason != p.availability.Reason {
		t.Fatal(sources, err)
	}
	if _, err := a.PreparePlaylistBinding("unavailable", "playlist-only", "a", ""); playlists.Code(err) != playlists.RuntimeUnavailable {
		t.Fatal(err)
	}
	p.availability.Available = true
	preview, err := a.PreparePlaylistBinding("cancel", "playlist-only", "a", "")
	if err != nil {
		t.Fatal(err)
	}
	if lists, err := a.ListSyncedPlaylists(); err != nil || len(lists) != 0 {
		t.Fatal("preview wrote", lists, err)
	}
	if err := a.CancelPlaylistOperation("cancel"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitPlaylistBinding(preview.Preview.ID, preview.Preview.Hash); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal(err)
	}
	preview, err = a.PreparePlaylistBinding("valid", "playlist-only", "a", " alias ")
	if err != nil {
		t.Fatal(err)
	}
	plan := preview.Preview
	if _, err := a.CommitPlaylistBinding(plan.ID, "forged"); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal(err)
	}
	if _, err := a.CommitPlaylistSync(plan.ID, plan.Hash, []int64{999}); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal(err)
	}
	result, err := a.CommitPlaylistBinding(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := a.CommitPlaylistBinding(plan.ID, plan.Hash)
	if err != nil || *repeated != *result {
		t.Fatal(repeated, err)
	}
	duplicate, err := a.PreparePlaylistBinding("duplicate", "playlist-only", "a", "ignored")
	if err != nil || duplicate.Preview != nil || duplicate.ExistingPlaylistID != result.PlaylistID {
		t.Fatal(duplicate, err)
	}
	rows, err := a.GetSyncedPlaylistEntries(result.PlaylistID, "")
	if err != nil || len(rows) != 0 {
		t.Fatal("pending leaked", rows, err)
	}
	itemID := reviewSyncedItem(t, a, result.BatchID)
	queue, err := a.GetReviewQueue()
	if err != nil || queue.Total != 1 || len(queue.Items[0].ImportDestinations) != 1 {
		t.Fatal(queue, err)
	}
	trackID, err := a.NoMatchNeeded(itemID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err = a.GetSyncedPlaylistEntries(result.PlaylistID, "Song")
	if err != nil || len(rows) != 1 || rows[0].EntryID == 0 || rows[0].TrackID != trackID || !rows[0].UpstreamPresent || rows[0].InLibrary || rows[0].Track.AddedAt != "" {
		t.Fatal(rows, err)
	}
	lists, err := a.ListSyncedPlaylists()
	if err != nil || len(lists) != 1 || lists[0].Name != "alias" || lists[0].ImportedCount != 1 {
		t.Fatal(lists, err)
	}
	for _, value := range []string{lists[0].FetchedAt, lists[0].LastSyncedAt, plan.ExpiresAt} {
		if _, err := time.Parse(time.RFC3339, value); err != nil {
			t.Fatal(value, err)
		}
	}
	for _, value := range []any{rows, lists, preview, queue} {
		raw, err := json.Marshal(value)
		if err != nil || !json.Valid(raw) {
			t.Fatal(string(raw), err)
		}
	}
	for _, id := range []int64{-1, 999} {
		if _, err := a.GetSyncedPlaylistEntries(id, ""); err == nil {
			t.Fatal("invalid list accepted")
		}
		if _, err := a.PreparePlaylistSync("invalid", id); err == nil {
			t.Fatal("invalid sync accepted")
		}
		if err := a.DeleteSyncedPlaylist(id); err == nil {
			t.Fatal("invalid delete accepted")
		}
	}
}

func TestPlaylistBindingMembershipAndSuccessOnlyEvents(t *testing.T) {
	a, _ := playlistApp(t)
	var mu sync.Mutex
	events := map[string]int{}
	a.emit = func(name string, values ...any) {
		mu.Lock()
		defer mu.Unlock()
		events[name]++
		if name == EventSyncedPlaylistsChanged {
			payload := values[0].(SyncedPlaylistsChanged)
			if !payload.All || payload.Playlists == nil {
				t.Error(payload)
			}
		}
	}
	count := func() int { mu.Lock(); defer mu.Unlock(); return events[EventLibraryChanged] }
	bound := bindPlaylist(t, a, "a")
	bindPlaylist(t, a, "b")
	item := reviewSyncedItem(t, a, bound.BatchID)
	if err := a.DiscardReviewItem(item); err != nil {
		t.Fatal(err)
	}
	before := count()
	if err := a.DiscardReviewItem(item); err == nil || before != count() {
		t.Fatal("failure notified", err)
	}
	plan, err := a.PreparePlaylistSync("restore", bound.PlaylistID)
	if err != nil || len(plan.Difference.Ignored) != 1 {
		t.Fatal(plan, err)
	}
	result, err := a.CommitPlaylistSync(plan.ID, plan.Hash, []int64{plan.Difference.Ignored[0].EntryID})
	if err != nil {
		t.Fatal(err)
	}
	item = reviewSyncedItem(t, a, result.BatchID)
	id, err := a.NoMatchNeeded(item)
	if err != nil {
		t.Fatal(err)
	}
	addition, err := a.PrepareLibraryAddition([]int64{id, id})
	if err != nil || len(addition.WillAdd) != 1 {
		t.Fatal(addition, err)
	}
	before = count()
	if _, err := a.CommitLibraryAddition(addition.ID, "forged"); err == nil || count() != before {
		t.Fatal("forged addition accepted/notified")
	}
	if err := a.CancelLibraryAddition(addition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitLibraryAddition(addition.ID, addition.Hash); err == nil {
		t.Fatal("cancelled addition accepted")
	}
	addition, err = a.PrepareLibraryAddition([]int64{id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitLibraryAddition(addition.ID, addition.Hash); err != nil {
		t.Fatal(err)
	}
	rows, err := a.GetSyncedPlaylistEntries(bound.PlaylistID, "")
	if err != nil || len(rows) != 1 || !rows[0].InLibrary || rows[0].Track.AddedAt == "" {
		t.Fatal(rows, err)
	}
	if err := a.DeleteTracks([]int64{id}); err != nil {
		t.Fatal(err)
	}
	if track, err := a.store.GetTrack(id); err != nil || track == nil || track.InLibrary {
		t.Fatal(track, err)
	}
	if err := a.DeleteSyncedPlaylist(bound.PlaylistID); err != nil {
		t.Fatal(err)
	}
	if track, err := a.store.GetTrack(id); err != nil || track != nil {
		t.Fatal("exclusive track not collected", track, err)
	}
	before = count()
	if err := a.DeleteSyncedPlaylist(bound.PlaylistID); err == nil || count() != before {
		t.Fatal("failed deletion notified")
	}
	mu.Lock()
	defer mu.Unlock()
	if events[EventLibraryChanged] == 0 || events[EventLibraryChanged] != events[EventImportQueueChanged] || events[EventLibraryChanged] != events[EventSyncedPlaylistsChanged] {
		t.Fatal(events)
	}
}

func TestPlaylistOfflineReconcileNotifiesOnceAndCandidateMembershipIsCurrent(t *testing.T) {
	a, _ := playlistApp(t)
	bound := bindPlaylist(t, a, "offline")
	item := reviewSyncedItem(t, a, bound.BatchID)
	id, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Song", Credit: strPtr("Artist"), Album: strPtr("Album"), MatchSource: strPtr("musicbrainz"), MatchRef: strPtr("exact")})
	if err != nil {
		t.Fatal(err)
	}
	notices := 0
	a.emit = func(name string, _ ...any) {
		if name == EventLibraryChanged {
			notices++
		}
	}
	q, err := a.GetReviewQueue()
	if err != nil || q.Total != 0 || notices != 1 {
		t.Fatal(q, notices, err)
	}
	if _, err := a.GetTaskSummary(); err != nil || notices != 1 {
		t.Fatal("unchanged reads emitted loop", notices, err)
	}
	stored, err := a.store.GetItem(item)
	if err != nil || stored.Status != store.StatusSkipped {
		t.Fatal(stored, err)
	}
	rows, err := a.GetSyncedPlaylistEntries(bound.PlaylistID, "")
	if err != nil || len(rows) != 1 || rows[0].TrackID != id {
		t.Fatal(rows, err)
	}
	candidate := CandidateRow{Provider: "musicbrainz", MatchRef: "exact", InLibrary: true, LocalTrackID: 999}
	search, err := a.RefreshSearchCandidates([]CandidateRow{candidate})
	if err != nil || search[0].LocalTrackID != id || search[0].InLibrary || search[0].Action != "加入曲库" {
		t.Fatal(search, err)
	}
	a.applyLocalMatch(&candidate, candidate.toCandidate())
	if candidate.Action != "跳过" || candidate.InLibrary || candidate.LocalTrackID != id {
		t.Fatal(candidate)
	}
	plan, err := a.PrepareLibraryAddition([]int64{id})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitLibraryAddition(plan.ID, plan.Hash); err != nil {
		t.Fatal(err)
	}
	search, err = a.RefreshSearchCandidates(search)
	if err != nil || !search[0].InLibrary || search[0].Action != "已在库中" {
		t.Fatal(search, err)
	}
}

func TestPlaylistAutoStartAndShutdownWaitForAllRunnerGenerations(t *testing.T) {
	a, _ := playlistApp(t)
	base, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		t.Fatal(err)
	}
	started, stopped := make(chan struct{}), make(chan struct{})
	searcher := &blockingPlaylistSearcher{MetadataProvider: base, started: started, stopped: stopped, st: a.store}
	a.registry = provider.NewRegistry()
	a.registry.Register(searcher)
	a.rebuildRunner(a.settings.Get())
	bindPlaylist(t, a, "running")
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("binding did not start import")
	}
	// Publishing settings must not strand an old Runner or its invalidation.
	if err := a.SaveSettings(a.settings.Get()); err != nil {
		t.Fatal(err)
	}
	a.shutdown(context.Background())
	select {
	case <-stopped:
	default:
		t.Fatal("shutdown did not wait for old Runner")
	}
	if searcher.closeErr != nil {
		t.Fatal(searcher.closeErr)
	}
}

type blockingPlaylistSearcher struct {
	provider.MetadataProvider
	started, stopped chan struct{}
	st               *store.Store
	closeErr         error
}

func (p *blockingPlaylistSearcher) SearchTracks(ctx context.Context, query provider.Query) ([]provider.Candidate, error) {
	close(p.started)
	<-ctx.Done()
	p.closeErr = p.st.DB().Ping()
	close(p.stopped)
	return nil, ctx.Err()
}

func TestPlaylistAutomaticAndManualSkipNotifyAllDestinations(t *testing.T) {
	for _, mode := range []string{"automatic", "manual"} {
		t.Run(mode, func(t *testing.T) {
			a, _ := playlistApp(t)
			id, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{InLibrary: false, Title: "Song", Credit: strPtr("Artist"), Album: strPtr("Album"), MatchSource: strPtr("musicbrainz"), MatchRef: strPtr("exact")})
			if err != nil {
				t.Fatal(err)
			}
			bound := bindPlaylist(t, a, "a")
			bindPlaylist(t, a, "b")
			var mu sync.Mutex
			delivered := false
			a.emit = func(event string, _ ...any) {
				if event != EventLibraryChanged {
					return
				}
				rows, err := a.store.ListSyncedTracks(bound.PlaylistID, "")
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				delivered = len(rows) == 1 && rows[0].TrackID == id
				mu.Unlock()
			}
			if mode == "automatic" {
				a.rebuildRunner(a.settings.Get())
				if err := a.runner.Start(context.Background(), bound.BatchID); err != nil {
					t.Fatal(err)
				}
				waitFor(t, "offline skip", func() bool { return !a.runner.IsRunning(bound.BatchID) })
				a.runner.Close()
			} else {
				item := reviewSyncedItem(t, a, bound.BatchID)
				row := CandidateRow{Provider: "musicbrainz", MatchRef: "exact", LocalTrackID: 999, InLibrary: true}
				if err := a.SkipReviewCandidate(item, row); err != nil {
					t.Fatal(err)
				}
			}
			mu.Lock()
			notified := delivered
			mu.Unlock()
			if !notified {
				t.Fatal("delivery not visible at event time")
			}
			lists, err := a.ListSyncedPlaylists()
			if err != nil || len(lists) != 2 || lists[0].ImportedCount != 1 || lists[1].ImportedCount != 1 {
				t.Fatal(lists, err)
			}
			view, err := a.store.GetTrackView(id)
			if err != nil || view.InLibrary {
				t.Fatal("skip changed membership", view, err)
			}
		})
	}
}

func TestPlaylistFailedTransactionDoesNotNotify(t *testing.T) {
	a, _ := playlistApp(t)
	p, err := a.PreparePlaylistBinding("rollback", "playlist-only", "rollback", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec(`CREATE TRIGGER fail_playlist BEFORE INSERT ON synced_playlist_entry BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	notices := 0
	a.emit = func(string, ...any) { notices++ }
	if _, err := a.CommitPlaylistBinding(p.Preview.ID, p.Preview.Hash); err == nil {
		t.Fatal("injected failure not returned")
	}
	if notices != 0 {
		t.Fatal("rollback emitted success")
	}
	if lists, err := a.ListSyncedPlaylists(); err != nil || len(lists) != 0 {
		t.Fatal(lists, err)
	}
}

func TestPlaylistNewAutomaticDeliveryNotifiesBeforeEnrichment(t *testing.T) {
	a, p := playlistApp(t)
	candidate := fullCandidate("auto-new")
	base, err := a.registry.Get(provider.MusicBrainz)
	if err != nil {
		t.Fatal(err)
	}
	a.registry = provider.NewRegistry()
	a.registry.Register(&recordingSearcher{MetadataProvider: base, hit: []provider.Candidate{candidate}})
	a.rebuildRunner(a.settings.Get())
	p.fetch = func(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
		s, err := p.reviewPlaylist.FetchPlaylist(ctx, ref)
		s.Entries[0].Title, s.Entries[0].Album = candidate.Title, candidate.AlbumTitle
		s.Entries[0].Artists = []string{*candidate.ArtistName}
		return s, err
	}
	// Match ordinary first-release automatic admission; no network fixture is needed.
	bound := bindPlaylist(t, a, "auto")
	waitFor(t, "automatic import", func() bool { return !a.runner.IsRunning(bound.BatchID) })
	a.runner.Close()
	rows, err := a.GetSyncedPlaylistEntries(bound.PlaylistID, "")
	if err != nil || len(rows) != 1 || rows[0].InLibrary {
		t.Fatal(rows, err)
	}
}

func TestPlaylistAppLifecycleGatePublicationDeleteAndShutdown(t *testing.T) {
	for _, action := range []string{"disable", "delete", "shutdown", "publish"} {
		t.Run(action, func(t *testing.T) {
			a, p := playlistApp(t)
			bound := bindPlaylist(t, a, action)
			started, finish := make(chan struct{}), make(chan error, 1)
			release := make(chan struct{})
			p.fetch = func(ctx context.Context, ref playlists.Ref) (*playlists.Snapshot, error) {
				close(started)
				if action == "publish" {
					select {
					case <-release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					return p.reviewPlaylist.FetchPlaylist(ctx, ref)
				}
				<-ctx.Done()
				// Provider unwind must finish while the database is still open.
				if err := a.store.DB().Ping(); err != nil {
					return nil, fmt.Errorf("store closed before fetch unwind: %w", err)
				}
				return nil, ctx.Err()
			}
			go func() { _, err := a.PreparePlaylistSync("in-flight", bound.PlaylistID); finish <- err }()
			<-started
			svc, fetcher := a.playlistService, a.playlistFetcher
			switch action {
			case "disable", "publish":
				settings := a.settings.Get()
				if action == "disable" {
					settings.MusicBrainz.Contact = ""
				}
				if err := a.SaveSettings(settings); err != nil {
					t.Fatal(err)
				}
				if svc != a.playlistService || fetcher != a.playlistFetcher {
					t.Fatal("settings replaced lifecycle/plans")
				}
				if action == "publish" {
					close(release)
				}
			case "delete":
				if err := a.DeleteSyncedPlaylist(bound.PlaylistID); err != nil {
					t.Fatal(err)
				}
			case "shutdown":
				a.shutdown(context.Background())
			}
			select {
			case err := <-finish:
				if action == "publish" {
					if err != nil {
						t.Fatal(err)
					}
				} else if err == nil {
					t.Fatal("cancelled fetch succeeded")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("fetch did not terminate")
			}
			if action == "disable" {
				if _, err := a.PreparePlaylistBinding("disabled", "playlist-only", "new", ""); err == nil {
					t.Fatal("disabled read accepted")
				}
				if _, err := a.ListSyncedPlaylists(); err != nil {
					t.Fatal("offline browsing unavailable", err)
				}
			}
			if action == "publish" {
				sources, err := a.ListPlaylistSources()
				if err != nil {
					t.Fatal(err)
				}
				for _, source := range sources {
					if source.ID == "playlist-only" {
						t.Fatal("old registry published")
					}
				}
			}
		})
	}
}
