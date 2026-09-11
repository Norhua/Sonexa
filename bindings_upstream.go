package main

import (
	"context"
	"fmt"
	"sync"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// CandidateRow 是候选结果表格的一行。
//
// 列定义在上游搜索页与复核队列之间完全一致；
// 动作按上下文变化，而列不变。
type CandidateRow struct {
	Provider string `json:"provider"`
	MatchRef string `json:"matchRef"`
	// Badges 可叠加，如 [本地][MB]；疑似命中时为 [本地?]。
	Badges []string `json:"badges"`
	// Note 给出附加说明，如「经 ISRC 匹配」。
	Note string `json:"note"`
	// Action 为 加入曲库 | 已在库中 | 接受 | 合并。
	Action string `json:"action"`
	// LocalTrackID 非零时表示库中已有对应曲目，可跳转。
	LocalTrackID int64 `json:"localTrackId"`

	ThumbKey     string  `json:"thumbKey"`
	Title        *string `json:"title"`
	Artist       *string `json:"artist"`
	Album        *string `json:"album"`
	PrimaryType  *string `json:"primaryType"`
	DurationMS   *int    `json:"durationMs"`
	TrackNumber  *int    `json:"trackNumber"`
	TrackTotal   *int    `json:"trackTotal"`
	Year         *int    `json:"year"`
	MediumFormat *string `json:"mediumFormat"`
	Country      *string `json:"country"`
	Confidence   float64 `json:"confidence"`
	// MatchedVariant 供复核队列显示该分值来自哪个变体。
	MatchedVariant string `json:"matchedVariant"`

	ReleaseRef      *string `json:"releaseRef"`
	ReleaseGroupRef *string `json:"releaseGroupRef"`
	RecordingRef    *string `json:"recordingRef"`
	ISRC            *string `json:"isrc"`
}

// ProviderResults 是一个上游的搜索结果。
type ProviderResults struct {
	Provider    string         `json:"provider"`
	DisplayName string         `json:"displayName"`
	Rows        []CandidateRow `json:"rows"`
	Count       int            `json:"count"`
	// Error 非空时该标签页显示错误，不影响其他上游。
	Error string `json:"error"`
	// Capabilities 驱动该标签页的列显示策略。
	Capabilities []string `json:"capabilities"`
}

// SearchResponse 是一次上游搜索的完整结果。
type SearchResponse struct {
	// Aggregate 是聚合标签页。
	Aggregate []CandidateRow `json:"aggregate"`
	// ByProvider 是各上游各一个标签页。
	ByProvider []ProviderResults `json:"byProvider"`
	// Capabilities 是当次结果涉及的上游集合的能力并集，
	// 列策略以它为准：全不支持则隐藏该列，部分支持则填占位。
	Capabilities []string    `json:"capabilities"`
	CountryHint  CountryHint `json:"countryHint"`
}

// SearchUpstream 在指定的上游中搜索。
//
// 三个输入字段可任意留空，留空的条件不参与查询。
// 各上游并发查询：先返回的先显示，不被慢的阻塞。
func (a *App) SearchUpstream(title, artist, album string, providerIDs []string, country string) (*SearchResponse, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}
	if len(providerIDs) == 0 {
		return nil, fmt.Errorf("请至少勾选一个上游")
	}

	q := provider.Query{Title: title, Artist: artist, Album: album, Country: country, Limit: 25}

	type task struct {
		p    provider.Provider
		rows []CandidateRow
		err  error
	}
	tasks := make([]*task, 0, len(providerIDs))
	for _, id := range providerIDs {
		p, err := a.registry.Get(provider.ProviderID(id))
		if err != nil {
			continue
		}
		tasks = append(tasks, &task{p: p})
	}

	var wg sync.WaitGroup
	for _, t := range tasks {
		wg.Add(1)
		go func(t *task) {
			defer wg.Done()
			if err := t.p.IsConfigured(); err != nil {
				t.err = err
				return
			}
			candidates, err := t.p.SearchTracks(context.Background(), q)
			if err != nil {
				t.err = err
				return
			}
			t.rows = a.toRows(candidates, q, "加入曲库")
		}(t)
	}
	wg.Wait()

	resp := &SearchResponse{
		CountryHint: a.SuggestCountry(title + " " + artist + " " + album),
	}
	var caps provider.Capabilities
	for _, t := range tasks {
		pr := ProviderResults{
			Provider:     string(t.p.ID()),
			DisplayName:  t.p.DisplayName(),
			Rows:         t.rows,
			Count:        len(t.rows),
			Capabilities: t.p.Capabilities().Names(),
		}
		if t.err != nil {
			pr.Error = t.err.Error()
		}
		resp.ByProvider = append(resp.ByProvider, pr)
		resp.Aggregate = append(resp.Aggregate, t.rows...)
		caps |= t.p.Capabilities()
	}
	resp.Capabilities = caps.Names()

	if resp.Aggregate == nil {
		resp.Aggregate = []CandidateRow{}
	}
	return resp, nil
}

// toRows 把候选转为表格行，并预查本地决定动作按钮。
//
// 渲染每行时预先查询本地是否已存在：
// 已存在则显示为禁用的「已在库中」且点击可跳转；不存在则显示可用的「加入曲库」。
func (a *App) toRows(candidates []provider.Candidate, q provider.Query, defaultAction string) []CandidateRow {
	weights := matching.WeightsFromSettings(a.settings.Get().Matching)
	rows := make([]CandidateRow, 0, len(candidates))

	for _, c := range candidates {
		score := weights.ScoreCandidate(q, c)
		if score.Rejected {
			continue
		}

		local, err := matching.ResolveLocal(a.store, c)
		if err != nil {
			local = matching.LocalMatch{Kind: matching.LocalNone}
		}

		row := CandidateRow{
			Provider: string(c.Provider), MatchRef: c.MatchRef,
			Badges: local.Badges(c.Provider), Note: local.Note(),
			LocalTrackID: local.TrackID,
			Title:        c.Title, Artist: c.ArtistName, Album: c.AlbumTitle,
			PrimaryType: c.PrimaryType, DurationMS: c.DurationMS,
			TrackNumber: c.TrackNumber, TrackTotal: c.TrackTotal,
			Year: c.Year, MediumFormat: c.MediumFormat, Country: c.Country,
			Confidence: score.Total,
			ReleaseRef: c.ReleaseRef, ReleaseGroupRef: c.ReleaseGroupRef,
			RecordingRef: c.RecordingRef, ISRC: c.ISRC,
		}

		switch {
		case local.Kind == matching.LocalNone:
			row.Action = defaultAction
		case defaultAction == "加入曲库":
			row.Action = "已在库中"
		default:
			row.Action = "合并"
		}

		if c.ReleaseRef != nil {
			row.ThumbKey = covers.CacheKey(string(c.Provider)+":"+*c.ReleaseRef, 250)
		}
		rows = append(rows, row)
	}
	return rows
}

// AddToLibrary 把一条上游候选加入曲库。
//
// 写入匹配来源与标识；真正写入时若撞上唯一约束（并发/竞态），
// 静默转为跳转到已有曲目，不向用户报错。
func (a *App) AddToLibrary(row CandidateRow) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	if row.Title == nil || *row.Title == "" {
		return 0, fmt.Errorf("候选缺少曲名")
	}

	source := row.Provider
	trackID, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: *row.Title, Artist: row.Artist, Album: row.Album,
		MatchSource: &source, MatchRef: &row.MatchRef,
		Metadata: &store.MetadataInput{
			Title: row.Title, Album: row.Album, TrackCredit: row.Artist,
			PrimaryType: row.PrimaryType, MediumFormat: row.MediumFormat,
			Country: row.Country, Year: row.Year,
			TrackNumber: row.TrackNumber, TrackTotal: row.TrackTotal,
			DurationMS: row.DurationMS, RecordingMBID: row.RecordingRef,
			ReleaseMBID: row.ReleaseRef, ReleaseGroupMBID: row.ReleaseGroupRef,
			ISRC: row.ISRC,
		},
	})

	var exists *store.ErrAlreadyExists
	if err != nil && asAlreadyExistsErr(err, &exists) {
		return exists.TrackID, nil
	}
	if err != nil {
		return 0, err
	}

	// 顺带登记发行，清单状态为 none——搜索响应不含完整清单
	if row.ReleaseRef != nil && *row.ReleaseRef != "" && row.Album != nil {
		a.store.UpsertRelease(&store.Release{
			Provider: source, ProviderRef: *row.ReleaseRef,
			ReleaseGroupRef: row.ReleaseGroupRef, Title: *row.Album,
			AlbumCredit: row.Artist, PrimaryType: row.PrimaryType,
			MediumFormat: row.MediumFormat, Country: row.Country,
			TrackTotal: row.TrackTotal, TracklistState: store.TracklistNone,
		})
	}

	a.notifyLibraryChanged()
	return trackID, nil
}

// PrefetchCovers 批量预取一页列表的封面。
//
// 前端一页只调一次（整批一次，不是每行一次），
// 这样并发度由 Go 的池决定，不受 webview 每主机连接数的限制。
func (a *App) PrefetchCovers(refs []covers.Ref) error {
	if a.coverSvc == nil {
		return nil
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil // 出网禁用时静默跳过，不报错
	}
	a.coverSvc.Prefetch(context.Background(), refs)
	return nil
}

// CancelCovers 取消已滚出视口的取图任务。
func (a *App) CancelCovers(visibleKeys []string) {
	if a.coverSvc != nil {
		a.coverSvc.CancelOutOfView(visibleKeys)
	}
}

// BuildCoverRefs 由候选行构造封面预取引用。
//
// 降级链由 Go 侧的 provider 逻辑生成，前端不实现这部分。
func (a *App) BuildCoverRefs(rows []CandidateRow) []covers.Ref {
	out := make([]covers.Ref, 0, len(rows))
	for _, r := range rows {
		if r.ReleaseRef == nil || *r.ReleaseRef == "" {
			continue
		}
		urls := a.coverLadder(r.Provider, *r.ReleaseRef, r.ReleaseGroupRef)
		if len(urls) == 0 {
			continue
		}
		out = append(out, covers.Ref{
			Key:       r.ThumbKey,
			SourceRef: *r.ReleaseRef,
			Size:      250,
			URLs:      urls,
		})
	}
	return out
}
