package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/library"
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
	library  *library.Service
	initErr  error
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
	// 由应用层注入，避免 importer 反向依赖 internal/app。
	enricher func(trackID int64, providerID, releaseRef string)
	// 成功提交后通知库内容变化，不能等下一次网络补全或整批结束。
	onLibraryChanged func()

	mu      sync.Mutex
	closed  bool
	work    sync.WaitGroup
	running map[int64]context.CancelFunc
	items   map[int64]runningImport
	// stale 记录各条目连续遇到计划失效的次数，成功或进入复核后清零。
	stale map[int64]int
}
type runningImport struct {
	revision int64
	cancel   context.CancelFunc
}

// NewRunner 建立队列驱动器。
//
// searcher 固定为 MusicBrainz：v1 的导入匹配阶段只用主上游，
// 且不对同一条目并发查询多个上游。用户如需其他上游可在复核队列中逐条改选。
func NewRunner(st *store.Store, engine *matching.Engine, searcher matching.Searcher,
	retry provider.RetryPolicy, fetchOnMatch bool, emit Emitter, services ...*library.Service) *Runner {
	r := &Runner{
		store:        st,
		engine:       engine,
		searcher:     searcher,
		emit:         emit,
		retry:        retry,
		fetchOnMatch: fetchOnMatch,
		running:      map[int64]context.CancelFunc{},
		items:        map[int64]runningImport{},
	}
	if len(services) > 0 {
		r.library = services[0]
	} else if source, ok := searcher.(provider.MetadataProvider); ok {
		registry := provider.NewRegistry()
		registry.Register(source)
		r.library, r.initErr = library.New(registry, st, nil, nil)
	}
	if r.library == nil && r.initErr == nil {
		r.initErr = fmt.Errorf("导入准入服务未装配")
	}
	st.SetImportInvalidationHandler(r.cancelInvalidatedImports)
	return r

}

// SetEnricher 注入详情补全回调。
//
// 设置中把清单拉取时机改为「匹配时」后，导入会在每条入库后立即补详情。
func (r *Runner) SetEnricher(fn func(trackID int64, providerID, releaseRef string)) {
	r.enricher = fn
}

// SetLibraryChanged 在启动前注入成功提交后的通知。
func (r *Runner) SetLibraryChanged(fn func()) {
	r.onLibraryChanged = fn
}

// Start 开始（或恢复）处理某批次。
func (r *Runner) Start(ctx context.Context, batchID int64) error {
	if r.initErr != nil {
		return r.initErr
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return fmt.Errorf("导入服务已关闭")
	}
	if _, already := r.running[batchID]; already {
		r.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	r.running[batchID] = cancel
	r.work.Add(1)
	r.mu.Unlock()

	if err := r.store.SetBatchStatus(batchID, store.BatchRunning); err != nil {
		r.mu.Lock()
		delete(r.running, batchID)
		r.mu.Unlock()
		cancel()
		r.work.Done()
		return err
	}
	r.emitProgress(batchID, "", false, false)

	go func() {
		defer r.work.Done()
		defer func() {
			r.mu.Lock()
			delete(r.running, batchID)
			r.mu.Unlock()
			// 先移除运行标记，再通知最终状态；前端事件后立即读到的也是最终状态。
			if batch, err := r.store.GetBatch(batchID); err == nil && batch != nil {
				if batch.Status == store.BatchRunning && runCtx.Err() != nil {
					_ = r.store.SetBatchStatus(batchID, store.BatchPaused)
					batch.Status = store.BatchPaused
				}
				r.emitProgress(batchID, r.summaryFor(batchID), batch.Status == store.BatchDone, batch.Status == store.BatchPaused)
			}
			cancel()
		}()
		r.loop(runCtx, batchID)
	}()
	return nil
}

// Pause 暂停某批次。恢复时从暂停处继续，不从头开始。
func (r *Runner) Pause(batchID int64) error {
	if err := r.store.SetBatchStatus(batchID, store.BatchPaused); err != nil {
		return err
	}
	r.mu.Lock()
	if cancel, ok := r.running[batchID]; ok {
		cancel()
	}
	r.mu.Unlock()
	return nil
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
			return
		}

		item, err := r.store.ClaimNextItem(batchID)
		if err != nil {
			r.emitProgress(batchID, matching.ErrorLogLine(err), false, false)
			return
		}
		if item == nil {
			// 队列空了：可能全部处理完，也可能都在退避等待
			pending, err := r.store.HasScheduledImportWork(batchID)
			if err != nil {
				r.emitProgress(batchID, matching.ErrorLogLine(err), false, false)
				return
			}
			if !pending {
				r.store.SetBatchStatus(batchID, store.BatchDone)
				return
			}
			// 有条目在退避中，稍后再看
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
				continue
			}
		}

		line := r.processItem(ctx, item)
		r.emitProgress(batchID, line, false, false)
	}
}

func (r *Runner) CancelInvalidatedImports() {
	r.cancelInvalidatedImports()
}

// Close stops both active batches and their callbacks before storage is closed.
func (r *Runner) Close() {
	r.mu.Lock()
	r.closed = true
	for _, cancel := range r.running {
		cancel()
	}
	r.mu.Unlock()
	r.work.Wait()
}

func (r *Runner) cancelInvalidatedImports() {
	r.mu.Lock()
	items := make(map[int64]runningImport, len(r.items))
	for id, item := range r.items {
		items[id] = item
	}
	r.mu.Unlock()
	for id, item := range items {
		revision, active, err := r.store.ImportWorkRevision(id)
		if err != nil || !active || revision != item.revision {
			item.cancel()
		}
	}
}

// processItem 处理一个条目并返回其日志行。
func (r *Runner) processItem(ctx context.Context, item *store.ImportItem) string {
	revision, active, err := r.store.ImportWorkRevision(item.ID)
	if err != nil || !active {
		return "[目标已移出] 不再处理失效输入"
	}
	ctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	r.items[item.ID] = runningImport{revision: revision, cancel: cancel}
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.items, item.ID); r.mu.Unlock(); cancel() }()
	r.cancelInvalidatedImports()
	if ctx.Err() != nil {
		_ = r.store.ReleaseImportClaim(item.ID)
		return "[重新核验] 导入目标已变化"
	}
	current, err := r.store.EffectiveImportItem(item.ID)
	if err != nil || current == nil {
		return "[目标已移出] 不再处理失效输入"
	}
	item = current
	defer func() {
		if ctx.Err() != nil {
			_ = r.store.ReleaseImportClaim(item.ID)
		}
	}()
	in := matching.Input{
		Title:      deref(item.InTitle),
		Artist:     deref(item.InArtist),
		Album:      deref(item.InAlbum),
		DurationMS: item.InDurationMS,
		DiscNo:     item.InDiscNo, TrackNo: item.InTrackNo,
	}

	// 全库离线证据先于默认上游；QQ 收藏或历史接受不依赖 MB 搜得到。
	if handled, err := r.store.ResolveImport(item.ID, nil, nil); err != nil {
		r.store.RecordFailure(item.ID, err.Error(), r.retry.Backoff(item.AttemptCount+1, err), r.retry.MaxAttempts)
		return matching.ErrorLogLine(err)
	} else if handled {
		return r.handledImportLine(item.ID, in)
	}

	// 记录实际执行的来源，不从用户当前选择或文件信息推断。
	if source, ok := r.searcher.(interface{ ID() provider.ProviderID }); ok {
		if err := r.store.SetImportSearchSource(item.ID, string(source.ID())); err != nil {
			return matching.ErrorLogLine(err)
		}
	}
	// 上游查询与评分；文本疑似和 ISRC 不再提前变成本地候选。
	res, err := r.engine.Run(ctx, r.searcher, in)
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

	// Track resolution is independent from release-group admission: a unique
	// candidate may still need review for missing grouping facts.
	// 已拥有判定的旁证只取可自动入库的候选：整个候选池里混着仅供人工查看的
	// 结果时，池子大小不该决定同一首已拥有的歌是「跳过」还是「复核」。
	eligible := matching.AutomaticCandidates(res.Scored)
	var resolved *provider.Candidate
	if len(eligible) == 1 {
		resolved = &eligible[0].Candidate
	}
	if handled, err := r.store.ResolveImport(item.ID, eligible, resolved); err != nil {
		r.store.RecordFailure(item.ID, err.Error(), r.retry.Backoff(item.AttemptCount+1, err), r.retry.MaxAttempts)
		return matching.ErrorLogLine(err)
	} else if handled {
		return r.handledImportLine(item.ID, in)
	}

	// [4] 同一用例执行按需摘要、自动选版和准入；成员变更始终进入复核。
	selection, err := r.library.SelectCandidate(ctx, res.Scored, item.ID)
	if err != nil {
		if ctx.Err() != nil {
			return ""
		}
		backoff := r.retry.Backoff(item.AttemptCount+1, err)
		r.store.RecordFailure(item.ID, err.Error(), backoff, r.retry.MaxAttempts)
		return matching.ErrorLogLine(err)
	}
	// 组内歌曲解析把候选替换成了已有发行的清单条目时，之后的判重、已解析身份、
	// 补全与日志都改用替换后的身份；候选自己的身份只用来证明输入与它一致。
	substitute := selection.Substitute
	if selection.Candidate != nil {
		var handled, owned bool
		var err error
		if substitute != nil {
			handled, owned, err = r.store.ResolveProvidedImport(item.ID, store.ProvidedImport{Candidate: *selection.Candidate, ReleaseID: substitute.ReleaseID, TrackRef: substitute.TrackRef})
		} else {
			handled, err = r.store.ResolveImport(item.ID, scoredFor(res.Scored, *selection.Candidate), selection.Candidate)
		}
		if errors.Is(err, store.ErrPlanStale) {
			return r.retryStale(item, in, err)
		}
		if err != nil {
			r.store.RecordFailure(item.ID, err.Error(), r.retry.Backoff(item.AttemptCount+1, err), r.retry.MaxAttempts)
			return matching.ErrorLogLine(err)
		} else if handled {
			return r.handledImportLine(item.ID, in)
		}
		if ctx.Err() != nil {
			return ""
		}
		// 精确绑定已在库但输入不能证明一致，交给人工，不以已有身份绕过复核。
		if owned || (selection.Plan != nil && selection.Plan.Baseline.ExistingTrackID != 0) {
			selection.Reason = library.ReasonAmbiguous
		}
	}
	if selection.Candidate != nil && selection.Reason == "" {
		result, err := r.library.CommitAdmission(ctx, selection.Plan.ID, selection.Plan.Hash, domain.AdmissionApproval{})
		if err == nil {
			r.clearStale(item.ID)
			if r.onLibraryChanged != nil {
				r.onLibraryChanged()
			}
			if result.Existing {
				return fmt.Sprintf("[复用本地] %s · %s，未自动入库", in.Title, in.Artist)
			}
			releaseRef := deref(selection.Candidate.ReleaseRef)
			if substitute != nil && substitute.Identity != nil {
				// 补全必须用曲目实际所挂发行的身份，否则快照会因发行身份不一致被拒。
				releaseRef = substitute.Identity.Ref
			}
			if r.fetchOnMatch && r.enricher != nil {
				r.enricher(result.TrackID, string(selection.Candidate.Provider), releaseRef)
			}
			line := matching.LogLine(matching.OutcomeImportUnique, in, findScored(res.Scored, selection.Candidate.MatchRef))
			if substitute != nil {
				line += fmt.Sprintf("（由已有发行「%s」提供；候选发行 %s 未新建）", deref(substitute.Title), deref(selection.Candidate.ReleaseRef))
			}
			return line
		}
		if errors.Is(err, store.ErrTypedConfirmationRequired) {
			// 自动路径永远不带输入确认凭证：无法确认或疑似已有发行提供这首歌。
			selection.Reason = library.ReasonSongUnresolved
		} else if errors.Is(err, store.ErrPlanStale) {
			return r.retryStale(item, in, err)
		} else if errors.Is(err, store.ErrConfirmationRequired) {
			selection.Reason = library.ReasonNewRelease
		} else {
			r.store.RecordFailure(item.ID, err.Error(), r.retry.Backoff(item.AttemptCount+1, err), r.retry.MaxAttempts)
			return matching.ErrorLogLine(err)
		}
	}
	reason := store.ReviewReason(selection.Reason)
	if reason == "" {
		reason = store.ReviewAmbiguous
	}
	candidatesJSON, _ := json.Marshal(res.Scored)
	evidence := ""
	if selection.Plan != nil {
		// Candidate 只在替换后出现：计划描述的是已有发行，它保留候选原本的身份供排查。
		var candidate *provider.Candidate
		if substitute != nil {
			candidate = selection.Candidate
		}
		raw, _ := json.Marshal(struct {
			domain.AdmissionBaseline
			Proposed  *domain.ReleaseSnapshot `json:"proposed"`
			Candidate *provider.Candidate     `json:"candidate,omitempty"`
		}{selection.Plan.Baseline, selection.Plan.Seed.Snapshot.Release, candidate})
		evidence = string(raw)
	}
	if err := r.store.MarkReview(item.ID, reason, string(candidatesJSON), string(variantsJSON), evidence); err != nil {
		return matching.ErrorLogLine(err)
	}
	r.clearStale(item.ID)

	return fmt.Sprintf("%s · %s：%s", in.Title, in.Artist, reviewReasonText(reason))
}
func (r *Runner) handledImportLine(itemID int64, in matching.Input) string {
	item, err := r.store.GetItem(itemID)
	if err == nil && item != nil && item.Status == store.StatusSkipped {
		if r.onLibraryChanged != nil {
			r.onLibraryChanged()
		}
		return fmt.Sprintf("[复用本地] %s · %s，未自动入库", in.Title, in.Artist)
	}
	return fmt.Sprintf("[合并/移出输入] %s · %s", in.Title, in.Artist)
}

func reviewReasonText(reason store.ReviewReason) string {
	switch string(reason) {
	case library.ReasonNoMatch:
		return "没有高置信匹配"
	case library.ReasonNewRelease:
		return "新增发行加入已有组，需确认"
	case library.ReasonSongUnresolved:
		return "无法确认已有发行是否包含这首歌，需复核"
	case library.ReasonGroupInsufficient:
		return "判组资料不足，需复核"
	case library.ReasonGroupConflict:
		return "组证据冲突，需复核"
	case library.ReasonUnknownTotal:
		return "发行总曲数未知，需复核"
	default:
		return "候选存在歧义，需复核"
	}
}

// summaryFor 生成完成统计文案，并逐行列出失败条目的原始输入。
func (r *Runner) summaryFor(batchID int64) string {
	stats, err := r.store.Stats(batchID)
	if err != nil {
		return ""
	}
	// 原四类互斥；去重新增 skipped/merged，和移出计数一起覆盖全部终态。
	// 靠 import_item.review_reason 分开——此前两者都取 stats.Review，永远相同。
	line := matching.SummaryLine(stats.Imported, stats.ReviewOnly(),
		stats.Unmatched, stats.Failed, r.retry.MaxAttempts)
	line += fmt.Sprintf("\n已跳过 %d 条，已合并输入 %d 条，已移出 %d 条", stats.Skipped, stats.Merged, stats.Discarded)

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

// plan_stale 的连续重排上限与退避步长：1s、2s、3s 之后转入普通失败流程。
const (
	maxStaleRetries = 3
	staleRetryStep  = time.Second
)

// retryStale 处理计划失效：新输入被吸收或库状态变化后重新决策，不伪报成新增发行复核。
// 连续失效说明有东西在反复改写基线：短退避重排，超过上限按普通失败处理
// （计次数、指数退避），不无限空转。
func (r *Runner) retryStale(item *store.ImportItem, in matching.Input, err error) string {
	if n := r.noteStale(item.ID); n > maxStaleRetries {
		r.store.RecordFailure(item.ID, err.Error(), r.retry.Backoff(item.AttemptCount+1, err), r.retry.MaxAttempts)
		return matching.ErrorLogLine(err)
	} else if retryErr := r.store.RetryItemAfter(item.ID, time.Duration(n)*staleRetryStep); retryErr != nil {
		return matching.ErrorLogLine(retryErr)
	}
	return "[重新核验] " + in.Title
}

func (r *Runner) noteStale(itemID int64) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stale == nil {
		r.stale = map[int64]int{}
	}
	r.stale[itemID]++
	return r.stale[itemID]
}

func (r *Runner) clearStale(itemID int64) {
	r.mu.Lock()
	delete(r.stale, itemID)
	r.mu.Unlock()
}

// scoredFor 取选中候选自己的评分记录，作为已拥有判定的唯一旁证。
func scoredFor(scored []matching.ScoredCandidate, c provider.Candidate) []matching.ScoredCandidate {
	for _, sc := range scored {
		if sc.Candidate.Provider == c.Provider && sc.Candidate.MatchRef == c.MatchRef && deref(sc.Candidate.ReleaseRef) == deref(c.ReleaseRef) {
			return []matching.ScoredCandidate{sc}
		}
	}
	return []matching.ScoredCandidate{{Candidate: c}}
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
