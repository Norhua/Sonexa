package store

import (
	"fmt"
)

// ArtistListItem 是艺术家列表（中列）的一项。
type ArtistListItem struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Type       *string `json:"type"`
	TrackCount int     `json:"trackCount"`
	// VoiceActor 非空时该艺术家是角色，视图标题渲染为「角色名 (CV: 声优名)」。
	VoiceActor *string `json:"voiceActor"`
}

// artistListMembers 是艺术家列表的成员判定。
//
// 一位艺术家出现在列表里，当且仅当他**在某曲目的署名中出现**，
// 或**在某曲目上担任演唱**。这正好覆盖用户要的两类：署名艺术家
// （如只作词作曲但仍署名的 DECO*27）与虚拟歌姬 / 真人主唱
// （凭 vocal 关系进来，哪怕没有出现在署名串里）。
//
// 未署名的作词、作曲、编曲、制作等不满足任一条，因此不进列表——
// 此前无条件 JOIN track_artist 把这些人也列了出来，正是「艺术家视图
// 出现未署名的作词作曲」的成因。进入某位艺术家之后，他的曲目列表
// 仍展示全部角色（见 buildTrackQuery 的 credited 分面），列表成员
// 与视图内容是两件事。
const artistListMembers = `
	SELECT track_id, artist_id FROM track_credit
	UNION
	SELECT track_id, artist_id FROM track_artist WHERE role = 'vocal'`

// ListArtists 列出应出现在艺术家列表中的艺术家，含曲目数。
func (s *Store) ListArtists() ([]ArtistListItem, error) {
	return s.ListArtistsForTag(0)
}

// ListArtistsForTag 按带标签的署名/演唱曲目限定名单，计数仍为完整收藏数。
func (s *Store) ListArtistsForTag(tagID int64) ([]ArtistListItem, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.name, a.type,
		       COUNT(DISTINCT x.track_id) AS track_count,
		       (SELECT va.name FROM artist_link l JOIN artist va ON va.id = l.to_artist_id
		        WHERE l.from_artist_id = a.id AND l.type = 'voice_of' LIMIT 1)
		FROM artist a
		JOIN (`+artistListMembers+`) x ON x.artist_id = a.id
		GROUP BY a.id
		HAVING ? = 0 OR MAX(EXISTS (SELECT 1 FROM collection_item ci
			WHERE ci.track_id = x.track_id AND ci.collection_id = ?))
		ORDER BY track_count DESC, a.name`, tagID, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ArtistListItem{}
	for rows.Next() {
		var it ArtistListItem
		if err := rows.Scan(&it.ID, &it.Name, &it.Type, &it.TrackCount, &it.VoiceActor); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// DisplayTitle 返回艺术家视图的标题。
//
// 角色类型且存在配音关系时渲染为「涼宮ハルヒ (CV: 平野綾)」。
func (a ArtistListItem) DisplayTitle() string {
	if a.VoiceActor != nil && *a.VoiceActor != "" {
		return fmt.Sprintf("%s (CV: %s)", a.Name, *a.VoiceActor)
	}
	return a.Name
}

// RoleFacet 是艺术家视图的一个角色分面。
type RoleFacet struct {
	Role  string `json:"role"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

// RoleFacets 返回某艺术家各参与角色的曲目数。
//
// 「全部 14」「署名 12」「演唱 12」「作曲 0」「作词 2」——
// 计数为零的分面也要列出，否则用户无法得知该艺术家在此角色上没有作品。
//
// 「全部」是该艺术家关联到的全部曲目：署名 ∪ 所有角色。它可能大于任何
// 单一角色分面之和，因为一首歌里他可能既被署名又担任某角色。「署名」
// 分面单列——它是列表成员判定的一半，用户应当能只看被署名的那些。
func (s *Store) RoleFacets(artistID int64) ([]RoleFacet, error) {
	var total int
	if err := s.db.QueryRow(`
		SELECT COUNT(DISTINCT track_id) FROM (
			SELECT track_id FROM track_credit WHERE artist_id = ?
			UNION
			SELECT track_id FROM track_artist WHERE artist_id = ?
		)`, artistID, artistID).Scan(&total); err != nil {
		return nil, err
	}

	var creditedCount int
	if err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT track_id) FROM track_credit WHERE artist_id = ?`,
		artistID).Scan(&creditedCount); err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`
		SELECT role, COUNT(DISTINCT track_id) FROM track_artist
		WHERE artist_id = ? GROUP BY role`, artistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			return nil, err
		}
		counts[role] = n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	facets := []RoleFacet{
		{Role: "", Label: "全部", Count: total},
		{Role: RoleCredited, Label: "署名", Count: creditedCount},
	}
	// 按固定顺序列出，使界面顺序稳定
	for _, role := range RoleDisplayOrder {
		facets = append(facets, RoleFacet{
			Role:  string(role),
			Label: RoleLabel(role),
			Count: counts[string(role)],
		})
	}
	return facets, nil
}

// RoleLabel 返回角色的中文名。
func RoleLabel(role ArtistRole) string {
	switch role {
	case RoleVocal:
		return "演唱"
	case RoleInstrument:
		return "器乐演奏"
	case RolePerformer:
		return "演出"
	case RoleComposer:
		return "作曲"
	case RoleLyricist:
		return "作词"
	case RoleWriter:
		return "词曲作者"
	case RoleArranger:
		return "编曲"
	case RoleProducer:
		return "制作"
	case RoleRemixer:
		return "混音"
	case RoleConductor:
		return "指挥"
	case RoleCharacter:
		return "角色"
	case RoleAlbumArtist:
		return "专辑艺术家"
	default:
		return string(role)
	}
}

// AlbumListItem 是专辑列表（中列）的一项。
//
// 专辑升格为实体后，一条列表项就是一条 release（或一个发行组的代表 release），
// 不再有查询时现场派生的分组。
type AlbumListItem struct {
	// ReleaseID 是这条专辑对应的歌集 id。
	ReleaseID int64 `json:"releaseId"`
	// GroupID 非零时这条专辑代表一个发行组。
	GroupID int64  `json:"groupId"`
	Title   string `json:"title"`
	// Credit 是发行级署名，缺失时退为空。
	Credit *string `json:"credit"`
	Year   *int    `json:"year"`
	// OwnedCount 是用户已入库的曲目数。
	OwnedCount int `json:"ownedCount"`
	// TotalCount 是上游声明的清单总数；本地歌集为 nil，界面呈现为 `3/-`。
	TotalCount *int `json:"totalCount"`
	// RepresentativeRef 是代表 release 的标识，供清单读取使用。
	RepresentativeRef *string `json:"representativeRef"`
	Provider          *string `json:"provider"`
	ReleaseGroupRef   *string `json:"releaseGroupRef"`
	// CoverPath / CoverSourceURL 供列表直接渲染封面。
	CoverPath      *string `json:"coverPath"`
	CoverSourceURL *string `json:"coverSourceUrl"`
}

// CountLabel 返回「已入库/清单总数」的呈现。
func (a AlbumListItem) CountLabel() string {
	if a.TotalCount == nil {
		return fmt.Sprintf("%d/-", a.OwnedCount)
	}
	return fmt.Sprintf("%d/%d", a.OwnedCount, *a.TotalCount)
}

// ListAlbums 列出用户至少拥有一首曲目的歌集。
//
// 发行组存在时，同组的多条 release 合为一条，代表 release 由优选阶梯选出
// （偏好实体载体、偏好国家等），封面与总数取自它。发行组不存在时一条
// release 就是一条专辑。一张专辑仅在用户至少拥有其中一首时出现——
// 灰色行用于补全已开始的收藏，不是上游目录浏览。
func (s *Store) ListAlbums() ([]AlbumListItem, error) {
	rows, err := s.db.Query(`
		SELECT r.id, r.release_group_id, r.title, r.credit, r.provider, r.provider_ref,
		       r.release_date, r.first_release_date, r.track_total,
		       r.cover_path, r.cover_source_url,
		       (SELECT rg.provider_ref FROM release_group rg WHERE rg.id = r.release_group_id),
		       COUNT(t.id)
		FROM release r
		JOIN track t ON t.release_id = r.id
		GROUP BY r.id
		ORDER BY r.title`)
	if err != nil {
		return nil, fmt.Errorf("查询专辑失败: %w", err)
	}
	defer rows.Close()

	var flat []AlbumListItem
	for rows.Next() {
		var (
			it                            AlbumListItem
			groupID                       *int64
			releaseDate, firstReleaseDate *string
		)
		if err := rows.Scan(&it.ReleaseID, &groupID, &it.Title, &it.Credit,
			&it.Provider, &it.RepresentativeRef, &releaseDate, &firstReleaseDate,
			&it.TotalCount, &it.CoverPath, &it.CoverSourceURL,
			&it.ReleaseGroupRef, &it.OwnedCount); err != nil {
			return nil, err
		}
		if groupID != nil {
			it.GroupID = *groupID
		}
		it.Year = (&Release{ReleaseDate: releaseDate, FirstReleaseDate: firstReleaseDate}).Year()
		flat = append(flat, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return s.mergeAlbumGroups(flat), nil
}

// ListAlbumsForTag 仅保留拥有带标签曲目的专辑；不改变代表版本、收藏数与清单总数。
func (s *Store) ListAlbumsForTag(tagID int64) ([]AlbumListItem, error) {
	albums, err := s.ListAlbums()
	if err != nil || tagID == 0 {
		return albums, err
	}
	rows, err := s.db.Query(`SELECT DISTINCT r.id, COALESCE(r.release_group_id, 0)
		FROM release r JOIN track t ON t.release_id = r.id
		JOIN collection_item ci ON ci.track_id = t.id WHERE ci.collection_id = ?`, tagID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	releases, groups := map[int64]bool{}, map[int64]bool{}
	for rows.Next() {
		var releaseID, groupID int64
		if err := rows.Scan(&releaseID, &groupID); err != nil {
			return nil, err
		}
		releases[releaseID] = true
		if groupID != 0 {
			groups[groupID] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []AlbumListItem{}
	for _, album := range albums {
		if releases[album.ReleaseID] || groups[album.GroupID] {
			out = append(out, album)
		}
	}
	return out, nil
}

// mergeAlbumGroups 把同一发行组的多条 release 合并为一条，保持首次出现的顺序。
//
// 合并后已入库数求和，代表 release 由优选阶梯选出，封面与清单总数取代表的、
// 缺失时退到组内任一非空值——同一张专辑的 CD 版与数字版在列表里是一条，
// 不该因为用户恰好从数字版入库而丢掉 CD 版已有的封面。
func (s *Store) mergeAlbumGroups(flat []AlbumListItem) []AlbumListItem {
	out := []AlbumListItem{}
	slotOfGroup := map[int64]int{}
	membersOfGroup := map[int64][]AlbumListItem{}

	for _, it := range flat {
		if it.GroupID == 0 {
			out = append(out, it)
			continue
		}
		if _, seen := slotOfGroup[it.GroupID]; !seen {
			slotOfGroup[it.GroupID] = len(out)
			out = append(out, it)
		}
		membersOfGroup[it.GroupID] = append(membersOfGroup[it.GroupID], it)
	}

	for groupID, members := range membersOfGroup {
		out[slotOfGroup[groupID]] = s.mergeOneGroup(groupID, members)
	}
	return out
}

// mergeOneGroup 合并一个发行组内的多条 release。
//
// 代表 release 由优选阶梯在**整个发行组**里选，而列表成员只有「用户有曲目的」
// 那几条。两者不一定重合：用户可能只从数字版入库，而阶梯偏好实体载体。
// 此时仍以阶梯选的那一条为代表——RepresentativeRef 是清单读取的依据，
// 它必须与 RepresentativeRelease 给出的一致，否则专辑视图会去读另一个版本的清单。
//
// 已入库数是全组求和；封面与清单总数取代表的，缺失时退到组内任一非空值——
// 同一张专辑的 CD 版与数字版在列表里是一条，不该因为用户恰好从数字版入库
// 而丢掉 CD 版已有的封面。
func (s *Store) mergeOneGroup(groupID int64, members []AlbumListItem) AlbumListItem {
	merged := members[0]
	if rep, err := s.RepresentativeRelease(groupID); err == nil && rep != nil {
		if owned := findByReleaseID(members, rep.ID); owned != nil {
			merged = *owned
		} else {
			// 阶梯选中的那一条上一首都不剩（例如用户删光了 CD 版那几首，
			// 而 release 行作为清单缓存保留）。它仍是代表，但本身没有
			// 已入库曲目，因此只取它的身份与发行事实。
			merged = albumItemFromRelease(rep, members[0].GroupID)
		}
	}

	owned := 0
	for _, m := range members {
		owned += m.OwnedCount
		if merged.CoverPath == nil {
			merged.CoverPath = m.CoverPath
		}
		if merged.CoverSourceURL == nil {
			merged.CoverSourceURL = m.CoverSourceURL
		}
		if merged.TotalCount == nil {
			merged.TotalCount = m.TotalCount
		}
		if merged.Credit == nil {
			merged.Credit = m.Credit
		}
		if merged.Year == nil {
			merged.Year = m.Year
		}
	}
	merged.OwnedCount = owned
	return merged
}

func findByReleaseID(items []AlbumListItem, id int64) *AlbumListItem {
	for i := range items {
		if items[i].ReleaseID == id {
			return &items[i]
		}
	}
	return nil
}

// albumItemFromRelease 由一条 release 构造列表项（不含已入库数）。
func albumItemFromRelease(r *Release, groupID int64) AlbumListItem {
	return AlbumListItem{
		ReleaseID: r.ID, GroupID: groupID,
		Title: r.Title, Credit: r.Credit, Year: r.Year(),
		TotalCount:        r.TrackTotal,
		RepresentativeRef: r.ProviderRef,
		Provider:          r.Provider,
		ReleaseGroupRef:   r.ReleaseGroupRef,
		CoverPath:         r.CoverPath,
		CoverSourceURL:    r.CoverSourceURL,
	}
}
