package importer

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// recordingSearcher 记录每次查询，并按曲名返回预设候选。
type recordingSearcher struct {
	mu      sync.Mutex
	byTitle map[string][]provider.Candidate
	queries []provider.Query
	failFor map[string]error
}

func (s *recordingSearcher) SearchTracks(_ context.Context, q provider.Query) ([]provider.Candidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	if err, ok := s.failFor[q.Title]; ok {
		return nil, err
	}
	if hit, ok := s.byTitle[q.Title]; ok {
		return hit, nil
	}
	// 模拟上游的**子短语**匹配：MusicBrainz 的引号短语只要求查询词元序列是
	// 被索引字段的一段连续子序列，因此 `recording:"God knows"` 会返回
	// `God knows...`。按精确相等查表会让检索侧任何合理的规范化
	// （去截断省略号、剥译名注释）都在桩件这里凭空落空。
	nq := matching.Normalize(q.Title)
	if nq == "" {
		return nil, nil
	}
	for title, candidates := range s.byTitle {
		if strings.Contains(matching.Normalize(title), nq) {
			return candidates, nil
		}
	}
	return nil, nil
}

func (s *recordingSearcher) queryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queries)
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "runner.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func sptr(s string) *string { return &s }

func mbCandidate(ref, title, artist, album string) provider.Candidate {
	return provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: ref,
		Title: sptr(title), ArtistName: sptr(artist), AlbumTitle: sptr(album),
		ReleaseRef: sptr("rel-" + ref), ReleaseGroupRef: sptr("rg-1"),
		ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
	}
}

func enqueueOne(t *testing.T, s *store.Store, title, artist, album string) int64 {
	t.Helper()
	batchID, err := s.CreateBatch(&store.ImportBatch{Filename: "t.csv", TotalRows: 1})
	if err != nil {
		t.Fatalf("建批次失败: %v", err)
	}
	raw, _ := json.Marshal([]string{title, artist, album})
	if err := s.EnqueueItems(batchID, []store.ImportItem{{
		RowIndex: 0, RawRow: string(raw),
		InTitle: &title, InArtist: &artist, InAlbum: &album,
	}}); err != nil {
		t.Fatalf("入队失败: %v", err)
	}
	return batchID
}

// runToCompletion 跑完一个批次并返回收到的进度事件。
func runToCompletion(t *testing.T, r *Runner, batchID int64) []Progress {
	t.Helper()
	done := make(chan struct{})
	var mu sync.Mutex
	var events []Progress

	r.emit = func(p Progress) {
		mu.Lock()
		events = append(events, p)
		finished := p.Done
		mu.Unlock()
		if finished {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}

	if err := r.Start(context.Background(), batchID); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("批次未在预期时间内完成")
	}

	mu.Lock()
	defer mu.Unlock()
	return append([]Progress{}, events...)
}

func newRunner(st *store.Store, s matching.Searcher) *Runner {
	engine := matching.NewEngine(config.Defaults().Matching)
	retry := provider.DefaultRetryPolicy()
	retry.BaseDelay = time.Millisecond
	return NewRunner(st, engine, s, retry, false, nil)
}

// TestRunnerImportsHighConfidence 覆盖 11.6 的直接入库分支。
func TestRunnerImportsHighConfidence(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{
		"God knows...": {mbCandidate("trk-1", "God knows...", "平野綾", "涼宮ハルヒの詰合")},
	}}
	batchID := enqueueOne(t, st, "God knows...", "平野綾", "涼宮ハルヒの詰合")

	events := runToCompletion(t, newRunner(st, searcher), batchID)

	stats, _ := st.Stats(batchID)
	if stats.Imported != 1 {
		t.Errorf("已入库 = %d, 期望 1", stats.Imported)
	}

	// 溯源被写入
	items, _ := st.AllReviewItems()
	if len(items) != 0 {
		t.Errorf("不应有待复核条目, 得到 %d 条", len(items))
	}
	trackID, err := st.FindByMatchRef("musicbrainz", "trk-1")
	if err != nil || trackID == 0 {
		t.Errorf("应按 track MBID 写入溯源, 得到 %d (%v)", trackID, err)
	}

	// 导入自动入库只写 release 行，清单状态为 none
	rel, _ := st.GetReleaseByRef("musicbrainz", "rel-trk-1")
	if rel == nil {
		t.Fatal("应写入 release 行")
	}
	if rel.TracklistState != store.TracklistNone {
		t.Errorf("清单状态 = %q, 期望 none（search 响应不含完整清单）", rel.TracklistState)
	}

	// 日志行可辨认
	var sawImport bool
	for _, e := range events {
		if containsStr(e.Line, "[直接入库]") {
			sawImport = true
		}
	}
	if !sawImport {
		t.Error("应输出直接入库的提示")
	}
}

// TestRunnerSendsToReview 覆盖跨组与无结果进复核。
func TestRunnerSendsToReview(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{}}
	batchID := enqueueOne(t, st, "查不到的曲", "艺术家", "专辑")

	events := runToCompletion(t, newRunner(st, searcher), batchID)

	stats, _ := st.Stats(batchID)
	if stats.Review != 1 {
		t.Errorf("待复核 = %d, 期望 1", stats.Review)
	}

	items, _ := st.AllReviewItems()
	if len(items) != 1 {
		t.Fatalf("复核条目 = %d 条", len(items))
	}
	// 已尝试变体被记录，供界面提示
	if items[0].TriedVariants == nil {
		t.Error("应记录已尝试的查询变体")
	}

	var sawFail bool
	for _, e := range events {
		if containsStr(e.Line, "[匹配失败]") {
			sawFail = true
		}
	}
	if !sawFail {
		t.Error("应输出匹配失败的提示")
	}
}

// TestRunnerLocalDuplicateGoesToReview 覆盖 11.3：
// 疑似重复送入复核，且不新建曲目；库中曲目作为一条候选出现。
func TestRunnerLocalDuplicateGoesToReview(t *testing.T) {
	st := openStore(t)

	// 库中已有一首无溯源的曲目（「无需匹配」入库的那种）
	existing, err := st.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "God knows...", Artist: sptr("平野綾"),
	})
	if err != nil {
		t.Fatalf("准备库中曲目失败: %v", err)
	}

	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{}}
	batchID := enqueueOne(t, st, "God knows...", "平野綾", "")

	events := runToCompletion(t, newRunner(st, searcher), batchID)

	stats, _ := st.Stats(batchID)
	if stats.Review != 1 {
		t.Errorf("待复核 = %d, 期望 1", stats.Review)
	}

	// 不新建曲目
	var count int
	st.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&count)
	if count != 1 {
		t.Errorf("曲目数 = %d, 期望仍为 1（疑似重复不新建）", count)
	}

	// 库中曲目作为一条候选出现在同一列表里
	items, _ := st.AllReviewItems()
	if len(items) != 1 || items[0].Candidates == nil {
		t.Fatal("应把库中曲目作为候选写入快照")
	}
	var snapshot []map[string]any
	if err := json.Unmarshal([]byte(*items[0].Candidates), &snapshot); err != nil {
		t.Fatalf("候选快照无法解析: %v", err)
	}
	if len(snapshot) != 1 || snapshot[0]["local"] != true {
		t.Errorf("候选快照 = %v, 期望含本地候选", snapshot)
	}
	if snapshot[0]["action"] != "合并" {
		t.Errorf("本地候选的动作 = %v, 期望 合并", snapshot[0]["action"])
	}
	if int64(snapshot[0]["trackId"].(float64)) != existing {
		t.Error("候选应指向库中已有的那首曲目")
	}

	// 疑似重复的提示同时输出两边信息
	var sawDup bool
	for _, e := range events {
		if containsStr(e.Line, "[疑似重复]") && containsStr(e.Line, "当前曲目库信息") {
			sawDup = true
		}
	}
	if !sawDup {
		t.Error("应输出疑似重复的提示并含库中曲目信息")
	}
}

// TestRunnerBacksOffAndFails 覆盖 11.7：
// 出错退避重排，累计达上限后标记失败且不再自动重排。
func TestRunnerBacksOffAndFails(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{},
		failFor: map[string]error{"总是失败的曲": errors.New("503 Service Unavailable")},
	}
	batchID := enqueueOne(t, st, "总是失败的曲", "艺术家", "专辑")

	runToCompletion(t, newRunner(st, searcher), batchID)

	stats, _ := st.Stats(batchID)
	if stats.Failed != 1 {
		t.Errorf("失败 = %d, 期望 1", stats.Failed)
	}

	items, _ := st.AllReviewItems()
	if len(items) != 1 {
		t.Fatalf("条目数 = %d", len(items))
	}
	if items[0].AttemptCount != provider.DefaultMaxAttempts {
		t.Errorf("重试次数 = %d, 期望 %d", items[0].AttemptCount, provider.DefaultMaxAttempts)
	}
	if items[0].NextRetryAt != nil {
		t.Error("达到上限后不应再有下次重试时间")
	}
	if items[0].LastError == nil {
		t.Error("应记录最后错误")
	}
}

// TestRunnerEmitsProgress 覆盖 11.9：
// 进度由后端主动推送，前端不轮询。
func TestRunnerEmitsProgress(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{
		"曲1": {mbCandidate("t1", "曲1", "艺术家", "专辑")},
		"曲2": {mbCandidate("t2", "曲2", "艺术家", "专辑")},
		"曲3": {mbCandidate("t3", "曲3", "艺术家", "专辑")},
	}}

	batchID, _ := st.CreateBatch(&store.ImportBatch{Filename: "t.csv", TotalRows: 3})
	var items []store.ImportItem
	for i, title := range []string{"曲1", "曲2", "曲3"} {
		raw, _ := json.Marshal([]string{title})
		tt, artist, album := title, "艺术家", "专辑"
		items = append(items, store.ImportItem{
			RowIndex: i, RawRow: string(raw),
			InTitle: &tt, InArtist: &artist, InAlbum: &album,
		})
	}
	st.EnqueueItems(batchID, items)

	events := runToCompletion(t, newRunner(st, searcher), batchID)

	if len(events) < 3 {
		t.Errorf("收到 %d 个进度事件, 期望至少 3 个（每条一个）", len(events))
	}

	// 进度递增且带总数
	last := events[len(events)-1]
	if !last.Done {
		t.Error("最后一个事件应标记完成")
	}
	if last.Total != 3 {
		t.Errorf("总数 = %d, 期望 3", last.Total)
	}
	if last.Processed != 3 {
		t.Errorf("已处理 = %d, 期望 3", last.Processed)
	}
	if last.Stats.Imported != 3 {
		t.Errorf("已入库 = %d, 期望 3", last.Stats.Imported)
	}

	// 完成统计文案
	if !containsStr(last.Line, "本次导入 3 条") {
		t.Errorf("完成统计 = %q", last.Line)
	}
}

// TestRunnerOnlyQueriesMusicBrainz 覆盖 11.11：
// 导入阶段只用 MusicBrainz，不对同一条目并发查询多个上游。
func TestRunnerOnlyQueriesMusicBrainz(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{
		"曲": {mbCandidate("t1", "曲", "艺术家", "专辑")},
	}}
	batchID := enqueueOne(t, st, "曲", "艺术家", "专辑")

	runToCompletion(t, newRunner(st, searcher), batchID)

	// Runner 只持有一个 searcher（MB），因此全部查询都发往它；
	// 且同一条目的查询是串行的，不并发多上游。
	if searcher.queryCount() == 0 {
		t.Fatal("应至少发出一次查询")
	}
	trackID, _ := st.FindByMatchRef("musicbrainz", "t1")
	if trackID == 0 {
		t.Error("入库的溯源来源应为 musicbrainz")
	}
	// 没有任何 itunes 来源的溯源
	var itunesCount int
	st.DB().QueryRow(
		`SELECT COUNT(*) FROM track_metadata WHERE match_source = 'itunes'`).Scan(&itunesCount)
	if itunesCount != 0 {
		t.Errorf("导入阶段不应产生 iTunes 溯源, 得到 %d 条", itunesCount)
	}
}

// TestRunnerFailedItemsListedInSummary 覆盖 11.10：
// 完成统计逐行列出失败条目的原始输入。
func TestRunnerFailedItemsListedInSummary(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{
		byTitle: map[string][]provider.Candidate{},
		failFor: map[string]error{"失败曲": errors.New("503")},
	}
	batchID := enqueueOne(t, st, "失败曲", "某艺术家", "某专辑")

	events := runToCompletion(t, newRunner(st, searcher), batchID)
	last := events[len(events)-1]

	if !containsStr(last.Line, "失败 1 条") {
		t.Errorf("统计应含失败计数: %q", last.Line)
	}
	// 逐行列出失败条目的原始输入
	if !containsStr(last.Line, "失败曲") || !containsStr(last.Line, "某艺术家") {
		t.Errorf("统计应逐行列出失败条目的原始输入: %q", last.Line)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestPauseEmitsSummary 覆盖 11.10：
// 「导入完成**或暂停**时」都要输出统计并逐行列出失败条目。
// 此前暂停路径发的是空行，用户暂停后无从核对本次导入的结果。
func TestPauseEmitsSummary(t *testing.T) {
	st := openStore(t)
	searcher := &recordingSearcher{byTitle: map[string][]provider.Candidate{}}
	batchID := enqueueOne(t, st, "某曲", "某艺术家", "某专辑")

	var mu sync.Mutex
	var lines []string
	done := make(chan struct{})

	r := NewRunner(st, matching.NewEngine(config.Defaults().Matching), searcher,
		provider.DefaultRetryPolicy(), false, func(p Progress) {
			mu.Lock()
			if p.Line != "" {
				lines = append(lines, p.Line)
			}
			paused := p.Paused
			mu.Unlock()
			if paused {
				select {
				case <-done:
				default:
					close(done)
				}
			}
		})

	// 先暂停再启动：loop 会立刻走到 ctx.Err() 分支
	if err := r.Pause(batchID); err != nil {
		t.Fatalf("暂停失败: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	running := make(chan struct{})
	go func() {
		r.Start(ctx, batchID)
		close(running)
	}()
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("暂停后未收到带统计的进度事件")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawSummary bool
	for _, l := range lines {
		if containsStr(l, "本次导入") && containsStr(l, "进入复核") {
			sawSummary = true
		}
	}
	if !sawSummary {
		t.Errorf("暂停时应输出统计而非空行, 实际收到: %v", lines)
	}
}
