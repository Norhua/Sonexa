package app

import (
	"encoding/json"
	"fmt"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// ReviewItem 是复核队列中的一个条目。
//
// 待选、匹配中、失败只是状态标签，界面结构保持一致——
// 不按进入原因分成结构不同的多种界面。
type ReviewItem struct {
	ReviewReason   *string `json:"reviewReason"`
	ReviewEvidence *string `json:"reviewEvidence"`
	Revision       int64   `json:"revision"`
	ID             int64   `json:"id"`
	BatchID        int64   `json:"batchId"`
	Status         string  `json:"status"`
	// 原始导入文本，上半区展示；改搜不改动它
	InTitle      *string               `json:"inTitle"`
	InArtist     *string               `json:"inArtist"`
	InAlbum      *string               `json:"inAlbum"`
	BatchTag     string                `json:"batchTag"`
	RowIndex     int                   `json:"rowIndex"`
	RawRow       string                `json:"rawRow"`
	InYear       *int                  `json:"inYear"`
	InTrackNo    *int                  `json:"inTrackNo"`
	InDiscNo     *int                  `json:"inDiscNo"`
	InDurationMS *int                  `json:"inDurationMs"`
	Profile      matching.InputProfile `json:"profile"`

	// TriedVariants 供「已尝试变体」提示行。
	TriedVariants []string `json:"triedVariants"`
	// Candidates 是候选快照。
	Candidates []CandidateRow `json:"candidates"`

	// 失败状态的状态说明
	AttemptCount int     `json:"attemptCount"`
	LastError    *string `json:"lastError"`
	// LastErrorAt 是 RFC3339 时间串；不用 time.Time，理由见 AlbumDetail.FetchedAt。
	LastErrorAt *string `json:"lastErrorAt"`
}

// ReviewQueue 是复核队列的整体状态。
type ReviewQueue struct {
	Items []ReviewItem `json:"items"`
	// Total 是待处置总数；Pending / Failed 是分项计数。
	Total   int `json:"total"`
	Pending int `json:"pending"`
	Failed  int `json:"failed"`
}

// GetReviewQueue 返回复核队列。
func (a *App) GetReviewQueue() (*ReviewQueue, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	items, err := a.store.AllReviewItems()
	if err != nil {
		return nil, err
	}

	out := &ReviewQueue{Items: []ReviewItem{}, Total: len(items)}
	for _, it := range items {
		r := a.reviewSource(it)
		if b, _ := a.store.GetBatch(it.BatchID); b != nil {
			r.BatchTag = b.Filename
		}

		switch it.Status {
		case store.StatusFailed:
			out.Failed++
		default:
			out.Pending++
		}
		out.Items = append(out.Items, r)
	}
	return out, nil
}

func (a *App) reviewSource(it store.ImportItem) ReviewItem {
	r := ReviewItem{
		ID: it.ID, BatchID: it.BatchID, Revision: it.Revision, Status: string(it.Status),
		ReviewReason: it.ReviewReason, ReviewEvidence: it.ReviewEvidence,
		InTitle: it.InTitle, InArtist: it.InArtist, InAlbum: it.InAlbum,
		RowIndex: it.RowIndex, RawRow: it.RawRow, Profile: store.ItemProfile(&it),
		InYear: it.InYear, InTrackNo: it.InTrackNo, InDiscNo: it.InDiscNo, InDurationMS: it.InDurationMS,
		AttemptCount: it.AttemptCount, LastError: it.LastError, LastErrorAt: formatTime(it.LastErrorAt),
		Candidates: []CandidateRow{}, TriedVariants: []string{},
	}
	if it.TriedVariants != nil {
		json.Unmarshal([]byte(*it.TriedVariants), &r.TriedVariants)
	}
	if it.Candidates != nil {
		r.Candidates = a.parseCandidateSnapshot(*it.Candidates)
	}
	if r.Candidates == nil {
		r.Candidates = []CandidateRow{}
	}
	return r
}

// parseCandidateSnapshot 只读取来源候选事实；本地关联和旧动作不能赋予收藏权限。
func (a *App) parseCandidateSnapshot(raw string) []CandidateRow {
	if facts, err := store.DecodeReviewCandidates(raw); err == nil {
		return a.rowsFrom(facts.Candidates, matching.CandidateAccept)
	}
	// 先按管线的评分候选解析
	var scored []matching.ScoredCandidate
	if err := json.Unmarshal([]byte(raw), &scored); err == nil && len(scored) > 0 && scored[0].Candidate.MatchRef != "" {
		// 复核队列的候选必须与上游搜索一样做本地判定，
		// 否则库里已有的那首不会显示双徽章、动作也不会变成「跳过」。
		return a.rowsFrom(scored, matching.CandidateAccept)
	}

	// 改搜保存 CandidateRow；仅还原事实，绝不沿用存储时的所有权和动作。
	var stored []CandidateRow
	if err := json.Unmarshal([]byte(raw), &stored); err == nil &&
		len(stored) > 0 && stored[0].MatchRef != "" {
		// 本地判定不随快照持久化：收藏状态可能在存快照之后变了
		for i := range stored {
			c := stored[i].toCandidate()
			a.applyLocalMatch(&stored[i], c)
		}
		return stored
	}

	return nil
}

// ResearchReviewItem 以改写后的条件重新搜索。
//
// 改写只影响本次查询，不改动条目保存的原始导入值。
func (a *App) ResearchReviewItem(itemID int64, title, artist, album, providerID, country string) ([]CandidateRow, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}

	item, err := a.store.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.Status.IsTerminal() {
		return nil, store.ErrPlanStale
	}
	scope, err := a.singleReviewProblemScope(itemID)
	if err != nil {
		return nil, err
	}
	return a.ResearchReviewProblem(scope, store.ReviewQuery{Version: store.ReviewProtocolVersion, Title: title, Artist: artist, Album: album, Provider: providerID, Country: country})
}

// applyLocalMatch 只按来源验证的精确 track 和当前发行关系查询所有权。
// 错误保留独立结果，不能伪装为未收藏；不请求上游。
func (a *App) applyLocalMatch(row *CandidateRow, c provider.Candidate) {
	// 从来源发行重新计算，绝不信任旧快照或客户端提供的本地封面 ID。
	row.ReleaseID, row.CoverRevision, row.CoverError = 0, 0, ""
	if c.ReleaseRef != nil && *c.ReleaseRef != "" {
		releaseID, revision, err := a.store.CandidateCoverTarget(string(c.Provider), *c.ReleaseRef)
		if err != nil {
			row.CoverError = "候选封面归属查询失败，请重试"
		} else {
			row.ReleaseID, row.CoverRevision = releaseID, revision
		}
	}
	a.mu.Lock()
	sources := a.identitySources
	a.mu.Unlock()
	local, _ := matching.ResolveLocal(a.store, sources, c)
	row.Ownership = local
	row.Badges = local.Badges(c.Provider)
	row.Action = local.Action()
	row.Note = local.Note()
	row.LocalTrackID = local.TrackID
}

func sortRowsByConfidence(rows []CandidateRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j-1].Confidence < rows[j].Confidence; j-- {
			rows[j-1], rows[j] = rows[j], rows[j-1]
		}
	}
}

// AcceptCandidate 采用某条上游候选入库并写入溯源。
//
// 精确候选无需额外请求；未解析候选先经来源能力定位真实 track。
// 若存在多个位置或资料不足，此兼容入口返回错误，交互界面通过
// PrepareAdmission 的 resolution 查看具体候选，不能按首项入库。
// 参与者等完整详情仍由后台补全，不借导入文本填充来源快照。
func (a *App) AcceptCandidate(itemID int64, row CandidateRow) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	plan, err := a.PrepareAdmission(row, itemID)
	if err != nil {
		return 0, err
	}
	return a.commitUnconfirmedAdmission(plan)
}

// NoMatchNeeded 按原始输入入库，丰富信息为空，溯源置空。
//
// 溯源为空即「无需匹配」：这条曲目不进补全任务的工作集
// （见 store.InitialEnrichState），因此界面上它既不是「未匹配」也不算待办。
func (a *App) NoMatchNeeded(itemID int64) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}

	plan, err := a.PrepareNoMatchNeeded(itemID)
	if err != nil {
		return 0, err
	}
	return a.commitUnconfirmedAdmission(plan)
}

// DiscardReviewItem 丢弃条目且不入库。
func (a *App) DiscardReviewItem(itemID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	scope, err := a.store.ItemProblemScope(itemID)
	if err != nil {
		return err
	}
	return a.store.DiscardProblemMembers(scope, []int64{itemID})
}

// NoneMatched 不接受当前结果但把条目保留在队列中。
//
// 区别于「无需匹配」：条目仍留在队列中可换上游再试，而不是立即入库并结束。
func (a *App) NoneMatched(itemID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	scope, err := a.singleReviewProblemScope(itemID)
	if err != nil {
		return err
	}
	return a.store.NoneMatchedProblem(scope)
}

// RetryReviewItem 让失败条目重新进入匹配。
func (a *App) RetryReviewItem(itemID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	scope, err := a.store.ItemProblemScope(itemID)
	if err != nil {
		return err
	}
	return a.RetryReviewProblemMembers(scope, []int64{itemID})
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
