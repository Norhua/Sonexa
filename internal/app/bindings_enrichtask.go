package app

import (
	"context"
	"fmt"
)

// GetEnrichStatus 返回补全信息匹配的当前状态。
//
// 界面初次渲染时读一次，之后由 EventEnrichProgress 推送——
// 与导入进度同一个道理：几百条的任务上轮询只会产生无谓的 IPC。
func (a *App) GetEnrichStatus() (*EnrichStatus, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if a.enricher == nil {
		return &EnrichStatus{State: enrichIdle}, nil
	}
	s := a.enricher.Status()
	return &s, nil
}

// StartEnrich 开始（或从暂停处恢复）补全信息匹配。
//
// 用户点「开始」意味着「再试一次」，因此重试计数清零：
// 连续失败达上限而退出工作集的那几条会被重新试一遍。
func (a *App) StartEnrich() error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.enricher == nil {
		return fmt.Errorf("补全任务未就绪")
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return fmt.Errorf("%s", gate.Reason)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.enricher.Start(ctx)
}

// PauseEnrich 暂停补全信息匹配。已取得的数据保留。
//
// 暂停是**用户的决定**：此后即使空闲下来也不自动开始，
// 直到他再次点「开始」。
func (a *App) PauseEnrich() error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.enricher == nil {
		return nil
	}
	a.enricher.Pause()
	return nil
}
