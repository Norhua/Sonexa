package app

// 本文件与 wiring_test.go 同在 app 包：测的是「接受候选」与后台补全任务
// 在真实生产入口上的行为，而不是零件各自能不能用。

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

// candidateRow 是一条带完整身份的 MusicBrainz 候选，形如搜索响应展开后的那种。
func candidateRow() CandidateRow {
	title, artist, album := "God knows...", "平野綾", "涼宮ハルヒの詰合"
	release, group, recording := "rel-haruhi", "rg-haruhi", "rec-haruhi"
	return CandidateRow{
		Provider: "musicbrainz", MatchRef: "trk-haruhi",
		Title: &title, Artist: &artist, Album: &album,
		ReleaseRef: &release, ReleaseGroupRef: &group, RecordingRef: &recording,
	}
}

// TestAcceptCandidateIssuesNoRequest 覆盖用户报告 2：
// 复核队列点「接受」时不得发任何出网请求——曲目立刻入库，详情交给后台。
//
// 先前这里同步补一次详情，在 1 req/s 之下就是 1–2 秒的卡顿，
// 而那次请求的产出与「这条曲目能不能进库」无关。
func TestAcceptCandidateIssuesNoRequest(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// 任何出网都算失败：把 HTTP 客户端换成一个必然报错并计数的传输层
	var requests atomic.Int32
	app.client.HTTP.Transport = countingTransport{&requests}

	itemID := enqueueOneItem(t, app, "God knows...", "平野綾", "涼宮ハルヒの詰合")

	start := time.Now()
	trackID, err := app.AcceptCandidate(itemID, candidateRow())
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("接受候选失败: %v", err)
	}
	if trackID == 0 {
		t.Fatal("接受后应得到曲目 id")
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("接受候选发了 %d 次请求, 期望 0 次——详情该由后台补", n)
	}
	// 1 req/s 的限流下，同步补一次详情至少要一秒
	if elapsed > 500*time.Millisecond {
		t.Errorf("接受候选耗时 %v, 期望立即返回", elapsed)
	}

	// 状态是「未匹配（待补全）」——这个中间态是如实的，不是丢了信息
	assertEnrichState(t, app, trackID, store.EnrichPending)

	targets, err := app.store.TracksNeedingEnrichment(0)
	if err != nil {
		t.Fatalf("查询工作集失败: %v", err)
	}
	if len(targets) != 1 || targets[0].TrackID != trackID {
		t.Fatalf("工作集 = %+v, 期望恰好含刚接受的那条", targets)
	}
	if targets[0].ReleaseRef != "rel-haruhi" || targets[0].Provider != "musicbrainz" {
		t.Errorf("工作集缺少取详情所需的来源: %+v", targets[0])
	}
}

// TestNoMatchNeededNeedsNoEnrichment 覆盖「无需匹配」那一支：
// 它没有上游身份，不该出现在补全任务的工作集里。
func TestNoMatchNeededNeedsNoEnrichment(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	itemID := enqueueOneItem(t, app, "自制曲", "我", "我的专辑")
	trackID, err := app.NoMatchNeeded(itemID)
	if err != nil {
		t.Fatalf("无需匹配入库失败: %v", err)
	}

	assertEnrichState(t, app, trackID, store.EnrichSkipped)

	counts, err := app.store.EnrichStats()
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if counts.Skipped != 1 || counts.Pending != 0 {
		t.Errorf("计数 = %+v, 期望 1 条无需匹配、0 条待补", counts)
	}
	// 界面上的分母是「已匹配 + 未匹配」：无需匹配的曲目不该把它顶起来，
	// 否则那个数字永远到不了头
	if s := app.enricher.Status(); s.Total != 0 {
		t.Errorf("分母 = %d, 期望 0——无需匹配的曲目不参与补全计数", s.Total)
	}
}

// TestEnrichWorkerDrainsPendingTracks 覆盖后台补全任务：
// 它把待补条目逐条补完，并把状态推到「已匹配」。
func TestEnrichWorkerDrainsPendingTracks(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// 造三条待补的曲目
	var ids []int64
	for i := 0; i < 3; i++ {
		ids = append(ids, createPendingTrack(t, app, fmt.Sprintf("曲目 %d", i)))
	}

	// 把取详情换成一个不出网的记账函数（真实取数在 provider 各自的测试里）
	var enriched atomic.Int32
	app.enricher.enrich = func(_ context.Context, target store.EnrichTarget) error {
		if target.ReleaseRef == "" {
			return fmt.Errorf("工作集里的条目缺少发行标识")
		}
		enriched.Add(1)
		return nil
	}

	if err := app.StartEnrich(); err != nil {
		t.Fatalf("启动补全失败: %v", err)
	}
	waitFor(t, "补全跑完", func() bool { return app.enricher.Status().State == enrichIdle })

	if got := enriched.Load(); got != 3 {
		t.Errorf("补了 %d 条, 期望 3 条", got)
	}
	for _, id := range ids {
		assertEnrichState(t, app, id, store.EnrichFetched)
	}
	if s := app.enricher.Status(); s.Done != 3 || s.Total != 3 {
		t.Errorf("进度 = %d/%d, 期望 3/3", s.Done, s.Total)
	}
}

// TestEnrichWorkerYieldsWhileImportRuns 覆盖「导入/搜索进行中自动暂停」：
// 后台补全与导入匹配打的是同一个 1 req/s 的上游，同时跑等于把导入砍半。
func TestEnrichWorkerYieldsWhileImportRuns(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	createPendingTrack(t, app, "等一等")

	var enriched atomic.Int32
	app.enricher.enrich = func(_ context.Context, _ store.EnrichTarget) error {
		enriched.Add(1)
		return nil
	}

	// 用交互式计数器模拟「用户正在检索」——outboundBusy 的两个来源之一
	release := app.beginInteractive()

	if err := app.StartEnrich(); err != nil {
		t.Fatalf("启动补全失败: %v", err)
	}
	waitFor(t, "任务进入让路态", func() bool { return app.enricher.Status().State == enrichWaiting })

	if got := enriched.Load(); got != 0 {
		t.Fatalf("让路期间补了 %d 条, 期望 0 条", got)
	}
	if reason := app.enricher.Status().Reason; reason == "" {
		t.Error("让路时应说明在等什么")
	}

	// 用户那边结束后自动接着跑
	release()
	waitFor(t, "让路结束后继续", func() bool { return enriched.Load() == 1 })
}

// TestPauseEnrichBlocksAutoStart 钉住「暂停是用户的决定」：
// 暂停之后即使又有新条目入库、又空闲下来，任务也不自动开始。
func TestPauseEnrichBlocksAutoStart(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	var enriched atomic.Int32
	app.enricher.enrich = func(_ context.Context, _ store.EnrichTarget) error {
		enriched.Add(1)
		return nil
	}

	if err := app.PauseEnrich(); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	createPendingTrack(t, app, "暂停期间入库")
	app.autoStartEnrich()

	// 给它一点时间证明自己没有偷跑
	time.Sleep(2 * enrichLetPassInterval)
	if got := enriched.Load(); got != 0 {
		t.Errorf("暂停期间补了 %d 条, 期望 0 条", got)
	}

	// 用户点「开始」后照常跑
	if err := app.StartEnrich(); err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	waitFor(t, "恢复后补完", func() bool { return enriched.Load() == 1 })
}

// TestEnrichFailureKeepsTrackAndCountsAttempt 覆盖补全失败的处置：
// 曲目留在库里、状态回到待补、重试计数累加，用户的备注不被冲掉。
func TestEnrichFailureKeepsTrackAndCountsAttempt(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	trackID := createPendingTrack(t, app, "取不到详情的曲目")
	if err := app.store.RecordEnrichFailure(trackID, "上游返回 503"); err != nil {
		t.Fatalf("记录失败原因失败: %v", err)
	}

	assertEnrichState(t, app, trackID, store.EnrichPending)
	if track, err := app.store.GetTrack(trackID); err != nil || track == nil {
		t.Fatal("补全失败不该影响曲目本身")
	}

	// 达到上限后退出工作集，否则它会在每一轮里都排在队首
	for i := 1; i < store.MaxEnrichAttempts; i++ {
		if err := app.store.RecordEnrichFailure(trackID, "上游返回 503"); err != nil {
			t.Fatalf("记录失败原因失败: %v", err)
		}
	}
	targets, err := app.store.TracksNeedingEnrichment(0)
	if err != nil {
		t.Fatalf("查询工作集失败: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("工作集含 %d 条, 期望达到重试上限的条目退出", len(targets))
	}

	// 用户点「开始」是在说「再试一次」：计数清零，那一条回到工作集
	if err := app.store.ResetEnrichAttempts(); err != nil {
		t.Fatalf("清零失败: %v", err)
	}
	targets, err = app.store.TracksNeedingEnrichment(0)
	if err != nil {
		t.Fatalf("查询工作集失败: %v", err)
	}
	if len(targets) != 1 {
		t.Errorf("清零后工作集含 %d 条, 期望 1 条", len(targets))
	}
}

// --- 辅助 ---

// countingTransport 计数并拒绝一切出网，使「不该发请求」变成硬失败。
type countingTransport struct{ n *atomic.Int32 }

func (c countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.n.Add(1)
	return nil, fmt.Errorf("测试中不应发出网络请求")
}

// enqueueOneItem 造一个只有一行的导入批次，返回那一行的条目 id。
func enqueueOneItem(t *testing.T, app *App, title, artist, album string) int64 {
	t.Helper()
	batchID, err := app.store.CreateBatch(&store.ImportBatch{Filename: "test.csv", TotalRows: 1})
	if err != nil {
		t.Fatalf("建批次失败: %v", err)
	}
	if err := app.store.EnqueueItems(batchID, []store.ImportItem{{
		RowIndex: 0, RawRow: "{}",
		InTitle: &title, InArtist: &artist, InAlbum: &album,
	}}); err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	items, err := app.store.AllReviewItems()
	if err != nil || len(items) == 0 {
		// 刚入队的条目状态是排队中而不是待复核，取批次里的第一条
		queued, qerr := app.store.ClaimNextItem(batchID)
		if qerr != nil || queued == nil {
			t.Fatalf("取不到刚入队的条目: %v / %v", err, qerr)
		}
		return queued.ID
	}
	return items[0].ID
}

// createPendingTrack 造一条「有上游身份、详情待补」的曲目。
//
// 发行标识现在落在**歌集**上：待补判定读的是所属 release 的 provider_ref，
// 因此这里必须连歌集一起建出来，否则状态会是「已匹配（无可补）」。
func createPendingTrack(t *testing.T, app *App, title string) int64 {
	t.Helper()
	source, ref := "musicbrainz", "trk-"+title
	release := "rel-" + title
	album := "专辑 " + title
	id, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: title, MatchSource: &source, MatchRef: &ref, Album: &album,
		Release: &store.ReleaseInput{Provider: &source, ProviderRef: &release},
	})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}
	return id
}

func assertEnrichState(t *testing.T, app *App, trackID int64, want store.EnrichState) {
	t.Helper()
	got, err := app.store.EnrichStateOf(trackID)
	if err != nil {
		t.Fatalf("读补全状态失败: %v", err)
	}
	if got != want {
		t.Errorf("曲目 %d 的补全状态 = %q, 期望 %q", trackID, got, want)
	}
}

// waitFor 轮询等待某个条件成立，超时即失败。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}
