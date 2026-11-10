package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/playlists"
)

// Combine durable state transitions with actual close/reopen boundaries. The
// source is the only fake; plans, queue operations and delivery use production
// services and SQLite transactions, not test-side state machines.
func TestPlaylistRestartPreservesSharedDemandAndRecovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restart.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			_ = s.Close()
		}
	}()
	source := &syncSource{snapshot: syncTracks("Song")}
	svc := newSyncService(t, s, source)
	restart := func() {
		t.Helper()
		svc.Close()
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		s, err = Open(path)
		if err != nil {
			t.Fatal(err)
		}
		svc = newSyncService(t, s, source)
	}
	first := bindSync(t, svc)
	bind := func(ref string) (*playlists.Preview, *playlists.CommitResult) {
		t.Helper()
		p, err := svc.PrepareBinding(context.Background(), "bind-"+ref, "playlist-only", ref, ref)
		if err != nil {
			t.Fatal(err)
		}
		r, err := svc.Commit(context.Background(), p.ID, p.Hash, nil)
		if err != nil {
			t.Fatal(err)
		}
		return p, r
	}
	source.snapshot.Entries[0].Title = ptr("Song (译名)")
	secondPlan, second := bind("second")
	_, third := bind("third")
	item, err := s.ClaimNextItem(first.BatchID)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	token, err := s.BeginReviewSearch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishReviewSearch(item.ID, token, "qq", "", "[]", `["saved manual draft"]`); err != nil {
		t.Fatal(err)
	}
	originals, err := s.OriginalInputs(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	lateToken, err := s.BeginReviewSearch(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	unconfirmed, err := svc.PrepareSync(context.Background(), "unconfirmed", second.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}

	// A real outstanding result returns only after the main input's binding
	// disappears. Remaining targets and the saved manual draft must survive.
	release, late := make(chan struct{}), make(chan error, 1)
	go func() {
		<-release
		late <- s.FinishReviewSearch(item.ID, lateToken, "stale", "", "[]", `["late result"]`)
	}()
	deleteErr := svc.Delete(context.Background(), first.PlaylistID)
	close(release)
	lateErr := <-late
	if deleteErr != nil || !errors.Is(lateErr, ErrItemNotActive) {
		t.Fatal(deleteErr, lateErr)
	}
	restart()
	if _, err := svc.Commit(context.Background(), unconfirmed.ID, unconfirmed.Hash, nil); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal("unconfirmed plan survived restart", err)
	}
	replayed, err := svc.Commit(context.Background(), secondPlan.ID, secondPlan.Hash, nil)
	if err != nil || !reflect.DeepEqual(replayed, second) {
		t.Fatal(replayed, err)
	}
	unchanged, err := s.OriginalInputs(item.ID)
	if err != nil || !reflect.DeepEqual(originals, unchanged) {
		t.Fatal(unchanged, err)
	}
	effective, err := s.EffectiveImportItem(item.ID)
	if err != nil || effective.ID != item.ID || stringValue(effective.InTitle) != "Song (译名)" || stringValue(effective.TriedVariants) != `["saved manual draft"]` {
		t.Fatal(effective, err)
	}
	targets, err := s.ActiveImportDestinations(item.ID)
	if err != nil || len(targets) != 2 || *targets[0].PlaylistID != second.PlaylistID || *targets[1].PlaylistID != third.PlaylistID {
		t.Fatal(targets, err)
	}
	if err := s.MarkDiscarded(item.ID); err != nil {
		t.Fatal(err)
	}
	restart()
	if err := s.FinishReviewSearch(item.ID, lateToken, "stale", "", "[]", "[]"); !errors.Is(err, ErrItemNotActive) {
		t.Fatal(err)
	}
	if rows, err := s.AllReviewItems(); err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	for _, id := range []int64{second.PlaylistID, third.PlaylistID} {
		state, err := s.ReadPlaylistState(id)
		if err != nil || len(state.Entries) != 1 || state.Entries[0].LocalState != playlists.Ignored {
			t.Fatal(state, err)
		}
	}
	p, err := svc.PrepareSync(context.Background(), "restore-second", second.PlaylistID)
	if err != nil || len(p.Difference.Ignored) != 1 {
		t.Fatal(p, err)
	}
	restore := []int64{p.Difference.Ignored[0].EntryID}
	before, err := s.ReadPlaylistState(second.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("restart acceptance rollback") }
	_, commitErr := svc.Commit(context.Background(), p.ID, p.Hash, restore)
	s.beforeCommit = nil
	if commitErr == nil {
		t.Fatal("rollback injection ignored")
	}
	after, err := s.ReadPlaylistState(second.PlaylistID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(after, err)
	}
	if result, err := s.PlaylistOperationResult(p.ID, p.Hash); err != nil || result != nil {
		t.Fatal(result, err)
	}

	// Concurrent double-confirmation shares a single committed batch. Restart
	// must replay that durable result, not resurrect the discarded/merged inputs.
	type outcome struct {
		result *playlists.CommitResult
		err    error
	}
	start, done := make(chan struct{}), make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			r, e := svc.Commit(context.Background(), p.ID, p.Hash, restore)
			done <- outcome{r, e}
		}()
	}
	close(start)
	one, two := <-done, <-done
	if one.err != nil || two.err != nil || !reflect.DeepEqual(one.result, two.result) {
		t.Fatal(one, two)
	}
	restart()
	replayed, err = svc.Commit(context.Background(), p.ID, p.Hash, restore)
	if err != nil || !reflect.DeepEqual(replayed, one.result) {
		t.Fatal(replayed, err)
	}
	restored, err := s.ClaimNextItem(replayed.BatchID)
	if err != nil || restored == nil || restored.ID == item.ID {
		t.Fatal(restored, err)
	}
	targets, err = s.ActiveImportDestinations(restored.ID)
	if err != nil || len(targets) != 1 || *targets[0].PlaylistID != second.PlaylistID {
		t.Fatal(targets, err)
	}
	plan := admissionForSync(t, s, restored.ID, false)
	result, err := s.CommitAdmission(plan, domain.AdmissionApproval{})
	if err != nil {
		t.Fatal(err)
	}
	restart()
	if next, err := s.ClaimNextItem(replayed.BatchID); err != nil || next != nil {
		t.Fatal(next, err)
	}
	track, err := s.GetTrack(result.TrackID)
	if err != nil || track == nil || track.InLibrary {
		t.Fatal(track, err)
	}
	secondState, err := s.ReadPlaylistState(second.PlaylistID)
	if err != nil || secondState.Entries[0].LocalState != playlists.Imported || *secondState.Entries[0].TrackID != result.TrackID {
		t.Fatal(secondState, err)
	}
	thirdState, err := s.ReadPlaylistState(third.PlaylistID)
	if err != nil || thirdState.Entries[0].LocalState != playlists.Ignored {
		t.Fatal(thirdState, err)
	}
	for _, b := range []*playlists.CommitResult{first, second, third} {
		stats, err := s.Stats(b.BatchID)
		if err != nil || stats.Total != 1 || (b == first && stats.Discarded != 1) || (b != first && stats.Merged != 1) {
			t.Fatal(stats, err)
		}
	}
	assertPlaylistIntegrity(t, s)
}

func assertPlaylistIntegrity(t *testing.T, s *Store) {
	t.Helper()
	var orphans int
	if err := s.db.QueryRow(`SELECT count(*) FROM track t WHERE NOT in_library AND NOT EXISTS(SELECT 1 FROM synced_playlist_entry e WHERE e.track_id=t.id)`).Scan(&orphans); err != nil || orphans != 0 {
		t.Fatal("orphan tracks", orphans, err)
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if rows.Next() {
		t.Fatal("broken foreign key")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

type restartClock struct{ now time.Time }

func (c *restartClock) Now() time.Time { return c.now }

func TestPlaylistExpiredPlanCannotReplaceDurableSnapshot(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("Song")}
	registry, err := playlists.NewRegistry(source)
	if err != nil {
		t.Fatal(err)
	}
	fetcher := playlists.NewFetcher()
	clock := &restartClock{now: time.Now()}
	svc, err := playlists.NewService(registry, s, fetcher, clock)
	if err != nil {
		t.Fatal(err)
	}
	defer fetcher.Close()
	defer svc.Close()
	bound := bindSync(t, svc)
	before, err := s.ReadPlaylistState(bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	source.snapshot = syncTracks()
	p, err := svc.PrepareSync(context.Background(), "expire-removal", bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(playlists.PlanTTL)
	if _, err := svc.Commit(context.Background(), p.ID, p.Hash, nil); !errors.Is(err, playlists.ErrPlanStale) {
		t.Fatal(err)
	}
	after, err := s.ReadPlaylistState(bound.PlaylistID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal(after, err)
	}
	assertPlaylistIntegrity(t, s)
}
