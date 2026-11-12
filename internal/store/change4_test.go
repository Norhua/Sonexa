package store

import "testing"

// P6：手搜过的复核条目仍按原始输入与当前库事实离线收敛，手搜草稿不被读写。
func TestManualSearchItemStillReconcilesOffline(t *testing.T) {
	s := openTestStore(t)
	batch := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	item, err := s.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err := s.MarkReview(item.ID, ReviewAmbiguous, `[{"Candidate":{"Provider":"musicbrainz","MatchRef":"draft"}}]`, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`UPDATE import_item SET search_manual=1,search_provider='qq' WHERE id=?`, item.ID); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.ReconcileReviewDuplicatesChanged(); err != nil || changed {
		t.Fatal("nothing owned yet", changed, err)
	}
	source, ref := "musicbrainz", "owned"
	if _, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: true, Title: "Song", Credit: ptr("Artist"), Album: ptr("Album"), AlbumCredit: ptr("Artist"), MatchSource: &source, MatchRef: &ref}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.ReconcileReviewDuplicatesChanged(); err != nil || !changed {
		t.Fatal("manual item not reconciled", changed, err)
	}
	got, err := s.GetItem(item.ID)
	if err != nil || got.Status != StatusSkipped {
		t.Fatal(got, err)
	}
	var manual int
	var provider, candidates string
	if err := s.DB().QueryRow(`SELECT search_manual,search_provider,candidates FROM import_item WHERE id=?`, item.ID).Scan(&manual, &provider, &candidates); err != nil || manual != 1 || provider != "qq" || candidates == "" {
		t.Fatal("manual draft was rewritten", manual, provider, candidates, err)
	}
}
