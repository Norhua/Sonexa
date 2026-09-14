package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// Progress 是推送给前端的进度事件。
type Progress struct {
	BatchID   int64            `json:"batchId"`
	Processed int              `json:"processed"`
	Total     int              `json:"total"`
	Stats     store.BatchStats `json:"stats"`
	// Line 是本次处理产生的日志行（6.3 的分支提示文案）。
	Line string `json:"line"`
	// Done 为真表示该批次已处理完毕。
	Done bool `json:"done"`
	// Paused 为真表示任务被暂停。
	Paused bool `json:"paused"`
}

// Emitter 把进度推送给前端。
//
// 进度由后端主动推送而非前端轮询——17 分钟的任务上轮询会产生
// 数千次无谓的 IPC 调用，而进度更新的自然形态就是推送。
type Emitter func(Progress)

// Runner 驱动匹配队列。
type Runner struct {
	store    *store.Store
	engine   *matching.Engine
	searcher matching.Searcher
	emit     Emitter
	retry    provider.RetryPolicy
	// fetchOnMatch 为真时在匹配阶段就补详情与清单；
	// 默认关闭（首次打开专辑时懒加载），因为 MB 1 req/s 下
	// 每行都补一次详情会让导入时间翻倍，而用户实际打开的专辑只是少数。
	fetchOnMatch bool

	// enricher 在「匹配时拉取」开启时被调用，把详情落到刚入库的曲目上。
	// 由绑定层注入，避免 importer 反向依赖 main 包。
	enricher func(trackID int64, providerID, releaseRef string)

	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

// NewRunner 建立队列驱动器。
//
// searcher 固定为 MusicBrainz：v1 的导入匹配阶段只用主上游，
// 且不对同一条目并发查询多个上游。用户如需其他上游可在复核队列中逐条改选。
func NewRunner(st *store.Store, engine *matching.Engine, searcher matching.Searcher,
	retry provider.RetryPolicy, fetchOnMatch bool, emit Emitter) *Runner {
	return &Runner{
		store:        st,
		engine:       engine,
		searcher:     searcher,
		emit:         emit,
		retry:        retry,
		fetchOnMatch: fetchOnMatch,
		running:      map[int64]context.CancelFunc{},
	}
}

// SetEnricher 注入详情补全回调。
//
// 设置中把清单拉取时机改为「匹配时」后，导入会在每条入库后立即补详情。
func (r *Runner) SetEnricher(fn func(trackID int64, providerID, releaseRef string)) {
	r.enricher = fn
}

// Start 开始（或恢复）处理某批次。
func (r *Runner) Start(ctx context.Context, batchID int64) error {
	r.mu.Lock()
	if _, already := r.running[batchID]; already {
		r.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.running[batchID] = cancel
	r.mu.Unlock()

	if err := r.store.SetBatchStatus(batchID, store.BatchRunning); err != nil {
		return err
	}

	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.running, batchID)
			r.mu.Unlock()
			cancel()
		}()
		r.loop(runCtx, batchID)
	}()
	return nil
}

// Pause 暂停某批次。恢复时从暂停处继续，不从头开始。
func (r *Runner) Pause(batchID int64) error {
	r.mu.Lock()
	if cancel, ok := r.running[batchID]; ok {
		cancel()
	}
	r.mu.Unlock()
	return r.store.SetBatchStatus(batchID, store.BatchPaused)
}

// IsRunning 报告某批次是否正在处理。
func (r *Runner) IsRunning(batchID int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.running[batchID]
	return ok
}

// AnyRunning 报告是否有任何批次正在处理。
//
// 后台的补全任务据它让路：两者打的是同一个 1 req/s 的上游，
// 同时跑等于把导入的速度砍半。
func (r *Runner) AnyRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.running) > 0
}

// ResumeAll 在应用启动时恢复全部未完成的批次。
func (r *Runner) ResumeAll(ctx context.Context) error {
	// 中断时停留在「匹配中」的条目退回排队
	if err := r.store.ResetStaleMatching(); err != nil {
		return err
	}
	batches, err := r.store.ActiveBatches()
	if err != nil {
		return err
	}
	for _, b := range batches {
		if b.Status == store.BatchRunning {
			if err := r.Start(ctx, b.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// loop 是队列主循环：逐条取出、处理、推送进度。
func (r *Runner) loop(ctx context.Context, batchID int64) {
	for {
		if ctx.Err() != nil {
			// 暂停与完成一样要给出统计：规格要求「导入完成或暂停时」
			// 都输出各类计数并逐行列出失败条目，空行会让用户无从核对。
			r.emitProgress(batchID, r.summaryFor(batchID), false, true)
			return
		}

		item, err := r.store.ClaimNextItem(batchID)
		if err != nil {
			r.emitProgress(batchID, matching.ErrorLogLine(err), false, false)
			return
		}
		if item == nil {
			// 队列空了：可能全部处理完，也可能都在退避等待
			stats, _ := r.store.Stats(batchID)
			if stats.Queued == 0 && stats.Matching == 0 {
				r.store.SetBatchStatus(batchID, store.BatchDone)
				r.emitProgress(batchID, r.summaryFor(batchID), true, false)
				return
			}
			// 有条目在退避中，稍后再看
			select {
			case <-ctx.Done():
				r.emitProgress(batchID, r.summaryFor(batchID), false, true)
				return
			case <-time.After(time.Second):
				continue
			}
		}

		line := r.processItem(ctx, item)
		r.emitProgress(batchID, line, false, false)
	}
}

// processItem 处理一个条目并返回其日志行。
func (r *Runner) processItem(ctx context.Context, item *store.ImportItem) string {
	in := matching.Input{
		Title:      deref(item.InTitle),
		Artist:     deref(item.InArtist),
		Album:      deref(item.InAlbum),
		DurationMS: item.InDurationMS,
	}

	// [1] 本地去重检查：疑似重复进复核，库中曲目作为一条候选
	if dupID, err := r.engine.CheckLocalDuplicate(r.store, in); err == nil && dupID != 0 {
		existing := r.describeTrack(dupID)
		candidates := r.localCandidateSnapshot(dupID)
		r.store.MarkReview(item.ID, store.ReviewDuplicate, candidates, "[]")
		return matching.DuplicateLogLine(in, existing)
	}

	// [2][3] 上游查询与评分
	res, err := r.engine.Run(ctx, r.searcher, r.store, in)
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		// 出错时退避重排；累计达上限则标记失败（规则 6）
		backoff := r.retry.Backoff(item.AttemptCount+1, err)
		status, _ := r.store.RecordFailure(item.ID, err.Error(), backoff, r.retry.MaxAttempts)
		if status == store.StatusFailed {
			return matching.ExhaustedLogLine(in, r.retry.MaxAttempts, err)
		}
		return matching.ErrorLogLine(err)
	}

	variantsJSON, _ := json.Marshal(res.TriedVariants)

	// [4] 分流
	switch res.Decision.Outcome {
	case matching.OutcomeImportUnique, matching.OutcomeImportLadder:
		sc := findScored(res.Scored, res.Decision.Selected.MatchRef)
		trackID, err := r.importCandidate(*res.Decision.Selected)
		if err != nil {
			backoff := r.retry.Backoff(item.AttemptCount+1, err)
			r.store.RecordFailure(item.ID, err.Error(), backoff, r.retry.MaxAttempts)
			return matching.ErrorLogLine(err)
		}
		r.store.MarkImported(item.ID, trackID)
		// 设置中选「匹配时」时才在这里补详情与清单；
		// 默认留给首次打开专辑时懒加载。
		if r.fetchOnMatch && r.enricher != nil {
			r.enricher(trackID, string(res.Decision.Selected.Provider), deref(res.Decision.Selected.ReleaseRef))
		}
		return matching.LogLine(res.Decision.Outcome, in, sc)

	default:
		candidatesJSON, _ := json.Marshal(res.Scored)
		r.store.MarkReview(item.ID, reviewReasonOf(res.Decision.Outcome),
			string(candidatesJSON), string(variantsJSON))
		return matching.LogLine(res.Decision.Outcome, in, nil)
	}
}

// reviewReasonOf 把分流结果映射为复核成因。
//
// 统计行里「进入复核」与「未匹配」是两个互不重叠的数，这里是它们唯一的分界点：
// 多义（跨发行组、同组多份发行）算进入复核，无结果算未匹配。
func reviewReasonOf(outcome matching.Outcome) store.ReviewReason {
	if outcome == matching.OutcomeReviewNoMatch {
		return store.ReviewNoMatch
	}
	return store.ReviewAmbiguous
}

// importCandidate 把一条候选入库并写入溯源。
//
// 只写候选自己带的字段，**不用表格里的原始输入去补**上游没给的署名或歌集——
// 曲目记录的是「上游怎么说」，表格那一行是「用户当时怎么写」。
// 补了会让一首曲目的字段一半来自上游、一半来自表格，且看不出哪一半是哪一半。
// 因此这里与复核队列的「接受」、搜索页的「加入曲库」走同一个落点，
// 三条路径入库的曲目没有任何区别。
func (r *Runner) importCandidate(c provider.Candidate) (int64, error) {
	trackID, err := r.store.AdmitCandidate(c)
	// 撞唯一键说明另一路径抢先入库了，静默转为已有曲目
	var exists *store.ErrAlreadyExists
	if err != nil && asAlreadyExists(err, &exists) {
		return exists.TrackID, nil
	}
	return trackID, err
}

// summaryFor 生成完成统计文案，并逐行列出失败条目的原始输入。
func (r *Runner) summaryFor(batchID int64) string {
	stats, err := r.store.Stats(batchID)
	if err != nil {
		return ""
	}
	// 四个数互不重叠，相加即本批总数。「进入复核」与「未匹配」都停在复核队列里，
	// 靠 import_item.review_reason 分开——此前两者都取 stats.Review，永远相同。
	line := matching.SummaryLine(stats.Imported, stats.ReviewOnly(),
		stats.Unmatched, stats.Failed, r.retry.MaxAttempts)

	failed, err := r.store.FailedItems(batchID)
	if err != nil || len(failed) == 0 {
		return line
	}
	for _, it := range failed {
		line += fmt.Sprintf("\n  %s · %s · %s",
			deref(it.InTitle), orDash(it.InArtist), orDash(it.InAlbum))
	}
	return line
}

func (r *Runner) emitProgress(batchID int64, line string, done, paused bool) {
	if r.emit == nil {
		return
	}
	stats, err := r.store.Stats(batchID)
	if err != nil {
		return
	}
	r.emit(Progress{
		BatchID:   batchID,
		Processed: stats.Processed(),
		Total:     stats.Total,
		Stats:     stats,
		Line:      line,
		Done:      done,
		Paused:    paused,
	})
}

// describeTrack 返回库中曲目的可读描述，供疑似重复提示使用。
func (r *Runner) describeTrack(trackID int64) string {
	t, err := r.store.GetTrack(trackID)
	if err != nil || t == nil {
		return fmt.Sprintf("曲目 #%d", trackID)
	}
	return fmt.Sprintf("%s · %s · %s", t.Title, orDash(t.Artist), orDash(t.Album))
}

// localCandidateSnapshot 把库中曲目作为一条候选写入快照，
// 使它出现在复核队列的同一个候选列表里。
func (r *Runner) localCandidateSnapshot(trackID int64) string {
	t, err := r.store.GetTrack(trackID)
	if err != nil || t == nil {
		return "[]"
	}
	snapshot := []map[string]any{{
		"local":   true,
		"trackId": trackID,
		"title":   t.Title,
		"artist":  t.Artist,
		"album":   t.Album,
		"action":  "合并",
		"badges":  []string{"本地?"},
	}}
	data, _ := json.Marshal(snapshot)
	return string(data)
}

func findScored(scored []matching.ScoredCandidate, ref string) *matching.ScoredCandidate {
	for i := range scored {
		if scored[i].Candidate.MatchRef == ref {
			return &scored[i]
		}
	}
	if len(scored) > 0 {
		return &scored[0]
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orDash(s *string) string {
	if s == nil || *s == "" {
		return "—"
	}
	return *s
}

// asAlreadyExists 是 errors.As 的小包装，避免在热路径引入额外导入。
func asAlreadyExists(err error, target **store.ErrAlreadyExists) bool {
	if e, ok := err.(*store.ErrAlreadyExists); ok {
		*target = e
		return true
	}
	return false
}
