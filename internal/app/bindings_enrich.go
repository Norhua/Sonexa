package app

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// applyDetailToTrack 把一次详情拉取的结果落到某首曲目上。
//
// 这是**唯一**把上游详情转为本地数据的地方：丰富信息、参与者关系、
// 可点击区间、清单都在这里写入。此前这些写入散落且从未被调用——
// 结果是艺术家子系统在生产路径上完全没跑起来，而单元测试各自都通过。
func (a *App) applyDetailToTrack(
	ctx context.Context,
	trackID int64,
	providerID string,
	d *provider.Detail,
) error {
	if d == nil {
		return fmt.Errorf("详情为空，无法补全曲目")
	}
	revision, err := a.store.TrackRevision(trackID)
	if err != nil {
		return err
	}
	return a.store.ApplySnapshot(trackID, provider.DetailSnapshot(d), revision)
}

// RefreshTrackDetail 为已入库曲目立即拉取并落库详情。
//
// 供编辑页的「更换」与用户显式的「重新获取」使用：**用户在等**，
// 因此走交互式优先级，插到后台补全的队列之前。
//
// 复核队列的「接受」不再走这里：那一次请求的产出与「这条曲目能不能进库」
// 无关，而在 1 req/s 之下它就是 1–2 秒的卡顿。接受只入库，
// 详情交给后台的补全信息匹配（见 enrichWorker）。
func (a *App) RefreshTrackDetail(trackID int64, providerID, releaseRef string) error {
	if err := a.ready(); err != nil {
		return err
	}
	done := a.beginInteractive()
	defer done()

	ctx := provider.WithPriority(context.Background(), provider.PriorityInteractive)
	if err := a.fetchAndApplyDetail(ctx, store.EnrichTarget{
		TrackID: trackID, Provider: providerID, ReleaseRef: releaseRef,
	}); err != nil {
		return err
	}
	if err := a.store.SetEnrichState(trackID, store.EnrichFetched); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// enrichOne 是后台补全任务处理一条曲目的入口。
//
// 与 RefreshTrackDetail 的差别只有两处：优先级为后台，
// 且不逐条通知界面重载（那由任务在一轮结束时做一次）。
// 状态由任务写，因为只有它知道失败该记第几次。
func (a *App) enrichOne(ctx context.Context, target store.EnrichTarget) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.fetchAndApplyDetail(provider.WithPriority(ctx, provider.PriorityBackground), target)
}

// fetchAndApplyDetail 取一次详情并落到某条曲目上。
//
// 两条路径（用户显式刷新、后台补全）共用它：出网前置检查、缺标识的判断、
// 错误包装都只写一份，否则两边的行为会慢慢漂移。
func (a *App) fetchAndApplyDetail(ctx context.Context, target store.EnrichTarget) error {
	if gate := a.CheckOutbound(); !gate.Allowed {
		return fmt.Errorf("%s", gate.Reason)
	}
	if target.ReleaseRef == "" {
		return fmt.Errorf("缺少发行标识，无法拉取详情")
	}

	// 在共用入口从当前记录读取身份，而不是只依赖排队时的发行快照。
	// 后台、手动刷新与导入时立即补全均经过这里，不能漏接某一条路径。
	meta, err := a.store.GetMetadata(target.TrackID)
	if err != nil {
		return err
	}
	if meta == nil || deref(meta.MatchRef) == "" {
		return fmt.Errorf("缺少曲目标识，无法定位详情")
	}
	if deref(meta.MatchSource) != target.Provider || deref(meta.ReleaseRef) != target.ReleaseRef {
		return fmt.Errorf("曲目的上游来源或所属发行已改变，取消本次补全")
	}
	trackRef := *meta.MatchRef
	revision, err := a.store.TrackRevision(target.TrackID)
	if err != nil {
		return err
	}

	p, err := a.registry.Get(provider.ProviderID(target.Provider))
	if err != nil {
		return err
	}
	identity := domain.Identity{Provider: provider.ProviderID(target.Provider), Ref: trackRef}
	snapshot, err := p.Complete(ctx, domain.DetailTarget{Release: domain.Identity{Provider: identity.Provider, Ref: target.ReleaseRef}, Track: &identity})
	if err != nil {
		return fmt.Errorf("拉取详情失败: %w", err)
	}
	if snapshot == nil || snapshot.Track == nil || snapshot.Track.Identity != identity {
		return fmt.Errorf("详情未匹配目标曲目，拒绝写入")
	}

	// 请求在途时可能发生重新匹配/移动歌集；迟到的旧详情不能覆盖新身份。
	current, err := a.store.GetMetadata(target.TrackID)
	if err != nil {
		return err
	}
	if current == nil || deref(current.MatchSource) != target.Provider || deref(current.MatchRef) != trackRef || deref(current.ReleaseRef) != target.ReleaseRef {
		return fmt.Errorf("曲目身份在拉取期间已改变，取消本次补全")
	}
	return a.store.ApplySnapshot(target.TrackID, snapshot, revision)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
