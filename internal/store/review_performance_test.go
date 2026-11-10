package store

import (
	"database/sql"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/matching"
)

func TestImportReviewRateRetainsHistoricalCount(t *testing.T) {
	s := openTestStore(t)
	var items []ImportItem
	for i := 0; i < 15; i++ {
		items = append(items, identityItem(fmt.Sprintf("Song %d", i), "Artist", "Album"))
	}
	batch := identityBatch(t, s, items...)
	var reviewIDs []int64
	for i := 0; i < 10; i++ {
		it, err := s.ClaimNextItem(batch)
		if err != nil || it == nil {
			t.Fatal(it, err)
		}
		if i < 7 {
			track, err := s.CreateTrackWithProvenance(NewTrackInput{Title: fmt.Sprintf("Saved %d", i), InLibrary: true})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.MarkImported(it.ID, track); err != nil {
				t.Fatal(err)
			}
		} else {
			if err = s.MarkReview(it.ID, ReviewNoMatch, "[]", "[]"); err != nil {
				t.Fatal(err)
			}
			reviewIDs = append(reviewIDs, it.ID)
		}
	}
	track, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "Accepted", InLibrary: true})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.MarkImported(reviewIDs[0], track); err != nil {
		t.Fatal(err)
	}
	stats, err := s.Stats(batch)
	if err != nil || stats.Imported != 8 || stats.Review != 2 || stats.Failed != 0 || stats.ReviewEntered != 3 || stats.ProcessedOnce != 10 {
		t.Fatal(stats, err)
	}
	if err = s.MarkDiscarded(reviewIDs[1]); err != nil {
		t.Fatal(err)
	}
	if err = s.RetryItem(reviewIDs[2]); err != nil {
		t.Fatal(err)
	}
	stats, err = s.Stats(batch)
	if err != nil || stats.ReviewEntered != 3 || stats.ProcessedOnce != 10 || stats.Review != 0 {
		t.Fatal(stats, err)
	}
	if _, err = s.RecordFailure(reviewIDs[2], "failed", 0, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.MarkReview(reviewIDs[2], ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	stats, err = s.Stats(batch)
	if err != nil || stats.ReviewEntered != 3 || stats.ProcessedOnce != 10 {
		t.Fatal(stats, err)
	}
	path := s.dbPathForTest(t)
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stats, err = reopened.Stats(batch)
	if err != nil || stats.ReviewEntered != 3 || stats.ProcessedOnce != 10 {
		t.Fatal(stats, err)
	}
}

func (s *Store) dbPathForTest(t *testing.T) string {
	t.Helper()
	var seq int
	var name, path string
	if err := s.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReviewRateIncludesMergedActiveInputsAndRollsBack(t *testing.T) {
	s := openTestStore(t)
	item := activeInput(t, s, identityItem("Song", "Artist", "Album"))
	b2 := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	if err := s.MarkReview(item.ID, ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	b3 := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	for _, batch := range []int64{item.BatchID, b2, b3} {
		st, err := s.Stats(batch)
		if err != nil || st.ReviewEntered != 1 || st.ProcessedOnce != 1 {
			t.Fatal(st, err)
		}
	}
	other := activeInput(t, s, identityItem("Other", "Artist", "Album"))
	s.beforeCommit = func(*sql.Tx) error { return fmt.Errorf("rollback") }
	if err := s.MarkReview(other.ID, ReviewNoMatch, "[]", "[]"); err == nil {
		t.Fatal("expected rollback")
	}
	s.beforeCommit = nil
	st, err := s.Stats(other.BatchID)
	if err != nil || st.ReviewEntered != 0 || st.ProcessedOnce != 0 {
		t.Fatal(st, err)
	}
}

func TestOwnedImportShortlistMatchesExhaustiveDecision(t *testing.T) {
	names := []string{"Song", "Song（译名）", "译名（Song）", "Song -live-", "Song (Deluxe Edition)", "Song...", "", "Tetra.", "原名（另名）", "Only (feat. X)"}
	rng := rand.New(rand.NewSource(42))
	for n := 0; n < 300; n++ {
		var tracks []ownedImportTrack
		var confirmed []confirmedImport
		for i := 0; i < 12; i++ {
			input := matching.Input{Title: names[rng.Intn(len(names))], Artist: "Artist", Album: "Album"}
			track := ownedImportTrack{id: int64(i + 1), current: input, source: "qq", ref: fmt.Sprint(i), bindingKey: "same"}
			if i%2 == 0 {
				track.snapshotValid = true
				track.snapshot = matching.Input{Title: names[rng.Intn(len(names))], Artist: "Artist", Album: "Album"}
			}
			tracks = append(tracks, track)
			if i%3 == 0 {
				confirmed = append(confirmed, confirmedImport{trackID: track.id, bindingKey: "same", input: identityItem(names[rng.Intn(len(names))], "Artist", "Album")})
			}
		}
		inputs := []ImportItem{identityItem(names[rng.Intn(len(names))], "Artist", "Album")}
		if n%4 == 0 {
			inputs = append(inputs, identityItem(names[rng.Intn(len(names))], "Artist", "Album"))
		}
		var candidates []matching.ScoredCandidate
		if n%2 == 0 {
			candidates = []matching.ScoredCandidate{{Candidate: ownedCandidate("qq", fmt.Sprint(rng.Intn(12)), "Song", "Artist", "Album")}}
		}
		want := decideOwnedImport(inputs, tracks, confirmed, exactOwnedRefs(tracks, candidates))
		got := newOwnedImportIndex(tracks, confirmed).decide(inputs, candidates)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("inputs=%+v got=%+v want=%+v", inputs, got, want)
		}
	}
}

func TestReviewRevisionCacheAndReadDoesNotReserveWriter(t *testing.T) {
	s := openTestStore(t)
	it := activeInput(t, s, identityItem("Song", "Artist", "Album"))
	if err := s.MarkReview(it.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileReviewDuplicates(); err != nil {
		t.Fatal(err)
	}
	// Another connection changes evidence; the persistent revision invalidates
	// the cache even when no App event was emitted.
	other, err := Open(s.dbPathForTest(t))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.AdmitCandidate(ownedCandidate("qq", "one", "Song", "Artist", "Album")); err != nil {
		t.Fatal(err)
	}
	changed, err := s.ReconcileReviewDuplicatesChanged()
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	changed, err = s.ReconcileReviewDuplicatesChanged()
	if err != nil || changed {
		t.Fatal(changed, err)
	}
	changed, err = s.ReconcileReviewDuplicatesChanged()
	if err != nil || changed {
		t.Fatal(changed, err)
	}
	writer, err := other.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	done := make(chan error, 1)
	go func() {
		_, err := s.ReadReviewItems()
		if err == nil {
			_, err = s.ListSyncedTracks(1, "")
		}
		if err == nil {
			_, err = s.ReconcileReviewDuplicatesChanged()
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		writer.Rollback()
		<-done
		t.Fatal("read blocked on writer")
	}
}
