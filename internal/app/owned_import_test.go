package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func enqueueOwnedInput(t *testing.T, a *App, title, artist, album string) int64 {
	t.Helper()
	batch, err := a.store.CreateBatch(&store.ImportBatch{TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal([]string{title, artist, album})
	if err = a.store.EnqueueItems(batch, []store.ImportItem{{RawRow: string(raw), InTitle: &title, InArtist: &artist, InAlbum: &album}}); err != nil {
		t.Fatal(err)
	}
	it, err := a.store.ClaimNextItem(batch)
	if err != nil || it == nil {
		t.Fatal(it, err)
	}
	return it.ID
}

func TestReviewReconcilesOwnedTetraAndOtherSource(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	tetra := fullCandidate("tetra-owned")
	tetra.Title = strPtr("Tetra.")
	tetra.ArtistName = strPtr("原田ひとみ")
	tetra.AlbumTitle = strPtr("Trinoline Original Soundtrack ~LINES~")
	tetra.ReleaseRef = strPtr("tetra-release")
	tetra.ReleaseGroupRef = strPtr("tetra-group")
	id := enqueueOwnedInput(t, a, "Tetra.", "原田瞳（原田ひとみ）", "LINES")
	raw, _ := json.Marshal([]matching.ScoredCandidate{{Candidate: tetra}})
	if err := a.store.MarkReview(id, store.ReviewAmbiguous, string(raw), "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: tetra})); err != nil {
		t.Fatal(err)
	}
	qqID := enqueueOwnedInput(t, a, "ツバサ（羽翼）", "4円（ヨエン）", "side-c")
	if err := a.store.MarkReview(qqID, store.ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	qq := tetra
	qq.Provider = provider.QQMusic
	qq.MatchRef = "tsubasaMID"
	qq.ReleaseRef = strPtr("sideCMID")
	qq.ReleaseGroupRef = nil
	qq.Title = strPtr("ツバサ (羽翼)")
	qq.ArtistName = strPtr("4円")
	qq.AlbumTitle = strPtr("side-c")
	if _, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: qq})); err != nil {
		t.Fatal(err)
	}
	q, err := a.GetReviewQueue()
	if err != nil || q.Total != 0 {
		t.Fatal(q, err)
	}
	summary, err := a.GetTaskSummary()
	if err != nil || summary.ReviewPending != 0 {
		t.Fatal(summary, err)
	}
	for _, itemID := range []int64{id, qqID} {
		it, err := a.store.GetItem(itemID)
		if err != nil || it.Status != store.StatusSkipped {
			t.Fatal(it, err)
		}
	}
}

func TestSubmitImportUsesOtherSourceOfflineAndPreservesPrimaryBinding(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	c := fullCandidate("tsubasaMID")
	searcher := &neverSearchOwned{}
	a.runner = importer.NewRunner(a.store, matching.NewEngine(a.settings.Get().Matching), searcher, provider.DefaultRetryPolicy(), false, nil, a.library)
	c.Provider = provider.QQMusic
	c.ReleaseRef = strPtr("sideCMID")
	c.ReleaseGroupRef = nil
	c.Title = strPtr("ツバサ (羽翼)")
	c.ArtistName = strPtr("4円")
	c.AlbumTitle = strPtr("side-c")
	trackID, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: c}))
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "input.csv")
	if err = os.WriteFile(path, []byte("曲名,艺术家,专辑\nツバサ（羽翼）,4円（ヨエン）,side-c\n"), 0600); err != nil {
		t.Fatal(err)
	}
	batch, err := a.SubmitImport(path, "", []string{"title", "artist", "album"}, "UTF-8", ",", true)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "offline duplicate finishes", func() bool { return !a.runner.IsRunning(batch) })
	stats, err := a.store.Stats(batch)
	if err != nil || stats.Skipped != 1 || stats.Review != 0 {
		t.Fatal(stats, err)
	}
	saved, err := a.store.GetMetadata(trackID)
	if err != nil || saved.MatchSource == nil || *saved.MatchSource != "qq" || *saved.MatchRef != c.MatchRef {
		t.Fatal(saved, err)
	}
}

type neverSearchOwned struct{}

func (*neverSearchOwned) SearchTracks(context.Context, provider.Query) ([]provider.Candidate, error) {
	panic("owned track must not query MB")
}

func TestAcceptedAliasRetainsInputsUntilBindingChanges(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	c := fullCandidate("accepted")
	item := enqueueOwnedInput(t, a, "Imported alias", "Export credit", "Export album")
	plan, err := a.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: c}), item)
	if err != nil {
		t.Fatal(err)
	}
	id, err := a.commitUnconfirmedAdmission(plan)
	if err != nil {
		t.Fatal(err)
	}
	repeated := enqueueOwnedInput(t, a, "Imported alias", "Export credit", "Export album")
	if handled, err := a.store.ResolveImport(repeated, nil, nil); err != nil || !handled {
		t.Fatal(handled, err)
	}
	var n int
	if err = a.store.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation WHERE track_id=?`, id).Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
}
