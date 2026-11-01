package library

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func queueInput(t *testing.T, st *store.Store) *store.ImportItem {
	t.Helper()
	batch, err := st.CreateBatch(&store.ImportBatch{Filename: "source.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `["原始曲名","原始专辑"]`}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatalf("queue input: %+v %v", item, err)
	}
	return item
}

func queueSeed(item *store.ImportItem, target domain.AdmissionSeed) domain.AdmissionSeed {
	target.ImportItemID, target.ItemRevision = item.ID, item.Revision
	return target
}

func skipPlan(t *testing.T, s *Service, item *store.ImportItem, target domain.AdmissionSeed) *domain.AdmissionPlan {
	t.Helper()
	candidate := provider.Candidate{Provider: target.Snapshot.Track.Identity.Provider, MatchRef: target.Snapshot.Track.Identity.Ref, ReleaseRef: &target.Snapshot.Release.Identity.Ref}
	plan, err := s.PrepareSkipExisting(context.Background(), candidate, item.ID, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Baseline.RequiresConfirmation || plan.Seed.Operation != domain.AdmissionSkipExisting {
		t.Fatalf("skip requested release confirmation: %+v", plan)
	}
	return plan
}

// Snapshot every library table, including FTS, cover references and revisions.
// Only queue and operation-result diagnostics are allowed to change on skip.
func libraryTables(t *testing.T, st *store.Store) string {
	t.Helper()
	rows, err := st.DB().Query(`SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "import_") || strings.HasPrefix(name, "review_problem") || name == "operation_result" || name == "task_change_state" {
			continue
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	out := map[string][]string{}
	for _, table := range tables {
		out[table] = []string{}
		rows, err := st.DB().Query(`SELECT * FROM "` + table + `"`)
		if err != nil {
			t.Fatalf("read %s: %v", table, err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			values, dest := make([]any, len(columns)), make([]any, len(columns))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			raw, err := json.Marshal(values)
			if err != nil {
				t.Fatalf("snapshot %s: %v", table, err)
			}
			out[table] = append(out[table], string(raw))
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		sort.Strings(out[table])
	}
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertExistingInput(t *testing.T, st *store.Store, before *store.ImportItem, trackID int64) *store.ImportItem {
	t.Helper()
	after, err := st.GetItem(before.ID)
	if err != nil || after.Status != store.StatusSkipped || after.TrackID == nil || *after.TrackID != trackID ||
		after.ResolutionReason == nil || *after.ResolutionReason != store.ResolutionExistingTrack ||
		after.ResolutionTarget == nil || after.Revision != before.Revision+1 || after.RawRow != before.RawRow {
		t.Fatalf("wrong exact skip outcome: %+v %v", after, err)
	}
	stats, err := st.Stats(before.BatchID)
	if err != nil || stats.Imported != 0 || stats.Skipped != 1 {
		t.Fatalf("existing counted as new: %+v %v", stats, err)
	}
	return after
}

func TestSingleInputSkipAndDuplicateAcceptNeverChangeLibrary(t *testing.T) {
	s, st, clock := newTestService(t)
	target := seed("release", "track", "group", "Album", "Artist")
	target.Snapshot.Track.Identifiers = domain.Known([]domain.Identifier{{Kind: "ISRC", Value: "JPE301500361"}})
	existing := commit(t, s, prepare(t, s, target), false)
	userTitle := "用户改名"
	if err := st.UpdateTrackFields(existing.TrackID, &userTitle, nil, true); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"skip", "accept"} {
		t.Run(mode, func(t *testing.T) {
			item := queueInput(t, st)
			before := libraryTables(t, st)
			var plan *domain.AdmissionPlan
			if mode == "skip" {
				plan = skipPlan(t, s, item, target)
			} else {
				plan = prepare(t, s, queueSeed(item, target))
			}
			result := commit(t, s, plan, false)
			if !result.Existing || result.TrackID != existing.TrackID {
				t.Fatal(result)
			}
			after := assertExistingInput(t, st, item, result.TrackID)
			if after.ResolutionTarget.Track != target.Snapshot.Track.Identity || after.ResolutionTarget.Release != target.Snapshot.Release.Identity {
				t.Fatal("wrong recorded identity", after.ResolutionTarget)
			}
			if got := libraryTables(t, st); got != before {
				t.Fatal("existing acceptance/skip mutated a library table")
			}
			clock.now = clock.now.Add(PlanTTL + time.Second)
			again := commit(t, s, plan, false)
			if !reflect.DeepEqual(again, result) {
				t.Fatal("idempotent result changed", again, result)
			}
			reloaded, err := st.GetItem(item.ID)
			if err != nil || !reflect.DeepEqual(after, reloaded) {
				t.Fatal("retry changed terminal input", reloaded, err)
			}
		})
	}
}

func TestSkipRevalidatesDeletionRebindingAndRelease(t *testing.T) {
	for _, mutation := range []string{"delete", "rebind", "rebind-back", "release"} {
		t.Run(mutation, func(t *testing.T) {
			s, st, _ := newTestService(t)
			target := seed("release", "track", "group", "Album", "Artist")
			existing := commit(t, s, prepare(t, s, target), false)
			item := queueInput(t, st)
			plan := skipPlan(t, s, item, target)
			switch mutation {
			case "delete":
				if err := st.DeleteTrack(existing.TrackID); err != nil {
					t.Fatal(err)
				}
			case "rebind", "rebind-back":
				if err := st.SetProvenance(existing.TrackID, "test", "another-track"); err != nil {
					t.Fatal(err)
				}
				if mutation == "rebind-back" {
					if err := st.SetProvenance(existing.TrackID, "test", "track"); err != nil {
						t.Fatal(err)
					}
				}
			case "release":
				if _, err := st.DB().Exec("UPDATE release SET provider_ref='other-release',revision=revision+1 WHERE id=?", existing.ReleaseID); err != nil {
					t.Fatal(err)
				}
			}
			before := libraryTables(t, st)
			if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
				t.Fatal("stale skip accepted")
			}
			after, err := st.GetItem(item.ID)
			if err != nil || !reflect.DeepEqual(item, after) {
				t.Fatal("rejected skip mutated input", after, err)
			}
			if libraryTables(t, st) != before {
				t.Fatal("rejected skip mutated library")
			}
		})
	}
}

func TestSingleInputOldPlansAndQueueWritesCannotRestoreTerminal(t *testing.T) {
	s, st, _ := newTestService(t)
	target := seed("release", "track", "group", "Album", "Artist")
	commit(t, s, prepare(t, s, target), false)
	item := queueInput(t, st)
	skip := skipPlan(t, s, item, target)
	oldAccept := prepare(t, s, queueSeed(item, seed("other", "other-track", "other-group", "Other album", "Other artist")))
	oldExisting := prepare(t, s, queueSeed(item, target))
	commit(t, s, skip, false)
	before, err := st.GetItem(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []*domain.AdmissionPlan{oldAccept, oldExisting} {
		if _, err := s.CommitAdmission(context.Background(), old.ID, old.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
			t.Fatalf("old plan accepted: %v", err)
		}
	}
	for _, update := range []func() error{
		func() error {
			return st.MarkReviewAtRevision(item.ID, item.Revision, store.ReviewAmbiguous, `[]`, `[]`)
		},
		func() error { return st.MarkReview(item.ID, store.ReviewAmbiguous, `[]`, `[]`) },
		func() error { return st.RetryItem(item.ID, item.Revision) },
		func() error { _, err := st.RecordFailure(item.ID, "late failure", time.Second, 1); return err },
		func() error { return st.MarkDiscarded(item.ID) },
	} {
		if err := update(); !errors.Is(err, store.ErrPlanStale) {
			t.Fatalf("late update accepted: %v", err)
		}
	}
	after, err := st.GetItem(item.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("terminal changed", after, err)
	}
	if n, err := st.CountTracks(store.TrackFilter{}); err != nil || n != 1 {
		t.Fatal("old accept created a track", n, err)
	}
}

func TestSingleInputRevisionCheckedBeforeExistingFastPath(t *testing.T) {
	s, st, _ := newTestService(t)
	target := seed("release", "track", "group", "Album", "Artist")
	commit(t, s, prepare(t, s, target), false)
	item := queueInput(t, st)
	plan := prepare(t, s, queueSeed(item, target))
	if err := st.MarkReviewAtRevision(item.ID, item.Revision, store.ReviewAmbiguous, `[]`, `[]`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatalf("existing fast path skipped revision: %v", err)
	}
	if err := st.MarkReviewAtRevision(item.ID, item.Revision, store.ReviewNoMatch, `[]`, `[]`); !errors.Is(err, store.ErrPlanStale) {
		t.Fatalf("old search revision accepted: %v", err)
	}
	if _, err := s.PrepareAdmission(context.Background(), queueSeed(item, target)); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("stale input refreshed silently", err)
	}
}

func TestConcurrentSingleInputAcceptsCreateOnceAndSkipRest(t *testing.T) {
	s, st, _ := newTestService(t)
	target := seed("release", "track", "group", "Album", "Artist")
	items := []*store.ImportItem{queueInput(t, st), queueInput(t, st)}
	plans := []*domain.AdmissionPlan{prepare(t, s, queueSeed(items[0], target)), prepare(t, s, queueSeed(items[1], target))}
	var wg sync.WaitGroup
	results := make(chan *domain.AdmissionResult, 2)
	for _, plan := range plans {
		wg.Add(1)
		go func(p *domain.AdmissionPlan) {
			defer wg.Done()
			result, err := s.CommitAdmission(context.Background(), p.ID, p.Hash, domain.AdmissionApproval{})
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}(plan)
	}
	wg.Wait()
	close(results)
	var id int64
	var count, existing int
	for result := range results {
		count++
		if result.Existing {
			existing++
		}
		if id != 0 && id != result.TrackID {
			t.Fatal("two tracks", result)
		}
		id = result.TrackID
	}
	if count != 2 || existing != 1 {
		t.Fatalf("outcomes count=%d existing=%d", count, existing)
	}
	imported, skipped := 0, 0
	for _, item := range items {
		got, err := st.GetItem(item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == store.StatusImported {
			imported++
		}
		if got.Status == store.StatusSkipped {
			skipped++
			assertExistingInput(t, st, item, id)
		}
	}
	if imported != 1 || skipped != 1 {
		t.Fatalf("row accounting %d/%d", imported, skipped)
	}
}

func TestSkipFailureRollsBackQueueAndOperationResult(t *testing.T) {
	s, st, _ := newTestService(t)
	target := seed("release", "track", "group", "Album", "Artist")
	existing := commit(t, s, prepare(t, s, target), false)
	item := queueInput(t, st)
	plan := skipPlan(t, s, item, target)
	if _, err := st.DB().Exec(`CREATE TRIGGER fail_skip BEFORE INSERT ON operation_result BEGIN SELECT RAISE(ABORT,'injected skip failure'); END`); err != nil {
		t.Fatal(err)
	}
	before := libraryTables(t, st)
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("skip failure was swallowed")
	}
	after, err := st.GetItem(item.ID)
	if err != nil || !reflect.DeepEqual(item, after) {
		t.Fatal("skip partially ended input", after, err)
	}
	if prior, err := st.AdmissionResult(plan.ID, plan.Hash); err != nil || prior != nil {
		t.Fatal("failed skip has operation result", prior, err)
	}
	if libraryTables(t, st) != before {
		t.Fatal("failed skip changed library")
	}
	if _, err := st.DB().Exec("DROP TRIGGER fail_skip"); err != nil {
		t.Fatal(err)
	}
	result := commit(t, s, plan, false)
	assertExistingInput(t, st, item, existing.TrackID)
	if !result.Existing {
		t.Fatal(result)
	}
}

func TestConcurrentPlansForSameInputCannotBothCommit(t *testing.T) {
	for _, samePlan := range []bool{false, true} {
		t.Run(map[bool]string{false: "different plans", true: "same plan retry"}[samePlan], func(t *testing.T) {
			s, st, _ := newTestService(t)
			item := queueInput(t, st)
			target := queueSeed(item, seed("release", "track", "group", "Album", "Artist"))
			first := prepare(t, s, target)
			second := first
			if !samePlan {
				second = prepare(t, s, target)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 2)
			for _, plan := range []*domain.AdmissionPlan{first, second} {
				wg.Add(1)
				go func(plan *domain.AdmissionPlan) {
					defer wg.Done()
					_, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
					errs <- err
				}(plan)
			}
			wg.Wait()
			close(errs)
			success, stale := 0, 0
			for err := range errs {
				if err == nil {
					success++
				} else if errors.Is(err, store.ErrPlanStale) {
					stale++
				} else {
					t.Fatal(err)
				}
			}
			if samePlan && (success != 2 || stale != 0) || !samePlan && (success != 1 || stale != 1) {
				t.Fatalf("success=%d stale=%d", success, stale)
			}
			got, err := st.GetItem(item.ID)
			if err != nil || got.Status != store.StatusImported || got.Revision != item.Revision+1 {
				t.Fatal("input finished more than once", got, err)
			}
			if n, err := st.CountTracks(store.TrackFilter{}); err != nil || n != 1 {
				t.Fatal("duplicate track", n, err)
			}
		})
	}
}

func TestConcurrentExistingTargetDoesNotRequireObsoleteAdditionApproval(t *testing.T) {
	s, st, _ := newTestService(t)
	commit(t, s, prepare(t, s, seed("a", "a-track", "g", "Album", "Artist")), false)
	target := seed("b", "b-track", "g", "Album", "Artist")
	item := queueInput(t, st)
	plan := prepare(t, s, queueSeed(item, target))
	if !plan.Baseline.RequiresConfirmation {
		t.Fatal("fixture needs new release")
	}
	created := commit(t, s, prepare(t, s, target), true)
	result := commit(t, s, plan, false)
	if !result.Existing || result.TrackID != created.TrackID {
		t.Fatal(result)
	}
	assertExistingInput(t, st, item, result.TrackID)
}
