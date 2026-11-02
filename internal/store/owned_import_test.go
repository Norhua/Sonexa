package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

func ownedCandidate(source, ref, title, artist, album string) provider.Candidate {
	return provider.Candidate{Provider: provider.ProviderID(source), MatchRef: ref, ReleaseRef: ptr("rel-" + album), Title: ptr(title), ArtistName: ptr(artist), AlbumTitle: ptr(album), AlbumCredit: ptr(artist)}
}
func activeInput(t *testing.T, s *Store, it ImportItem) *ImportItem {
	t.Helper()
	b := identityBatch(t, s, it)
	item, err := s.ClaimNextItem(b)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	return item
}
func TestConfirmationSurvivesReopenAndDoesNotTurnISRCIntoTrack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	c := ownedCandidate("qq", "owned", "Canonical", "Artist", "Album")
	id, err := s.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	original := identityItem("Alias", "Export artist", "Export album")
	item := activeInput(t, s, original)
	if err = s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err = s.SkipReviewCandidate(item.ID, "qq", "owned"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next := activeInput(t, s, original)
	if handled, err := s.ResolveImport(next.ID, nil, nil); err != nil || !handled {
		t.Fatal(handled, err)
	}
	if _, err = s.DB().Exec(`INSERT INTO track_identifier(track_id,kind,value,position) VALUES(?,'ISRC','SAME',0)`, id); err != nil {
		t.Fatal(err)
	}
	other := activeInput(t, s, identityItem("Canonical", "Artist", "Other Album"))
	candidate := ownedCandidate("musicbrainz", "unowned", "Canonical", "Artist", "Other Album")
	candidate.ISRC = ptr("SAME")
	if handled, err := s.ResolveImport(other.ID, []matching.ScoredCandidate{{Candidate: candidate}}, nil); err != nil || handled {
		t.Fatal("ISRC bypassed album identity", handled, err)
	}
}

func TestOwnedConfirmationIsAtomicAndInvalidatesOnIdentityChanges(t *testing.T) {
	for _, change := range []string{"rename", "rebind", "position", "delete", "delete_recreate"} {
		t.Run(change, func(t *testing.T) {
			s := openTestStore(t)
			c := ownedCandidate("qq", "ref", "Canonical", "Artist", "Long Album")
			id, err := s.AdmitCandidate(c)
			if err != nil {
				t.Fatal(err)
			}
			original := identityItem("Alias", "Different export credit", "Abbreviated Album")
			item := activeInput(t, s, original)
			if err = s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
				t.Fatal(err)
			}
			if err = s.SkipReviewCandidate(item.ID, "qq", "ref"); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "rename":
				err = s.UpdateTrackFields(id, ptr("User name"), nil, true)
			case "rebind":
				_, err = s.DB().Exec(`UPDATE track SET match_ref='new-ref' WHERE id=?`, id)
			case "position":
				_, err = s.DB().Exec(`UPDATE track_current SET track_number=9 WHERE track_id=?`, id)
			case "delete", "delete_recreate":
				err = s.DeleteTrack(id)
				if err == nil && change == "delete_recreate" {
					_, err = s.AdmitCandidate(c)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			next := activeInput(t, s, original)
			handled, err := s.ResolveImport(next.ID, nil, nil)
			if err != nil || handled != (change == "rename") {
				t.Fatal(handled, err)
			}
		})
	}
	s := openTestStore(t)
	c := ownedCandidate("qq", "ref", "Song", "Artist", "Album")
	_, err := s.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	it := activeInput(t, s, identityItem("Alias", "Artist", "Album"))
	if err = s.MarkReview(it.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	s.beforeCommit = func(*sql.Tx) error { return errors.New("injected rollback") }
	if err = s.SkipReviewCandidate(it.ID, "qq", "ref"); err == nil {
		t.Fatal("expected failure")
	}
	s.beforeCommit = nil
	var count int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	saved, _ := s.GetItem(it.ID)
	if saved.Status != StatusReview {
		t.Fatal(saved)
	}
}
func TestOwnedInputsDoNotChooseAmongAmbiguousLibraryOrMixViews(t *testing.T) {
	s := openTestStore(t)
	for _, ref := range []string{"one", "two"} {
		if _, err := s.AdmitCandidate(ownedCandidate("qq", ref, "Song", "Artist", "Album")); err != nil {
			t.Fatal(err)
		}
	}
	it := activeInput(t, s, identityItem("Song", "Artist", "Album"))
	if handled, err := s.ResolveImport(it.ID, nil, nil); err != nil || handled {
		t.Fatal(handled, err)
	}
	// Exact binding breaks metadata-only ambiguity, but two exact targets do not.
	candidates := []matching.ScoredCandidate{{Candidate: ownedCandidate("qq", "one", "Song", "Artist", "Album")}, {Candidate: ownedCandidate("qq", "two", "Song", "Artist", "Album")}}
	if handled, err := s.ResolveImport(it.ID, candidates, nil); err != nil || handled {
		t.Fatal(handled, err)
	}
	if handled, err := s.ResolveImport(it.ID, candidates[:1], nil); err != nil || !handled {
		t.Fatal(handled, err)
	}
}
func TestOwnedSnapshotSurvivesDisplayRenameButMustMatchBinding(t *testing.T) {
	s := openTestStore(t)
	c := ownedCandidate("qq", "one", "Song", "Artist", "Album")
	id, err := s.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	// This legacy fixture helper omits the release link from track snapshots;
	// install the coherent provider snapshot as the real admission service does.
	snapshot, err := provider.CandidateSnapshot(c.Provider, c)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(snapshot.Track)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec(`UPDATE track_snapshot SET body=? WHERE track_id=?`, string(body), id); err != nil {
		t.Fatal(err)
	}
	if err = s.UpdateTrackFields(id, ptr("User title"), nil, true); err != nil {
		t.Fatal(err)
	}
	it := activeInput(t, s, identityItem("Song", "Artist", "Album"))
	if handled, err := s.ResolveImport(it.ID, nil, nil); err != nil || !handled {
		t.Fatal(handled, err)
	}
	if _, err = s.DB().Exec(`UPDATE track SET match_ref='different' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	it = activeInput(t, s, identityItem("Song", "Artist", "Album"))
	if handled, err := s.ResolveImport(it.ID, nil, nil); err != nil || handled {
		t.Fatal("stale snapshot used", handled, err)
	}
}
func TestOwnedConflictMatrixAndNoMixedViews(t *testing.T) {
	base := ownedImportTrack{id: 1, source: "qq", ref: "track", bindingKey: "binding", current: matching.Input{Title: "Song", Artist: "Artist", Album: "Album"}}
	for name, change := range map[string]func(*ownedImportTrack){
		"artist":  func(x *ownedImportTrack) { x.current.Artist = "Other" },
		"album":   func(x *ownedImportTrack) { x.current.Album = "Another Album" },
		"version": func(x *ownedImportTrack) { x.current.Title = "Song (Live)" },
		"edition": func(x *ownedImportTrack) { x.current.Album = "Album (Deluxe Edition)" },
		"mixed views": func(x *ownedImportTrack) {
			x.snapshot = x.current
			x.snapshot.Title = "Different"
			x.current.Album = "Different Album"
		},
	} {
		t.Run(name, func(t *testing.T) {
			track := base
			change(&track)
			d := decideOwnedImport([]ImportItem{identityItem("Song", "Artist", "Album")}, []ownedImportTrack{track}, nil, nil)
			if d.TrackID != 0 {
				t.Fatal(d)
			}
		})
	}
	// Confirmations of the SAME input to two extant targets are ambiguous.
	other := base
	other.id = 2
	input := identityItem("Alias", "Export credit", "Export album")
	history := []confirmedImport{{input: input, trackID: 1, bindingKey: "binding"}, {input: input, trackID: 2, bindingKey: "binding"}}
	if d := decideOwnedImport([]ImportItem{input}, []ownedImportTrack{base, other}, history, nil); d.TrackID != 0 {
		t.Fatal(d)
	}
}

func TestReconcileReviewOriginalsOnly(t *testing.T) {
	s := openTestStore(t)
	c := ownedCandidate("musicbrainz", "tetra", "Tetra.", "原田ひとみ", "Trinoline Original Soundtrack ~LINES~")
	it := activeInput(t, s, identityItem("Tetra.", "原田瞳（原田ひとみ）", "LINES"))
	raw, _ := json.Marshal([]matching.ScoredCandidate{{Candidate: c}})
	if err := s.MarkReview(it.ID, ReviewAmbiguous, string(raw), "[]"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdmitCandidate(c); err != nil {
		t.Fatal(err)
	}
	if err := s.ReconcileReviewDuplicates(); err != nil {
		t.Fatal(err)
	}
	saved, _ := s.GetItem(it.ID)
	if saved.Status != StatusSkipped {
		t.Fatal(saved)
	}
	// Even with the same exact candidate, a manual search is not approval to
	// resolve an unrelated original input, nor should reading the queue erase it.
	manual := activeInput(t, s, identityItem("Unrelated", "Artist", "Album"))
	if err := s.MarkReview(manual.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	rev, err := s.BeginReviewSearch(manual.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.FinishReviewSearch(manual.ID, rev, "musicbrainz", "", string(raw), "[]"); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileReviewDuplicates(); err != nil {
		t.Fatal(err)
	}
	saved, _ = s.GetItem(manual.ID)
	if saved.Status != StatusReview {
		t.Fatal(saved)
	}
}
