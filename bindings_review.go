package main

import (
	"context"
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
		r := ReviewItem{
			ID: it.ID, BatchID: it.BatchID, Status: string(it.Status),
			InTitle: it.InTitle, InArtist: it.InArtist, InAlbum: it.InAlbum,
			AttemptCount: it.AttemptCount,
			LastError:    it.LastError, LastErrorAt: formatTime(it.LastErrorAt),
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
				Title: c.Title, Artist: c.ArtistName, Album: c.AlbumTitle,
				PrimaryType: c.PrimaryType, DurationMS: c.DurationMS,
				TrackNumber: c.TrackNumber, TrackTotal: c.TrackTotal,
				Year: c.Year, MediumFormat: c.MediumFormat, Country: c.Country,
				Confidence: sc.Score.Total, MatchedVariant: sc.MatchedVariant,
				ReleaseRef: c.ReleaseRef, ReleaseGroupRef: c.ReleaseGroupRef,
				RecordingRef: c.RecordingRef, ISRC: c.ISRC,
				// 封面来源：MB 用 release 标识，iTunes 只有基准图地址。
				// 不带出来的话复核队列里 iTunes 候选的缩略图列永远是空的。
				ArtworkURL: c.ThumbURL,
			})
			// 复核队列的候选必须与上游搜索一样做本地判定，
			// 否则库里已有的那首不会显示双徽章、动作也不会变成「合并」。
			a.applyLocalMatch(&out[len(out)-1], c)
		}
		return out
	}

	// 再按 CandidateRow 解析：ResearchReviewItem 写快照时存的就是这个形状。
	//
	// 没有这一支时它会掉到下面的「本地候选」分支——那个分支只认
	// badges/action/title/artist/album 五个字段，于是用户改搜出来的候选
	// 一旦经过一次队列重载，置信度、年份、时长、厂牌等列全部变成占位短横线。
	var stored []CandidateRow
	if err := json.Unmarshal([]byte(raw), &stored); err == nil &&
		len(stored) > 0 && stored[0].MatchRef != "" {
		// 本地判定不随快照持久化：入库/合并状态可能在存快照之后变了
		for i := range stored {
			c := provider.Candidate{
				Provider: provider.ProviderID(stored[i].Provider),
				MatchRef: stored[i].MatchRef,
				// 曲名/署名/时长是 ResolveLocal 第三步（文本模糊比对）的输入，
				// 漏掉它们的话「本地?」这个疑似徽章永远不会出现
				Title: stored[i].Title, ArtistName: stored[i].Artist,
				AlbumTitle: stored[i].Album, DurationMS: stored[i].DurationMS,
				RecordingRef: stored[i].RecordingRef, ReleaseRef: stored[i].ReleaseRef,
				ReleaseGroupRef: stored[i].ReleaseGroupRef, ISRC: stored[i].ISRC,
			}
			a.applyLocalMatch(&stored[i], c)
		}
		return stored
	}

	// 最后按本地疑似重复的简化快照解析
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

	// 与手动检索、导入匹配共用同一套文本处理：复核队列里「改搜」时填的
	// 往往正是从表格里复制过来的原串，带着译名注释直接查必然落空。
	q := upstreamQuery(title, artist, album, country)

	// 用户在等这次结果：交互式优先级，插到后台补全的队列之前
	done := a.beginInteractive()
	defer done()
	ctx := provider.WithPriority(context.Background(), provider.PriorityInteractive)

	candidates, err := p.SearchTracks(ctx, q)
	if err != nil {
		return nil, err
	}

	// 评分用原始输入而不是剥离后的主干，理由同 SearchUpstream
	scoreQ := provider.Query{Title: title, Artist: artist, Album: album, Country: country}
	rows := a.toRows(candidates, scoreQ, "接受")
	// 结果按置信度排序
	sortRowsByConfidence(rows)

	// 把本次候选写入快照，但不动原始导入值
	snapshot, _ := json.Marshal(rows)
	item, err := a.store.GetItem(itemID)
	if err == nil && item != nil {
		variants, _ := json.Marshal([]string{title})
		a.store.MarkReview(itemID, store.ReviewReasonUnchanged, string(snapshot), string(variants))
	}
	return rows, nil
}

// applyLocalMatch 按一次本地判定填好某行的徽章、动作与附注。
//
// 三者本来是三个各自调用 ResolveLocal 的函数——同一条候选要查三遍库，
// 而 ResolveLocal 每次最多查三张表。一个 200 条的复核队列因此会打出
// 上千次本来只需三分之一的查询。
//
// 库中已有且上游也存在时同时标注两个徽章；经 ISRC 桥接命中的标注
// 「经 ISRC 匹配」；文本模糊比对的疑似命中带问号。
func (a *App) applyLocalMatch(row *CandidateRow, c provider.Candidate) {
	local, err := matching.ResolveLocal(a.store, c)
	if err != nil || local.Kind == matching.LocalNone {
		row.Badges = []string{string(c.Provider)}
		row.Action = "接受"
		row.Note = ""
		row.LocalTrackID = 0
		return
	}
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
// **它不发任何网络请求。** 候选已经带着完整身份（match_ref 与四个标识）
// 与 track / medium 层的全部字段，够写成曲库里合法的一行。参与者、作曲作词、
// 厂牌、目录号这些发行层事实由后台的补全信息匹配按 release 补一次
// （见 enrichWorker）。
//
// 先前这里同步发一次详情请求，在 1 req/s 之下就是 1–2 秒的卡顿——
// 用户点一下「接受」要等着看它转完，而那次请求的产出与
// 「这条曲目能不能进库」无关。曲目入库时状态即为「未匹配（待补全）」，
// 那个中间态是如实的，不是丢了信息。
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
	// 刚出现一条待补的曲目：空闲时立刻接着补，用户不必去点「开始」
	a.autoStartEnrich()
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
//
// 溯源为空即「无需匹配」：这条曲目不进补全任务的工作集
// （见 store.InitialEnrichState），因此界面上它既不是「未匹配」也不算待办。
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
	return a.store.MarkReview(itemID, store.ReviewReasonUnchanged, "", derefOr(item.TriedVariants, ""))
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
