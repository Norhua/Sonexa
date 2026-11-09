package store

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/playlists"
)

func admissionForSync(t *testing.T, s *Store, itemID int64, automatic bool) domain.AdmissionPlan {
	t.Helper()
	seed := domain.AdmissionSeed{ImportItemID: itemID, AutomaticImport: automatic, Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{Title: domain.Known("Song"), Identity: domain.Identity{Provider: "metadata-test", Ref: "track"}}}}
	baseline, err := s.PreviewAdmission(seed)
	if err != nil {
		t.Fatal(err)
	}
	seed.Destinations = baseline.Destinations
	plan := domain.AdmissionPlan{ID: "admission-test", Seed: seed, Baseline: baseline, ExpiresAt: time.Now().Add(time.Hour).Format(time.RFC3339)}
	plan.Hash, err = AdmissionHash(plan)
	if err != nil {
		t.Fatal(err)
	}
	s.SetAdmissionVerifier(func(domain.AdmissionPlan, domain.AdmissionApproval) bool { return true })
	return plan
}
func TestImportDeliveryAllTerminalPathsAreAtomic(t *testing.T) {
	for _, path := range []string{"new_accept", "no_match", "automatic_skip", "manual_skip", "reconcile"} {
		for _, rollback := range []bool{false, true} {
			t.Run(path+map[bool]string{true: "/rollback", false: "/commit"}[rollback], func(t *testing.T) {
				s := openTestStore(t)
				p1, _, batch := saveSyncFixture(t, s, syncFixture("playlist", "a", "Song"), true)
				p2, _, otherBatch := saveSyncFixture(t, s, syncFixture("playlist", "b", "Song"), true)
				item, err := s.ClaimNextItem(batch)
				if err != nil || item == nil {
					t.Fatal(item, err)
				}
				if err := s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]"); err != nil {
					t.Fatal(err)
				}
				var trackID int64
				if path != "new_accept" && path != "no_match" {
					trackID, err = s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "Song", Credit: ptr("Artist"), Album: ptr("Album"), MatchSource: ptr("metadata-test"), MatchRef: ptr("track")})
					if err != nil {
						t.Fatal(err)
					}
				}
				plan := admissionForSync(t, s, item.ID, false)
				if path == "no_match" {
					plan.Seed.Snapshot.Track.Identity = domain.Identity{}
					plan.Hash, _ = AdmissionHash(plan)
				}
				if rollback {
					s.beforeCommit = func(*sql.Tx) error { return errors.New("injected") }
				}
				switch path {
				case "new_accept", "no_match":
					var result *domain.AdmissionResult
					result, err = s.CommitAdmission(plan, domain.AdmissionApproval{})
					if err == nil {
						trackID = result.TrackID
						if len(result.PlaylistIDs) != 2 {
							t.Fatal(result)
						}
					}
				case "automatic_skip":
					_, err = s.ResolveImport(item.ID, nil, nil)
				case "manual_skip":
					err = s.SkipReviewCandidate(item.ID, "metadata-test", "track")
				case "reconcile":
					err = s.ReconcileReviewDuplicates()
				}
				s.beforeCommit = nil
				if (err != nil) != rollback {
					t.Fatal(err)
				}
				for _, id := range []int64{p1, p2} {
					entries, err := s.SyncedPlaylistState(id)
					if err != nil || len(entries) != 1 {
						t.Fatal(entries, err)
					}
					if rollback {
						if entries[0].LocalState != playlists.Pending {
							t.Fatal(entries)
						}
					} else if entries[0].LocalState != playlists.Imported || entries[0].TrackID == nil || *entries[0].TrackID != trackID {
						t.Fatal(entries)
					}
				}
				stats, err := s.Stats(otherBatch)
				if err != nil || stats.Merged != 1 || stats.Total != 1 {
					t.Fatal(stats, err)
				}
				var confirmations int
				if err := s.db.QueryRow(`SELECT count(*) FROM import_confirmation`).Scan(&confirmations); err != nil {
					t.Fatal(err)
				}
				if rollback && confirmations != 0 || !rollback && confirmations != 2 {
					t.Fatal(confirmations)
				}
				if !rollback {
					track, err := s.GetTrack(trackID)
					if err != nil || track.InLibrary {
						t.Fatal(track, err)
					}
					destinations, err := s.ActiveImportDestinations(item.ID)
					if err != nil || len(destinations) != 0 {
						t.Fatal(destinations, err)
					}
				}
			})
		}
	}
}

func TestAdmissionRevalidatesTargetsBeforeExistingIdentity(t *testing.T) {
	for _, action := range []string{"ignore", "merge", "remove"} {
		t.Run(action, func(t *testing.T) {
			s := openTestStore(t)
			p, e, b := saveSyncFixture(t, s, syncFixture("playlist", "a", "Song"), true)
			item, err := s.ClaimNextItem(b)
			if err != nil || item == nil {
				t.Fatal(item, err)
			}
			plan := admissionForSync(t, s, item.ID, false)
			if _, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: false, Title: "Song", MatchSource: ptr("metadata-test"), MatchRef: ptr("track")}); err != nil {
				t.Fatal(err)
			}
			switch action {
			case "ignore":
				if err := s.MarkDiscarded(item.ID); err != nil {
					t.Fatal(err)
				}
			case "merge":
				saveSyncFixture(t, s, syncFixture("playlist", "b", "Song"), true)
			case "remove":
				if err := s.withTx(func(tx *sql.Tx) error {
					if err := detachSyncedEntryDemandsTx(tx, e, "deleted"); err != nil {
						return err
					}
					_, err := tx.Exec(`DELETE FROM synced_playlist WHERE id=?`, p)
					return err
				}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.CommitAdmission(plan, domain.AdmissionApproval{}); !errors.Is(err, ErrPlanStale) {
				t.Fatal("stale target delivered via identity shortcut", err)
			}
			var count int
			if err := s.db.QueryRow(`SELECT count(*) FROM synced_playlist_entry WHERE local_state='imported'`).Scan(&count); err != nil || count != 0 {
				t.Fatal(count, err)
			}
		})
	}
}

func TestAdmissionAndSyncTransactionOrdering(t *testing.T) {
	for _, acceptFirst := range []bool{true, false} {
		t.Run(map[bool]string{true: "accept-first", false: "sync-first"}[acceptFirst], func(t *testing.T) {
			s := openTestStore(t)
			source := &syncSource{snapshot: syncTracks("Song")}
			svc := newSyncService(t, s, source)
			bound := bindSync(t, svc)
			item, err := s.ClaimNextItem(bound.BatchID)
			if err != nil || item == nil {
				t.Fatal(item, err)
			}
			plan := admissionForSync(t, s, item.ID, false)
			source.snapshot = syncTracks()
			preview, err := svc.PrepareSync(context.Background(), "delete", bound.PlaylistID)
			if err != nil {
				t.Fatal(err)
			}
			if acceptFirst {
				if _, err := s.CommitAdmission(plan, domain.AdmissionApproval{}); err != nil {
					t.Fatal(err)
				}
				if _, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil); !errors.Is(err, playlists.ErrPlanStale) {
					t.Fatal(err)
				}
				svc.Cancel(preview.ID)
				preview, err = svc.PrepareSync(context.Background(), "delete-again", bound.PlaylistID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil); err != nil {
					t.Fatal(err)
				}
				entries, err := s.SyncedPlaylistState(bound.PlaylistID)
				if err != nil || len(entries) != 1 || entries[0].UpstreamPresent || entries[0].TrackID == nil {
					t.Fatal(entries, err)
				}
			} else {
				if _, err := svc.Commit(context.Background(), preview.ID, preview.Hash, nil); err != nil {
					t.Fatal(err)
				}
				if _, err := s.CommitAdmission(plan, domain.AdmissionApproval{}); !errors.Is(err, ErrPlanStale) {
					t.Fatal(err)
				}
				for _, fn := range []func() error{func() error { return s.RetryItem(item.ID) }, func() error { return s.MarkReview(item.ID, ReviewNoMatch, "[]", "[]") }, func() error { _, err := s.RecordFailure(item.ID, "late", 0, 1); return err }} {
					if !errors.Is(fn(), ErrItemNotActive) {
						t.Fatal("late callback revived removed input")
					}
				}
				var tracks int
				if err := s.db.QueryRow(`SELECT count(*) FROM track`).Scan(&tracks); err != nil || tracks != 0 {
					t.Fatal(tracks, err)
				}
			}
		})
	}
}

func TestDiscardSharedTargetsAndSelectiveRestore(t *testing.T) {
	s := openTestStore(t)
	source := &syncSource{snapshot: syncTracks("Song")}
	svc := newSyncService(t, s, source)
	bound := bindSync(t, svc)
	other := syncTracks("Song")
	other.Ref.ID = "other"
	otherID, _, otherBatch := saveSyncFixture(t, s, other, true)
	item, err := s.ClaimNextItem(bound.BatchID)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	fileBatch := identityBatch(t, s, identityItem("Song", "Artist", "Album"))
	if err := s.MarkDiscarded(item.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDiscarded(item.ID); !errors.Is(err, ErrItemNotActive) {
		t.Fatal(err)
	}
	for _, p := range []int64{bound.PlaylistID, otherID} {
		entries, err := s.SyncedPlaylistState(p)
		if err != nil || entries[0].LocalState != playlists.Ignored {
			t.Fatal(entries, err)
		}
	}
	for _, b := range []int64{otherBatch, fileBatch} {
		stats, err := s.Stats(b)
		if err != nil || stats.Merged != 1 || stats.Discarded != 0 {
			t.Fatal(stats, err)
		}
	}
	p, err := svc.PrepareSync(context.Background(), "restore-one", bound.PlaylistID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := svc.Commit(context.Background(), p.ID, p.Hash, []int64{p.Difference.Ignored[0].EntryID})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.ClaimNextItem(result.BatchID)
	if err != nil || restored == nil || restored.ID == item.ID {
		t.Fatal(restored, err)
	}
	targets, err := s.ActiveImportDestinations(restored.ID)
	if err != nil || len(targets) != 1 || *targets[0].PlaylistID != bound.PlaylistID {
		t.Fatal(targets, err)
	}
}

func TestWholeTrackDuplicateEvidenceIgnoresMembership(t *testing.T) {
	for _, library := range []bool{true, false} {
		s := openTestStore(t)
		id, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: library, Title: "Song", Credit: ptr("Artist"), Album: ptr("Album")})
		if err != nil {
			t.Fatal(err)
		}
		item := activeInput(t, s, identityItem("Song", "Artist", "Album"))
		if handled, err := s.ResolveImport(item.ID, nil, nil); err != nil || !handled {
			t.Fatal(handled, err)
		}
		track, err := s.GetTrack(id)
		if err != nil || track.InLibrary != library {
			t.Fatal(track, err)
		}
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{InLibrary: !library, Title: "Song", Credit: ptr("Artist"), Album: ptr("Album")}); err != nil {
			t.Fatal(err)
		}
		ambiguous := activeInput(t, s, identityItem("Song", "Artist", "Album"))
		// No borrowed confirmation when target contexts differ is covered in
		// owned_import tests; an unconfirmed equivalent input stays ambiguous.
		if err := s.withTx(func(tx *sql.Tx) error { _, err := tx.Exec(`DELETE FROM import_confirmation`); return err }); err != nil {
			t.Fatal(err)
		}
		if handled, err := s.ResolveImport(ambiguous.ID, []matching.ScoredCandidate{}, nil); err != nil || handled {
			t.Fatal(handled, err)
		}
	}
}
