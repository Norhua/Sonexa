package app

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

func appProblemRows(t *testing.T, a *App) (int64, int64, store.ProblemScope) {
	t.Helper()
	var ids []int64
	for _, title := range []string{"Song", "Song（译名：歌曲）"} {
		batch, err := a.store.CreateBatch(&store.ImportBatch{Filename: "source.csv", TotalRows: 1})
		if err != nil {
			t.Fatal(err)
		}
		artist, album := "Artist", "Album"
		year, disc, position, duration := 2020, 2, 3, 620000
		if err := a.store.EnqueueItems(batch, []store.ImportItem{{RawRow: `["` + title + `"]`, InTitle: &title, InArtist: &artist, InAlbum: &album, InYear: &year, InDiscNo: &disc, InTrackNo: &position, InDurationMS: &duration}}); err != nil {
			t.Fatal(err)
		}
		item, err := a.store.ClaimNextItem(batch)
		if err != nil || item == nil {
			t.Fatal(item, err)
		}
		id := item.ID
		if err := a.store.MarkReview(id, store.ReviewNoMatch, "[]", "[]"); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	scope, err := a.store.ItemProblemScope(ids[0])
	if err != nil || len(scope.Members) != 2 {
		t.Fatal(scope, err)
	}
	return ids[0], ids[1], scope
}

func TestAppMultiInputAdmissionAndExactSkip(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "accept", true: "skip"}[existing], func(t *testing.T) {
			a, cleanup := newTestApp(t)
			defer cleanup()
			a.enricher.Pause()
			row := candidateRow()
			if existing {
				if _, err := a.AddToLibrary(row); err != nil {
					t.Fatal(err)
				}
			}
			one, two, token := appProblemRows(t, a)
			scope, err := a.GetReviewAdmissionScope(token)
			if err != nil {
				t.Fatal(err)
			}
			var plan *AdmissionPlanView
			if existing {
				plan, err = a.PrepareProblemSkipExisting(row, *scope)
			} else {
				plan, err = a.PrepareProblemAdmission(row, *scope)
			}
			if err != nil || plan.Resolution != nil || !reflect.DeepEqual(plan.Baseline.InputScope, scope) {
				t.Fatal(plan, err)
			}
			result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
			if err != nil || result.Existing != existing || len(result.Inputs) != 2 {
				t.Fatal(result, err)
			}
			for i, id := range []int64{one, two} {
				item, err := a.store.GetItem(id)
				want := store.StatusSkipped
				if i == 0 && !existing {
					want = store.StatusImported
				}
				if err != nil || item.Status != want || item.TrackID == nil || *item.TrackID != result.TrackID {
					t.Fatal(item, err)
				}
			}
			if count, err := a.store.CountTracks(store.TrackFilter{}); err != nil || count != 1 {
				t.Fatal(count, err)
			}
			if again, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil || !reflect.DeepEqual(result, again) {
				t.Fatal(again, err)
			}
		})
	}
}

func TestAppNoMatchIsOnlySelectedOriginalRow(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	one, two, token := appProblemRows(t, a)
	before, err := a.store.GetItem(two)
	if err != nil {
		t.Fatal(err)
	}
	// Query drafts must not become local originals.
	if _, err := a.store.BeginProblemQuery(token, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: "Different draft"}); err != nil {
		t.Fatal(err)
	}
	plan, err := a.PrepareNoMatchNeeded(one)
	if err != nil || len(plan.Baseline.InputScope.Members) != 1 || plan.Baseline.InputScope.Members[0].ItemID != one {
		t.Fatal(plan, err)
	}
	result, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{})
	if err != nil || len(result.Inputs) != 1 || result.Inputs[0].Reason != string(store.ResolutionWithoutMatch) {
		t.Fatal(result, err)
	}
	track, err := a.store.GetTrack(result.TrackID)
	if err != nil || track.Title != "Song" {
		t.Fatal(track, err)
	}
	var year, disc, position int
	if err := a.store.DB().QueryRow("SELECT year,disc_number,track_number FROM track_current WHERE track_id=?", result.TrackID).Scan(&year, &disc, &position); err != nil || year != 2020 || disc != 2 || position != 3 {
		t.Fatal(year, disc, position, err)
	}
	// Duration remains an original input fact, not a fabricated source
	// snapshot or a new editable current field.
	original, err := a.store.GetItem(one)
	if err != nil || original.InDurationMS == nil || *original.InDurationMS != 620000 {
		t.Fatal(original, err)
	}
	var source, ref *string
	if err := a.store.DB().QueryRow("SELECT match_source,match_ref FROM track WHERE id=?", result.TrackID).Scan(&source, &ref); err != nil || source != nil || ref != nil {
		t.Fatal(source, ref, err)
	}
	after, err := a.store.GetItem(two)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatal("other row changed", after, err)
	}
	if count, err := a.store.CountTracks(store.TrackFilter{}); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	current, err := a.GetReviewProblemScope(token.ProblemID)
	if err != nil || len(current.Members) != 1 || current.Members[0].ItemID != two {
		t.Fatal(current, err)
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, domain.AdmissionApproval{}); err != nil {
		t.Fatal(err)
	}
	if count, err := a.store.CountTracks(store.TrackFilter{}); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}

func TestAppProblemDiscardAndRetryKeepExplicitSubset(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	// This tests the API's selected-row persistence, not Runner scheduling.
	// Leave the production start branch to importer integration tests.
	runner := a.runner
	a.runner = nil
	defer func() { a.runner = runner }()
	one, two, token := appProblemRows(t, a)
	before, err := a.store.GetItem(one)
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.store.GetItem(two)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.RecordFailure(two, "fixture failed", time.Second, 1, second.Revision); err != nil {
		t.Fatal(err)
	}
	current, err := a.GetReviewProblemScope(token.ProblemID)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.RetryReviewProblemMembers(current, []int64{one, two}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("review row retried", err)
	}
	if err := a.RetryReviewProblemMembers(token, []int64{two}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("stale retry", err)
	}
	if err := a.RetryReviewProblemMembers(current, []int64{two}); err != nil {
		t.Fatal(err)
	}
	other, err := a.store.GetItem(one)
	if err != nil || !reflect.DeepEqual(other, before) {
		t.Fatal(other, err)
	}
	second, err = a.store.GetItem(two)
	if err != nil || second.Status != store.StatusQueued || second.LastError == nil || *second.LastError != "fixture failed" {
		t.Fatal(second, err)
	}
	if err := a.DiscardReviewProblemMembers(current, []int64{one}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("stale discard", err)
	}
	if err := a.DiscardReviewItem(one); err != nil {
		t.Fatal(err)
	}
	unchanged, err := a.store.GetItem(two)
	if err != nil || !reflect.DeepEqual(unchanged, second) {
		t.Fatal(unchanged, err)
	}
	current, err = a.GetReviewProblemScope(token.ProblemID)
	if err != nil || len(current.Members) != 1 {
		t.Fatal(current, err)
	}
	if err := a.DiscardReviewProblemMembers(current, []int64{two}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.GetReviewProblemScope(token.ProblemID); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal(err)
	}
}
