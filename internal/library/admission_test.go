package library

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

type testClock struct{ now time.Time }

func (c *testClock) Now() time.Time { return c.now }
func newTestService(t *testing.T) (*Service, *store.Store, *testClock) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	clock := &testClock{now: time.Now()}
	svc, err := New(provider.NewRegistry(), st, nil, clock)
	if err != nil {
		t.Fatal(err)
	}
	return svc, st, clock
}
func seed(release, track, native, title, credit string) domain.AdmissionSeed {
	r := &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: "test", Ref: release}, Title: domain.Known(title), Credit: domain.Known(domain.Credit{Text: credit})}
	if native != "" {
		r.Group = &domain.Identity{Provider: "test", Ref: native}
	}
	t := &domain.TrackSnapshot{Identity: domain.Identity{Provider: "test", Ref: track}, Release: &r.Identity, Title: domain.Known(track)}
	return domain.AdmissionSeed{Snapshot: domain.Snapshot{Release: r, Track: t}}
}
func prepare(t *testing.T, s *Service, seed domain.AdmissionSeed) *domain.AdmissionPlan {
	t.Helper()
	plan, err := s.PrepareAdmission(context.Background(), seed)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}
func commit(t *testing.T, s *Service, plan *domain.AdmissionPlan, confirm bool) *domain.AdmissionResult {
	t.Helper()
	var approval domain.AdmissionApproval
	if confirm {
		a, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
		if err != nil {
			t.Fatal(err)
		}
		approval = *a
	}
	result, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, approval)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestAdmissionRequiresBoundConfirmationAndIsIdempotent(t *testing.T) {
	s, st, clock := newTestService(t)
	first := commit(t, s, prepare(t, s, seed("a", "a1", "g", "Album", "Artist")), false)
	next := prepare(t, s, seed("b", "b1", "g", "Album", "Artist"))
	if !next.Baseline.RequiresConfirmation || len(next.Baseline.Resolution.Groups) != 1 || len(next.Baseline.Resolution.Groups[0].Members) != 1 {
		t.Fatalf("missing impact: %+v", next.Baseline)
	}
	if _, err := s.CommitAdmission(context.Background(), next.ID, next.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrConfirmationRequired) {
		t.Fatal(err)
	}
	// Editing a returned plan neither mutates the server plan nor produces a valid signature.
	changed := *next
	changed.Seed.Snapshot = domain.Snapshot{Track: &domain.TrackSnapshot{Title: domain.Known("tampered")}}
	if _, err := st.CommitAdmission(changed, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("changed plan accepted", err)
	}
	approval, err := s.ConfirmReleaseAdditions(next.ID, next.Hash)
	if err != nil {
		t.Fatal(err)
	}
	other := prepare(t, s, seed("c", "c1", "g", "Album", "Artist"))
	if _, err := s.CommitAdmission(context.Background(), other.ID, other.Hash, *approval); !errors.Is(err, store.ErrConfirmationRequired) {
		t.Fatal("approval reused for other candidate", err)
	}
	result, err := s.CommitAdmission(context.Background(), next.ID, next.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	if result.GroupID != first.GroupID || result.ReleaseID == first.ReleaseID {
		t.Fatalf("wrong member: %+v", result)
	}
	clock.now = clock.now.Add(PlanTTL + time.Second)
	again, err := s.CommitAdmission(context.Background(), next.ID, next.Hash, *approval)
	if err != nil || *again != *result {
		t.Fatal("successful retry changed result", again, err)
	}
	expired := prepare(t, s, seed("d", "d1", "g", "Album", "Artist"))
	clock.now = clock.now.Add(PlanTTL)
	if _, err := s.CommitAdmission(context.Background(), expired.ID, expired.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("expired accepted", err)
	}
	var n int
	if err := st.DB().QueryRow("SELECT count(*) FROM release").Scan(&n); err != nil || n != 2 {
		t.Fatal("unapproved rows written", n, err)
	}
}
func TestConcurrentNewGroupsAndMembersRevalidate(t *testing.T) {
	s, st, _ := newTestService(t)
	a := prepare(t, s, seed("a", "a1", "", "Album", "Artist"))
	b := prepare(t, s, seed("b", "b1", "", "ＡＬＢＵＭ", " artist "))
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, p := range []*domain.AdmissionPlan{a, b} {
		wg.Add(1)
		go func(p *domain.AdmissionPlan) {
			defer wg.Done()
			_, err := s.CommitAdmission(context.Background(), p.ID, p.Hash, domain.AdmissionApproval{})
			errs <- err
		}(p)
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
	if success != 1 || stale != 1 {
		t.Fatalf("new group race: %d %d", success, stale)
	}
	plans := []*domain.AdmissionPlan{prepare(t, s, seed("c", "c1", "", "Album", "Artist")), prepare(t, s, seed("d", "d1", "", "Album", "Artist"))}
	approvals := make([]*domain.AdmissionApproval, 2)
	for i, p := range plans {
		a, err := s.ConfirmReleaseAdditions(p.ID, p.Hash)
		if err != nil {
			t.Fatal(err)
		}
		approvals[i] = a
	}
	commit(t, s, plans[0], true)
	if _, err := s.CommitAdmission(context.Background(), plans[1].ID, plans[1].Hash, *approvals[1]); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("stale member set accepted", err)
	}
	var n int
	if err := st.DB().QueryRow("SELECT count(*) FROM release_group").Scan(&n); err != nil || n != 1 {
		t.Fatal("duplicate group", n, err)
	}
}
func TestExactIdentityConcurrencyReturnsOneTrack(t *testing.T) {
	s, st, _ := newTestService(t)
	plans := []*domain.AdmissionPlan{prepare(t, s, seed("a", "track", "g", "Album", "Artist")), prepare(t, s, seed("a", "track", "g", "Album", "Artist"))}
	var wg sync.WaitGroup
	ids := make(chan int64, 2)
	for _, p := range plans {
		wg.Add(1)
		go func(p *domain.AdmissionPlan) {
			defer wg.Done()
			result, err := s.CommitAdmission(context.Background(), p.ID, p.Hash, domain.AdmissionApproval{})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- result.TrackID
		}(p)
	}
	wg.Wait()
	close(ids)
	var id int64
	for next := range ids {
		if id != 0 && id != next {
			t.Fatal("two exact tracks")
		}
		id = next
	}
	var count int
	st.DB().QueryRow("SELECT count(*) FROM track").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestDualEvidenceConflictAndExplicitMerge(t *testing.T) {
	s, st, _ := newTestService(t)
	first := commit(t, s, prepare(t, s, seed("a", "a1", "native-a", "Album A", "Artist A")), false)
	second := commit(t, s, prepare(t, s, seed("b", "b1", "native-b", "Album B", "Artist B")), false)
	conflictSeed := seed("c", "c1", "native-a", "Album B", "Artist B")
	plan := prepare(t, s, conflictSeed)
	if !plan.Baseline.Conflict || len(plan.Baseline.Resolution.Groups) != 2 {
		t.Fatalf("evidence not unioned: %+v", plan.Baseline)
	}
	if _, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash); !errors.Is(err, store.ErrGroupConflict) {
		t.Fatal(err)
	}
	if _, err := s.ReplanAdmissionGroup(context.Background(), plan.ID, plan.Hash, 99999); !errors.Is(err, store.ErrGroupConflict) {
		t.Fatalf("unknown merge target: %v", err)
	}
	merged, err := s.ReplanAdmissionGroup(context.Background(), plan.ID, plan.Hash, second.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Seed.MergeTargetID != 0 || merged.Seed.Snapshot.Track.Identity != plan.Seed.Snapshot.Track.Identity {
		t.Fatal("replan mutated candidate")
	}
	if _, err := s.CommitAdmission(context.Background(), merged.ID, merged.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrConfirmationRequired) {
		t.Fatalf("merge bypass: %v", err)
	}
	if !merged.Baseline.RequiresConfirmation {
		t.Fatal("merge bypassed approval")
	}
	result := commit(t, s, merged, true)
	if result.GroupID != second.GroupID {
		t.Fatal(result)
	}
	var group int64
	if err := st.DB().QueryRow("SELECT release_group_id FROM release WHERE id=?", first.ReleaseID).Scan(&group); err != nil || group != second.GroupID {
		t.Fatal("member not moved", err)
	}
	var count int
	st.DB().QueryRow("SELECT count(*) FROM release_group").Scan(&count)
	if count != 1 {
		t.Fatal(count)
	}
}
func TestMissingFieldsAndNameNormalization(t *testing.T) {
	s, st, _ := newTestService(t)
	for i := 0; i < 2; i++ {
		local := domain.AdmissionSeed{Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{}, Release: &domain.ReleaseSnapshot{Title: domain.Known("Only title")}}}
		p := prepare(t, s, local)
		if !p.Baseline.Resolution.Insufficient {
			t.Fatal("missing credit treated as evidence")
		}
		commit(t, s, p, false)
	}
	var n int
	st.DB().QueryRow("SELECT count(*) FROM release_group").Scan(&n)
	if n != 2 {
		t.Fatal("empty credit grouped", n)
	}
	noRelease := commit(t, s, prepare(t, s, domain.AdmissionSeed{Snapshot: domain.Snapshot{Track: &domain.TrackSnapshot{}}}), false)
	if noRelease.ReleaseID != 0 {
		t.Fatal("placeholder release created")
	}
	base := commit(t, s, prepare(t, s, seed("full", "full1", "", " ＡＬＢＵＭ  Name ", "Artist feat. B")), false)
	for i, title := range []string{"album name (Deluxe)", "album name"} {
		candidate := seed(fmt.Sprint("r", i), fmt.Sprint("t", i), "", title, "Artist")
		p := prepare(t, s, candidate)
		if p.Baseline.TargetGroupID == base.GroupID {
			t.Fatal("suffix/feat removed")
		}
	}
	normalized := prepare(t, s, seed("normal", "norm1", "other-native", "album name", "ARTIST FEAT. B"))
	if normalized.Baseline.TargetGroupID != base.GroupID || !normalized.Baseline.RequiresConfirmation {
		t.Fatal("independent normalized path missed", normalized.Baseline)
	}
	if err := st.RebuildGroupEvidence(); err != nil {
		t.Fatal(err)
	}
	st.DB().QueryRow("SELECT count(*) FROM release_group_match_key").Scan(&n)
	if n == 0 {
		t.Fatal("empty evidence index")
	}
}

func TestLateFactsCreateProposalWithoutMovingAndRecheckOnUse(t *testing.T) {
	s, st, _ := newTestService(t)
	firstSeed := seed("a", "a1", "", "Album", "")
	firstSeed.Snapshot.Release.Credit = domain.Missing[domain.Credit]()
	first := commit(t, s, prepare(t, s, firstSeed), false)
	second := commit(t, s, prepare(t, s, seed("b", "b1", "", "Album", "Artist")), false)
	late := firstSeed.Snapshot
	copyRelease := *late.Release
	late.Release = &copyRelease
	late.Release.Credit = domain.Known(domain.Credit{Text: "Artist"})
	revision, _ := st.TrackRevision(first.TrackID)
	if err := st.ApplySnapshot(first.TrackID, &late, revision); err != nil {
		t.Fatal(err)
	}
	release, err := st.GetRelease(first.ReleaseID)
	if err != nil || *release.ReleaseGroupID != first.GroupID {
		t.Fatal("background moved member", err)
	}
	proposals, err := s.ListGroupProposals()
	if err != nil || len(proposals) != 1 {
		t.Fatal("missing proposal", proposals, err)
	}
	revision, _ = st.TrackRevision(first.TrackID)
	if err := st.ApplySnapshot(first.TrackID, &late, revision); err != nil {
		t.Fatal(err)
	}
	proposals, err = s.ListGroupProposals()
	if err != nil || len(proposals) != 1 {
		t.Fatal("success cleared/duplicated proposal", proposals, err)
	}
	p, err := s.PrepareGroupProposal(context.Background(), proposals[0].ID, second.GroupID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), p.ID, p.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrConfirmationRequired) {
		t.Fatal("proposal bypassed confirmation", err)
	}
	result := commit(t, s, p, true)
	if result.TrackID != 0 || result.ReleaseID != first.ReleaseID || result.GroupID != second.GroupID {
		t.Fatal("proposal created track instead of moving membership", result)
	}
	var n int
	st.DB().QueryRow("SELECT count(*) FROM track").Scan(&n)
	if n != 2 {
		t.Fatal(n)
	}
	proposals, err = s.ListGroupProposals()
	if err != nil || len(proposals) != 0 {
		t.Fatal("accepted proposal pending", err)
	}
}

func TestQueueAndIdentityRollBackTogether(t *testing.T) {
	s, st, _ := newTestService(t)
	batch, err := st.CreateBatch(&store.ImportBatch{Filename: "test.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `{"曲名":"原始"}`}}); err != nil {
		t.Fatal(err)
	}
	var item int64
	st.DB().QueryRow("SELECT id FROM import_item").Scan(&item)
	draft := seed("a", "a1", "g", "Album", "Artist")
	draft.ImportItemID = item
	p := prepare(t, s, draft)
	if _, err := st.DB().Exec("CREATE TRIGGER fail_result BEFORE INSERT ON operation_result BEGIN SELECT RAISE(ABORT,'failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), p.ID, p.Hash, domain.AdmissionApproval{}); err == nil {
		t.Fatal("failure not propagated")
	}
	for _, table := range []string{"track", "release", "release_group", "track_fts_shadow"} {
		var n int
		if err := st.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatal("partial commit", table, n, err)
		}
	}
	var status, raw string
	st.DB().QueryRow("SELECT status,raw_row FROM import_item WHERE id=?", item).Scan(&status, &raw)
	if status != "queued" || raw != `{"曲名":"原始"}` {
		t.Fatal("queue mutated after failure", status, raw)
	}
	if _, err := st.DB().Exec("DROP TRIGGER fail_result"); err != nil {
		t.Fatal(err)
	}
	result := commit(t, s, p, false)
	var trackID int64
	st.DB().QueryRow("SELECT status,track_id FROM import_item WHERE id=?", item).Scan(&status, &trackID)
	if status != "imported" || trackID != result.TrackID {
		t.Fatal("queue not atomic", status, trackID)
	}
}

func TestGroupEvidenceStopsUsingWithdrawnCurrentName(t *testing.T) {
	s, st, _ := newTestService(t)
	local := domain.AdmissionSeed{Snapshot: domain.Snapshot{Release: &domain.ReleaseSnapshot{Title: domain.Known("Before"), Credit: domain.Known(domain.Credit{Text: "Artist"})}, Track: &domain.TrackSnapshot{}}}
	existing := commit(t, s, prepare(t, s, local), false)
	if err := st.UpdateReleaseFields(existing.ReleaseID, map[string]string{"title": "After"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	// 组摘要也是当前有效证据，明确撤销它后旧名称不应继续作为别名。
	if _, err := st.DB().Exec("UPDATE release_group_current SET title='After' WHERE group_id=?", existing.GroupID); err != nil {
		t.Fatal(err)
	}
	p := prepare(t, s, seed("new", "new1", "", "Before", "Artist"))
	if p.Baseline.TargetGroupID != 0 {
		t.Fatal("withdrawn name still matched", p.Baseline)
	}
	if err := st.RebuildGroupEvidence(); err != nil {
		t.Fatal(err)
	}
	var n int
	st.DB().QueryRow("SELECT count(*) FROM release_group_match_key WHERE title_key='before'").Scan(&n)
	if n != 0 {
		t.Fatal("old index survived rebuild", n)
	}
}

func TestRevisionChangesInvalidateApprovedPlan(t *testing.T) {
	s, st, _ := newTestService(t)
	first := commit(t, s, prepare(t, s, seed("a", "a1", "g", "Album", "Artist")), false)
	plan := prepare(t, s, seed("b", "b1", "g", "Album", "Artist"))
	approval, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateReleaseFields(first.ReleaseID, map[string]string{"primaryType": "single"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, *approval); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("revision not rechecked", err)
	}
	var n int
	st.DB().QueryRow("SELECT count(*) FROM release").Scan(&n)
	if n != 1 {
		t.Fatal("stale plan wrote release")
	}
}
