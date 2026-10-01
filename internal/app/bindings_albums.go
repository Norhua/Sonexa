package app

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
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
	AddedAt     *string `json:"addedAt"`

	// State 为 owned | missing。
	State string `json:"state"`
	// TrackID 非零时可点开详情；灰色行为 0。
	TrackID int64 `json:"trackId"`
	// Ghost 为真表示灰色行：不可选中、单击不开抽屉、右键仅两项、
	// 不计入选中计数与曲目数、不参与导出。
	Ghost bool `json:"ghost"`
	// TrackRef 供灰色行的「加入曲库」直接带溯源入库。
	TrackRef         *string `json:"trackRef"`
	RecordingRef     *string `json:"recordingRef"`
	OutsideTracklist bool    `json:"outsideTracklist"`
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
	PrimaryType   *string       `json:"primaryType"`
	Year          *int          `json:"year"`
	MediumFormat  *string       `json:"mediumFormat"`
	Country       *string       `json:"country"`
	Label         *string       `json:"label"`
	CatalogNumber *string       `json:"catalogNumber"`
	DiscTotal     *int          `json:"discTotal"`
	Media         []AlbumMedium `json:"media"`
	Provider      *string       `json:"provider"`
	ReleaseRef    *string       `json:"releaseRef"`
	// ReleaseGroupRef 与 Provider/ReleaseRef 一起构成封面地址的来源。
	ReleaseGroupRef *string `json:"releaseGroupRef"`
	// CoverPath 非空时是已采用封面的落盘路径，优先于临时缩略图。
	CoverPath     *string `json:"coverPath"`
	CoverRevision int64   `json:"coverRevision"`
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

type AlbumMedium struct {
	Number     int  `json:"number"`
	TrackTotal *int `json:"trackTotal"`
}

// AlbumGroupDetail 保留所有发行边界，组收藏数不按录音去重。
type AlbumGroupDetail struct {
	GroupID          int64          `json:"groupId"`
	Title            string         `json:"title"`
	Credit           *string        `json:"credit"`
	RepresentativeID int64          `json:"representativeId"`
	OwnedCount       int            `json:"ownedCount"`
	MemberCount      int            `json:"memberCount"`
	Releases         []*AlbumDetail `json:"releases"`
}

func (a *App) GetAlbumGroupDetail(groupID int64) (*AlbumGroupDetail, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	members, err := a.store.ListGroupReleases(groupID)
	if err != nil {
		return nil, err
	}
	group, err := a.store.GetReleaseGroup(groupID)
	if err != nil {
		return nil, err
	}
	if group == nil {
		return nil, fmt.Errorf("专辑组不存在")
	}
	result := &AlbumGroupDetail{GroupID: groupID, Title: group.Title, MemberCount: len(members), Releases: []*AlbumDetail{}}
	result.Credit, err = a.store.GroupCredit(groupID)
	if err != nil {
		return nil, err
	}
	rep, err := a.store.RepresentativeRelease(groupID)
	if err != nil {
		return nil, err
	}
	if rep != nil {
		result.RepresentativeID = rep.ID
	}
	for _, member := range members {
		detail, err := a.GetAlbumDetail(member.ID)
		if err != nil {
			return nil, err
		}
		result.Releases = append(result.Releases, detail)
		result.OwnedCount += detail.OwnedCount
	}
	return result, nil
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
		CoverRevision: a.CoverRevision(rel.ID),
		ReleaseID:     rel.ID, Title: rel.Title, Credit: rel.Credit,
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
	media, err := a.store.ReleaseMedia(rel.ID)
	if err != nil {
		return nil, err
	}
	detail.Media = []AlbumMedium{}
	for _, m := range media {
		var count *int
		if m.TrackTotal.State == domain.Value {
			n := m.TrackTotal.Value
			count = &n
		}
		detail.Media = append(detail.Media, AlbumMedium{Number: m.Number, TrackTotal: count})
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

	// 本地发行只按用户完整填写的碟结构显示总数，不虚构灰色行
	if rel.ProviderRef == nil || rel.Provider == nil {
		if detail.DiscTotal != nil && *detail.DiscTotal == len(detail.Media) {
			total, known := 0, true
			for _, m := range detail.Media {
				if m.TrackTotal == nil {
					known = false
					break
				}
				total += *m.TrackTotal
			}
			if known {
				detail.TotalCount = &total
			}
		}
		detail.Rows = owned
		return detail, nil
	}

	// 已存清单按真实状态读取，不因来源或获取日期作废。
	effective := rel.TracklistState
	detail.TracklistState = string(effective)

	if effective != store.TracklistComplete {
		// 不伪装完整：只列已入库曲目，末尾提示清单不完整
		detail.Rows = owned
		detail.Incomplete = true
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
	// 清单提供灰色位置；收藏始终显示当前值，清单外收藏也不能丢失。
	detail.Rows = []AlbumRow{}
	inList := map[int64]bool{}
	current := map[int64]AlbumRow{}
	for _, row := range owned {
		current[row.TrackID] = row
	}
	for _, row := range rowsFromPositions(positions) {
		if row.TrackID != 0 {
			inList[row.TrackID] = true
			row = current[row.TrackID]
		}
		detail.Rows = append(detail.Rows, row)
	}
	for _, row := range owned {
		if !inList[row.TrackID] {
			row.OutsideTracklist = true
			detail.Rows = append(detail.Rows, row)
		}
	}
	sortRows(detail.Rows)
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
			AddedAt:     p.AddedAt,
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

	tracks, err := a.store.ListTracks(f)
	if err != nil {
		return nil, err
	}

	out := make([]AlbumRow, 0, len(tracks))
	for _, t := range tracks {
		row := AlbumRow{
			Title: t.Title, Credit: t.Artist, DurationMS: t.DurationMS,
			State: string(store.PositionOwned), TrackID: t.ID,
			TrackRef: t.MatchRef, AddedAt: &t.AddedAt,
		}
		if t.DiscNumber != nil {
			row.DiscNumber = *t.DiscNumber
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
		// 完整清单不自动重取；显式 force 绕过状态与响应缓存。
		if rel.TracklistState == store.TracklistComplete {
			return nil
		}
	}

	if rel == nil {
		return fmt.Errorf("发行尚未加入曲库，请先预览准入")
	}
	revision, err := a.store.ReleaseRevision(rel.ID)
	if err != nil {
		return err
	}
	done := a.beginInteractive()
	defer done()
	snapshot, err := p.Complete(provider.WithPriority(context.Background(), provider.PriorityInteractive), domain.DetailTarget{Release: domain.Identity{Provider: provider.ProviderID(providerID), Ref: releaseRef}, SkipCache: force})
	if err != nil {
		return fmt.Errorf("补拉清单失败: %w", err)
	}
	if err := a.store.ApplyReleaseSnapshot(rel.ID, snapshot, revision); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// PrepareGhostAdmission 根据发行及位置预览准入。
func (a *App) PrepareGhostAdmission(releaseID int64, disc, position int) (*domain.AdmissionPlan, error) {
	service, err := a.libraryService()
	if err != nil {
		return nil, err
	}
	return service.PrepareGhostAdmission(a.ctx, releaseID, disc, position)
}
func (a *App) AddGhostRowToLibrary(releaseID int64, disc, position int) (int64, error) {
	plan, err := a.PrepareGhostAdmission(releaseID, disc, position)
	if err != nil {
		return 0, err
	}
	return a.commitUnconfirmedAdmission(plan)
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
