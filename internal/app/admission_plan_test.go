package app

import (
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
)

func TestAppAdmissionCannotBypassExtraReleaseConfirmation(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()
	a := fullCandidate("a1")
	b := fullCandidate("b1")
	firstRef, secondRef := "release-a", "release-b"
	a.ReleaseRef = &firstRef
	b.ReleaseRef = &secondRef
	artist := "Album Artist"
	a.AlbumCredit = &artist
	b.AlbumCredit = &artist
	p, err := app.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: a}), 0)
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.CommitAdmission(p.ID, p.Hash, domain.AdmissionApproval{})
	if err != nil {
		t.Fatal(err)
	}
	p, err = app.PrepareAdmission(rowFrom(matching.ScoredCandidate{Candidate: b}), 0)
	if err != nil {
		t.Fatal(err)
	}
	// 组内已有发行的清单没有加载，无法排除它能提供这首歌：必须输入确认词，
	// 普通成员确认与错误的确认词都不能代替。
	if !p.Baseline.NeedsTypedConfirm {
		t.Fatalf("missing typed gate: %+v", p.Baseline)
	}
	if _, err := app.CommitAdmission(p.ID, p.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("App bypassed confirmation", err)
	}
	if _, err := app.ConfirmReleaseAdditions(p.ID, p.Hash); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("membership approval issued for typed plan", err)
	}
	for _, word := range []string{"yes", "YES", "", "Yes!"} {
		if _, err := app.ConfirmTypedAdmission(p.ID, p.Hash, word); err == nil {
			t.Fatalf("wrong word %q accepted", word)
		}
	}
	approval, err := app.ConfirmTypedAdmission(p.ID, p.Hash, " Yes ")
	if err != nil {
		t.Fatal(err)
	}
	forged := *approval
	forged.Kind = domain.ApprovalMembership
	if _, err := app.CommitAdmission(p.ID, p.Hash, forged); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("typed credential accepted as membership approval", err)
	}
	result, err := app.CommitAdmission(p.ID, p.Hash, *approval)
	if err != nil || result.GroupID != first.GroupID || result.ReleaseID == first.ReleaseID {
		t.Fatal(result, err)
	}
}

func TestLegacyAdmissionBindingsEnforcePlanAndAtomicQueue(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	a.enricher.Pause()
	first := fullCandidate("first")
	if _, err := a.AddToLibrary(rowFrom(matching.ScoredCandidate{Candidate: first})); err != nil {
		t.Fatal(err)
	}
	next := fullCandidate("next")
	next.ReleaseRef = strPtrOrNil("next-release")
	row := rowFrom(matching.ScoredCandidate{Candidate: next})
	itemID := enqueueOneItem(t, a, "原曲名", "原署名", "原专辑")
	for _, call := range []func() (int64, error){func() (int64, error) { return a.AddToLibrary(row) }, func() (int64, error) { return a.AcceptCandidate(itemID, row) }} {
		if _, err := call(); !errors.Is(err, store.ErrTypedConfirmationRequired) {
			t.Fatalf("绕过了确认: %v", err)
		}
	}
	item, _ := a.store.GetItem(itemID)
	if item.Status.IsTerminal() {
		t.Fatal("未确认却结束条目")
	}
	if n, _ := a.store.CountTracks(store.TrackFilter{}); n != 1 {
		t.Fatal("未确认创建收藏", n)
	}
	plan, err := a.PrepareAdmission(row, itemID)
	if err != nil {
		t.Fatal(err)
	}
	// 打开后放弃计划不会改变条目；确认后故障必须原子回滚。
	approval, err := approveView(a, plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.store.DB().Exec(`CREATE TRIGGER fail_accept BEFORE UPDATE OF status ON import_item WHEN NEW.status='imported' BEGIN SELECT RAISE(ABORT,'injected queue failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CommitAdmission(plan.ID, plan.Hash, *approval); err == nil {
		t.Fatal("应注入失败")
	}
	item, _ = a.store.GetItem(itemID)
	if item.Status.IsTerminal() {
		t.Fatal("事务失败改变条目")
	}
	if rel, _ := a.store.GetReleaseByRef("musicbrainz", "next-release"); rel != nil {
		t.Fatal("事务失败留下发行")
	}
	if _, err := a.store.DB().Exec("DROP TRIGGER fail_accept"); err != nil {
		t.Fatal(err)
	}
	result, err := a.CommitAdmission(plan.ID, plan.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	item, _ = a.store.GetItem(itemID)
	if item.Status != store.StatusImported || item.TrackID == nil || *item.TrackID != result.TrackID {
		t.Fatal("未原子完成条目")
	}
}

func TestLocalBindingsAllowEmptyBasicsWithoutInventingAlbumCredit(t *testing.T) {
	a, cleanup := newTestApp(t)
	defer cleanup()
	id, err := a.CreateTrackManual("", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	track, _ := a.store.GetTrack(id)
	if track.Title != "" || track.ReleaseID != nil {
		t.Fatalf("empty=%+v", track)
	}
	itemID := enqueueOneItem(t, a, "", "独立歌曲署名", "专辑")
	id, err = a.NoMatchNeeded(itemID)
	if err != nil {
		t.Fatal(err)
	}
	track, _ = a.store.GetTrack(id)
	rel, err := a.store.GetRelease(*track.ReleaseID)
	if err != nil || rel.Credit != nil {
		t.Fatalf("虚构了专辑署名: %+v %v", rel, err)
	}
	item, _ := a.store.GetItem(itemID)
	if item.Status != store.StatusImported {
		t.Fatal("条目未完成")
	}
}

// approveView 按基线要求取批准：需要输入确认时输入确认词，否则是普通成员确认。
func approveView(a *App, plan *AdmissionPlanView) (*domain.AdmissionApproval, error) {
	if plan.Baseline.NeedsTypedConfirm {
		return a.ConfirmTypedAdmission(plan.ID, plan.Hash, "Yes")
	}
	return a.ConfirmReleaseAdditions(plan.ID, plan.Hash)
}
