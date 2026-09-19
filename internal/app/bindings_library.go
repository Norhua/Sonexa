package app

import (
	"encoding/json"
	"fmt"
	"strings"

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
	ArtistID  int64   `json:"artistId"`
	Name      string  `json:"name"`
	Attribute *string `json:"attribute"`
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
	Track    store.TrackView      `json:"track"`
	Metadata *store.TrackMetadata `json:"metadata"`
	/** Sources 保留逐字段来源数据；当前前端暂不显示来源徽标。 */
	Sources []FieldSource      `json:"sources"`
	Tags    []store.Collection `json:"tags"`
	// CreditText 是曲目级署名原串，不作为演唱关系的依据。
	CreditText string `json:"creditText"`
	// CreditSpans 是原串中可点击的片段；对不上的片段不在其中，渲染为纯文本。
	CreditSpans []CreditSpan `json:"creditSpans"`
	// Participants 是全部实际角色的分组，按固定顺序排列。
	// character 不单独成组——它只出现在署名原串的可点击片段中。
	Participants []ParticipantGroup `json:"participants"`
	/** CoverProvider / CoverRef / CoverGroupRef 是临时缩略图的来源标识。 */
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

	// 署名原串与成员区间：抽屉在无角色分组时以「署名」兜底，不推断演唱。
	if meta != nil && meta.Credit != nil {
		detail.CreditText = *meta.Credit
	} else if track.Artist != nil {
		detail.CreditText = *track.Artist
	}
	spans, err := a.creditSpans(trackID, detail.CreditText)
	if err != nil {
		return nil, err
	}
	detail.CreditSpans = spans

	groups, err := a.participantGroups(trackID)
	if err != nil {
		return nil, err
	}
	detail.Participants = groups
	detail.Sources = sourcesOf(meta, track)

	// 已采用的封面优先：它是用户确认过的那张。封面挂在歌集上——
	// 采用一次即对该歌集全部曲目生效。
	if meta != nil && meta.CoverPath != nil {
		detail.AdoptedCoverPath = *meta.CoverPath
	}
	// 临时缩略图给来源标识，由前端拼成 /cover 地址——handler 据此就地构造降级链。
	// 详情与列表用同一个尺寸取图，由 <img> 的显示尺寸决定呈现大小：
	// 换一个尺寸就是另一个缓存键，等于为同一张图再出网取一次。
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
func (a *App) creditSpans(trackID int64, text string) ([]CreditSpan, error) {
	entries, err := a.store.GetTrackCredits(trackID)
	if err != nil {
		return nil, err
	}
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
	return out, nil
}

// participantGroups 按 role 分组并按固定顺序排列。
func (a *App) participantGroups(trackID int64) ([]ParticipantGroup, error) {
	rows, err := a.store.DB().Query(`
		SELECT ta.role, ta.artist_id, ar.name, ta.attribute, ta.position
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

// CreateTrackManual 手动创建一首曲目（仅基本信息）。
func (a *App) CreateTrackManual(title string, artist, album *string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	id, err := a.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: title, Credit: artist, Album: album, AlbumCredit: artist,
	})
	if err != nil {
		return 0, err
	}
	a.notifyLibraryChanged()
	return id, nil
}

// UpdateTrackBasic 手动修改曲名与署名。被修改的字段加锁。
//
// 歌集名不在这里改：歌集升格为实体后，一首曲目的「歌集名」是它所属歌集的
// 属性，改它对该歌集的全部曲目生效。那需要用户先决定影响范围，
// 因此走 PlanAlbumRename / ApplyAlbumRename 两步。
func (a *App) UpdateTrackBasic(trackID int64, title, artist *string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.store.UpdateTrackFields(trackID, title, artist, true); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// AlbumRenamePlan 描述一次「改歌集名」会影响什么，供界面二次确认。
type AlbumRenamePlan struct {
	// TrackCount 是当前歌集下的曲目数。为 1 时无需二次确认。
	TrackCount int `json:"trackCount"`
	// CurrentTitle 是当前歌集名。
	CurrentTitle string `json:"currentTitle"`
	// ExistingReleaseID 非零时库中已有叫新名字的歌集，可直接关联过去。
	ExistingReleaseID int64 `json:"existingReleaseId"`
	// NeedsConfirm 为真时界面必须让用户选择影响范围。
	NeedsConfirm bool `json:"needsConfirm"`
}

// PlanAlbumRename 计算改歌集名的影响范围。
//
// 歌集是共享实体，改名默认影响该歌集下的全部曲目。只有一首时没有歧义，
// 直接改；不止一首时界面要让用户在「改这张歌集 / 只把这首挪走 /
// 关联到已有的同名歌集」之间选。
func (a *App) PlanAlbumRename(trackID int64, newTitle string) (*AlbumRenamePlan, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	track, err := a.store.GetTrack(trackID)
	if err != nil {
		return nil, err
	}
	if track == nil {
		return nil, fmt.Errorf("曲目不存在")
	}

	plan := &AlbumRenamePlan{}
	if track.Album != nil {
		plan.CurrentTitle = *track.Album
	}
	if track.ReleaseID != nil {
		n, err := a.store.ReleaseTrackCount(*track.ReleaseID)
		if err != nil {
			return nil, err
		}
		plan.TrackCount = n
	}

	existing, err := a.store.FindReleaseByTitleCredit(newTitle, track.Credit)
	if err != nil {
		return nil, err
	}
	if track.ReleaseID == nil || existing != *track.ReleaseID {
		plan.ExistingReleaseID = existing
	}
	plan.NeedsConfirm = plan.TrackCount > 1
	return plan, nil
}

// 改歌集名的三种处置。
const (
	// AlbumRenameThisRelease 改这张歌集本身，影响它下面的全部曲目。
	AlbumRenameThisRelease = "release"
	// AlbumRenameMoveTrack 只把这首曲目挪到新歌集下，原歌集不动。
	AlbumRenameMoveTrack = "move"
	// AlbumRenameLinkExisting 把这首曲目关联到库中已有的同名歌集。
	AlbumRenameLinkExisting = "link"
)

// ApplyAlbumRename 按用户选定的处置改歌集名。
func (a *App) ApplyAlbumRename(trackID int64, newTitle, mode string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if strings.TrimSpace(newTitle) == "" && mode != AlbumRenameMoveTrack {
		return fmt.Errorf("歌集名不能为空")
	}
	track, err := a.store.GetTrack(trackID)
	if err != nil {
		return err
	}
	if track == nil {
		return fmt.Errorf("曲目不存在")
	}

	switch mode {
	case AlbumRenameThisRelease:
		if track.ReleaseID == nil {
			// 还没有歌集：等同于挪到一条新的本地歌集
			return a.moveTrackToNewRelease(trackID, newTitle, track.Credit)
		}
		if err := a.store.UpdateReleaseFields(*track.ReleaseID,
			map[string]string{"title": newTitle},
			map[string]string{"title": "user"},
			map[string]bool{"title": true}); err != nil {
			return err
		}
	case AlbumRenameLinkExisting:
		existing, err := a.store.FindReleaseByTitleCredit(newTitle, track.Credit)
		if err != nil {
			return err
		}
		if existing == 0 {
			return fmt.Errorf("库中没有叫「%s」的歌集", newTitle)
		}
		if err := a.store.MoveTrackToRelease(trackID, existing); err != nil {
			return err
		}
	case AlbumRenameMoveTrack:
		if err := a.moveTrackToNewRelease(trackID, newTitle, track.Credit); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知的处置方式: %s", mode)
	}

	a.notifyLibraryChanged()
	return nil
}

// moveTrackToNewRelease 新建一条本地歌集并把曲目挪过去。
func (a *App) moveTrackToNewRelease(trackID int64, title string, credit *string) error {
	releaseID, err := a.store.FindOrCreateLocalRelease(title, credit)
	if err != nil {
		return err
	}
	return a.store.MoveTrackToRelease(trackID, releaseID)
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
