package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestReviewExactBindingOnlyAndSkipRevalidates(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	c := fullCandidate("bound")
	id, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}
	other := c
	other.MatchRef = "other-track" // same recording, ISRC and all text
	rows := a.rowsFrom([]matching.ScoredCandidate{{Candidate: c}, {Candidate: other}}, "接受")
	if rows[0].Action != "跳过" || rows[0].LocalTrackID != id || rows[1].Action != "接受" || rows[1].LocalTrackID != 0 {
		t.Fatal(rows)
	}
	item := enqueueOneItem(t, a, "Song", "Artist", "Album")
	if err = a.store.MarkReview(item, store.ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err = a.SkipReviewCandidate(item, rows[1]); err == nil {
		t.Fatal("unbound candidate allowed skip")
	}
	if err = a.SkipReviewCandidate(item, rows[0]); err != nil {
		t.Fatal(err)
	}
	saved, _ := a.store.GetItem(item)
	if saved.Status != store.StatusSkipped || saved.TrackID == nil || *saved.TrackID != id {
		t.Fatal(saved)
	}
	if err = a.store.MarkReview(item, store.ReviewAmbiguous, "[]", "[]"); err == nil {
		t.Fatal("terminal item restored")
	}
}

func TestSubmitDifferentFilesAggregatesOriginalInputAndDropsPath(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.runner = nil
	data := []string{"ツバサ,4円,side-c\nツバサ,4円,side-c\n", "ツバサ（羽翼）,4円（ヨエン）,side-c（译名）\n"}
	var first int64
	for i, content := range data {
		path := filepath.Join(t.TempDir(), "anything.csv")
		if err := os.WriteFile(path, []byte("曲名,艺术家,专辑\n"+content), 0600); err != nil {
			t.Fatal(err)
		}
		batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album"}, "UTF-8", ",", true)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := a.store.GetBatch(batch)
		if b.Filename != "" {
			t.Fatal(b)
		}
		if i == 0 {
			it, err := a.store.ClaimNextItem(batch)
			if err != nil || it == nil {
				t.Fatal(it, err)
			}
			first = it.ID
			if err = a.store.MarkReview(first, store.ReviewAmbiguous, "[]", "[]"); err != nil {
				t.Fatal(err)
			}
		}
		if err = os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	queue, err := a.GetReviewQueue()
	if err != nil || queue.Total != 1 {
		t.Fatal(queue, err)
	}
	if len(queue.Items[0].OriginalInputs) != 2 {
		t.Fatal(queue.Items)
	}
	body, _ := json.Marshal(queue)
	if strings.Contains(string(body), "batchTag") || strings.Contains(string(body), "anything.csv") {
		t.Fatal("path leaked")
	}
}
