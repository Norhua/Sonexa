package main

import (
	"context"
	"fmt"
	"time"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// AlbumRow 是专辑详情中的一行。
//
// 三种状态：已入库（正常行）、已有其他版本（正常行 + 徽章）、
// 未入库（灰色行）。灰色行的列定义与普通行完全相同。
type AlbumRow struct {
	DiscNumber  int     `json:"discNumber"`
	TrackNumber int     `json:"trackNumber"`
	NumberText  *string `json:"numberText"`
	Title       string  `json:"title"`
	Credit      *string `json:"credit"`
	DurationMS  *int    `json:"durationMs"`

	// State 为 owned | other_version | missing。
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
	Key   string `json:"key"`
	Level string `json:"level"`
	Title string `json:"title"`
	// 头部发行信息
	PrimaryType  *string `json:"primaryType"`
	Year         *int    `json:"year"`
	MediumFormat *string `json:"mediumFormat"`
	Country      *string `json:"country"`
	Provider     *string `json:"provider"`
	ReleaseRef   *string `json:"releaseRef"`

	// TracklistState 为 none | partial | complete | failed。
	TracklistState string     `json:"tracklistState"`
	FetchedAt      *time.Time `json:"fetchedAt"`
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
func (a *App) GetAlbumDetail(key, level string) (*AlbumDetail, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	albums, err := a.store.ListAlbums()
	if err != nil {
		return nil, err
	}
	var item *store.AlbumListItem
	for i := range albums {
		if albums[i].Key == key && string(albums[i].Level) == level {
			item = &albums[i]
			break
		}
	}
	if item == nil {
		return nil, fmt.Errorf("专辑不存在")
	}

	detail := &AlbumDetail{
		Key: key, Level: level, Title: item.Title,
		Year: item.Year, Provider: item.Provider,
		ReleaseRef: item.RepresentativeRef,
		OwnedCount: item.OwnedCount, TotalCount: item.TotalCount,
		TracklistState: string(store.TracklistNone),
	}

	// 已入库曲目：立即渲染，不等清单
	owned, err := a.ownedRowsFor(*item)
	if err != nil {
		return nil, err
	}

	// 派生分组没有上游事实：不显示灰色行也不显示总数
	if item.Level == store.GroupDerived {
		detail.Rows = owned
		detail.Incomplete = false
		detail.TotalCount = nil
		return detail, nil
	}

	if item.RepresentativeRef == nil || item.Provider == nil {
		detail.Rows = owned
		detail.Incomplete = true
		return detail, nil
	}

	rel, err := a.store.GetReleaseByRef(*item.Provider, *item.RepresentativeRef)
	if err != nil {
		return nil, err
	}
	if rel == nil {
		detail.Rows = owned
		detail.Incomplete = true
		return detail, nil
	}

	detail.PrimaryType = rel.PrimaryType
	detail.MediumFormat = rel.MediumFormat
	detail.Country = rel.Country
	detail.FetchedAt = rel.FetchedAt

	// 清单过期判定收口在 isStale：所有读取清单的路径都经过它
	staleCfg := a.staleConfig()
	effective := staleCfg.EffectiveTracklistState(rel)
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

	// 清单完整：三状态解析
	positions, err := a.store.ResolveTracklist(rel.ID, rel.Provider)
	if err != nil {
		return nil, err
	}
	detail.Rows = rowsFromPositions(positions)
	detail.OwnedCount = countOwned(detail.Rows)
	if rel.TrackTotal != nil {
		detail.TotalCount = rel.TrackTotal
	} else {
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
func (a *App) ownedRowsFor(item store.AlbumListItem) ([]AlbumRow, error) {
	f := store.TrackFilter{}
	switch item.Level {
	case store.GroupByReleaseGroup:
		f.ReleaseGroupRef = item.Key
	case store.GroupByRelease:
		f.ReleaseRef = item.Key
	default:
		f.DerivedAlbumKey = item.Key
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
	return a.store.UpsertRelease(&store.Release{
		Provider: providerID, ProviderRef: releaseRef,
		ReleaseGroupRef: d.ReleaseGroupRef, Title: title,
		AlbumCredit: d.AlbumCredit, PrimaryType: d.PrimaryType,
		ReleaseStatus: d.ReleaseStatus, MediumFormat: d.MediumFormat,
		Country: d.Country, ReleaseDate: d.ReleaseDate,
		FirstReleaseDate: d.FirstReleaseDate,
		TrackTotal:       d.TrackTotal, DiscTotal: d.DiscTotal,
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

	trackID, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: title, Artist: credit,
		MatchSource: &providerID, MatchRef: &trackRef,
		Metadata: &store.MetadataInput{
			TrackCredit: credit, DurationMS: durationMS, RecordingMBID: recordingRef,
		},
	})
	var exists *store.ErrAlreadyExists
	if err != nil && asAlreadyExistsErr(err, &exists) {
		// 并发加入同一结果：静默跳转到已有曲目
		return exists.TrackID, nil
	}
	if err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return trackID, nil
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
