package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// ReviewItem 是复核队列中的一个条目。
//
// 待选、匹配中、失败只是状态标签，界面结构保持一致——
// 不按进入原因分成结构不同的多种界面。
type ReviewItem struct {
	ID      int64  `json:"id"`
	BatchID int64  `json:"batchId"`
	Status  string `json:"status"`
	// 原始导入文本，上半区展示；改搜不改动它
	InTitle  *string `json:"inTitle"`
	InArtist *string `json:"inArtist"`
	InAlbum  *string `json:"inAlbum"`
	BatchTag string  `json:"batchTag"`

	// TriedVariants 供「已尝试变体」提示行。
	TriedVariants []string `json:"triedVariants"`
	// Candidates 是候选快照。
	Candidates []CandidateRow `json:"candidates"`

	// 失败状态的状态说明
	AttemptCount int        `json:"attemptCount"`
	LastError    *string    `json:"lastError"`
	LastErrorAt  *time.Time `json:"lastErrorAt"`
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
		r := ReviewItem{
			ID: it.ID, BatchID: it.BatchID, Status: string(it.Status),
			InTitle: it.InTitle, InArtist: it.InArtist, InAlbum: it.InAlbum,
			AttemptCount: it.AttemptCount,
			LastError:    it.LastError, LastErrorAt: it.LastErrorAt,
		}
		if b, _ := a.store.GetBatch(it.BatchID); b != nil {
			r.BatchTag = b.Filename
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

// parseCandidateSnapshot 解析候选快照。
//
// 快照可能是管线写入的 ScoredCandidate 数组，
// 也可能是本地疑似重复写入的简化结构。
func (a *App) parseCandidateSnapshot(raw string) []CandidateRow {
	// 先按管线的评分候选解析
	var scored []matching.ScoredCandidate
	if err := json.Unmarshal([]byte(raw), &scored); err == nil && len(scored) > 0 && scored[0].Candidate.MatchRef != "" {
		out := make([]CandidateRow, 0, len(scored))
		for _, sc := range scored {
			c := sc.Candidate
			out = append(out, CandidateRow{
				Provider: string(c.Provider), MatchRef: c.MatchRef,
				// 复核队列的候选必须与上游搜索一样做本地判定，
				// 否则库里已有的那首不会显示双徽章、动作也不会变成「合并」。
				Badges: a.badgesFor(c), Action: a.actionFor(c), Note: a.noteFor(c),
				Title: c.Title, Artist: c.ArtistName, Album: c.AlbumTitle,
				PrimaryType: c.PrimaryType, DurationMS: c.DurationMS,
				TrackNumber: c.TrackNumber, TrackTotal: c.TrackTotal,
				Year: c.Year, MediumFormat: c.MediumFormat, Country: c.Country,
				Confidence: sc.Score.Total, MatchedVariant: sc.MatchedVariant,
				ReleaseRef: c.ReleaseRef, ReleaseGroupRef: c.ReleaseGroupRef,
				RecordingRef: c.RecordingRef, ISRC: c.ISRC,
			})
		}
		return out
	}

	// 再按本地候选解析
	var local []struct {
		Local   bool     `json:"local"`
		TrackID int64    `json:"trackId"`
		Title   string   `json:"title"`
		Artist  *string  `json:"artist"`
		Album   *string  `json:"album"`
		Action  string   `json:"action"`
		Badges  []string `json:"badges"`
	}
	if err := json.Unmarshal([]byte(raw), &local); err == nil {
		out := make([]CandidateRow, 0, len(local))
		for _, l := range local {
			title := l.Title
			out = append(out, CandidateRow{
				Badges: l.Badges, Action: l.Action, LocalTrackID: l.TrackID,
				Title: &title, Artist: l.Artist, Album: l.Album,
			})
		}
		return out
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

	p, err := a.registry.Get(provider.ProviderID(providerID))
	if err != nil {
		return nil, err
	}
	if err := p.IsConfigured(); err != nil {
		return nil, err
	}

	q := provider.Query{Title: title, Artist: artist, Album: album, Country: country, Limit: 25}
	candidates, err := p.SearchTracks(context.Background(), q)
	if err != nil {
		return nil, err
	}

	rows := a.toRows(candidates, q, "接受")
	// 结果按置信度排序
	sortRowsByConfidence(rows)

	// 把本次候选写入快照，但不动原始导入值
	snapshot, _ := json.Marshal(rows)
	item, err := a.store.GetItem(itemID)
	if err == nil && item != nil {
		variants, _ := json.Marshal([]string{title})
		a.store.MarkReview(itemID, string(snapshot), string(variants))
	}
	return rows, nil
}

// badgesFor 返回某条候选应显示的徽章。
//
// 库中已有且上游也存在时同时标注两个徽章；经 ISRC 桥接命中的标注
// 「经 ISRC 匹配」；文本模糊比对的疑似命中带问号。
func (a *App) badgesFor(c provider.Candidate) []string {
	local, err := matching.ResolveLocal(a.store, c)
	if err != nil || local.Kind == matching.LocalNone {
		return []string{string(c.Provider)}
	}
	return local.Badges(c.Provider)
}

// actionFor 返回某条候选应显示的动作。
func (a *App) actionFor(c provider.Candidate) string {
	local, err := matching.ResolveLocal(a.store, c)
	if err != nil || local.Kind == matching.LocalNone {
		return "接受"
	}
	return local.Action()
}

// noteFor 返回候选的附加说明（如「经 ISRC 匹配」）。
func (a *App) noteFor(c provider.Candidate) string {
	local, err := matching.ResolveLocal(a.store, c)
	if err != nil {
		return ""
	}
	return local.Note()
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
// 入库后补一次详情：search 的候选只有扁平字段，参与者关系与作曲作词
// 只有在详情里才有。补详情失败不回滚入库——曲目已经进来了，
// 缺的丰富信息可以之后在编辑页补。
func (a *App) AcceptCandidate(itemID int64, row CandidateRow) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	trackID, err := a.AddToLibrary(row)
	if err != nil {
		return 0, err
	}
	if err := a.store.MarkImported(itemID, trackID); err != nil {
		return 0, err
	}
	if row.ReleaseRef != nil && *row.ReleaseRef != "" {
		if err := a.RefreshTrackDetail(trackID, row.Provider, *row.ReleaseRef); err != nil {
			// 记下失败原因供界面显示，但不当作接受失败
			msg := err.Error()
			_ = a.store.RecordEnrichFailure(trackID, msg)
		}
	}
	return trackID, nil
}

// MergeCandidate 把条目标记为与库中已有曲目相同。
//
// 不新建曲目：标记合并目标后条目从队列中消失，库中曲目数不变。
func (a *App) MergeCandidate(itemID, targetTrackID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.MarkMerged(itemID, targetTrackID)
}

// NoMatchNeeded 按原始输入入库，丰富信息为空，溯源置空。
func (a *App) NoMatchNeeded(itemID int64) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}

	item, err := a.store.GetItem(itemID)
	if err != nil {
		return 0, err
	}
	if item == nil {
		return 0, fmt.Errorf("条目不存在")
	}
	if item.InTitle == nil || *item.InTitle == "" {
		return 0, fmt.Errorf("该条目没有曲名，无法入库")
	}

	trackID, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: *item.InTitle, Artist: item.InArtist, Album: item.InAlbum,
		// 溯源置空：不写 MatchSource / MatchRef
	})
	if err != nil {
		return 0, err
	}
	if err := a.store.MarkImported(itemID, trackID); err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return trackID, nil
}

// DiscardReviewItem 丢弃条目且不入库。
func (a *App) DiscardReviewItem(itemID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.MarkDiscarded(itemID)
}

// NoneMatched 不接受当前结果但把条目保留在队列中。
//
// 区别于「无需匹配」：条目仍留在队列中可换上游再试，而不是立即入库并结束。
func (a *App) NoneMatched(itemID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	item, err := a.store.GetItem(itemID)
	if err != nil || item == nil {
		return err
	}
	// 保持 review 状态，仅清空候选快照
	return a.store.MarkReview(itemID, "", derefOr(item.TriedVariants, ""))
}

// RetryReviewItem 让失败条目重新进入匹配。
func (a *App) RetryReviewItem(itemID int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.RetryItem(itemID); err != nil {
		return err
	}
	// 重新排队后由对应批次的驱动器继续处理
	item, err := a.store.GetItem(itemID)
	if err != nil || item == nil {
		return err
	}
	if a.runner != nil && !a.runner.IsRunning(item.BatchID) {
		return a.runner.Start(context.Background(), item.BatchID)
	}
	return nil
}

func derefOr(s *string, fallback string) string {
	if s == nil {
		return fallback
	}
	return *s
}
