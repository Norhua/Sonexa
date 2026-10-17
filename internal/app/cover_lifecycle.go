package app

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/covers"
)

// coverWork registers the entire preview/adoption, including store access after
// download. Shutdown must not close the store in the gap after a pool operation.
// An empty searchID is an automatic image request, cancelled by the app lifetime.
func (a *App) coverWork(searchID string) (*covers.Service, *coverSession, context.Context, func(), error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.coverClosing || a.coverSvc == nil {
		return nil, nil, nil, nil, fmt.Errorf("封面服务不可用")
	}
	if a.coverLife == nil {
		base := a.ctx
		if base == nil {
			base = context.Background()
		}
		a.coverLife, a.cancelCoverWork = context.WithCancel(base)
	}
	ctx := a.coverLife
	var session *coverSession
	if searchID != "" {
		session = a.coverSessions[searchID]
		if session == nil || session.ctx.Err() != nil {
			return nil, nil, nil, nil, fmt.Errorf("封面搜索会话已结束")
		}
		ctx = session.ctx
	}
	a.coverWorkers.Add(1)
	return a.coverSvc, session, ctx, a.coverWorkers.Done, nil
}
