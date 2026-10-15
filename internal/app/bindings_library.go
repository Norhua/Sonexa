package app

import (
	"encoding/json"
	"fmt"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/store"
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
	ArtistID   int64            `json:"artistId"`
	Name       string           `json:"name"`
	Attribute  *string          `json:"attribute"`
	Attributes []string         `json:"attributes"`
	Identity   *domain.Identity `json:"identity"`
	Source     *string          `json:"source"`
}

// FieldSource 是某个字段的来源。
//
// 详情页要「逐字段显示来源徽标」：来源为 provider id、user 或 import。
type FieldSource struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Source string `json:"source"`
}

// TrackDetail 是详情抽屉的完整内容。
type TrackDetail struct {
	Release  *AlbumDetail         `json:"release"`
	Track    store.TrackView      `json:"track"`
	Metadata *store.TrackMetadata `json:"metadata"`
	/** Sources 保留逐字段来源数据；当前前端暂不显示来源徽标。 */
	Sources []FieldSource      `json:"sources"`
	Tags    []store.Collection `json:"tags"`
	// CreditText 是曲目级署名原串；可靠 CV 可作为单独标记来源的演唱兜底。
	CreditText string `json:"creditText"`
	// CreditSpans 是原串中可点击的片段；对不上的片段不在其中，渲染为纯文本。
	CreditSpans []CreditSpan `json:"creditSpans"`
	// Participants 是角色分组，按固定顺序排列；CV 派生演唱通过 Source 区分。
	// character 不单独成组——它只出现在署名原串的可点击片段中。
	Participants []ParticipantGroup `json:"participants"`
	/** 兼容来源身份字段；本地目标和采用版本由 Track.ReleaseID/CoverRevision 提供。 */
	CoverProvider string `json:"coverProvider"`
	CoverRef      string `json:"coverRef"`
	CoverGroupRef string `json:"coverGroupRef"`
	/** AdoptedCoverPath 非空时用已采用的封面，优先于临时缩略图。 */
	AdoptedCoverPath string `json:"adoptedCoverPath"`
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
	if track.ReleaseID != nil {
		detail.Release, err = a.GetAlbumDetail(*track.ReleaseID)
		if err != nil {
			return nil, err
		}
	}

	// 署名原串与成员区间：抽屉在无角色分组时以「署名」兜底；CV 派生在存储投影中完成。
	if meta != nil && meta.Credit != nil {
		detail.CreditText = *meta.Credit
	} else if track.Artist != nil {
		detail.CreditText = *track.Artist
	}
	entries, err := a.store.GetTrackCredits(trackID)
	if err != nil {
		return nil, err
	}
	detail.CreditSpans = creditSpans(entries, detail.CreditText)

	groups, err := a.participantGroups(trackID)
	if err != nil {
		return nil, err
	}
	detail.Participants = displayCVParticipants(groups, entries, detail.CreditText)
	detail.Sources = sourcesOf(meta, track)

	// 已采用的封面优先：它是用户确认过的那张。封面挂在歌集上——
	// 采用一次即对该歌集全部曲目生效。
	if meta != nil && meta.CoverPath != nil {
		detail.AdoptedCoverPath = *meta.CoverPath
	}
	// 前端显式传递 thumbnail/detail；歌集目标由后端重新解析身份和采用版本。
	if meta != nil && meta.ReleaseRef != nil && meta.MatchSource != nil {
		detail.CoverProvider = *meta.MatchSource
		detail.CoverRef = *meta.ReleaseRef
		if meta.ReleaseGroupRef != nil {
			detail.CoverGroupRef = *meta.ReleaseGroupRef
		}
	}
	return detail, nil
}

// fieldLabels 是逐字段来源徽标用的字段中文名。
var fieldLabels = map[string]string{
	"title": "曲名", "artist": "艺术家", "album": "专辑名",
	"year": "年份", "trackNumber": "音轨号", "discNumber": "碟号",
	"durationMs": "时长", "genre": "风格", "label": "厂牌",
	"catalogNumber": "目录号", "mediumFormat": "载体",
	"country": "国家", "primaryType": "类型", "comment": "注释",
}

// sourcesOf 汇总某曲目的逐字段来源，供详情抽屉显示来源徽标。
//
// 只列出有值的字段：给缺失字段标来源没有意义。
func sourcesOf(meta *store.TrackMetadata, track *store.TrackView) []FieldSource {
	provenance := map[string]string{}
	if meta != nil && meta.ProvenanceJSON != nil {
		json.Unmarshal([]byte(*meta.ProvenanceJSON), &provenance)
	}

	var out []FieldSource
	add := func(key, source string) {
		if source == "" {
			return
		}
		out = append(out, FieldSource{Key: key, Label: fieldLabels[key], Source: source})
	}

	if track != nil {
		// 曲名与署名的锁并入 locked_json，由 TrackView 解出
		add("title", basicSource(provenance, "title", track.TitleLocked))
		add("artist", basicSource(provenance, "credit", track.ArtistLocked))
		add("album", basicSource(provenance, "album", track.AlbumLocked))
	}
	if meta != nil {
		type rich struct {
			key    string
			absent bool
		}
		fields := []rich{
			{"year", meta.Year == nil}, {"trackNumber", meta.TrackNumber == nil},
			{"discNumber", meta.DiscNumber == nil}, {"durationMs", meta.DurationMS == nil},
			{"genre", meta.Genre == nil}, {"label", meta.Label == nil},
			{"catalogNumber", meta.CatalogNumber == nil}, {"mediumFormat", meta.MediumFormat == nil},
			{"country", meta.Country == nil}, {"primaryType", meta.PrimaryType == nil},
		}
		for _, f := range fields {
			if f.absent {
				continue
			}
			src := provenance[f.key]
			if src == "" {
				// 有值但没有来源记录：按匹配来源归属
				if meta.MatchSource != nil {
					src = *meta.MatchSource
				}
			}
			add(f.key, src)
		}
	}
	return out
}

// basicSource 返回基本信息字段的来源。
func basicSource(provenance map[string]string, key string, locked bool) string {
	if s := provenance[key]; s != "" {
		return s
	}
	if locked {
		return "user"
	}
	return ""
}

// creditSpans 读取署名原串中的可点击片段。
//
// 区间在解析署名时一并生成并落在 track_credit 表上，这里不反向猜测。
// 对不上的片段不在其中，由前端渲染为纯文本。
func creditSpans(entries []store.CreditEntry, text string) []CreditSpan {
	out := make([]CreditSpan, 0, len(entries))
	for _, e := range entries {
		if e.SpanStart == nil || e.SpanEnd == nil {
			continue
		}
		start, end := *e.SpanStart, *e.SpanEnd
		if start < 0 || end > len(text) || start >= end {
			continue // 区间越界则跳过，不猜测
		}
		if e.CreditedName == nil || *e.CreditedName != text[start:end] {
			continue // 署名已编辑而成员区间未重建时，不能把新文本误链到旧艺术家。
		}
		out = append(out, CreditSpan{
			Start: start, End: end, ArtistID: e.ArtistID,
			Text: text[start:end],
		})
	}
	return out
}

// participantGroups 按 role 分组并按固定顺序排列。
func (a *App) participantGroups(trackID int64) ([]ParticipantGroup, error) {
	rows, err := a.store.DB().Query(`
		SELECT ta.role, ta.artist_id, ar.name, ta.attribute, ta.position, ta.attributes_json, ar.provider, ar.provider_ref, ta.source
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
		var attributes string
		var source, provider, ref *string
		if err := rows.Scan(&role, &artistID, &name, &attribute, &position, &attributes, &provider, &ref, &source); err != nil {
			return nil, err
		}
		var attrs []string
		if err := json.Unmarshal([]byte(attributes), &attrs); err != nil {
			return nil, err
		}
		if attrs == nil {
			attrs = []string{}
		}
		var identity *domain.Identity
		if provider != nil && ref != nil {
			identity = &domain.Identity{Provider: domain.ProviderID(*provider), Ref: *ref}
		}
		byRole[role] = append(byRole[role], Participant{
			ArtistID: artistID, Name: name, Attribute: attribute,
			Attributes: attrs, Identity: identity, Source: source,
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

// CreateTrackManual 手动创建一首曲目（仅基本信息）。
func (a *App) CreateTrackManual(title string, artist, album *string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	plan, err := a.PrepareLocalAdmission(LocalAdmissionInput{Title: &title, Artist: artist, Album: album})
	if err != nil {
		return 0, err
	}
	return a.commitUnconfirmedAdmission(plan)
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

// ListArtistsForTag 根据标签收窄艺术家名单；零表示不过滤。
func (a *App) ListArtistsForTag(tagID int64) ([]store.ArtistListItem, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListArtistsForTag(tagID)
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

// ListAlbums 列出用户至少拥有一首曲目的歌集。
//
// 同一发行组的多条 release 合为一条，代表版本由优选阶梯选出——
// 阶梯配置在 App 启动与设置保存时注入 store，与匹配阶段共用同一份。
func (a *App) ListAlbums() ([]store.AlbumListItem, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListAlbums()
}

// ListAlbumsForTag 根据标签收窄专辑名单；零表示不过滤。
func (a *App) ListAlbumsForTag(tagID int64) ([]store.AlbumListItem, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	return a.store.ListAlbumsForTag(tagID)
}

// notifyLibraryChanged 通知前端曲库已变更。
func (a *App) notifyLibraryChanged() {
	a.emitEvent(EventLibraryChanged)
}
