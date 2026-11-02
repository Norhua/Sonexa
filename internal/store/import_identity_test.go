package store

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func identityItem(title, artist, album string) ImportItem {
	raw, _ := json.Marshal([]string{title, artist, album})
	return ImportItem{RawRow: string(raw), InTitle: &title, InArtist: &artist, InAlbum: &album, RowIndex: 999}
}
func identityBatch(t *testing.T, s *Store, items ...ImportItem) int64 {
	t.Helper()
	id, err := s.CreateBatch(&ImportBatch{Filename: "/private/moved.csv", TotalRows: len(items)})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnqueueItems(id, items); err != nil {
		t.Fatal(err)
	}
	return id
}
func TestImportInputsCoalesceAcrossBatchesAndKeepDistinctOriginals(t *testing.T) {
	s := openTestStore(t)
	a := identityItem("ツバサ", "4円", "side-c")
	b := identityItem("ツバサ（羽翼）", "4円（ヨエン）", "side-c（译名）")
	first := identityBatch(t, s, a, a, b)
	item, err := s.ClaimNextItem(first)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if item.RowIndex != 0 {
		t.Fatal("source row persisted", item.RowIndex)
	}
	if err = s.MarkReview(item.ID, ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	second := identityBatch(t, s, b)
	stats, _ := s.Stats(first)
	if stats.Total != 3 || stats.Merged != 2 || stats.Review != 1 {
		t.Fatal(stats)
	}
	stats, _ = s.Stats(second)
	if stats.Merged != 1 || stats.Processed() != stats.Total {
		t.Fatal(stats)
	}
	originals, err := s.OriginalInputs(item.ID)
	if err != nil || len(originals) != 2 {
		t.Fatal(originals, err)
	}
	batch, _ := s.GetBatch(first)
	if batch.Filename != "" {
		t.Fatal("filename retained", batch.Filename)
	}
	if err = s.MarkDiscarded(item.ID); err != nil {
		t.Fatal(err)
	}
	for _, fn := range []func() error{func() error { return s.MarkReview(item.ID, ReviewAmbiguous, "[]", "[]") }, func() error { return s.RetryItem(item.ID) }} {
		if !errors.Is(fn(), ErrItemNotActive) {
			t.Fatal("terminal item resurrected")
		}
	}
}
func TestConcurrentInputCoalescing(t *testing.T) {
	s := openTestStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			batch, err := s.CreateBatch(&ImportBatch{TotalRows: 1})
			if err == nil {
				err = s.EnqueueItems(batch, []ImportItem{identityItem("Song", "Artist", "Album")})
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var active, merged int
	s.DB().QueryRow(`SELECT COUNT(*) FROM import_item WHERE status='queued'`).Scan(&active)
	s.DB().QueryRow(`SELECT COUNT(*) FROM import_item WHERE status='merged'`).Scan(&merged)
	if active != 1 || merged != 11 {
		t.Fatal(active, merged)
	}
}
func TestImportIdentityDoesNotMergeVersionsOrBridgePositions(t *testing.T) {
	s := openTestStore(t)
	a := identityItem("Song", "Artist", "Album")
	b := a
	one, two := 1, 2
	a.InTrackNo = &one
	b.InTrackNo = &two
	batch := identityBatch(t, s, a, b, identityItem("Song", "Artist", "Album"), identityItem("Song (Live)", "Artist", "Album"), identityItem("Song", "Artist", "Other Album"))
	stats, _ := s.Stats(batch)
	if stats.Queued != 5 {
		t.Fatal(stats)
	}
}
func TestResolvedTrackMergesDifferentInputsButNotCandidateOverlap(t *testing.T) {
	s := openTestStore(t)
	b1 := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	b2 := identityBatch(t, s, identityItem("Song", "Other credit", "Album"))
	i1, _ := s.ClaimNextItem(b1)
	i2, _ := s.ClaimNextItem(b2)
	c := provider.Candidate{Provider: "test", MatchRef: "track", Title: ptr("Song"), AlbumTitle: ptr("Album"), RecordingRef: ptr("rec"), ISRC: ptr("isrc")}
	if handled, err := s.ResolveImport(i1.ID, nil, &c); handled || err != nil {
		t.Fatal(handled, err)
	}
	if err := s.MarkReview(i1.ID, ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if handled, err := s.ResolveImport(i2.ID, []matching.ScoredCandidate{{Candidate: c}}, nil); handled || err != nil {
		t.Fatal("overlap merged", handled, err)
	}
	if handled, err := s.ResolveImport(i2.ID, nil, &c); !handled || err != nil {
		t.Fatal(handled, err)
	}
	originals, err := s.OriginalInputs(i1.ID)
	if err != nil || len(originals) != 2 {
		t.Fatal(originals, err)
	}
}
func TestResolvedIdentityRejectsDifferentTrackContextAndClearsOnNoneMatched(t *testing.T) {
	s := openTestStore(t)
	b1 := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	b2 := identityBatch(t, s, identityItem("Song", "Artist", "Other Album"))
	i1, _ := s.ClaimNextItem(b1)
	i2, _ := s.ClaimNextItem(b2)
	c := provider.Candidate{Provider: "test", MatchRef: "track", Title: ptr("Song"), AlbumTitle: ptr("Album")}
	if _, err := s.ResolveImport(i1.ID, nil, &c); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkReview(i1.ID, ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if handled, err := s.ResolveImport(i2.ID, nil, &c); err != nil || handled {
		t.Fatal("wrong album merged", handled, err)
	}
	if err := s.MarkReview(i1.ID, ReviewReasonUnchanged, "", "[]"); err != nil {
		t.Fatal(err)
	}
	var ref *string
	if err := s.DB().QueryRow(`SELECT resolved_ref FROM import_item WHERE id=?`, i1.ID).Scan(&ref); err != nil || ref != nil {
		t.Fatal(ref, err)
	}
}

func TestReviewSearchCannotOverwriteNewerResultOrTerminalState(t *testing.T) {
	s := openTestStore(t)
	b := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	it, _ := s.ClaimNextItem(b)
	if err := s.MarkReview(it.ID, ReviewAmbiguous, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	first, err := s.BeginReviewSearch(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.BeginReviewSearch(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishReviewSearch(it.ID, second, "qq", "", "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	source, country, manual, err := s.ReviewSearchSource(it.ID)
	if err != nil || source != "qq" || country != "" || !manual {
		t.Fatal(source, country, manual, err)
	}
	if !errors.Is(s.FinishReviewSearch(it.ID, first, "musicbrainz", "", "[]", "[]"), ErrItemNotActive) {
		t.Fatal("old search won")
	}
	third, _ := s.BeginReviewSearch(it.ID)
	if err = s.MarkDiscarded(it.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(s.FinishReviewSearch(it.ID, third, "qq", "", "[]", "[]"), ErrItemNotActive) {
		t.Fatal("search resurrected terminal")
	}
}
