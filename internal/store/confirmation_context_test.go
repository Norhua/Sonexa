package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// Establish a human-approved mapping with the same version discrepancy as the
// screenshot, without relying on the song name or current search candidates.
func confirmedVersion(t *testing.T, s *Store, knownPosition bool) (int64, ImportItem) {
	t.Helper()
	c := ownedCandidate("musicbrainz", "version-track", "Song -Singer Ver.-", "Artist", "Original Soundtrack Album")
	if knownPosition {
		c.DiscNumber = ptr(1)
		c.TrackNumber = ptr(2)
	}
	id, err := s.AdmitCandidate(c)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := provider.CandidateSnapshot(c.Provider, c)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := s.TrackRevision(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplySnapshot(id, snapshot, revision); err != nil {
		t.Fatal(err)
	}
	original := identityItem("Song", "Exported artist", "Exported album")
	item := activeInput(t, s, original)
	if err = s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err = s.SkipReviewCandidate(item.ID, "musicbrainz", "version-track"); err != nil {
		t.Fatal(err)
	}
	return id, original
}

func TestConfirmedVersionMappingReimport(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, true)
	for _, input := range []ImportItem{original, identityItem("Song（译名）", "Exported artist（读音）", "Exported album（译名）")} {
		item := activeInput(t, s, input)
		handled, err := s.ResolveImport(item.ID, nil, nil)
		if err != nil || !handled {
			t.Fatalf("human-approved version rejected: %v %v", handled, err)
		}
		saved, _ := s.GetItem(item.ID)
		if saved.TrackID == nil || *saved.TrackID != id || saved.Status != StatusSkipped {
			t.Fatal(saved)
		}
	}
}

func TestConfirmationSurvivesPreviouslyUnknownPositionEnrichment(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, false)
	c := ownedCandidate("musicbrainz", "version-track", "Song -Singer Ver.-", "Artist", "Original Soundtrack Album")
	c.DiscNumber = ptr(1)
	c.TrackNumber = ptr(2)
	snapshot, err := provider.CandidateSnapshot(c.Provider, c)
	if err != nil {
		t.Fatal(err)
	}
	revision, err := s.TrackRevision(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplySnapshot(id, snapshot, revision); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation WHERE track_id=?`, id).Scan(&n); err != nil || n != 1 {
		t.Fatalf("enrichment erased confirmation: %d %v", n, err)
	}
	item := activeInput(t, s, original)
	if handled, err := s.ResolveImport(item.ID, nil, nil); err != nil || !handled {
		t.Fatal(handled, err)
	}
	// Later changes to an already known position really invalidate the relation,
	// including relations whose approved baseline initially had no position.
	if _, err = s.DB().Exec(`UPDATE track_current SET track_number=3 WHERE track_id=?`, id); err != nil {
		t.Fatal(err)
	}
	next := activeInput(t, s, original)
	if handled, err := s.ResolveImport(next.ID, nil, nil); err != nil || handled {
		t.Fatal("known position change reused confirmation", handled, err)
	}
}

func TestConfirmedTargetContextRejectsChanges(t *testing.T) {
	for _, change := range []string{"other_version_same_keyword", "instrumental", "current_version_only", "snapshot_version_only", "duration", "rebind", "release_binding", "clear_position", "snapshot_position", "delete"} {
		t.Run(change, func(t *testing.T) {
			s := openTestStore(t)
			id, original := confirmedVersion(t, s, true)
			var err error
			switch change {
			case "current_version_only":
				err = s.UpdateTrackFields(id, ptr("Song -Other Singer Ver.-"), nil, true)
			case "rebind":
				_, err = s.DB().Exec(`UPDATE track SET match_ref='other-track' WHERE id=?`, id)
			case "release_binding":
				_, err = s.DB().Exec(`UPDATE release SET provider_ref='other-release' WHERE id=(SELECT release_id FROM track WHERE id=?)`, id)
			case "clear_position":
				_, err = s.DB().Exec(`UPDATE track_current SET track_number=NULL WHERE track_id=?`, id)
			case "delete":
				err = s.DeleteTrack(id)
			default:
				var raw string
				if err = s.DB().QueryRow(`SELECT body FROM track_snapshot WHERE track_id=?`, id).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var track domain.TrackSnapshot
				if err = json.Unmarshal([]byte(raw), &track); err != nil {
					t.Fatal(err)
				}
				switch change {
				case "other_version_same_keyword", "snapshot_version_only":
					track.Title = domain.Known("Song -Other Singer Ver.-")
				case "instrumental":
					track.Title = domain.Known("Song -Instrumental-")
				case "duration":
					// First establish a known baseline, then make an incompatible change.
					track.DurationMS = domain.Known(200000)
					applyConfirmationSnapshot(t, s, id, &track)
					var confirmedItem int64
					if err = s.DB().QueryRow(`SELECT item_id FROM import_confirmation WHERE track_id=?`, id).Scan(&confirmedItem); err != nil {
						t.Fatal(err)
					}
					if err = s.withTx(func(tx *sql.Tx) error {
						return rememberImportConfirmationTx(tx, confirmedItem, id, map[string]string{"kind": "manual_skip"})
					}); err != nil {
						t.Fatal(err)
					}
					track.DurationMS = domain.Known(240000)
				case "snapshot_position":
					track.TrackNumber = domain.Known(4)
				}
				if change == "snapshot_version_only" || change == "snapshot_position" {
					body, e := json.Marshal(track)
					if e != nil {
						t.Fatal(e)
					}
					_, err = s.DB().Exec(`UPDATE track_snapshot SET body=? WHERE track_id=?`, string(body), id)
				} else {
					applyConfirmationSnapshot(t, s, id, &track)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			item := activeInput(t, s, original)
			if handled, err := s.ResolveImport(item.ID, nil, nil); err != nil || handled {
				t.Fatal("changed target reused confirmation", handled, err)
			}
		})
	}
}

func applyConfirmationSnapshot(t *testing.T, s *Store, id int64, track *domain.TrackSnapshot) {
	t.Helper()
	rev, err := s.TrackRevision(id)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ApplySnapshot(id, &domain.Snapshot{Track: track}, rev); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmedContextNeverGrantsUnconfirmedVersionOrInput(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, true)
	for _, input := range []ImportItem{
		identityItem("Song (Live)", "Exported artist", "Exported album"),
		identityItem("Song", "Another artist", "Exported album"),
		identityItem("Song", "Exported artist", "Another album"),
	} {
		it := activeInput(t, s, input)
		if handled, err := s.ResolveImport(it.ID, nil, nil); err != nil || handled {
			t.Fatal(handled, err)
		}
	}
	wrongPosition := original
	wrongPosition.InTrackNo = ptr(3)
	it := activeInput(t, s, wrongPosition)
	if handled, err := s.ResolveImport(it.ID, nil, nil); err != nil || handled {
		t.Fatal("explicit input position ignored", handled, err)
	}
	if err := s.MarkDiscarded(it.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB().Exec(`DELETE FROM import_confirmation WHERE track_id=?`, id); err != nil {
		t.Fatal(err)
	}
	it = activeInput(t, s, original)
	c := ownedCandidate("musicbrainz", "version-track", "Song -Singer Ver.-", "Artist", "Original Soundtrack Album")
	if handled, err := s.ResolveImport(it.ID, []matching.ScoredCandidate{{Candidate: c, Score: matching.Score{ReviewOnly: true}}}, nil); err != nil || handled {
		t.Fatal("unconfirmed review candidate skipped", handled, err)
	}
}

func TestConfirmedContextReopenAndConcurrentReimports(t *testing.T) {
	path := filepath.Join(t.TempDir(), "confirmation.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	id, original := confirmedVersion(t, s, false)
	if _, err = s.DB().Exec(`UPDATE track_current SET disc_number=1,track_number=2 WHERE track_id=?`, id); err != nil {
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
	item := activeInput(t, s, original)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			handled, err := s.ResolveImport(item.ID, nil, nil)
			if err == nil && !handled {
				err = ErrItemNotActive
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
	var n int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation WHERE item_id=?`, item.ID).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
}

func TestConfirmedReimportIsAtomicAndReconcilesReview(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, true)
	it := activeInput(t, s, original)
	if err := s.MarkReview(it.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected confirmation commit failure")
	s.beforeCommit = func(*sql.Tx) error { return failure }
	if err := s.ReconcileReviewDuplicates(); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	s.beforeCommit = nil
	saved, err := s.GetItem(it.ID)
	if err != nil || saved.Status != StatusReview {
		t.Fatal(saved, err)
	}
	var n int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation WHERE item_id=?`, it.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if err = s.ReconcileReviewDuplicates(); err != nil {
		t.Fatal(err)
	}
	saved, err = s.GetItem(it.ID)
	if err != nil || saved.Status != StatusSkipped || saved.TrackID == nil || *saved.TrackID != id {
		t.Fatal(saved, err)
	}
}

func TestConfirmedVersionRelationsDoNotResolveTwoTargets(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, true)
	second := ownedCandidate("qq", "other-version", "Song -Singer Ver.-", "Artist", "Original Soundtrack Album")
	other, err := s.AdmitCandidate(second)
	if err != nil {
		t.Fatal(err)
	}
	it := activeInput(t, s, original)
	if err = s.MarkReview(it.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err = s.SkipReviewCandidate(it.ID, "qq", "other-version"); err != nil {
		t.Fatal(err)
	}
	it = activeInput(t, s, original)
	if handled, err := s.ResolveImport(it.ID, nil, nil); err != nil || handled {
		t.Fatalf("two confirmations chose %d or %d: %v %v", id, other, handled, err)
	}
}

func TestLegacyConfirmationCannotInventAcceptedVersionContext(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, true)
	if _, err := s.DB().Exec(`UPDATE import_confirmation SET target_context=NULL WHERE track_id=?`, id); err != nil {
		t.Fatal(err)
	}
	item := activeInput(t, s, original)
	if handled, err := s.ResolveImport(item.ID, nil, nil); err != nil || handled {
		t.Fatal("legacy missing baseline expanded", handled, err)
	}
}

func TestMergedInputsRetainConfirmedTargetContexts(t *testing.T) {
	s := openTestStore(t)
	id, original := confirmedVersion(t, s, true)
	// Forget the seed decision, then confirm a newly grouped pair of inputs.
	if _, err := s.DB().Exec(`DELETE FROM import_confirmation WHERE track_id=?`, id); err != nil {
		t.Fatal(err)
	}
	alias := identityItem("Song（译名）", "Exported artist（读音）", "Exported album（译名）")
	batch := identityBatch(t, s, original, alias)
	it, err := s.ClaimNextItem(batch)
	if err != nil || it == nil {
		t.Fatal(it, err)
	}
	if err = s.MarkReview(it.ID, ReviewNoMatch, "[]", "[]"); err != nil {
		t.Fatal(err)
	}
	if err = s.SkipReviewCandidate(it.ID, "musicbrainz", "version-track"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation WHERE target_context IS NOT NULL`).Scan(&n); err != nil || n != 2 {
		t.Fatal(n, err)
	}
	for _, input := range []ImportItem{original, alias} {
		next := activeInput(t, s, input)
		if handled, err := s.ResolveImport(next.ID, nil, nil); err != nil || !handled {
			t.Fatal(handled, err)
		}
	}
}

func TestConfirmationBindingKeyAllowsOnlyUnknownPositionCompletion(t *testing.T) {
	base := `["musicbrainz","track",1,"musicbrainz","release",null,null]`
	if !confirmationBindingCompatible(base, `["musicbrainz","track",1,"musicbrainz","release",1,2]`) {
		t.Fatal("unknown position completion rejected")
	}
	for _, changed := range []string{
		`["qq","track",1,"musicbrainz","release",null,null]`,
		`["musicbrainz","other",1,"musicbrainz","release",null,null]`,
		`["musicbrainz","track",2,"musicbrainz","release",null,null]`,
		`["musicbrainz","track",1,"qq","release",null,null]`,
		`["musicbrainz","track",1,"musicbrainz","other",null,null]`,
		`["musicbrainz"]`,
	} {
		if confirmationBindingCompatible(base, changed) {
			t.Fatal(changed)
		}
	}
	known := `["musicbrainz","track",1,"musicbrainz","release",1,2]`
	if confirmationBindingCompatible(known, base) || confirmationBindingCompatible(known, `["musicbrainz","track",1,"musicbrainz","release",1,3]`) {
		t.Fatal("known position change accepted")
	}
}

func TestConfirmationContextMigrationKeepsLegacyBaselineUnknown(t *testing.T) {
	// Construct a v3 database through the real migration SQL, without changing
	// the global migration list or editing user data.
	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	if _, err = currentVersion(db); err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations {
		if m.Version <= 3 {
			if err = s.applyMigration(m); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, query := range []string{
		`INSERT INTO import_batch(id,filename,created_at) VALUES(1,'',datetime('now'))`,
		`INSERT INTO import_item(id,batch_id,row_index,raw_row,status,updated_at) VALUES(1,1,0,'[]','imported',datetime('now'))`,
		`INSERT INTO track(id,created_at,updated_at) VALUES(1,datetime('now'),datetime('now'))`,
		`INSERT INTO track_current(track_id) VALUES(1)`,
		`INSERT INTO import_confirmation VALUES(1,1,'["","",null,"","",null,null]','{}',datetime('now'))`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	var context *string
	if err = s.DB().QueryRow(`SELECT target_context FROM import_confirmation WHERE item_id=1`).Scan(&context); err != nil || context != nil {
		t.Fatal("manufactured baseline", context, err)
	}
	if _, err = s.DB().Exec(`UPDATE track_current SET disc_number=1 WHERE track_id=1`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation`).Scan(&n); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err = s.DB().Exec(`UPDATE track_current SET disc_number=2 WHERE track_id=1`); err != nil {
		t.Fatal(err)
	}
	if err = s.DB().QueryRow(`SELECT COUNT(*) FROM import_confirmation`).Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}
