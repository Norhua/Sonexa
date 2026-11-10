package store

import (
	"database/sql"
	"reflect"
	"testing"
)

func TestCandidateLocalStatesOrderedExactAndUncollected(t *testing.T) {
	s := openTestStore(t)
	id, err := s.AdmitCandidate(ownedCandidate("qq", "exact", "Song", "Artist", "Album"))
	if err != nil {
		t.Fatal(err)
	}
	view, err := s.GetTrackView(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`UPDATE track SET in_library=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	keys := []CandidateLocalKey{{"qq", "exact", "rel-Album"}, {"qq", "missing", "rel-Album"}, {"itunes", "exact", "rel-Album"}, {"qq", "exact", "rel-Album"}}
	states, err := s.CandidateLocalStates(keys)
	if err != nil || len(states) != 4 {
		t.Fatal(states, err)
	}
	if states[0].TrackID != id || states[0].InLibrary || states[0].ReleaseID != *view.ReleaseID || states[1].TrackID != 0 || states[1].ReleaseID != *view.ReleaseID || states[2].TrackID != 0 || states[2].ReleaseID != 0 || states[3] != states[0] {
		t.Fatal(states)
	}
	if _, err = s.db.Exec(`UPDATE track SET match_ref='new' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	states, err = s.CandidateLocalStates(keys)
	if err != nil || states[0].TrackID != 0 {
		t.Fatal(states, err)
	}
	states, err = s.CandidateLocalStates(nil)
	if err != nil || states == nil || len(states) != 0 {
		t.Fatal(states, err)
	}
}

func TestReviewReadBatchMatchesExistingProjection(t *testing.T) {
	s := openTestStore(t)
	_, _, first := saveSyncFixture(t, s, syncFixture("test-a", "one", "Song"), true)
	_, _, second := saveSyncFixture(t, s, syncFixture("test-b", "two", "Song"), true)
	item, err := s.ClaimNextItem(first)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	if err = s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.BeginReviewSearch(item.ID); err != nil {
		t.Fatal(err)
	}
	// Revoke the original shell target while a merged input remains active.
	targets, err := s.ActiveImportDestinations(item.ID)
	if err != nil || len(targets) != 2 {
		t.Fatal(targets, err, second)
	}
	if err = s.withTx(func(tx *sql.Tx) error { return cancelImportDestinationTx(tx, item.ID, "removed") }); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadReviewItems()
	if err != nil || len(got) != 1 {
		t.Fatal(got, err)
	}
	effective, err := s.EffectiveImportItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	original, err := s.OriginalInputs(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	targets, err = s.ActiveImportDestinations(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[0].OriginalInputs, original) || !reflect.DeepEqual(got[0].Destinations, targets) || !reflect.DeepEqual(got[0].InTitle, effective.InTitle) || !got[0].Searched {
		t.Fatal(got)
	}
}
