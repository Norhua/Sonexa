package library

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
)

// 修改 4 阶段 D：覆盖结论、输入确认闸门、「使用发行 A」、编辑器与归并提议。
//
// 约定：来源 test，原生组 "group"。发行的清单条目写成 {曲目标识, 录音标识}。

// songSeed 是带录音标识的候选种子。native 为空表示候选没有上游原生组。
func songSeed(release, track, recording, native string) domain.AdmissionSeed {
	s := seed(release, track, native, "Album", "Artist")
	s.Snapshot.Track.Title = domain.Known("Song " + recording)
	if recording != "" {
		s.Snapshot.Track.Recording = domain.Known(domain.Identity{Provider: "test", Ref: recording})
	}
	return s
}

// loadTracklist 给已在库的发行保存一份完整清单。
func loadTracklist(t *testing.T, st *store.Store, releaseID int64, release string, entries ...[2]string) {
	t.Helper()
	detail := releaseDetail(release, entries...)
	for i := range detail.Release.Tracklist.Value {
		detail.Release.Tracklist.Value[i].Title = domain.Known("Song " + entries[i][1])
	}
	revision, err := st.ReleaseRevision(releaseID)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyReleaseSnapshot(releaseID, detail, revision); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, st *store.Store, query string, args ...any) int {
	t.Helper()
	var n int
	if err := st.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCoverageConclusions(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)

	// 清单未加载：不可判定，要求输入确认。
	if p := prepare(t, s, songSeed("b", "b1", "r2", "group")); p.Baseline.Coverage != domain.CoverageUnknown || !p.Baseline.NeedsTypedConfirm || len(p.Baseline.Undecidable) != 1 {
		t.Fatalf("unknown: %+v", p.Baseline)
	}
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})

	// 已有发行按录音含这首歌：provided，目标不是主发行，要求输入确认。
	provided := prepare(t, s, songSeed("b", "b1", "r2", "group"))
	if b := provided.Baseline; b.Coverage != domain.CoverageProvided || !b.NeedsTypedConfirm || len(b.Providers) != 1 ||
		b.Providers[0].TrackRef != "a2" || b.Providers[0].Evidence != store.CoverageEvidenceRecording || b.Providers[0].ReleaseID != a.ReleaseID {
		t.Fatalf("provided: %+v", b)
	}
	// 组内全部发行录音级可判定且不含这首歌、原生组已映射：不需要任何确认。
	if b := prepare(t, s, songSeed("b", "b9", "r9", "group")).Baseline; b.Coverage != domain.CoverageAbsentProven || b.NeedsTypedConfirm || b.RequiresConfirmation {
		t.Fatalf("absent proven: %+v", b)
	}
	// 同样已证明独有，但组只经名称证据命中：仍是普通成员确认。
	if b := prepare(t, s, songSeed("b", "b9", "r9", "")).Baseline; b.Coverage != domain.CoverageAbsentProven || b.NeedsTypedConfirm || !b.RequiresConfirmation {
		t.Fatalf("name-only group: %+v", b)
	}
	// 候选没有录音标识：只有文本级证据。未命中 → absent_text，普通确认。
	if b := prepare(t, s, songSeed("b", "b9", "", "group")).Baseline; b.Coverage != domain.CoverageAbsentText || b.NeedsTypedConfirm || !b.RequiresConfirmation {
		t.Fatalf("absent text: %+v", b)
	}
	// 文本级命中 → possible（疑似），要求输入确认。
	textual := songSeed("b", "b1", "", "group")
	textual.Snapshot.Track.Title = domain.Known("Song r2")
	if b := prepare(t, s, textual).Baseline; b.Coverage != domain.CoveragePossible || !b.NeedsTypedConfirm || b.Providers[0].Evidence != store.CoverageEvidenceText {
		t.Fatalf("possible: %+v", b)
	}
	// 目标发行就是主发行（灰色行准入）：不要求任何确认。
	ghost, err := s.PrepareGhostAdmission(context.Background(), a.ReleaseID, 1, 2)
	if err != nil || ghost.Baseline.NeedsTypedConfirm || ghost.Baseline.RequiresConfirmation {
		t.Fatalf("ghost on primary: %+v %v", ghost, err)
	}
	// 组不存在：不适用。
	if b := prepare(t, s, seed("z", "z1", "elsewhere", "Other Album", "Someone")).Baseline; b.Coverage != domain.CoverageNotApplicable || b.NeedsTypedConfirm {
		t.Fatalf("no group: %+v", b)
	}
}

func TestTypedConfirmationGate(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	plan := prepare(t, s, songSeed("b", "b1", "r2", "group"))
	ctx := context.Background()

	if _, err := s.CommitAdmission(ctx, plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("committed without approval", err)
	}
	if _, err := s.ConfirmReleaseAdditions(plan.ID, plan.Hash); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("membership approval issued", err)
	}
	// 伪造：用普通成员确认的签名冒充，或把输入确认凭证标成普通确认，存储层都拒绝。
	membership := domain.AdmissionApproval{PlanID: plan.ID, Hash: plan.Hash, Credential: s.sign("confirmed", plan.ID, plan.Hash)}
	for _, forged := range []domain.AdmissionApproval{
		membership,
		{PlanID: plan.ID, Hash: plan.Hash, Kind: domain.ApprovalTyped, Credential: membership.Credential},
		{PlanID: plan.ID, Hash: plan.Hash, Credential: s.sign("typed", plan.ID, plan.Hash)},
	} {
		if _, err := st.CommitAdmission(*plan, forged); !errors.Is(err, store.ErrTypedConfirmationRequired) {
			t.Fatalf("forged approval accepted: %+v %v", forged, err)
		}
	}
	for _, word := range []string{"yes", "YES", "y", "", "Yes."} {
		if _, err := s.ConfirmTypedAdmission(plan.ID, plan.Hash, word); err == nil {
			t.Fatalf("word %q accepted", word)
		}
	}
	approval, err := s.ConfirmTypedAdmission(plan.ID, plan.Hash, "  Yes\n")
	if err != nil {
		t.Fatal(err)
	}
	// 凭证只对这份计划有效。
	other := prepare(t, s, songSeed("c", "c1", "r2", "group"))
	if _, err := s.CommitAdmission(ctx, other.ID, other.Hash, *approval); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("approval reused", err)
	}
	result, err := s.CommitAdmission(ctx, plan.ID, plan.Hash, *approval)
	if err != nil || result.ReleaseID == a.ReleaseID || result.GroupID != a.GroupID {
		t.Fatal(result, err)
	}
	// 留痕与并存记录。
	if count(t, st, `SELECT count(*) FROM release_membership_decision WHERE plan_id=?`, plan.ID) != 1 ||
		count(t, st, `SELECT count(*) FROM release_coexistence WHERE release_a=? AND release_b=?`, a.ReleaseID, result.ReleaseID) != 1 {
		t.Fatal("decision or coexistence not recorded")
	}
	// 不需要输入确认的计划不签发这种凭证。
	plain := prepare(t, s, songSeed("b", "b9", "r9", "group"))
	if _, err := s.ConfirmTypedAdmission(plain.ID, plain.Hash, "Yes"); err == nil {
		t.Fatal("typed approval issued for a plan that does not need it")
	}
}

func TestCoverageChangeInvalidatesPlan(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"})
	// 生成计划时已证明独有，不需要确认。
	plan := prepare(t, s, songSeed("b", "b1", "r2", "group"))
	if plan.Baseline.Coverage != domain.CoverageAbsentProven || plan.Baseline.RequiresConfirmation {
		t.Fatalf("%+v", plan.Baseline)
	}
	// 之后已有发行的清单变了，含这首歌：提交必须失效，而不是照旧新建发行。
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	if _, err := s.CommitAdmission(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("stale coverage committed", err)
	}
	if count(t, st, `SELECT count(*) FROM release`) != 1 {
		t.Fatal("release created from a stale plan")
	}
}

func TestPrimaryReleaseOrderingGate(t *testing.T) {
	s, st, _ := newTestService(t)
	// A 有两条曲目引用，B 一条；两者都含录音 r5。
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	commit(t, s, prepare(t, s, songSeed("a", "a2", "r2", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"}, [2]string{"a5", "r5"})
	b := commit(t, s, prepare(t, s, songSeed("b", "b1", "r9", "group")), false)
	loadTracklist(t, st, b.ReleaseID, "b", [2]string{"b1", "r9"}, [2]string{"b5", "r5"})

	onPrimary, err := s.PrepareGhostAdmission(context.Background(), a.ReleaseID, 1, 3)
	if err != nil || onPrimary.Baseline.Coverage != domain.CoverageProvided || onPrimary.Baseline.NeedsTypedConfirm {
		t.Fatalf("primary release gated: %+v %v", onPrimary, err)
	}
	onOther, err := s.PrepareGhostAdmission(context.Background(), b.ReleaseID, 1, 2)
	if err != nil || !onOther.Baseline.NeedsTypedConfirm || onOther.Baseline.Providers[0].ReleaseID != a.ReleaseID {
		t.Fatalf("non-primary release not gated: %+v %v", onOther, err)
	}
}

func TestUseSongProvider(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	batch, err := st.CreateBatch(&store.ImportBatch{TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.EnqueueItems(batch, []store.ImportItem{{RawRow: `[]`, InTitle: ptrField(domain.Known("Song r2")), InArtist: ptrField(domain.Known("Artist")), InAlbum: ptrField(domain.Known("Album"))}}); err != nil {
		t.Fatal(err)
	}
	item, err := st.ClaimNextItem(batch)
	if err != nil || item == nil {
		t.Fatal(item, err)
	}
	chosen := songSeed("b", "b1", "r2", "group")
	chosen.ImportItemID, chosen.ItemStatus = item.ID, "imported"
	plan := prepare(t, s, chosen)
	if !plan.Baseline.NeedsTypedConfirm {
		t.Fatalf("%+v", plan.Baseline)
	}
	if _, err := s.UseSongProvider(context.Background(), plan.ID, plan.Hash, 987654); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("unknown provider release accepted", err)
	}
	next, err := s.UseSongProvider(context.Background(), plan.ID, plan.Hash, a.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	if next.Seed.Snapshot.Track.Identity.Ref != "a2" || next.Seed.ImportItemID != item.ID || next.Baseline.NeedsTypedConfirm || next.Baseline.RequiresConfirmation || next.Baseline.ExistingReleaseID != a.ReleaseID {
		t.Fatalf("replanned: %+v %+v", next.Seed, next.Baseline)
	}
	result, err := s.CommitAdmission(context.Background(), next.ID, next.Hash, domain.AdmissionApproval{})
	if err != nil || result.ReleaseID != a.ReleaseID {
		t.Fatal(result, err)
	}
	// 导入条目终态与投递正确；没有新建发行 B，也没有并存记录。
	got, err := st.GetItem(item.ID)
	if err != nil || got.Status != store.StatusImported || got.TrackID == nil || *got.TrackID != result.TrackID {
		t.Fatal(got, err)
	}
	if count(t, st, `SELECT count(*) FROM release`) != 1 || count(t, st, `SELECT count(*) FROM release_coexistence`) != 0 {
		t.Fatal("release B or coexistence created")
	}
}

func TestLocalReleaseParticipatesAtTextLevel(t *testing.T) {
	s, st, _ := newTestService(t)
	// 没有上游身份的本地发行：用它的曲目行当清单，按文本级参与。
	local := domain.AdmissionSeed{Snapshot: domain.Snapshot{
		Release: &domain.ReleaseSnapshot{Title: domain.Known("Album"), Credit: domain.Known(domain.Credit{Text: "Artist"})},
		Track:   &domain.TrackSnapshot{Title: domain.Known("Song r2"), Credit: domain.Known(domain.Credit{Text: "Artist"})},
	}}
	owned := commit(t, s, prepare(t, s, local), false)
	plan := prepare(t, s, songSeed("b", "b1", "r2", ""))
	b := plan.Baseline
	if b.Coverage != domain.CoveragePossible || !b.NeedsTypedConfirm || len(b.Providers) != 1 || b.Providers[0].TrackID != owned.TrackID || b.Providers[0].Identity != nil {
		t.Fatalf("local release: %+v", b)
	}
	// 「使用发行 A」：提供者是本地已有曲目时等同于复用该曲目。
	next, err := s.UseSongProvider(context.Background(), plan.ID, plan.Hash, owned.ReleaseID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.CommitAdmission(context.Background(), next.ID, next.Hash, domain.AdmissionApproval{})
	if err != nil || !result.Existing || result.TrackID != owned.TrackID {
		t.Fatal(result, err)
	}
	if count(t, st, `SELECT count(*) FROM track`) != 1 || count(t, st, `SELECT count(*) FROM release`) != 1 {
		t.Fatal("reuse created rows")
	}
}

func TestCrossSourceReleaseIsTextLevel(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	other := func(title string) domain.AdmissionSeed {
		r := &domain.ReleaseSnapshot{Identity: domain.Identity{Provider: "other", Ref: "o"}, Title: domain.Known("Album"), Credit: domain.Known(domain.Credit{Text: "Artist"})}
		return domain.AdmissionSeed{Snapshot: domain.Snapshot{Release: r, Track: &domain.TrackSnapshot{Identity: domain.Identity{Provider: "other", Ref: "o1"}, Release: &r.Identity,
			Title: domain.Known(title), Recording: domain.Known(domain.Identity{Provider: "other", Ref: "r2"})}}}
	}
	// 另一个来源的相同标识数值不构成录音级证据：命中只算疑似，未命中只有文本级结论。
	if b := prepare(t, s, other("Song r2")).Baseline; b.Coverage != domain.CoveragePossible || !b.NeedsTypedConfirm {
		t.Fatalf("cross-source hit: %+v", b)
	}
	if b := prepare(t, s, other("Unrelated")).Baseline; b.Coverage != domain.CoverageAbsentText || b.NeedsTypedConfirm || !b.RequiresConfirmation {
		t.Fatalf("cross-source miss: %+v", b)
	}
}

func TestUnknownCoverageRefreshAndCoexistence(t *testing.T) {
	s, st, _ := newTestService(t)
	p := &summaryProvider{details: map[string]*domain.Snapshot{}}
	s.registry.Register(p)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	plan := prepare(t, s, songSeed("b", "b1", "r2", "group"))
	if plan.Baseline.Coverage != domain.CoverageUnknown {
		t.Fatalf("%+v", plan.Baseline)
	}
	// 获取清单并重新预览：取不到时仍不可判定。
	again, err := s.RefreshAdmissionCoverage(context.Background(), plan.ID, plan.Hash)
	if err != nil || again.Baseline.Coverage != domain.CoverageUnknown || p.detailCalls != 1 {
		t.Fatalf("refresh without tracklist: %+v %v", again, err)
	}
	// 用户输入 Yes 创建 B：对不可判定的发行与 B 写并存记录。
	approval, err := s.ConfirmTypedAdmission(again.ID, again.Hash, "Yes")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CommitAdmission(context.Background(), again.ID, again.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	if count(t, st, `SELECT count(*) FROM release_coexistence WHERE release_a=? AND release_b=?`, a.ReleaseID, b.ReleaseID) != 1 {
		t.Fatal("coexistence missing for an undecidable release")
	}
	// 之后清单到达且两者曲目相同：不生成归并提议。
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	loadTracklist(t, st, b.ReleaseID, "b", [2]string{"b2", "r2"}, [2]string{"b1", "r1"})
	if proposals, err := s.ListReleaseMergeProposals(); err != nil || len(proposals) != 0 {
		t.Fatal("confirmed pair proposed for merge", proposals, err)
	}
	// 取到清单后结论变为可判定。
	p.details["a"] = releaseDetail("a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	t.Run("refresh resolves", func(t *testing.T) {
		s, st, _ := newTestService(t)
		s.registry.Register(p)
		commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
		plan := prepare(t, s, songSeed("b", "b9", "r9", "group"))
		next, err := s.RefreshAdmissionCoverage(context.Background(), plan.ID, plan.Hash)
		if err != nil || next.Baseline.Coverage != domain.CoverageAbsentProven || next.Baseline.NeedsTypedConfirm {
			t.Fatalf("%+v %v", next, err)
		}
		if count(t, st, `SELECT count(*) FROM release_track`) != 2 {
			t.Fatal("tracklist not saved")
		}
	})
}

func TestReleaseMergeProposals(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	// B 的独有歌曲让它自动成立；此时两者清单不同，没有提议。
	b := commit(t, s, prepare(t, s, songSeed("b", "b3", "r3", "group")), false)
	loadTracklist(t, st, b.ReleaseID, "b", [2]string{"b3", "r3"})
	if proposals, _ := s.ListReleaseMergeProposals(); len(proposals) != 0 {
		t.Fatal("different tracklists proposed", proposals)
	}
	// 清单后到：两发行录音多重集相同（曲序不同）→ 一条提议；重复同步不重复生成。
	loadTracklist(t, st, b.ReleaseID, "b", [2]string{"b2", "r2"}, [2]string{"b1", "r1"})
	loadTracklist(t, st, b.ReleaseID, "b", [2]string{"b2", "r2"}, [2]string{"b1", "r1"})
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	proposals, err := s.ListReleaseMergeProposals()
	if err != nil || len(proposals) != 1 || proposals[0].A.ReleaseID != a.ReleaseID || proposals[0].B.ReleaseID != b.ReleaseID || proposals[0].TrackTotal != 2 {
		t.Fatal(proposals, err)
	}
	// 不自动改任何关系。
	if count(t, st, `SELECT count(*) FROM release`) != 2 || count(t, st, `SELECT count(*) FROM track WHERE release_id=?`, b.ReleaseID) != 1 {
		t.Fatal("proposal changed relations")
	}
	// 忽略后证据不变不再生成。
	if err := s.DismissReleaseMergeProposal(proposals[0].ID); err != nil {
		t.Fatal(err)
	}
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	if proposals, _ := s.ListReleaseMergeProposals(); len(proposals) != 0 {
		t.Fatal("dismissed proposal resurfaced", proposals)
	}
	if err := s.DismissReleaseMergeProposal(proposals[0].ID); !errors.Is(err, store.ErrPlanStale) {
		t.Fatal("dismissed twice", err)
	}
}

func TestGroupProposalMoveIsNotGated(t *testing.T) {
	s, st, _ := newTestService(t)
	// 沿用迟到事实的夹具：发行 a 起初署名缺失，补全后名称命中另一组，生成归组提议。
	firstSeed := songSeed("a", "a1", "r1", "")
	firstSeed.Snapshot.Release.Credit = domain.Missing[domain.Credit]()
	first := commit(t, s, prepare(t, s, firstSeed), false)
	second := commit(t, s, prepare(t, s, songSeed("b", "b1", "r1", "")), false)
	late := firstSeed.Snapshot
	release := *late.Release
	late.Release = &release
	late.Release.Credit = domain.Known(domain.Credit{Text: "Artist"})
	revision, _ := st.TrackRevision(first.TrackID)
	if err := st.ApplySnapshot(first.TrackID, &late, revision); err != nil {
		t.Fatal(err)
	}
	proposals, err := s.ListGroupProposals()
	if err != nil || len(proposals) != 1 {
		t.Fatal(proposals, err)
	}
	// 整个发行换组：没有单首歌的归属变化，覆盖结论不适用，不要求输入确认——
	// 即使目标组里已有发行含同一录音。
	plan, err := s.PrepareGroupProposal(context.Background(), proposals[0].ID, second.GroupID)
	if err != nil || plan.Baseline.Coverage != domain.CoverageNotApplicable || plan.Baseline.NeedsTypedConfirm {
		t.Fatalf("%+v %v", plan, err)
	}
}

func TestEditorMigrationIsGated(t *testing.T) {
	s, st, _ := newTestService(t)
	a := commit(t, s, prepare(t, s, songSeed("a", "a1", "r1", "group")), false)
	loadTracklist(t, st, a.ReleaseID, "a", [2]string{"a1", "r1"}, [2]string{"a2", "r2"})
	// 被编辑的曲目在发行 C 上（录音 r2，A 也含它）；C 另有一首歌，迁走后仍有引用。
	typed := prepare(t, s, songSeed("c", "c2", "r2", "group"))
	approval, err := s.ConfirmTypedAdmission(typed.ID, typed.Hash, "Yes")
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.CommitAdmission(context.Background(), typed.ID, typed.Hash, *approval)
	if err != nil {
		t.Fatal(err)
	}
	commit(t, s, prepare(t, s, songSeed("c", "c9", "r9", "group")), false)
	loadTracklist(t, st, edited.ReleaseID, "c", [2]string{"c2", "r2"}, [2]string{"c9", "r9"})
	before, err := st.ReadEditorSession(edited.TrackID)
	if err != nil {
		t.Fatal(err)
	}

	// 只改字段、不换发行：不要求输入确认。
	fieldOnly := domain.EditDraft{TrackID: edited.TrackID, Revisions: before.Revisions, Fields: domain.EditFields{"track.genre": {State: domain.Value, ValueJSON: `"Pop"`}}}
	plan, err := s.PlanEditSave(context.Background(), fieldOnly)
	if err != nil || plan.Baseline.NeedsTypedConfirm || plan.Baseline.Coverage != domain.CoverageNotApplicable {
		t.Fatalf("field-only edit gated: %+v %v", plan, err)
	}

	// 把曲目改到新的专辑信息（同组新建发行）：组内已有发行 A 含同一录音 → 要求输入确认。
	draft := domain.EditDraft{TrackID: edited.TrackID, Revisions: before.Revisions, AlbumAction: "create",
		Fields:   domain.EditFields{"release.primaryType": {State: domain.Value, ValueJSON: `"single"`}},
		Mappings: []domain.EditMapping{{TrackID: edited.TrackID, Selected: true}}}
	impact, err := s.PreviewEditSave(context.Background(), draft)
	if err != nil || impact.Admission == nil || !impact.Admission.NeedsTypedConfirm || impact.Admission.Coverage != domain.CoverageProvided {
		t.Fatalf("editor preview: %+v %v", impact, err)
	}
	plan, err = s.PlanEditSave(context.Background(), draft)
	if err != nil || !plan.Baseline.NeedsTypedConfirm {
		t.Fatalf("editor plan: %+v %v", plan, err)
	}
	if _, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, domain.AdmissionApproval{}); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("editor bypassed the gate", err)
	}
	membership := domain.AdmissionApproval{PlanID: plan.ID, Hash: plan.Hash, Credential: s.sign("confirmed", plan.ID, plan.Hash)}
	if _, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, membership); !errors.Is(err, store.ErrTypedConfirmationRequired) {
		t.Fatal("editor accepted a membership approval", err)
	}
	ok, err := s.ConfirmTypedAdmission(plan.ID, plan.Hash, "Yes")
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.SaveEdit(context.Background(), plan.ID, plan.Hash, *ok)
	if err != nil || result.ReleaseID == edited.ReleaseID {
		t.Fatal(result, err)
	}
	if count(t, st, `SELECT count(*) FROM release_coexistence WHERE release_a=? OR release_b=?`, result.ReleaseID, result.ReleaseID) == 0 {
		t.Fatal("editor confirmation left no coexistence record")
	}
}
