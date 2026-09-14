package main

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

// EnrichStatus 是「补全信息匹配」任务的状态快照。
//
// Done / Total 是**全库**口径而不是本轮口径：`已匹配 / (已匹配 + 未匹配)`。
// 本轮口径在这里读起来是错的——用户接受一条候选、任务跑起来、他再接受一条，
// 本轮的分母就变了，界面上那个数字会往回跳。全库口径的分母只随入库变动，
// 而每补完一条，分子加一，分母不变。
//
// 无需匹配的曲目不在这两个数里：它们不需要补，算进分母只会让它永远到不了头。
type EnrichStatus struct {
	// State 为 idle | running | waiting | paused | stalled。
	State string `json:"state"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
	// Pending / Skipped 是分项，供界面在需要时细分显示。
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`
	// Reason 说明 waiting 是在等什么（导入在跑 / 用户在检索）。
	Reason string `json:"reason"`
	// LastError 是最近一次补全失败的原因。
	LastError string `json:"lastError"`
}

// 任务状态取值。
const (
	enrichIdle    = "idle"
	enrichRunning = "running"
	enrichWaiting = "waiting"
	enrichPaused  = "paused"
	enrichStalled = "stalled"
)

// maxConsecutiveEnrichFailures 是连续失败几次就停下并保留进度。
//
// 上游持续不可用时接着跑只会把同一条错误刷满日志，而每一条都要占一个
// 1 req/s 的名额。停下来并把情况说清楚，比无声地失败几百次好。
const maxConsecutiveEnrichFailures = 5

// enrichLetPassInterval 是让路期间的复查间隔。
//
// 轮询而不是等信号：要让的路有两种（导入在跑、用户的检索在飞），
// 它们本来就不是同一个信号源，而 400ms 的复查在这个量级上是免费的。
const enrichLetPassInterval = 400 * time.Millisecond

// enrichWorker 是后台的「补全信息匹配」任务。
//
// 它的存在理由是把出网从「接受候选」那一步挪走：搜索响应里的候选已经带着
// 完整身份与 track/medium 层字段，够写成曲库里合法的一行；发行层事实
// （参与者、作曲作词、厂牌、目录号、风格）由这个任务按 release 补一次。
// 先前那次请求同步发在「接受」里，在 1 req/s 之下就是 1–2 秒的卡顿，
// 而它的产出与「这条曲目能不能进库」无关。
//
// 与导入匹配任务分开：两者目标不同（一个找身份、一个补细节），
// 用户也会想分别控制它们的启停。
type enrichWorker struct {
	store *store.Store
	// enrich 取详情并落到一条曲目上。由绑定层注入（背景优先级）。
	enrich func(ctx context.Context, target store.EnrichTarget) error
	// busy 报告此刻是否该让路：导入在跑，或用户触发的出网请求在飞。
	busy func() (bool, string)
	// emit 把状态推给前端；done 为真表示本轮跑完了。
	emit func(EnrichStatus)
	// onRunFinished 在一轮结束后调用，用于刷新界面上的曲目列表。
	// 逐条刷新会让 200 条的补全触发 200 次整表重载。
	onRunFinished func()

	mu sync.Mutex
	// state 与 reason 只在这里改，读取一律经 Status()。
	state  string
	reason string
	// userPaused 为真时自动启动不生效——用户按了暂停就是暂停。
	userPaused bool
	// active 是「循环 goroutine 还在」的真相，与展示用的 state 分开。
	// 合成一个字段的话，Pause() 恰好落在循环收尾那一刻时，
	// 状态会永远停在 paused 而没有任何东西在跑。
	active    bool
	lastError string
	cancel    context.CancelFunc
}

func newEnrichWorker(st *store.Store) *enrichWorker {
	return &enrichWorker{store: st, state: enrichIdle}
}

// Status 返回当前状态快照，计数现算。
func (w *enrichWorker) Status() EnrichStatus {
	w.mu.Lock()
	s := EnrichStatus{State: w.state, Reason: w.reason, LastError: w.lastError}
	w.mu.Unlock()

	if w.store != nil {
		if counts, err := w.store.EnrichStats(); err == nil {
			s.Done = counts.Matched
			s.Pending = counts.Pending
			s.Skipped = counts.Skipped
			s.Total = counts.Matched + counts.Pending
		}
	}
	return s
}

// Start 启动（或恢复）补全任务。
//
// 用户点「开始」时调用：重试计数清零——那是他在说「再试一次」，
// 而达到上限退出工作集的那几条必须有一条回来的路。
func (w *enrichWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	w.userPaused = false
	active := w.active
	w.mu.Unlock()

	if active {
		// 循环还在（此前只是暂停着）：放它继续，不叠第二个
		w.setState(enrichRunning, "")
		return nil
	}
	if w.store != nil {
		if err := w.store.ResetEnrichAttempts(); err != nil {
			return err
		}
	}
	return w.launch(ctx)
}

// AutoStart 在空闲时自动开始，用户主动暂停过则不动。
//
// 调用点是「刚出现新的待补条目」与「刚空闲下来」：导入跑完、接受一条候选、
// 应用启动。它不清零重试计数——自动重试一条一直失败的曲目只会白烧配额。
func (w *enrichWorker) AutoStart(ctx context.Context) {
	w.mu.Lock()
	blocked := w.userPaused || w.active
	w.mu.Unlock()
	if blocked {
		return
	}
	_ = w.launch(ctx)
}

// launch 枚举工作集并起一个循环。工作集为空时不进入运行态。
//
// 枚举在这里**同步**做，而不是留给 goroutine：调用方需要当场知道
// 「有没有要做的事」，否则界面会先闪一下 0/0 再跳成别的数。
func (w *enrichWorker) launch(ctx context.Context) error {
	targets, err := w.collect()
	if err != nil {
		w.setError(err.Error())
		return err
	}
	if len(targets) == 0 {
		w.setState(enrichIdle, "")
		w.publish()
		return nil
	}

	runCtx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.cancel = cancel
	w.active = true
	w.state = enrichRunning
	w.reason = ""
	w.mu.Unlock()

	go w.run(runCtx, targets)
	w.publish()
	return nil
}

// Pause 暂停补全，已取得的数据保留。
func (w *enrichWorker) Pause() {
	w.mu.Lock()
	w.userPaused = true
	if w.state == enrichRunning || w.state == enrichWaiting {
		w.state = enrichPaused
		w.reason = ""
	}
	w.mu.Unlock()
	w.publish()
}

// Stop 停止补全（应用退出时用）。
func (w *enrichWorker) Stop() {
	w.mu.Lock()
	cancel := w.cancel
	w.cancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (w *enrichWorker) collect() ([]store.EnrichTarget, error) {
	if w.store == nil {
		return nil, nil
	}
	return w.store.TracksNeedingEnrichment(0)
}

// run 逐条推进工作集，跑完再取一次——期间用户可能又接受了几条候选。
func (w *enrichWorker) run(ctx context.Context, targets []store.EnrichTarget) {
	defer func() {
		w.mu.Lock()
		w.active = false
		if w.state != enrichStalled {
			w.state = enrichIdle
			w.reason = ""
		}
		w.cancel = nil
		w.mu.Unlock()
		w.publish()
		if w.onRunFinished != nil {
			w.onRunFinished()
		}
	}()

	consecutiveFailures := 0
	for len(targets) > 0 {
		for _, target := range targets {
			if err := w.waitTurn(ctx); err != nil {
				return
			}
			if !w.step(ctx, target, &consecutiveFailures) {
				return
			}
		}

		next, err := w.collect()
		if err != nil {
			w.setError(err.Error())
			return
		}
		targets = next
	}
}

// step 推进一条曲目。返回 false 表示整个任务该停下了。
func (w *enrichWorker) step(ctx context.Context, target store.EnrichTarget, consecutive *int) bool {
	err := w.enrich(ctx, target)
	switch {
	case err == nil:
		*consecutive = 0
		_ = w.store.SetEnrichState(target.TrackID, store.EnrichFetched)
		w.setError("")

	case errors.Is(err, context.Canceled), ctx.Err() != nil:
		return false

	default:
		*consecutive++
		_ = w.store.RecordEnrichFailure(target.TrackID, err.Error())
		w.setError(err.Error())
		if *consecutive >= maxConsecutiveEnrichFailures {
			w.mu.Lock()
			w.state = enrichStalled
			w.reason = "上游持续不可用，补全已停下并保留进度"
			w.mu.Unlock()
			w.publish()
			return false
		}
	}
	w.publish()
	return true
}

// waitTurn 在暂停或该让路时等待。
//
// 让路的两种情形（导入在跑、用户的检索在飞）都由 busy 报告。
// 令牌桶前面那道优先级闸门已经保证用户的请求插队，但插队只解决**次序**：
// 后台任务照样在占用同一个 1 req/s 的配额，用户那一次搜索仍要多等一轮。
// 因此这里整体让路，而不是只靠闸门。
func (w *enrichWorker) waitTurn(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		w.mu.Lock()
		paused := w.userPaused
		w.mu.Unlock()

		busy, why := false, ""
		if w.busy != nil {
			busy, why = w.busy()
		}
		if !paused && !busy {
			w.setState(enrichRunning, "")
			return nil
		}
		if paused {
			w.setState(enrichPaused, "")
		} else {
			w.setState(enrichWaiting, why)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(enrichLetPassInterval):
		}
	}
}

// setState 只在状态真的变了时推送，免得让路期间每 400ms 发一次事件。
func (w *enrichWorker) setState(state, reason string) {
	w.mu.Lock()
	changed := w.state != state || w.reason != reason
	w.state, w.reason = state, reason
	w.mu.Unlock()
	if changed {
		w.publish()
	}
}

func (w *enrichWorker) setError(msg string) {
	w.mu.Lock()
	w.lastError = msg
	w.mu.Unlock()
}

func (w *enrichWorker) publish() {
	if w.emit != nil {
		w.emit(w.Status())
	}
}
