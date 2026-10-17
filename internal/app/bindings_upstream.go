package app

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// CandidateRow 是候选结果表格的一行。
//
// 列定义在上游搜索页与复核队列之间完全一致；
// 动作按上下文变化，而列不变。
type CandidateRow struct {
	AlbumCredit        *string                 `json:"albumCredit"`
	GroupTitle         *string                 `json:"groupTitle"`
	GroupCredit        *string                 `json:"groupCredit"`
	ReleaseTrackTotal  *int                    `json:"releaseTrackTotal"`
	MediumTrackTotal   *int                    `json:"mediumTrackTotal"`
	Credits            []provider.CreditArtist `json:"credits"`
	AlbumCredits       []provider.CreditArtist `json:"albumCredits"`
	GroupCredits       []provider.CreditArtist `json:"groupCredits"`
	Media              []domain.Medium         `json:"media"`
	Labels             []domain.Label          `json:"labels"`
	Identifiers        []domain.Identifier     `json:"identifiers"`
	ReleaseIdentifiers []domain.Identifier     `json:"releaseIdentifiers"`
	UpstreamScore      *int                    `json:"upstreamScore"`
	HasCover           bool                    `json:"hasCover"`
	Provider           string                  `json:"provider"`
	MatchRef           string                  `json:"matchRef"`
	// Badges 可叠加，如 [本地][MB]；疑似命中时为 [本地?]。
	Badges []string `json:"badges"`
	// Note 给出附加说明，如「经 ISRC 匹配」。
	Note string `json:"note"`
	// Action 为 加入曲库 | 已在库中 | 接受 | 合并。
	Action string `json:"action"`
	// LocalTrackID 非零时表示库中已有对应曲目，可跳转。
	LocalTrackID int64 `json:"localTrackId"`

	// 封面目标仅按同一发行身份关联，不能借文本疑似命中跨歌集取图。
	ReleaseID     int64 `json:"releaseId"`
	CoverRevision int64 `json:"coverRevision"`

	ThumbKey      string  `json:"thumbKey"`
	Title         *string `json:"title"`
	Artist        *string `json:"artist"`
	Album         *string `json:"album"`
	PrimaryType   *string `json:"primaryType"`
	DurationMS    *int    `json:"durationMs"`
	TrackNumber   *int    `json:"trackNumber"`
	TrackTotal    *int    `json:"trackTotal"`
	Year          *int    `json:"year"`
	MediumFormat  *string `json:"mediumFormat"`
	Country       *string `json:"country"`
	Label         *string `json:"label"`
	CatalogNumber *string `json:"catalogNumber"`
	Confidence    float64 `json:"confidence"`
	// MatchedVariant 供复核队列显示该分值来自哪个变体。
	MatchedVariant string `json:"matchedVariant"`

	/** ArtworkURL 保留候选元信息；后端将其登记为受控图片目录。 */
	ArtworkURL *string `json:"artworkUrl"`

	ReleaseRef      *string `json:"releaseRef"`
	ReleaseGroupRef *string `json:"releaseGroupRef"`
	RecordingRef    *string `json:"recordingRef"`
	ISRC            *string `json:"isrc"`

	// 以下三个字段界面不显示，但入库要写。行必须能**无损**还原成候选，
	// 否则「搜索页加入」与「导入自动入库」写出的曲目会差这几列。
	DiscNumber       *int    `json:"discNumber"`
	ReleaseStatus    *string `json:"releaseStatus"`
	FirstReleaseDate *string `json:"firstReleaseDate"`
}

// toCandidate 把行还原成候选，供入库。与 rowFrom 互为逆运算。
func (r CandidateRow) toCandidate() provider.Candidate {
	return provider.Candidate{
		AlbumCredit:        r.AlbumCredit,
		GroupTitle:         r.GroupTitle,
		GroupCredit:        r.GroupCredit,
		ReleaseTrackTotal:  r.ReleaseTrackTotal,
		MediumTrackTotal:   r.MediumTrackTotal,
		Credits:            r.Credits,
		AlbumCredits:       r.AlbumCredits,
		GroupCredits:       r.GroupCredits,
		Media:              r.Media,
		Labels:             r.Labels,
		Identifiers:        r.Identifiers,
		ReleaseIdentifiers: r.ReleaseIdentifiers,
		UpstreamScore:      r.UpstreamScore,
		HasCover:           r.HasCover,
		Provider:           provider.ProviderID(r.Provider), MatchRef: r.MatchRef,
		Title: r.Title, ArtistName: r.Artist, AlbumTitle: r.Album,
		PrimaryType: r.PrimaryType, DurationMS: r.DurationMS,
		TrackNumber: r.TrackNumber, TrackTotal: r.TrackTotal, DiscNumber: r.DiscNumber,
		Year: r.Year, MediumFormat: r.MediumFormat, Country: r.Country,
		Label: r.Label, CatalogNumber: r.CatalogNumber,
		ReleaseRef: r.ReleaseRef, ReleaseGroupRef: r.ReleaseGroupRef,
		RecordingRef: r.RecordingRef, ISRC: r.ISRC,
		ReleaseStatus: r.ReleaseStatus, FirstReleaseDate: r.FirstReleaseDate,
		ThumbURL: r.ArtworkURL,
	}
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

	// 用户在等这次结果：交互式优先级插到后台补全之前，且补全整体让路。
	// 只靠插队不够——后台任务仍会占掉这一秒的配额，用户那次搜索还是要多等一轮。
	done := a.beginInteractive()
	defer done()
	searchCtx := provider.WithPriority(context.Background(), provider.PriorityInteractive)
	in := matching.Input{Title: title, Artist: artist, Album: album, Country: country}

	type task struct {
		p    provider.MetadataProvider
		rows []CandidateRow
		err  error
	}
	registry := a.metadataRegistry()
	tasks := make([]*task, 0, len(providerIDs))
	for _, id := range providerIDs {
		p, err := registry.Get(provider.ProviderID(id))
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
			scored, _, err := a.matchUpstream(searchCtx, t.p, in)
			if err != nil {
				t.err = err
				return
			}
			t.rows = a.rowsFrom(scored, "加入曲库")

			// 该上游一完成就推给前端：先返回的先显示，不被慢的上游阻塞。
			// 仍等到 wg.Wait() 是为了让本次调用的返回值完整，
			// 前端可以只用事件做增量渲染。
			a.emitEvent(EventProviderResults, providerResult{
				Provider:     string(t.p.ID()),
				DisplayName:  t.p.DisplayName(),
				Rows:         t.rows,
				Count:        len(t.rows),
				Capabilities: t.p.Capabilities().Names(),
			})
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

// matchUpstream 在一个上游里为一条输入找候选。
//
// 这是绑定层里**唯一**的检索入口：搜索页、复核队列的「改搜」、编辑页的
// 「重新匹配」都经由它，而它做的事与导入匹配完全相同——同一条变体阶梯、
// 同一套剥离规则、同一对闸门、同一份评分（见 matching.Engine.Run）。
// 用户留空的字段不参与闸门，因此「只填曲名」时它退化为曲名闸。
//
// 返回按置信度降序的候选池与已尝试的变体。
func (a *App) matchUpstream(ctx context.Context, p provider.MetadataProvider, in matching.Input,
) ([]matching.ScoredCandidate, []string, error) {
	if err := p.IsConfigured(); err != nil {
		return nil, nil, err
	}
	engine := matching.NewEngine(a.settings.Get().Matching)
	res, err := engine.Run(ctx, p, a.store, in)
	if err != nil {
		return nil, nil, err
	}
	return res.Scored, res.TriedVariants, nil
}

// rowsFrom 把评分后的候选转为表格行，并预查本地决定动作按钮。
//
// 已存在则显示为禁用的「已在库中」（或复核队列里的「合并」）且点击可跳转；
// 不存在则显示 defaultAction。
func (a *App) rowsFrom(scored []matching.ScoredCandidate, defaultAction string) []CandidateRow {
	rows := make([]CandidateRow, 0, len(scored))
	for _, sc := range scored {
		// 搜索已给出 artwork，无需为预览每个候选再发一次 Lookup。
		// 失败不吞掉有效的歌曲候选，后续仍可按发行身份恢复目录。
		if err := a.store.RememberCandidateArtwork(sc.Candidate); err != nil {
			log.Printf("登记候选封面失败: %v", err)
		}
		row := rowFrom(sc)
		a.applyLocalMatch(&row, sc.Candidate)
		if row.LocalTrackID == 0 {
			row.Action = defaultAction
		} else if defaultAction == "加入曲库" {
			row.Action = "已在库中"
		}
		rows = append(rows, row)
	}
	return rows
}

// RefreshSearchCandidates 只重查本地身份和封面归属，不请求上游、不改候选事实或顺序。
func (a *App) RefreshSearchCandidates(rows []CandidateRow) ([]CandidateRow, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	out := make([]CandidateRow, len(rows))
	for i, row := range rows {
		// 删除/解除绑定后旧的具体发行不能继续用于封面。
		row.ReleaseID, row.CoverRevision = 0, 0
		a.applyLocalMatch(&row, row.toCandidate())
		row.Action = "加入曲库"
		if row.LocalTrackID != 0 {
			row.Action = "已在库中"
		}
		out[i] = row
	}
	return out, nil
}

// rowFrom 把一条评分候选映射为行，不查库。与 CandidateRow.toCandidate 互为逆运算。
func rowFrom(sc matching.ScoredCandidate) CandidateRow {
	c := sc.Candidate
	row := CandidateRow{
		AlbumCredit:        c.AlbumCredit,
		GroupTitle:         c.GroupTitle,
		GroupCredit:        c.GroupCredit,
		ReleaseTrackTotal:  c.ReleaseTrackTotal,
		MediumTrackTotal:   c.MediumTrackTotal,
		Credits:            c.Credits,
		AlbumCredits:       c.AlbumCredits,
		GroupCredits:       c.GroupCredits,
		Media:              c.Media,
		Labels:             c.Labels,
		Identifiers:        c.Identifiers,
		ReleaseIdentifiers: c.ReleaseIdentifiers,
		UpstreamScore:      c.UpstreamScore,
		HasCover:           c.HasCover,
		Provider:           string(c.Provider), MatchRef: c.MatchRef,
		Title: c.Title, Artist: c.ArtistName, Album: c.AlbumTitle,
		PrimaryType: c.PrimaryType, DurationMS: c.DurationMS,
		TrackNumber: c.TrackNumber, TrackTotal: c.TrackTotal, DiscNumber: c.DiscNumber,
		Year: c.Year, MediumFormat: c.MediumFormat, Country: c.Country,
		Label: c.Label, CatalogNumber: c.CatalogNumber,
		Confidence: sc.Score.Total, MatchedVariant: sc.MatchedVariant,
		ReleaseRef: c.ReleaseRef, ReleaseGroupRef: c.ReleaseGroupRef,
		RecordingRef: c.RecordingRef, ISRC: c.ISRC,
		ReleaseStatus: c.ReleaseStatus, FirstReleaseDate: c.FirstReleaseDate,
		// 保留已知 artwork；自动取图仍只接收发行身份，不信任前端 URL。
		ArtworkURL: c.ThumbURL,
	}
	switch {
	case c.ReleaseRef != nil && *c.ReleaseRef != "":
		row.ThumbKey = covers.CacheKey(string(c.Provider)+":"+*c.ReleaseRef, coverThumbSize)
	case c.ThumbURL != nil && *c.ThumbURL != "":
		row.ThumbKey = covers.CacheKey(string(c.Provider)+":"+*c.ThumbURL, coverThumbSize)
	}
	return row
}

// AddToLibrary 把一条上游候选加入曲库。
//
// 真正写入时若撞上唯一约束（并发/竞态），静默转为跳转到已有曲目，不向用户报错。
func (a *App) AddToLibrary(row CandidateRow) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	return a.admitCandidate(row.toCandidate())
}

// admitCandidate 保留旧调用协议，所有写入委托准入计划；需要确认时明确返回错误。
func (a *App) admitCandidate(c provider.Candidate) (int64, error) {
	service, err := a.libraryService()
	if err != nil {
		return 0, err
	}
	plan, err := service.PrepareCandidate(a.ctx, c, 0)
	if err != nil {
		return 0, err
	}
	view, err := admissionView(plan, nil)
	if err != nil {
		return 0, err
	}
	return a.commitUnconfirmedAdmission(view)
}

// PrefetchCovers 批量预取一页列表的封面。
//
// 前端一页只调一次（整批一次，不是每行一次），
// 这样并发度由 Go 的池决定，不受 webview 每主机连接数的限制。
func (a *App) PrefetchCovers(refs []covers.Ref) error {
	if a.coverSvc == nil {
		return nil
	}
	// 本地预取不受出网设置影响；实际网络请求由共享工作池检查 gate。
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
		if a.coverSvc != nil && a.coverSvc.Library() != nil {
			if r.ReleaseRef == nil || *r.ReleaseRef == "" {
				continue
			}
			target := covers.Target{ReleaseID: r.ReleaseID, Provider: r.Provider, Ref: *r.ReleaseRef}
			if r.ReleaseGroupRef != nil {
				target.GroupRef = *r.ReleaseGroupRef
			}
			out = append(out, covers.Ref{Key: target.Key(covers.Thumbnail, r.CoverRevision), Target: target, Usage: covers.Thumbnail})
			continue
		}
		// iTunes 的候选没有 release 级标识，只有 artworkUrl；
		// 只要有一个可用于构造降级链的来源就应预取。
		hasRef := r.ReleaseRef != nil && *r.ReleaseRef != ""
		hasArtwork := r.ArtworkURL != nil && *r.ArtworkURL != ""
		if !hasRef && !hasArtwork {
			continue
		}
		artwork := ""
		if r.ArtworkURL != nil {
			artwork = *r.ArtworkURL
		}
		urls := a.coverLadder(r.Provider, releaseRefOf(r), artwork, r.ReleaseGroupRef)
		if len(urls) == 0 {
			continue
		}
		sourceRef := releaseRefOf(r)
		if sourceRef == "" && r.ArtworkURL != nil {
			// iTunes 无 release 标识，用基准图地址作为缓存来源；
			// 取源须与 toRows 里算 ThumbKey 时一致
			sourceRef = *r.ArtworkURL
		}
		out = append(out, covers.Ref{
			Key:       r.ThumbKey,
			SourceRef: sourceRef,
			Size:      coverThumbSize,
			URLs:      urls,
		})
	}
	return out
}

// providerResult 是单个上游的搜索结果事件载荷。
type providerResult struct {
	Provider     string         `json:"provider"`
	DisplayName  string         `json:"displayName"`
	Rows         []CandidateRow `json:"rows"`
	Count        int            `json:"count"`
	Capabilities []string       `json:"capabilities"`
}
