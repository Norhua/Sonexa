package store

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func seedBatch(t *testing.T, s *Store, rows int) int64 {
	t.Helper()
	batchID, err := s.CreateBatch(&ImportBatch{Filename: "music_list.csv", TotalRows: rows})
	if err != nil {
		t.Fatalf("建立批次失败: %v", err)
	}

	items := make([]ImportItem, rows)
	for i := range items {
		raw, _ := json.Marshal([]string{"曲目" + itoa(i), "艺术家", "专辑"})
		items[i] = ImportItem{
			RowIndex: i,
			RawRow:   string(raw),
			InTitle:  ptr("曲目" + itoa(i)),
			InArtist: ptr("艺术家"),
		}
	}
	if err := s.EnqueueItems(batchID, items); err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	return batchID
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

// TestRawRowNeverModified 覆盖 11.1：
// raw_row 存原始整行且永不修改——复核队列改搜后原值不变。
func TestRawRowNeverModified(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 1)

	item, err := s.ClaimNextItem(batchID)
	if err != nil || item == nil {
		t.Fatalf("取出条目失败: %v", err)
	}
	original := item.RawRow

	// 模拟用户在复核队列改写搜索条件后重新搜索、并最终进入复核
	if err := s.MarkReview(item.ID, `[{"ref":"x"}]`, `["改写后的曲名"]`); err != nil {
		t.Fatalf("置为待复核失败: %v", err)
	}

	after, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("读取条目失败: %v", err)
	}
	if after.RawRow != original {
		t.Errorf("原始行被改写: %q → %q", original, after.RawRow)
	}
	// 改写只影响本次查询的记录，不动原始导入值
	if after.InTitle == nil || *after.InTitle != "曲目0" {
		t.Errorf("原始导入值被改写: %v", after.InTitle)
	}
}

// TestStateMachineTransitions 覆盖 11.2 的状态流转。
func TestStateMachineTransitions(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 6)

	// queued → matching
	item, _ := s.ClaimNextItem(batchID)
	if item.Status != StatusMatching {
		t.Errorf("取出后状态 = %q, 期望 matching", item.Status)
	}

	// matching → imported
	trackID, _ := s.CreateTrack(&Track{Title: "入库曲目"})
	if err := s.MarkImported(item.ID, trackID); err != nil {
		t.Fatalf("置为已入库失败: %v", err)
	}
	got, _ := s.GetItem(item.ID)
	if got.Status != StatusImported {
		t.Errorf("状态 = %q, 期望 imported", got.Status)
	}
	if got.TrackID == nil || *got.TrackID != trackID {
		t.Errorf("曲目 id = %v, 期望 %d", got.TrackID, trackID)
	}

	// matching → review
	item2, _ := s.ClaimNextItem(batchID)
	s.MarkReview(item2.ID, `[]`, `["变体A"]`)
	got2, _ := s.GetItem(item2.ID)
	if got2.Status != StatusReview {
		t.Errorf("状态 = %q, 期望 review", got2.Status)
	}

	// → merged（不新建曲目）
	item3, _ := s.ClaimNextItem(batchID)
	s.MarkMerged(item3.ID, trackID)
	got3, _ := s.GetItem(item3.ID)
	if got3.Status != StatusMerged {
		t.Errorf("状态 = %q, 期望 merged", got3.Status)
	}
	if got3.MergedInto == nil || *got3.MergedInto != trackID {
		t.Errorf("合并目标 = %v", got3.MergedInto)
	}

	// → discarded
	item4, _ := s.ClaimNextItem(batchID)
	s.MarkDiscarded(item4.ID)
	got4, _ := s.GetItem(item4.ID)
	if got4.Status != StatusDiscarded {
		t.Errorf("状态 = %q, 期望 discarded", got4.Status)
	}

	// → skipped
	item5, _ := s.ClaimNextItem(batchID)
	s.MarkSkipped(item5.ID)
	got5, _ := s.GetItem(item5.ID)
	if got5.Status != StatusSkipped {
		t.Errorf("状态 = %q, 期望 skipped", got5.Status)
	}

	// 终态判定
	for _, st := range []ItemStatus{StatusImported, StatusSkipped, StatusMerged, StatusDiscarded} {
		if !st.IsTerminal() {
			t.Errorf("%q 应为终态", st)
		}
	}
	for _, st := range []ItemStatus{StatusQueued, StatusMatching, StatusReview, StatusFailed} {
		if st.IsTerminal() {
			t.Errorf("%q 不应为终态", st)
		}
	}
}

// TestBackoffRequeueUntilLimit 覆盖 11.7：
// 累计 5 次失败后标记失败状态并停止自动重排。
func TestBackoffRequeueUntilLimit(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 1)
	item, _ := s.ClaimNextItem(batchID)

	const maxAttempts = 5
	// 前 4 次：退避重排
	for attempt := 1; attempt <= 4; attempt++ {
		status, err := s.RecordFailure(item.ID, "503 Service Unavailable", time.Millisecond, maxAttempts)
		if err != nil {
			t.Fatalf("第 %d 次记录失败出错: %v", attempt, err)
		}
		if status != StatusQueued {
			t.Errorf("第 %d 次失败后状态 = %q, 期望回到 queued", attempt, status)
		}
		got, _ := s.GetItem(item.ID)
		if got.AttemptCount != attempt {
			t.Errorf("失败计数 = %d, 期望 %d", got.AttemptCount, attempt)
		}
		if got.NextRetryAt == nil {
			t.Error("应设置下次重试时间")
		}
		if got.LastError == nil || *got.LastError != "503 Service Unavailable" {
			t.Errorf("最后错误 = %v", got.LastError)
		}
		// 重新取出以继续下一轮
		s.ClaimNextItem(batchID)
	}

	// 第 5 次：标记失败，不再自动重排
	status, err := s.RecordFailure(item.ID, "503 Service Unavailable", time.Millisecond, maxAttempts)
	if err != nil {
		t.Fatalf("第 5 次记录失败出错: %v", err)
	}
	if status != StatusFailed {
		t.Errorf("第 5 次失败后状态 = %q, 期望 failed", status)
	}

	got, _ := s.GetItem(item.ID)
	if got.AttemptCount != 5 {
		t.Errorf("失败计数 = %d, 期望 5", got.AttemptCount)
	}
	if got.NextRetryAt != nil {
		t.Error("失败条目不应再有下次重试时间——不再自动重排")
	}
	// 该条目不再被队列取出
	if next, _ := s.ClaimNextItem(batchID); next != nil {
		t.Errorf("失败条目不应再返回队列, 却取出了 %d", next.ID)
	}
}

// TestManualRetryRequeues 覆盖 11.7 与 18.8：
// 失败条目可由「立即重试」手动重排。
func TestManualRetryRequeues(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 1)
	item, _ := s.ClaimNextItem(batchID)
	s.RecordFailure(item.ID, "503", time.Millisecond, 1) // 立刻达到上限

	if got, _ := s.GetItem(item.ID); got.Status != StatusFailed {
		t.Fatalf("前置条件不成立, 状态 = %q", got.Status)
	}

	if err := s.RetryItem(item.ID); err != nil {
		t.Fatalf("手动重试失败: %v", err)
	}
	got, _ := s.GetItem(item.ID)
	if got.Status != StatusQueued {
		t.Errorf("重试后状态 = %q, 期望 queued", got.Status)
	}
	// 重新可被取出
	if next, _ := s.ClaimNextItem(batchID); next == nil {
		t.Error("手动重试后应能被队列取出")
	}
}

// TestResumeAfterRestart 覆盖 11.8：
// 中途关闭应用再打开时从中断处继续，已完成条目不重复处理。
func TestResumeAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resume.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	batchID := seedBatch(t, s1, 5)

	// 处理两条：一条入库、一条正在匹配时被"关闭应用"打断
	first, _ := s1.ClaimNextItem(batchID)
	trackID, _ := s1.CreateTrack(&Track{Title: "已完成"})
	s1.MarkImported(first.ID, trackID)

	interrupted, _ := s1.ClaimNextItem(batchID)
	if interrupted.Status != StatusMatching {
		t.Fatalf("前置条件不成立: %q", interrupted.Status)
	}
	s1.Close() // 模拟关闭应用

	// 重新打开
	s2, err := Open(path)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	defer s2.Close()

	// 中断时停留在 matching 的条目退回排队
	if err := s2.ResetStaleMatching(); err != nil {
		t.Fatalf("重置中断条目失败: %v", err)
	}

	stats, err := s2.Stats(batchID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Imported != 1 {
		t.Errorf("已入库 = %d, 期望 1（已完成条目不重复处理）", stats.Imported)
	}
	if stats.Matching != 0 {
		t.Errorf("匹配中 = %d, 期望 0（中断条目应退回排队）", stats.Matching)
	}
	if stats.Queued != 4 {
		t.Errorf("排队中 = %d, 期望 4（含被中断的那条）", stats.Queued)
	}

	// 从中断处继续：下一条取出的不是已完成的那条
	next, _ := s2.ClaimNextItem(batchID)
	if next == nil {
		t.Fatal("应能继续取出条目")
	}
	if next.ID == first.ID {
		t.Error("已完成的条目不应被重复处理")
	}
}

// TestPauseAndResume 覆盖 11.8 的暂停与恢复。
func TestPauseAndResume(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 3)

	if err := s.SetBatchStatus(batchID, BatchPaused); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	b, _ := s.GetBatch(batchID)
	if b.Status != BatchPaused {
		t.Errorf("状态 = %q, 期望 paused", b.Status)
	}

	// 恢复后从暂停处继续，不从头开始
	s.SetBatchStatus(batchID, BatchRunning)
	stats, _ := s.Stats(batchID)
	if stats.Queued != 3 {
		t.Errorf("恢复后排队 = %d, 期望 3", stats.Queued)
	}

	// 完成时记录结束时间
	s.SetBatchStatus(batchID, BatchDone)
	done, _ := s.GetBatch(batchID)
	if done.FinishedAt == nil {
		t.Error("完成时应记录结束时间")
	}
}

// TestActiveBatchesForResume 确保重启后能找到未完成的批次。
func TestActiveBatchesForResume(t *testing.T) {
	s := openTestStore(t)
	running := seedBatch(t, s, 2)
	finished := seedBatch(t, s, 2)
	s.SetBatchStatus(finished, BatchDone)

	active, err := s.ActiveBatches()
	if err != nil {
		t.Fatalf("查询未完成批次失败: %v", err)
	}
	if len(active) != 1 || active[0].ID != running {
		t.Errorf("未完成批次 = %v, 期望只有 %d", active, running)
	}
}

// TestStatsAndFailedItems 覆盖 11.10：
// 完成统计需逐行列出失败条目的原始输入。
func TestStatsAndFailedItems(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 4)

	a, _ := s.ClaimNextItem(batchID)
	trackID, _ := s.CreateTrack(&Track{Title: "x"})
	s.MarkImported(a.ID, trackID)

	b, _ := s.ClaimNextItem(batchID)
	s.MarkReview(b.ID, `[]`, `[]`)

	c, _ := s.ClaimNextItem(batchID)
	s.RecordFailure(c.ID, "503 Service Unavailable", time.Millisecond, 1)

	stats, _ := s.Stats(batchID)
	if stats.Imported != 1 || stats.Review != 1 || stats.Failed != 1 || stats.Queued != 1 {
		t.Errorf("统计 = %+v", stats)
	}
	if stats.Total != 4 {
		t.Errorf("总数 = %d, 期望 4", stats.Total)
	}
	if stats.Processed() != 3 {
		t.Errorf("已处理 = %d, 期望 3", stats.Processed())
	}

	failed, err := s.FailedItems(batchID)
	if err != nil {
		t.Fatalf("查询失败条目出错: %v", err)
	}
	if len(failed) != 1 {
		t.Fatalf("失败条目数 = %d, 期望 1", len(failed))
	}
	// 逐行列出时需要原始输入信息
	if failed[0].InTitle == nil {
		t.Error("失败条目应保留原始输入，供统计逐行列出")
	}
	if failed[0].LastError == nil || *failed[0].LastError == "" {
		t.Error("失败条目应记录最后错误")
	}
	if failed[0].LastErrorAt == nil {
		t.Error("失败条目应记录错误时间")
	}
}

// TestAllReviewItemsAcrossBatches 覆盖复核队列的逐条浏览。
func TestAllReviewItemsAcrossBatches(t *testing.T) {
	s := openTestStore(t)
	b1 := seedBatch(t, s, 2)
	b2 := seedBatch(t, s, 2)

	i1, _ := s.ClaimNextItem(b1)
	s.MarkReview(i1.ID, `[]`, `[]`)
	i2, _ := s.ClaimNextItem(b2)
	s.RecordFailure(i2.ID, "503", time.Millisecond, 1)

	items, err := s.AllReviewItems()
	if err != nil {
		t.Fatalf("查询复核条目失败: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("待处置条目 = %d, 期望 2（跨批次）", len(items))
	}

	// 待选与失败都在同一个列表中——界面结构一致，只是状态标签不同
	var review, failed int
	for _, it := range items {
		switch it.Status {
		case StatusReview:
			review++
		case StatusFailed:
			failed++
		}
	}
	if review != 1 || failed != 1 {
		t.Errorf("待选 %d 条、失败 %d 条, 期望各 1 条", review, failed)
	}
}

// TestClaimRespectsRetryTime 确保退避未到期的条目不被提前取出。
func TestClaimRespectsRetryTime(t *testing.T) {
	s := openTestStore(t)
	batchID := seedBatch(t, s, 1)
	item, _ := s.ClaimNextItem(batchID)

	// 退避 1 小时
	s.RecordFailure(item.ID, "503", time.Hour, 5)

	if next, _ := s.ClaimNextItem(batchID); next != nil {
		t.Error("退避未到期的条目不应被取出")
	}
}
