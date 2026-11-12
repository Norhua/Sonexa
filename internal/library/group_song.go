package library

import (
	"context"
	"errors"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// fetchGroupTracklists 让组内指定来源、有曲目引用而清单还不完整的发行各取一次详情并保存。
//
// 清单默认懒加载，不补取的话组内另一个发行没打开过专辑页，覆盖结论就恒为「不可判定」。
// 每个发行成功一次后即为 complete，上游响应也有缓存；不同来源的发行不在这里补取。
// except 是不需要补取的发行（通常是这次准入的目标发行），传 0 表示不排除。
// 请求失败原样返回，由调用方按请求失败处理；取回后仍不完整的保持不可判定。
func (s *Service) fetchGroupTracklists(ctx context.Context, groupID int64, source domain.ProviderID, except int64) (bool, error) {
	if groupID == 0 || source == "" {
		return false, nil
	}
	gaps, err := s.store.GroupTracklistGaps(groupID, source, except)
	if err != nil || len(gaps) == 0 {
		return false, err
	}
	p, err := s.provider(source)
	if err != nil {
		return false, nil
	}
	fetched := false
	for _, gap := range gaps {
		snapshot, err := p.Complete(ctx, domain.DetailTarget{Release: gap.Identity})
		if errors.Is(err, provider.ErrUnsupported) {
			continue
		}
		if err != nil {
			return fetched, fmt.Errorf("补取发行清单失败: %w", err)
		}
		if snapshot == nil || snapshot.Release == nil || snapshot.Release.Identity != gap.Identity {
			return fetched, store.ErrSnapshotTarget
		}
		// 只保存发行层事实；曲目层由各自的补全负责。
		if err := s.store.ApplyReleaseSnapshot(gap.ReleaseID, &domain.Snapshot{Release: snapshot.Release, Group: snapshot.Group}, gap.Revision); err != nil {
			return fetched, err
		}
		fetched = true
	}
	return fetched, nil
}

// providerSeed 用组内已有发行的清单条目构造准入种子（与灰色行准入同一构造），
// 保留原计划的导入条目、条目终态与自动导入标记。提供者是没有上游身份的本地曲目时
// 直接复用该曲目。
func (s *Service) providerSeed(base domain.AdmissionSeed, p domain.SongProvider) (domain.AdmissionSeed, error) {
	seed := domain.AdmissionSeed{ImportItemID: base.ImportItemID, AutomaticImport: base.AutomaticImport, ItemStatus: base.ItemStatus}
	if p.Identity == nil {
		if p.TrackID == 0 {
			return seed, fmt.Errorf("该发行没有可复用的曲目")
		}
		seed.ReuseTrackID = p.TrackID
		seed.Snapshot = domain.Snapshot{Track: &domain.TrackSnapshot{}}
		return seed, nil
	}
	if p.TrackRef == "" {
		return seed, fmt.Errorf("已有发行的这条清单没有曲目标识，无法直接使用")
	}
	release, err := s.store.GetReleaseSnapshot(p.ReleaseID)
	if err != nil {
		return seed, err
	}
	if release == nil || release.Tracklist.State != domain.Value || release.Identity != *p.Identity {
		return seed, store.ErrPlanStale
	}
	for _, entry := range release.Tracklist.Value {
		if entry.Track.State != domain.Value || entry.Track.Value.Ref != p.TrackRef {
			continue
		}
		snapshot, err := tracklistSnapshot(release, entry)
		if err != nil {
			return seed, err
		}
		seed.Snapshot = snapshot
		return seed, nil
	}
	return seed, store.ErrPlanStale
}

// tracklistSnapshot 从正式发行快照的一条清单构造曲目，不接受客户端拼装的上下文。
func tracklistSnapshot(release *domain.ReleaseSnapshot, entry domain.TracklistEntry) (domain.Snapshot, error) {
	if entry.Track.State != domain.Value || entry.Track.Value.Ref == "" || entry.Track.Value.Provider != release.Identity.Provider {
		return domain.Snapshot{}, store.ErrSnapshotTarget
	}
	track := &domain.TrackSnapshot{Identity: entry.Track.Value, Release: &release.Identity, Title: entry.Title, Credit: entry.Credit, DurationMS: entry.DurationMS, Recording: entry.Recording, Identifiers: entry.Identifiers, DiscNumber: domain.Known(entry.DiscNumber), TrackNumber: domain.Known(entry.TrackNumber)}
	return domain.Snapshot{Release: release, Track: track}, nil
}

// primaryProvider 取主发行里与这首歌对应的那一条清单。
//
// 提供者列表已按主发行在前排序。主发行内恰有一条就用它；同一录音出现在多个位置时
// 取碟号、音轨号与候选相同的那条，取不到则给不出唯一结论。
func primaryProvider(providers []domain.SongProvider, disc, position *int) *domain.SongProvider {
	if len(providers) == 0 {
		return nil
	}
	var same []domain.SongProvider
	for _, p := range providers {
		if p.ReleaseID == providers[0].ReleaseID && p.Evidence == providers[0].Evidence {
			same = append(same, p)
		}
	}
	if len(same) == 1 {
		return &same[0]
	}
	if disc == nil || position == nil {
		return nil
	}
	var hit *domain.SongProvider
	for i := range same {
		if same[i].DiscNumber == *disc && same[i].TrackNumber == *position {
			if hit != nil {
				return nil
			}
			hit = &same[i]
		}
	}
	return hit
}

// resolveGroupSong 是自动路径的组内歌曲解析：候选所在发行并非主发行，而组内已有
// 发行能提供这首歌时，改为准入已有发行的那条清单；无法确认时给出复核原因。
//
// 自动决策只接受录音级相同。文本级相同（曲名、时长、署名）只是启发式证据：当前
// 自动导入默认只使用 MusicBrainz，它为每条曲目提供可追溯的录音标识，所以这里用不到
// 文本级。将来若实现多来源自动导入（例如 MusicBrainz 无结果时自动切换其他上游搜索），
// 其他来源没有录音标识，届时必须先实现判断文本级可信度的逻辑，明确哪些文本级相同
// 可以自动入库，再放宽这条限制。
func (s *Service) resolveGroupSong(ctx context.Context, selection Selection, seed domain.AdmissionSeed) (Selection, error) {
	plan := selection.Plan
	source := seed.Snapshot.Track.Identity.Provider
	reprepare := func() error {
		next, err := s.PrepareAdmission(ctx, seed)
		if err != nil {
			return err
		}
		plan, selection.Plan = next, next
		return nil
	}
	if plan.Baseline.Coverage == domain.CoverageNotApplicable || plan.Baseline.TargetGroupID == 0 {
		return selection, nil
	}
	// 1) 组内同来源、有曲目引用的其他发行都要有完整清单，否则给不出结论。
	if len(plan.Baseline.Undecidable) > 0 {
		fetched, err := s.fetchGroupTracklists(ctx, plan.Baseline.TargetGroupID, source, plan.Baseline.ExistingReleaseID)
		if err != nil {
			return selection, err
		}
		if fetched {
			if err := reprepare(); err != nil {
				return selection, err
			}
		}
	}
	if !plan.Baseline.NeedsTypedConfirm {
		return selection, nil
	}
	// 2) 候选所在发行已在库而不是主发行：先补齐它自己的清单再比较，主发行排序
	//    才不会因为「一侧清单没加载、总曲数为零」而偏向另一侧。
	if plan.Baseline.Coverage == domain.CoverageProvided && plan.Baseline.ExistingReleaseID != 0 {
		fetched, err := s.fetchGroupTracklists(ctx, plan.Baseline.TargetGroupID, source, 0)
		if err != nil {
			return selection, err
		}
		if fetched {
			if err := reprepare(); err != nil {
				return selection, err
			}
			if !plan.Baseline.NeedsTypedConfirm {
				return selection, nil
			}
		}
	}
	selection.Reason = ReasonSongUnresolved
	if plan.Baseline.Coverage != domain.CoverageProvided || plan.Baseline.Conflict {
		// 疑似（文本级）或不可判定：交给人工，证据里带着疑似提供者与不可判定的发行。
		return selection, nil
	}
	chosen := primaryProvider(plan.Baseline.Providers, selection.Candidate.DiscNumber, selection.Candidate.TrackNumber)
	if chosen == nil || chosen.Evidence != store.CoverageEvidenceRecording || chosen.Identity == nil || chosen.TrackRef == "" {
		return selection, nil
	}
	next, err := s.providerSeed(seed, *chosen)
	if err != nil {
		if errors.Is(err, store.ErrPlanStale) || errors.Is(err, store.ErrSnapshotTarget) {
			return selection, nil
		}
		return selection, err
	}
	substituted, err := s.PrepareAdmission(ctx, next)
	if err != nil {
		return selection, err
	}
	// 替换后仍需要任何人工确认（组冲突、新增成员、另一个更靠前的提供者）就不自动提交，
	// 复核证据保留候选原本所在发行的计划，便于看清疑似提供者。
	if substituted.Baseline.Conflict || substituted.Baseline.RequiresConfirmation || substituted.Baseline.NeedsTypedConfirm {
		return selection, nil
	}
	selection.Plan, selection.Substitute, selection.Reason = substituted, chosen, ""
	return selection, nil
}

// UseSongProvider 是交互路径的「使用发行 A」：基于当前计划重新准备，把曲目换成
// 指定发行里的提供者条目，保留导入条目与条目终态。不替用户静默改选，只在用户点击时执行。
func (s *Service) UseSongProvider(ctx context.Context, id, hash string, releaseID int64) (*domain.AdmissionPlan, error) {
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	if plan.Seed.EditJSON != "" || plan.Seed.MoveReleaseID != 0 {
		return nil, fmt.Errorf("当前操作不能改用其他发行，请取消后在目标发行中操作")
	}
	var inRelease []domain.SongProvider
	for _, p := range plan.Baseline.Providers {
		if p.ReleaseID == releaseID {
			inRelease = append(inRelease, p)
		}
	}
	track := plan.Seed.Snapshot.Track
	var disc, position *int
	if track != nil && track.DiscNumber.State == domain.Value && track.TrackNumber.State == domain.Value {
		disc, position = &track.DiscNumber.Value, &track.TrackNumber.Value
	}
	chosen := primaryProvider(inRelease, disc, position)
	if chosen == nil {
		if len(inRelease) == 0 {
			return nil, store.ErrPlanStale
		}
		// 同一首歌在该发行里出现多次且位置对不上：取清单里靠前的一条。
		chosen = &inRelease[0]
	}
	seed, err := s.providerSeed(plan.Seed, *chosen)
	if err != nil {
		return nil, err
	}
	seed.AutomaticImport = false
	return s.PrepareAdmission(ctx, seed)
}

// RefreshAdmissionCoverage 为计划里不可判定的发行各取一次清单并保存，然后重新准备计划。
// 这是显式的交互式网络请求：调用方负责出站闸门与请求优先级。
func (s *Service) RefreshAdmissionCoverage(ctx context.Context, id, hash string) (*domain.AdmissionPlan, error) {
	plan, err := s.plan(id, hash)
	if err != nil {
		return nil, err
	}
	sources := map[domain.ProviderID]bool{}
	for _, u := range plan.Baseline.Undecidable {
		if u.Identity != nil {
			sources[u.Identity.Provider] = true
		}
	}
	for source := range sources {
		if _, err := s.fetchGroupTracklists(ctx, plan.Baseline.TargetGroupID, source, plan.Baseline.ExistingReleaseID); err != nil {
			return nil, err
		}
	}
	return s.PrepareAdmission(ctx, plan.Seed)
}

// ListReleaseMergeProposals 列出「同组两个发行曲目相同」的提议，只供查看与忽略。
func (s *Service) ListReleaseMergeProposals() ([]store.ReleaseMergeProposal, error) {
	return s.store.ListReleaseMergeProposals()
}
func (s *Service) DismissReleaseMergeProposal(id int64) error {
	return s.store.DismissReleaseMergeProposal(id)
}
