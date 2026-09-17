package main

import (
	"context"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// AlbumRow 是专辑详情中的一行。
//
// 两种状态：已入库（正常行）、未入库（灰色行）。
// 灰色行的列定义与普通行完全相同。
type AlbumRow struct {
	DiscNumber  int     `json:"discNumber"`
	TrackNumber int     `json:"trackNumber"`
	NumberText  *string `json:"numberText"`
	Title       string  `json:"title"`
	Credit      *string `json:"credit"`
	DurationMS  *int    `json:"durationMs"`

	// State 为 owned | missing。
	State string `json:"state"`
	// TrackID 非零时可点开详情；灰色行为 0。
	TrackID int64 `json:"trackId"`
	// Ghost 为真表示灰色行：不可选中、单击不开抽屉、右键仅两项、
	// 不计入选中计数与曲目数、不参与导出。
	Ghost bool `json:"ghost"`
	// TrackRef 供灰色行的「加入曲库」直接带溯源入库。
	TrackRef     *string `json:"trackRef"`
	RecordingRef *string `json:"recordingRef"`
}

// AlbumDetail 是专辑详情视图。
type AlbumDetail struct {
	// ReleaseID 是这张专辑对应的歌集 id，也是前端的选中键。
	ReleaseID int64 `json:"releaseId"`
	// GroupID 非零时这条专辑代表一个发行组。
	GroupID int64   `json:"groupId"`
	Title   string  `json:"title"`
	Credit  *string `json:"credit"`
	// 头部发行信息
	PrimaryType   *string `json:"primaryType"`
	Year          *int    `json:"year"`
	MediumFormat  *string `json:"mediumFormat"`
	Country       *string `json:"country"`
	Label         *string `json:"label"`
	CatalogNumber *string `json:"catalogNumber"`
	DiscTotal     *int    `json:"discTotal"`
	Provider      *string `json:"provider"`
	ReleaseRef    *string `json:"releaseRef"`
	// ReleaseGroupRef 与 Provider/ReleaseRef 一起构成封面地址的来源。
	ReleaseGroupRef *string `json:"releaseGroupRef"`
	// CoverPath 非空时是已采用封面的落盘路径，优先于临时缩略图。
	CoverPath *string `json:"coverPath"`
	// ReleaseCount 是该发行组下共有几个 release。
	ReleaseCount *int `json:"releaseCount"`

	// TracklistState 为 none | partial | complete | failed。
	TracklistState string `json:"tracklistState"`
	// FetchedAt 是 RFC3339 时间串。
	//
	// 绑定层不导出 time.Time：Wails 的 TS 模型生成器不认识它，
	// 会打印 `Not found: time.Time` 并把字段生成为 any。
	FetchedAt *string `json:"fetchedAt"`
	// TracklistError 在状态为 failed 时给出错误信息。
	TracklistError string `json:"tracklistError"`

	// Rows 固定按碟号、音轨号排序，不受曲目表格默认排序影响。
	Rows []AlbumRow `json:"rows"`
	// OwnedCount 不含灰色行；TotalCount 为 nil 时界面呈现为 `n/-`。
	OwnedCount int  `json:"ownedCount"`
	TotalCount *int `json:"totalCount"`
	// Incomplete 为真时末尾显示「清单不完整」与补全动作。
	Incomplete bool `json:"incomplete"`
}

// GetAlbumDetail 读取专辑详情。
//
// 清单状态不是完整时只列出已入库曲目，并在末尾显示「清单不完整」——
// 不显示虚假的总数，也不显示灰色行。
//
// 参数是歌集 id：专辑升格为实体后不再有查询时现场派生的分组键，
// 一条列表项就是一条 release。
func (a *App) GetAlbumDetail(releaseID int64) (*AlbumDetail, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	rel, err := a.store.GetRelease(releaseID)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		return nil, fmt.Errorf("专辑不存在")
	}

	detail := &AlbumDetail{
		ReleaseID: rel.ID, Title: rel.Title, Credit: rel.Credit,
		PrimaryType: rel.PrimaryType, Year: rel.Year(),
		MediumFormat: rel.MediumFormat, Country: rel.Country,
		Label: rel.Label, CatalogNumber: rel.CatalogNumber,
		DiscTotal: rel.DiscTotal,
		Provider:  rel.Provider, ReleaseRef: rel.ProviderRef,
		ReleaseGroupRef: rel.ReleaseGroupRef, CoverPath: rel.CoverPath,
		TotalCount:     rel.TrackTotal,
		TracklistState: string(store.TracklistNone),
		FetchedAt:      formatTime(rel.FetchedAt),
	}
	if rel.ReleaseGroupID != nil {
		detail.GroupID = *rel.ReleaseGroupID
		if g, err := a.store.GetReleaseGroup(*rel.ReleaseGroupID); err == nil && g != nil {
			detail.ReleaseCount = g.ReleaseCount
		}
	}

	// 已入库曲目：立即渲染，不等清单
	owned, err := a.ownedRowsFor(rel)
	if err != nil {
		return nil, err
	}
	detail.OwnedCount = len(owned)

	// 本地歌集没有上游事实：不显示灰色行也不显示总数
	if rel.ProviderRef == nil || rel.Provider == nil {
		detail.Rows = owned
		detail.TotalCount = nil
		return detail, nil
	}

	// 清单过期判定收口在 EffectiveTracklistState：所有读取清单的路径都经过它
	effective := a.staleConfig().EffectiveTracklistState(rel)
	detail.TracklistState = string(effective)

	if effective != store.TracklistComplete {
		// 不伪装完整：只列已入库曲目，末尾提示清单不完整
		detail.Rows = owned
		detail.Incomplete = true
		detail.TotalCount = nil
		if rel.TracklistState == store.TracklistFailed {
			detail.TracklistError = "清单获取失败"
		}
		return detail, nil
	}

	// 清单完整：按曲目标识解析已入库/未入库
	positions, err := a.store.ResolveTracklist(rel.ID, *rel.Provider)
	if err != nil {
		return nil, err
	}
	detail.Rows = rowsFromPositions(positions)
	detail.OwnedCount = countOwned(detail.Rows)
	if rel.TrackTotal == nil {
		total := len(positions)
		detail.TotalCount = &total
	}
	return detail, nil
}

// rowsFromPositions 把清单位置转为行，固定按碟号与音轨号排序。
func rowsFromPositions(positions []store.ResolvedPosition) []AlbumRow {
	out := make([]AlbumRow, 0, len(positions))
	for _, p := range positions {
		row := AlbumRow{
			DiscNumber:  p.Track.DiscNumber,
			TrackNumber: p.Track.TrackNumber,
			NumberText:  p.Track.TrackNumberText,
			Title:       p.Track.Title,
			Credit:      p.Track.TrackCredit,
			DurationMS:  p.Track.DurationMS,
			State:       string(p.State),
			TrackID:     p.TrackID,
			// 未入库即灰色行
			Ghost:        p.State == store.PositionMissing,
			TrackRef:     p.Track.TrackRef,
			RecordingRef: p.Track.RecordingRef,
		}
		out = append(out, row)
	}
	return out
}

// countOwned 统计非灰色行——灰色行不计入曲目数。
func countOwned(rows []AlbumRow) int {
	var n int
	for _, r := range rows {
		if !r.Ghost {
			n++
		}
	}
	return n
}

// ownedRowsFor 返回某专辑已入库曲目的行，按碟号与音轨号排序。
//
// 有发行组时取整组的曲目：列表里同组的多条 release 合为一条，
// 详情也该显示用户在这张专辑上拥有的全部曲目，而不只是代表版本那一条上的。
func (a *App) ownedRowsFor(rel *store.Release) ([]AlbumRow, error) {
	f := store.TrackFilter{ReleaseID: rel.ID}
	if rel.ReleaseGroupID != nil {
		f = store.TrackFilter{ReleaseGroupID: *rel.ReleaseGroupID}
	}

	tracks, err := a.store.ListTracks(f)
	if err != nil {
		return nil, err
	}

	out := make([]AlbumRow, 0, len(tracks))
	for _, t := range tracks {
		row := AlbumRow{
			Title: t.Title, Credit: t.Artist, DurationMS: t.DurationMS,
			State: string(store.PositionOwned), TrackID: t.ID,
			TrackRef: t.MatchRef,
		}
		if t.DiscNumber != nil {
			row.DiscNumber = *t.DiscNumber
		} else {
			row.DiscNumber = 1
		}
		if t.TrackNumber != nil {
			row.TrackNumber = *t.TrackNumber
		}
		out = append(out, row)
	}
	sortRows(out)
	return out, nil
}

// sortRows 按碟号、音轨号升序排序。专辑就该按曲序读。
func sortRows(rows []AlbumRow) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0; j-- {
			a, b := rows[j-1], rows[j]
			if a.DiscNumber > b.DiscNumber ||
				(a.DiscNumber == b.DiscNumber && a.TrackNumber > b.TrackNumber) {
				rows[j-1], rows[j] = b, a
				continue
			}
			break
		}
	}
}

// FetchTracklist 补拉某专辑的曲目清单。
//
// 首次打开该专辑视图且清单状态不是完整时调用；
// 不阻塞渲染——已入库曲目已经显示，灰色行随后补入。
// 用户手动「补全清单」时 force 为真，强制重取。
func (a *App) FetchTracklist(providerID, releaseRef string, force bool) error {
	if err := a.ready(); err != nil {
		return err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return fmt.Errorf("%s", gate.Reason)
	}

	p, err := a.registry.Get(provider.ProviderID(providerID))
	if err != nil {
		return err
	}
	if err := p.IsConfigured(); err != nil {
		return err
	}

	rel, err := a.store.GetReleaseByRef(providerID, releaseRef)
	if err != nil {
		return err
	}
	if rel != nil && !force {
		// 清单已完整且未过期时无需重取
		if a.staleConfig().EffectiveTracklistState(rel) == store.TracklistComplete {
			return nil
		}
	}

	detail, err := p.FetchDetail(context.Background(), provider.Ref{
		ID: releaseRef, Country: countryOf(releaseRef),
	})
	if err != nil {
		// 标记失败，界面显示错误与重试动作
		if rel != nil {
			a.store.ReplaceTracklist(rel.ID, nil, store.TracklistFailed)
		}
		return fmt.Errorf("补拉清单失败: %w", err)
	}

	releaseID, err := a.upsertReleaseFromDetail(providerID, releaseRef, detail)
	if err != nil {
		return err
	}

	// 详情已经拉回来了，顺带把参与者与丰富信息落到已入库的同位置曲目上。
	// 此前只在单元测试里调用的艺术家子系统，到这里才真正接进生产路径。
	if err := a.enrichOwnedTracksFromDetail(providerID, releaseRef, detail); err != nil {
		// 丰富失败不应让清单补全整体失败
		_ = err
	}

	entries := make([]store.ReleaseTrack, 0, len(detail.Tracklist))
	for _, e := range detail.Tracklist {
		entries = append(entries, store.ReleaseTrack{
			DiscNumber: e.DiscNumber, TrackNumber: e.TrackNumber,
			TrackRef: e.TrackRef, RecordingRef: e.RecordingRef,
			Title: e.Title, TrackCredit: e.TrackCredit,
			DurationMS: e.DurationMS, TrackNumberText: e.TrackNumberText,
		})
	}
	state := store.TracklistComplete
	if len(entries) == 0 {
		state = store.TracklistPartial
	}
	if err := a.store.ReplaceTracklist(releaseID, entries, state); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// upsertReleaseFromDetail 由详情写入或更新发行记录。
func (a *App) upsertReleaseFromDetail(providerID, releaseRef string, d *provider.Detail) (int64, error) {
	title := releaseRef
	if d.AlbumTitle != nil {
		title = *d.AlbumTitle
	}
	now := time.Now().UTC()

	// 发行组：详情带着它时一并登记，同一专辑的不同版本才能在列表里合为一条
	var groupID *int64
	if d.ReleaseGroupRef != nil && *d.ReleaseGroupRef != "" {
		id, err := a.store.UpsertReleaseGroup(&store.ReleaseGroup{
			Provider: &providerID, ProviderRef: d.ReleaseGroupRef, Title: title,
			PrimaryType:      strPtrOrNil(store.NormalizeReleaseType(deref(d.PrimaryType))),
			FirstReleaseDate: d.FirstReleaseDate,
		})
		if err != nil {
			return 0, err
		}
		groupID = &id
	}

	return a.store.UpsertRelease(&store.Release{
		ReleaseGroupID: groupID,
		Provider:       &providerID, ProviderRef: &releaseRef,
		Title:         title,
		Credit:        d.AlbumCredit,
		PrimaryType:   strPtrOrNil(store.NormalizeReleaseType(deref(d.PrimaryType))),
		ReleaseStatus: d.ReleaseStatus, MediumFormat: d.MediumFormat,
		Country: d.Country, ReleaseDate: d.ReleaseDate,
		FirstReleaseDate: d.FirstReleaseDate,
		TrackTotal:       d.TrackTotal, DiscTotal: d.DiscTotal,
		Label:          d.Label,
		CatalogNumber:  d.CatalogNumber,
		TracklistState: store.TracklistComplete, FetchedAt: &now,
	})
}

// AddGhostRowToLibrary 把灰色行直接入库。
//
// 清单里已有 track 标识，可直接带完整溯源入库——不需要再搜一次。
func (a *App) AddGhostRowToLibrary(providerID, trackRef, title string,
	credit *string, durationMS *int, recordingRef *string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	if trackRef == "" {
		return 0, fmt.Errorf("缺少上游标识")
	}

	// 与其他三条入库路径共用同一个落点（见 admitCandidate）
	return a.admitCandidate(provider.Candidate{
		Provider: provider.ProviderID(providerID), MatchRef: trackRef,
		Title: &title, ArtistName: credit, DurationMS: durationMS, RecordingRef: recordingRef,
	})
}

func (a *App) staleConfig() store.StaleConfig {
	cfg := store.DefaultStaleConfig()
	if a.settings != nil {
		days := a.settings.Get().Cache.ITunesTracklistTTLd
		if days > 0 {
			cfg.ITunesTTL = time.Duration(days) * 24 * time.Hour
		}
	}
	return cfg
}

// countryOf 从 {country}:{id} 形态的标识中取出地区。
func countryOf(ref string) string {
	for i := 0; i < len(ref); i++ {
		if ref[i] == ':' {
			return ref[:i]
		}
	}
	return ""
}

func asAlreadyExistsErr(err error, target **store.ErrAlreadyExists) bool {
	if e, ok := err.(*store.ErrAlreadyExists); ok {
		*target = e
		return true
	}
	return false
}

// ladderConfig 返回发行优选阶梯的配置。
//
// 取自用户设置，与匹配阶段用的是同一套配置——
// 否则用户改了偏好国家或载体优先级后，两处会选出不同的代表 release。
func (a *App) ladderConfig() matching.LadderConfig {
	if a.settings == nil {
		return matching.DefaultLadderConfig()
	}
	return matching.LadderFromSettings(a.settings.Get().Matching)
}

// enrichOwnedTracksFromDetail 把一次已取得的详情落到对应的那首已入库曲目上。
//
// 一次详情请求只解析一个 recording 的关系（MB 的 release 详情里，
// recording-level-rels 与 work-rels 是挂在被解析的那条 recording 上的），
// 因此这里是「按录音标识精确对上一首」而不是喂给整张专辑——
// 把一条 recording 的作曲作词写满整张专辑，会把甲曲的作曲安到乙曲上。
//
// 匹配用录音标识而非曲目标识：用户从 CD 版入库、当前显示数字版清单时，
// track MBID 不同但 recording MBID 相同，按录音匹配才能落到用户已有的那首。
func (a *App) enrichOwnedTracksFromDetail(providerID, releaseRef string, d *provider.Detail) error {
	if d == nil || d.RecordingRef == nil || *d.RecordingRef == "" {
		return nil
	}
	if len(d.Artists) == 0 && d.TrackCredit == nil {
		return nil
	}

	rel, err := a.store.GetReleaseByRef(providerID, releaseRef)
	if err != nil || rel == nil {
		return err
	}
	positions, err := a.store.ResolveTracklist(rel.ID, providerID)
	if err != nil {
		return err
	}

	for _, pos := range positions {
		if pos.TrackID == 0 {
			continue // 灰色行没有曲目可丰富
		}
		meta, err := a.store.GetMetadata(pos.TrackID)
		if err != nil || meta == nil || meta.RecordingRef == nil {
			continue
		}
		if *meta.RecordingRef != *d.RecordingRef {
			continue // 不是同一条录音，不作丰富
		}
		perTrack := detailForPosition(d, deref(pos.Track.TrackRef))
		return a.ApplyDetailToTrack(context.Background(), pos.TrackID, providerID, perTrack)
	}
	return nil
}

// detailForPosition 从整份详情里取出某一个曲目位置的字段。
//
// 清单位置携带的标题与署名归该位置所有；作曲家等 Work 层级的关系
// 在 MB 的响应里挂在 recording 上，因此这里复用整份详情的参与者列表。
func detailForPosition(d *provider.Detail, trackRef string) *provider.Detail {
	if d == nil {
		return nil
	}
	out := *d
	out.MatchRef = trackRef

	for _, e := range d.Tracklist {
		if e.TrackRef == nil || *e.TrackRef != trackRef {
			continue
		}
		out.Title = strPtrOrNil(e.Title)
		out.TrackCredit = e.TrackCredit
		out.TrackNumber = &e.TrackNumber
		out.DiscNumber = &e.DiscNumber
		out.DurationMS = e.DurationMS
		out.RecordingRef = e.RecordingRef
		out.TrackNumberText = e.TrackNumberText
		if e.TrackCredit == nil {
			out.TrackCredit = d.AlbumCredit
		}
		return &out
	}
	return &out
}
