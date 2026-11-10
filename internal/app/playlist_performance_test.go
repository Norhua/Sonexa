package app

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// Synthetic data only: exercise real bindings with 1,000 local tracks and 320
// unrelated review items (10 candidates each), without upstream requests.
func TestLargePlaylistReadPerformance(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	tx, err := a.store.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO synced_playlist(id,provider,playlist_ref,upstream_name,alias,revision,snapshot_digest,completeness_json,excluded_json,fetched_at,last_synced_at) VALUES(1,'qq','123','Large','',1,'test','{}','[]',?,?)`, time.Now().UTC(), time.Now().UTC())
	exec(`INSERT INTO import_batch(id,filename,source_kind,destination_kind,destination_name,total_rows,status,created_at) VALUES(1,'','file','library','音乐库',320,'done',?)`, time.Now().UTC())
	for i := 1; i <= 1000; i++ {
		exec(`INSERT INTO track(id,in_library,created_at,updated_at) VALUES(?,0,?,?)`, i, time.Now().UTC(), time.Now().UTC())
		exec(`INSERT INTO track_current(track_id,title,credit) VALUES(?,?,?)`, i, fmt.Sprintf("Local %d（本地曲目）", i), "Local Artist")
		exec(`INSERT INTO synced_playlist_entry(playlist_id,entry_key,track_ref,position,artists_json,upstream_present,local_state,track_id) VALUES(1,?,?,?,'[]',1,'imported',?)`, fmt.Sprint(i), fmt.Sprint(i), i, i)
	}
	for i := 1; i <= 320; i++ {
		title := fmt.Sprintf("Review %d（待复核曲目）", i)
		var candidates []matching.ScoredCandidate
		for j := 0; j < 10; j++ {
			candidates = append(candidates, matching.ScoredCandidate{Candidate: provider.Candidate{Provider: provider.MusicBrainz, MatchRef: fmt.Sprintf("candidate-%d-%d", i, j), Title: &title, ArtistName: strPtr("Review Artist"), AlbumTitle: strPtr("Review Album"), ReleaseRef: strPtr(fmt.Sprintf("release-%d-%d", i, j))}})
		}
		raw, _ := json.Marshal(candidates)
		exec(`INSERT INTO import_item(id,batch_id,row_index,raw_row,in_title,in_artist,in_album,status,candidates,updated_at) VALUES(?,1,?,'[]',?,'Review Artist','Review Album','review',?,?)`, i, i, title, string(raw), time.Now().UTC())
		exec(`INSERT INTO import_destination(input_id,kind,state,display_name) VALUES(?,'library','active','音乐库')`, i)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	measure := func(name string, fn func()) {
		t.Helper()
		start := time.Now()
		fn()
		t.Logf("%s: %s", name, time.Since(start))
	}
	measure("reconcile 320 x 1000", func() {
		if err := a.reconcileReviewDuplicates(); err != nil {
			t.Fatal(err)
		}
	})
	measure("review queue 320 x 10", func() {
		q, err := a.GetReviewQueue()
		if err != nil || q.Total != 320 {
			t.Fatal(q, err)
		}
	})
	measure("task summary", func() {
		q, err := a.GetTaskSummary()
		if err != nil || q.ReviewPending != 320 {
			t.Fatal(q, err)
		}
	})
	measure("synced tracks 1000", func() {
		rows, err := a.GetSyncedPlaylistEntries(1, "")
		if err != nil || len(rows) != 1000 {
			t.Fatal(len(rows), err)
		}
	})
	measure("accept without match", func() {
		if _, err := a.NoMatchNeeded(1); err != nil {
			t.Fatal(err)
		}
	})
	measure("accept candidate and refresh", func() {
		candidate := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "accepted-candidate", Title: strPtr("Accepted candidate")}
		if _, err := a.AcceptCandidate(2, rowFrom(matching.ScoredCandidate{Candidate: candidate})); err != nil {
			t.Fatal(err)
		}
		q, err := a.GetReviewQueue()
		if err != nil || q.Total != 318 {
			t.Fatal(q, err)
		}
		if _, err := a.GetTaskSummary(); err != nil {
			t.Fatal(err)
		}
	})
}
