package store

import (
	"fmt"
	"strings"

	"github.com/norhua/sonexa/internal/matching"
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

// ListArtists 列出全部有曲目关联的艺术家，含曲目数。
func (s *Store) ListArtists() ([]ArtistListItem, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.name, a.type,
		       COUNT(DISTINCT ta.track_id) AS track_count,
		       (SELECT va.name FROM artist_link l JOIN artist va ON va.id = l.to_artist_id
		        WHERE l.from_artist_id = a.id AND l.type = 'voice_of' LIMIT 1)
		FROM artist a
		JOIN track_artist ta ON ta.artist_id = a.id
		GROUP BY a.id
		ORDER BY track_count DESC, a.name`)
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
// 「全部 14」「演唱 12」「作曲 0」「作词 2」——
// 计数为零的分面也要列出，否则用户无法得知该艺术家在此角色上没有作品。
func (s *Store) RoleFacets(artistID int64) ([]RoleFacet, error) {
	var total int
	if err := s.db.QueryRow(
		`SELECT COUNT(DISTINCT track_id) FROM track_artist WHERE artist_id = ?`,
		artistID).Scan(&total); err != nil {
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

	facets := []RoleFacet{{Role: "", Label: "全部", Count: total}}
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

// AlbumGroupLevel 是专辑分组键的降级层级。
type AlbumGroupLevel string

const (
	// GroupByReleaseGroup 按发行组分组：同一专辑的不同版本合为一条。
	GroupByReleaseGroup AlbumGroupLevel = "release_group"
	// GroupByRelease 按单个发行分组：上游无发行组概念时。
	GroupByRelease AlbumGroupLevel = "release"
	// GroupDerived 派生分组：没有任何上游标识时的兜底。
	GroupDerived AlbumGroupLevel = "derived"
)

// AlbumListItem 是专辑列表（中列）的一项。
type AlbumListItem struct {
	// Key 是分组键，取决于 Level。
	Key   string          `json:"key"`
	Level AlbumGroupLevel `json:"level"`
	Title string          `json:"title"`
	// Credit 是发行级署名，缺失时退为艺术家。
	Credit *string `json:"credit"`
	Year   *int    `json:"year"`
	// OwnedCount 是用户已入库的曲目数。
	OwnedCount int `json:"ownedCount"`
	// TotalCount 是上游声明的清单总数；派生分组下为 nil，界面呈现为 `3/-`。
	TotalCount *int `json:"totalCount"`
	// RepresentativeRef 是代表 release 的标识，供清单读取使用。
	RepresentativeRef *string `json:"representativeRef"`
	Provider          *string `json:"provider"`
	CoverPath         *string `json:"coverPath"`
}

// CountLabel 返回「已入库/清单总数」的呈现。
//
// 派生分组没有上游事实，不假装知道这张专辑共有几首。
func (a AlbumListItem) CountLabel() string {
	if a.TotalCount == nil {
		return fmt.Sprintf("%d/-", a.OwnedCount)
	}
	return fmt.Sprintf("%d/%d", a.OwnedCount, *a.TotalCount)
}

// ListAlbums 列出专辑，按分组键三级降级。
//
//  1. release_group_mbid 存在 → 按发行组分组（CD 版与数字版合为一条）
//  2. 否则 release_mbid     → 按单个发行分组
//  3. 两者都无              → 派生分组：normalize(专辑名) + 署名
//
// 一张专辑仅在用户至少拥有其中一首曲目时出现——
// 灰色行用于补全已开始的收藏，而不是上游目录浏览。
func (s *Store) ListAlbums() ([]AlbumListItem, error) {
	rows, err := s.db.Query(`
		SELECT
			m.release_group_mbid,
			m.release_mbid,
			COALESCE(t.album, ''),
			COALESCE(m.album_credit, t.artist),
			m.match_source,
			MIN(m.year),
			COUNT(DISTINCT t.id),
			MAX(m.cover_path)
		FROM track t
		LEFT JOIN track_metadata m ON m.track_id = t.id
		GROUP BY
			COALESCE(m.release_group_mbid, ''),
			CASE WHEN m.release_group_mbid IS NULL THEN COALESCE(m.release_mbid, '') ELSE '' END,
			CASE WHEN m.release_group_mbid IS NULL AND m.release_mbid IS NULL
			     THEN LOWER(COALESCE(t.album, '')) || '\x1f' || LOWER(COALESCE(m.album_credit, t.artist, ''))
			     ELSE '' END`)
	if err != nil {
		return nil, fmt.Errorf("查询专辑失败: %w", err)
	}
	defer rows.Close()

	out := []AlbumListItem{}
	for rows.Next() {
		var (
			rgRef, relRef, credit, source, coverPath *string
			title                                    string
			year                                     *int
			owned                                    int
		)
		if err := rows.Scan(&rgRef, &relRef, &title, &credit, &source, &year, &owned, &coverPath); err != nil {
			return nil, err
		}
		if owned == 0 {
			continue
		}

		item := AlbumListItem{
			Title: title, Credit: credit, Year: year,
			OwnedCount: owned, Provider: source, CoverPath: coverPath,
		}
		switch {
		case rgRef != nil && *rgRef != "":
			item.Level = GroupByReleaseGroup
			item.Key = *rgRef
		case relRef != nil && *relRef != "":
			item.Level = GroupByRelease
			item.Key = *relRef
			item.RepresentativeRef = relRef
		default:
			// 第 3 级：没有上游事实，不显示灰色行也不显示总数
			item.Level = GroupDerived
			item.Key = derivedAlbumKey(title, credit)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 为前两级补上代表 release 与清单总数
	for i := range out {
		if out[i].Level == GroupDerived {
			continue
		}
		if err := s.fillAlbumTotals(&out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// derivedAlbumKey 生成派生分组键：normalize(专辑名) + 署名。
func derivedAlbumKey(title string, credit *string) string {
	c := ""
	if credit != nil {
		c = *credit
	}
	return strings.ToLower(title) + "\x1f" + strings.ToLower(c)
}

// DerivedAlbumKey 导出派生分组键的构造，供查询该分组的曲目使用。
func DerivedAlbumKey(title string, credit *string) string { return derivedAlbumKey(title, credit) }

// fillAlbumTotals 为某专辑条目选出代表 release 并取其清单总数。
func (s *Store) fillAlbumTotals(item *AlbumListItem) error {
	if item.Level == GroupByReleaseGroup {
		rep, err := s.representativeRelease(item.Key)
		if err != nil {
			return err
		}
		if rep == nil {
			return nil
		}
		item.RepresentativeRef = &rep.ProviderRef
		item.Provider = &rep.Provider
		if rep.TrackTotal != nil {
			item.TotalCount = rep.TrackTotal
		}
		if item.CoverPath == nil {
			item.CoverPath = rep.CoverPath
		}
		return nil
	}

	if item.RepresentativeRef == nil || item.Provider == nil {
		return nil
	}
	rel, err := s.GetReleaseByRef(*item.Provider, *item.RepresentativeRef)
	if err != nil || rel == nil {
		return err
	}
	if rel.TrackTotal != nil {
		item.TotalCount = rel.TrackTotal
	}
	return nil
}

// representativeRelease 在一个发行组下选出代表 release。
//
// 复用匹配阶段的优选阶梯（4.6/4.8），不另写一套排序——
// 两处对「哪个版本更有代表性」的判断没有理由不同，分成两套必然漂移。
func (s *Store) representativeRelease(releaseGroupRef string) (*Release, error) {
	rows, err := s.db.Query(`
		SELECT provider, provider_ref, release_status, medium_format, country,
		       first_release_date, cover_path, track_total
		FROM release WHERE release_group_ref = ?`, releaseGroupRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		rel       Release
		candidate matchingCandidate
	}
	var rowsOut []row
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.Provider, &r.ProviderRef, &r.ReleaseStatus, &r.MediumFormat,
			&r.Country, &r.FirstReleaseDate, &r.CoverPath, &r.TrackTotal); err != nil {
			return nil, err
		}
		r.ReleaseGroupRef = &releaseGroupRef
		rowsOut = append(rowsOut, row{rel: r, candidate: toLadderCandidate(r)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(rowsOut) == 0 {
		return nil, nil
	}

	candidates := make([]matchingCandidate, 0, len(rowsOut))
	for _, r := range rowsOut {
		candidates = append(candidates, r.candidate)
	}
	best := matching.DefaultLadderConfig().SelectRelease(candidates)
	if best == nil {
		return &rowsOut[0].rel, nil
	}
	for i := range rowsOut {
		if rowsOut[i].rel.ProviderRef == best.MatchRef {
			return &rowsOut[i].rel, nil
		}
	}
	return &rowsOut[0].rel, nil
}
