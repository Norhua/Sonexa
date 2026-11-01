package library

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func TestMultiInputAdmissionAtomicAccounting(t *testing.T) {
	for _, mode := range []string{"new", "duplicate-accept", "skip", "concurrent-target"} {
		t.Run(mode, func(t *testing.T) {
			s, st, _ := newTestService(t)
			commit(t, s, prepare(t, s, seed("existing", "other", "group", "Album", "Artist")), false)
			target := seed("release", "track", "group", "Album", "Artist")
			if mode == "duplicate-accept" || mode == "skip" {
				commit(t, s, prepare(t, s, target), true)
			}
			a, b := problemInput(t, st, "Song"), problemInput(t, st, "Song（译名：歌曲）")
			scope, token := problemScope(t, st, a.ID)
			target.InputScope = scope
			var plan *domain.AdmissionPlan
			if mode == "skip" {
				var err error
				plan, err = s.PrepareScopedSkipExisting(context.Background(), candidate("release", "track", nil), *scope)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				plan = prepare(t, s, target)
			}
			if mode == "new" || mode == "concurrent-target" {
				if !plan.Baseline.RequiresConfirmation {
					t.Fatal("missing member confirmation")
				}
				if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrConfirmationRequired) {
					t.Fatal(err)
				}
			}
			if mode == "concurrent-target" {
				target.InputScope = nil
				commit(t, s, prepare(t, s, target), true)
			}
			before := libraryTables(t, st)
			result := commit(t, s, plan, mode == "new")
			if result.Existing != (mode != "new") || len(result.Inputs) != 2 || result.Target == nil || result.Target.Track != target.Snapshot.Track.Identity || result.Target.Release != target.Snapshot.Release.Identity {
				t.Fatal(result)
			}
			for i, item := range []*store.ImportItem{a, b} {
				got, err := st.GetItem(item.ID)
				status, reason := store.StatusSkipped, store.ResolutionExistingTrack
				if mode == "new" && i == 0 {
					status, reason = store.StatusImported, store.ResolutionImported
				}
				if err != nil || got.Status != status || got.ResolutionReason == nil || *got.ResolutionReason != reason || got.RawRow != item.RawRow || got.TrackID == nil || *got.TrackID != result.TrackID || got.ResolutionTarget == nil || got.ResolutionTarget.Track != target.Snapshot.Track.Identity || got.ResolutionProblemRevision == nil || *got.ResolutionProblemRevision != token.Revision {
					t.Fatal(got, err)
				}
				if result.Inputs[i] != (domain.AdmissionInputResult{ItemID: item.ID, BatchID: item.BatchID, TrackID: result.TrackID, Status: string(status), Reason: string(reason)}) {
					t.Fatal(result.Inputs)
				}
				stats, err := st.Stats(item.BatchID)
				if err != nil || stats.Review != 0 || stats.Imported+stats.Skipped != 1 || stats.ExistingSkipped != stats.Skipped || stats.Problems.ReviewProblems != 0 || (status == store.StatusImported && stats.Imported != 1) || (status == store.StatusSkipped && stats.Skipped != 1) {
					t.Fatal(stats, err)
				}
			}
			if _, err := st.ReadProblemScope(token.ProblemID); !errors.Is(err, store.ErrPlanStale) {
				t.Fatal("problem not closed", err)
			}
			if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != 2 {
				t.Fatal(count, err)
			}
			if mode != "new" && libraryTables(t, st) != before {
				t.Fatal("existing result changed library")
			}
			if again := commit(t, s, plan, false); !reflect.DeepEqual(result, again) {
				t.Fatal(again, result)
			}
		})
	}
}

func TestMultiInputFailureRollsBackEveryMemberAndEntity(t *testing.T) {
	for _, mode := range []string{"member-write", "operation-result", "file", "skip-member"} {
		t.Run(mode, func(t *testing.T) {
			s, st, _ := newTestService(t)
			target := seed("release", "track", "group", "Album", "Artist")
			if mode == "skip-member" {
				commit(t, s, prepare(t, s, target), false)
			}
			a, b := problemInput(t, st, "Song"), problemInput(t, st, "Song")
			scope, token := problemScope(t, st, a.ID)
			target.InputScope = scope
			if mode == "file" {
				// Admission must not publish a file-valued field without the
				// existing file staging transaction. Rejection occurs after
				// tentative entity creation and must roll all of it back.
				target.Snapshot.Track.Lyrics = domain.Known("unprepared-file.lrc")
			}
			if mode == "skip-member" {
				target.Operation = domain.AdmissionSkipExisting
			}
			plan := prepare(t, s, target)
			switch mode {
			case "member-write", "skip-member":
				if _, err := st.DB().Exec(`CREATE TRIGGER fail_second BEFORE UPDATE OF status ON import_item WHEN NEW.id=` + strconv.FormatInt(b.ID, 10) + ` AND NEW.status IN ('imported','skipped') BEGIN SELECT RAISE(ABORT,'injected member failure'); END`); err != nil {
					t.Fatal(err)
				}
			case "operation-result":
				if _, err := st.DB().Exec(`CREATE TRIGGER fail_result BEFORE INSERT ON operation_result BEGIN SELECT RAISE(ABORT,'injected operation failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			before := libraryTables(t, st)
			if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); err == nil {
				t.Fatal("failure swallowed")
			}
			if libraryTables(t, st) != before {
				t.Fatal("partial library writes")
			}
			for _, item := range []*store.ImportItem{a, b} {
				got, err := st.GetItem(item.ID)
				if err != nil || !reflect.DeepEqual(got, item) {
					t.Fatal("partial input result", got, err)
				}
			}
			current, err := st.ReadProblemScope(token.ProblemID)
			if err != nil || !reflect.DeepEqual(current, token) {
				t.Fatal("partial membership", current, err)
			}
			if result, err := st.AdmissionResult(plan.ID, plan.Hash); err != nil || result != nil {
				t.Fatal(result, err)
			}
		})
	}
}

func TestMultiInputNoMatchSeedIsRejected(t *testing.T) {
	s, st, _ := newTestService(t)
	a := problemInput(t, st, "Song")
	problemInput(t, st, "Song")
	scope, _ := problemScope(t, st, a.ID)
	_, err := s.PrepareAdmission(context.Background(), domain.AdmissionSeed{InputScope: scope, Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{Title: domain.Known("Song")}}})
	if !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("multi-row local creation accepted", err)
	}
	if n, err := st.CountTracks(store.TrackFilter{}); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestConcurrentMultiInputPlansFinishOnlyTheirOwnScope(t *testing.T) {
	s, st, _ := newTestService(t)
	a := problemInput(t, st, "Song")
	problemInput(t, st, "Song")
	b := problemInput(t, st, "Different song")
	problemInput(t, st, "Different song")
	var plans []*domain.AdmissionPlan
	for _, id := range []int64{a.ID, b.ID} {
		scope, _ := problemScope(t, st, id)
		target := seed("release", "track", "group", "Album", "Artist")
		target.InputScope = scope
		plans = append(plans, prepare(t, s, target))
	}
	var wg sync.WaitGroup
	results := make(chan *domain.AdmissionResult, 2)
	for _, plan := range plans {
		wg.Add(1)
		go func(plan *domain.AdmissionPlan) {
			defer wg.Done()
			result, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{})
			if err != nil {
				t.Error(err)
				return
			}
			results <- result
		}(plan)
	}
	wg.Wait()
	close(results)
	imported, skipped := 0, 0
	for result := range results {
		for _, input := range result.Inputs {
			if input.Status == string(store.StatusImported) {
				imported++
			} else if input.Status == string(store.StatusSkipped) {
				skipped++
			}
		}
	}
	if imported != 1 || skipped != 3 {
		t.Fatal(imported, skipped)
	}
	if count, err := st.CountTracks(store.TrackFilter{}); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
