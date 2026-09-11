package main

import (
	"encoding/json"
	"fmt"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// TrackPage 是一页曲目查询结果。
type TrackPage struct {
	Tracks []store.TrackView `json:"tracks"`
	Total  int               `json:"total"`
}

// ListTracks 按条件查询曲目。
func (a *App) ListTracks(f store.TrackFilter) (TrackPage, error) {
	if err := a.ready(); err != nil {
		return TrackPage{}, err
	}
	tracks, err := a.store.ListTracks(f)
	if err != nil {
		return TrackPage{}, err
	}
	total, err := a.store.CountTracks(f)
	if err != nil {
		return TrackPage{}, err
	}
	if tracks == nil {
		tracks = []store.TrackView{}
	}
	return TrackPage{Tracks: tracks, Total: total}, nil
}

// CreditSpan 是署名原串中的一段可点击区间。
type CreditSpan struct {
	Start    int    `json:"start"`
	End      int    `json:"end"`
	ArtistID int64  `json:"artistId"`
	Text     string `json:"text"`
}

// ParticipantGroup 是详情抽屉参与者区的一个分组。
type ParticipantGroup struct {
	Role    string        `json:"role"`
	Label   string        `json:"label"`
	Artists []Participant `json:"artists"`
}

// Participant 是一位参与者。
type Participant struct {
	ArtistID  int64   `json:"artistId"`
	Name      string  `json:"name"`
	Attribute *string `json:"attribute"`
}

// TrackDetail 是详情抽屉的完整内容。
type TrackDetail struct {
	Track    store.TrackView      `json:"track"`
	Metadata *store.TrackMetadata `json:"metadata"`
	Tags     []store.Collection   `json:"tags"`
	// CreditText 是曲目级署名原串，演唱区优先渲染它。
	CreditText string `json:"creditText"`
	// CreditSpans 是原串中可点击的片段；对不上的片段不在其中，渲染为纯文本。
	CreditSpans []CreditSpan `json:"creditSpans"`
	// Participants 是除演唱外的角色分组，按固定顺序排列。
	// character 不单独成组——它只出现在署名原串的可点击片段中。
	Participants []ParticipantGroup `json:"participants"`
	CoverKey     string             `json:"coverKey"`
}

// GetTrackDetail 读取详情抽屉所需的全部内容。
//
// 丰富信息整体为空时（「无需匹配」入库的曲目）也要正常返回，
// 由前端对缺失字段显示占位短横线。
func (a *App) GetTrackDetail(trackID int64) (*TrackDetail, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	track, err := a.store.GetTrackView(trackID)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, fmt.Errorf("曲目不存在")
	}

	meta, err := a.store.GetMetadata(trackID)
	if err != nil {
		return nil, err
	}
	tags, err := a.store.TagsOfTrack(trackID)
	if err != nil {
		return nil, err
	}

	detail := &TrackDetail{Track: *track, Metadata: meta, Tags: tags}

	// 演唱区：优先渲染署名原串 + 可点击区间
	if meta != nil && meta.TrackCredit != nil {
		detail.CreditText = *meta.TrackCredit
		detail.CreditSpans = parseSpans(meta.CreditSpansJSON, detail.CreditText)
	} else if track.Artist != nil {
		detail.CreditText = *track.Artist
	}

	groups, err := a.participantGroups(trackID)
	if err != nil {
		return nil, err
	}
	detail.Participants = groups

	// 封面键必须与注册降级链时用的尺寸一致，否则 key 对不上、handler 取不到图。
	// BuildCoverRefs 一律按缩略图尺寸注册，详情也用同一尺寸取图，
	// 由 <img> 的显示尺寸决定呈现大小。
	if meta != nil && meta.ReleaseMBID != nil && meta.MatchSource != nil {
		detail.CoverKey = coverKeyFor(*meta.MatchSource, *meta.ReleaseMBID, coverThumbSize)
	}
	return detail, nil
}

// parseSpans 解析 credit_spans_json。
//
// 这是解析时一并生成的纯渲染辅助数据，不在这里反向猜测。
func parseSpans(raw *string, text string) []CreditSpan {
	if raw == nil || *raw == "" {
		return nil
	}
	var parsed matching.CreditSpans
	if err := json.Unmarshal([]byte(*raw), &parsed); err != nil {
		return nil
	}
	out := make([]CreditSpan, 0, len(parsed.Spans))
	for _, s := range parsed.Spans {
		if s.Start < 0 || s.End > len(text) || s.Start >= s.End {
			continue // 区间越界则跳过，不猜测
		}
		out = append(out, CreditSpan{
			Start: s.Start, End: s.End, ArtistID: s.ArtistID,
			Text: text[s.Start:s.End],
		})
	}
	return out
}

// participantGroups 按 role 分组并按固定顺序排列。
func (a *App) participantGroups(trackID int64) ([]ParticipantGroup, error) {
	rows, err := a.store.DB().Query(`
		SELECT ta.role, ta.artist_id, COALESCE(ta.credited_name, ar.name), ta.attribute, ta.position
		FROM track_artist ta JOIN artist ar ON ar.id = ta.artist_id
		WHERE ta.track_id = ?
		ORDER BY ta.position`, trackID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byRole := map[string][]Participant{}
	for rows.Next() {
		var role, name string
		var artistID int64
		var attribute *string
		var position *int
		if err := rows.Scan(&role, &artistID, &name, &attribute, &position); err != nil {
			return nil, err
		}
		byRole[role] = append(byRole[role], Participant{
			ArtistID: artistID, Name: name, Attribute: attribute,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []ParticipantGroup{}
	// 固定顺序：演唱 → 器乐演奏 → 演出 → 作曲 → 作词 → 编曲 → 制作 → 混音 → 指挥
	// character 不在 RoleDisplayOrder 中，因此不会单独成组。
	for _, role := range store.RoleDisplayOrder {
		artists := byRole[string(role)]
		if len(artists) == 0 {
			continue
		}
		out = append(out, ParticipantGroup{
			Role:    string(role),
			Label:   store.RoleLabel(role),
			Artists: artists,
		})
	}
	return out, nil
}

// coverKeyFor 生成封面缓存键。
func coverKeyFor(provider, ref string, size int) string {
	return covers_CacheKey(provider+":"+ref, size)
}

// CreateTrackManual 手动创建一首曲目（仅基本信息）。
func (a *App) CreateTrackManual(title string, artist, album *string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	id, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: title, Artist: artist, Album: album,
	})
	if err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return id, nil
}

// UpdateTrackBasic 手动修改基本信息。被修改的字段加锁。
func (a *App) UpdateTrackBasic(trackID int64, title, artist, album *string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.UpdateTrackFields(trackID, title, artist, album, true); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// DeleteTracks 删除曲目。
//
// 一并清理丰富信息、歌单与标签归属、艺术家关联；不影响发行清单数据——
// 被删的位置在专辑视图中回落为灰色行。
func (a *App) DeleteTracks(trackIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	for _, id := range trackIDs {
		if err := a.store.DeleteTrack(id); err != nil {
			return err
		}
	}
	a.notifyLibraryChanged()
	return nil
}

// --- 歌单与标签 ---

// ListCollections 列出歌单或标签。
func (a *App) ListCollections(kind string) ([]store.Collection, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListCollections(store.CollectionKind(kind))
}

// CreateCollection 新建歌单或标签。
func (a *App) CreateCollection(name, kind string, color *string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	id, err := a.store.CreateCollection(name, store.CollectionKind(kind), color)
	if err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return id, nil
}

// RenameCollection 重命名歌单或标签。
func (a *App) RenameCollection(id int64, name string) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.RenameCollection(id, name)
}

// DeleteCollection 删除歌单或标签。曲目本身不受影响。
func (a *App) DeleteCollection(id int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.DeleteCollection(id); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// AddToCollection 把曲目加入歌单或标签。
//
// 该入口只在库内的曲目表格中提供；上游搜索页与复核队列不提供。
func (a *App) AddToCollection(collectionID int64, trackIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.AddToCollection(collectionID, trackIDs); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// RemoveFromCollection 把曲目从歌单或标签中移出。
func (a *App) RemoveFromCollection(collectionID int64, trackIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.RemoveFromCollection(collectionID, trackIDs); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// ReorderCollection 按给定顺序重排歌单。
func (a *App) ReorderCollection(collectionID int64, orderedTrackIDs []int64) error {
	if err := a.ready(); err != nil {
		return err
	}
	return a.store.ReorderCollection(collectionID, orderedTrackIDs)
}

// --- 浏览视图 ---

// ListArtists 列出全部有曲目关联的艺术家。
func (a *App) ListArtists() ([]store.ArtistListItem, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListArtists()
}

// ArtistHeader 是艺术家视图的头部信息。
type ArtistHeader struct {
	ID     int64             `json:"id"`
	Title  string            `json:"title"`
	Facets []store.RoleFacet `json:"facets"`
}

// GetArtistHeader 返回艺术家视图的标题与角色分面。
func (a *App) GetArtistHeader(artistID int64) (*ArtistHeader, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	artists, err := a.store.ListArtists()
	if err != nil {
		return nil, err
	}
	var title string
	for _, it := range artists {
		if it.ID == artistID {
			title = it.DisplayTitle()
			break
		}
	}
	if title == "" {
		artist, err := a.store.GetArtist(artistID)
		if err != nil {
			return nil, err
		}
		if artist == nil {
			return nil, fmt.Errorf("艺术家不存在")
		}
		title = artist.Name
	}

	facets, err := a.store.RoleFacets(artistID)
	if err != nil {
		return nil, err
	}
	return &ArtistHeader{ID: artistID, Title: title, Facets: facets}, nil
}

// ListAlbums 列出专辑，按分组键三级降级。
func (a *App) ListAlbums() ([]store.AlbumListItem, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListAlbums(a.ladderConfig())
}

// notifyLibraryChanged 通知前端曲库已变更。
func (a *App) notifyLibraryChanged() {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, EventLibraryChanged)
	}
}
