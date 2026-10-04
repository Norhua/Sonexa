package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TrackView 是曲目表格的一行。
//
// 全部可空字段用指针：界面对缺失字段显示占位短横线，
// 而不是显示 0 或空白单元格。
type TrackView struct {
	ID     int64   `json:"id"`
	Title  string  `json:"title"`
	Artist *string `json:"artist"`
	Album  *string `json:"album"`

	TitleLocked  bool `json:"titleLocked"`
	ArtistLocked bool `json:"artistLocked"`
	AlbumLocked  bool `json:"albumLocked"`

	Year          *int    `json:"year"`
	TrackNumber   *int    `json:"trackNumber"`
	TrackTotal    *int    `json:"trackTotal"`
	DiscNumber    *int    `json:"discNumber"`
	DurationMS    *int    `json:"durationMs"`
	Genre         *string `json:"genre"`
	MediumFormat  *string `json:"mediumFormat"`
	Country       *string `json:"country"`
	PrimaryType   *string `json:"primaryType"`
	CoverPath     *string `json:"coverPath"`
	CoverRevision int64   `json:"coverRevision"`
	MatchSource   *string `json:"matchSource"`
	MatchRef      *string `json:"matchRef"`

	// ReleaseID 是所属歌集，供编辑界面定位要改的那条 release。
	ReleaseID *int64 `json:"releaseId"`
	// ReleaseRef / ReleaseGroupRef 是封面的来源标识。
	//
	// 列表要显示缩略图就必须把它们带出来：封面地址是
	// /cover?provider=<matchSource>&ref=<releaseRef>&rg=<releaseGroupRef>。
	ReleaseRef      *string `json:"releaseRef"`
	ReleaseGroupRef *string `json:"releaseGroupRef"`

	// AddedAt 为首次入库时间，以 RFC3339 字符串导出，避免 Wails 生成 time.Time 告警。
	AddedAt string `json:"addedAt"`
}

// TrackFilter 描述一次曲目查询的条件。
type TrackFilter struct {
	// CollectionID 非零时限定在该歌单/标签内。
	CollectionID int64 `json:"collectionId"`
	// ArtistID 非零时限定为该艺术家关联的曲目（署名 ∪ 全部角色）。
	ArtistID int64 `json:"artistId"`
	// Role 非空时进一步限定：取值为角色名，或 RoleCredited（只看被署名的）。
	Role string `json:"role"`
	// ReleaseID 非零时限定为该歌集的曲目。
	ReleaseID int64 `json:"releaseId"`
	// ReleaseGroupID 非零时限定为该发行组下全部歌集的曲目。
	ReleaseGroupID int64 `json:"releaseGroupId"`
	// RecentOnly 为真时只取最近添加。
	RecentOnly bool `json:"recentOnly"`

	// 高级搜索条件
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album"`
	TagID  int64  `json:"tagId"`
	// Query 是快速搜索的关键词，走 FTS。
	Query string `json:"query"`

	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// trackSelectColumns 是曲目视图的公共列。
//
// 发行级字段全部来自 join 出来的歌集——它们只存在歌集上一份。
const trackSelectColumns = `
	t.id, t.title, t.credit, r.title,
	t.locked_json,
	t.year,
	t.track_number, r.track_total, t.disc_number, t.duration_ms,
	t.genre, r.medium_format, r.country, r.primary_type,
	r.cover_path, t.match_source, t.match_ref,
	t.release_id, r.provider_ref,
	(SELECT rg.provider_ref FROM release_group_read rg WHERE rg.id = r.release_group_id),
	t.created_at,
	COALESCE((SELECT revision FROM release_cover_selection WHERE release_id=t.release_id),0)`

func scanTrackView(sc interface{ Scan(...any) error }) (*TrackView, error) {
	var v TrackView
	var createdAt time.Time
	var lockedJSON *string
	if err := sc.Scan(&v.ID, &v.Title, &v.Artist, &v.Album,
		&lockedJSON,
		&v.Year,
		&v.TrackNumber, &v.TrackTotal, &v.DiscNumber, &v.DurationMS,
		&v.Genre, &v.MediumFormat, &v.Country, &v.PrimaryType,
		&v.CoverPath, &v.MatchSource, &v.MatchRef,
		&v.ReleaseID, &v.ReleaseRef, &v.ReleaseGroupRef, &createdAt, &v.CoverRevision); err != nil {
		return nil, err
	}
	v.AddedAt = createdAt.UTC().Format(time.RFC3339)
	locks := ParseLocks(lockedJSON)
	v.TitleLocked, v.ArtistLocked, v.AlbumLocked = locks["title"], locks["credit"], locks["album"]
	return &v, nil
}

// ParseLocks 解析 locked_json。
//
// 基本信息的三个布尔列并入了这个 JSON——合表之后没有理由留两套锁机制，
// 而逐字段锁本就要求「新增字段无需改动锁的存储结构」。
func ParseLocks(lockedJSON *string) map[string]bool {
	out := map[string]bool{}
	if lockedJSON == nil || *lockedJSON == "" {
		return out
	}
	_ = json.Unmarshal([]byte(*lockedJSON), &out)
	return out
}

// ListTracks 按条件查询曲目。
func (s *Store) ListTracks(f TrackFilter) ([]TrackView, error) {
	query, args, err := s.buildTrackQuery(f, false)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询曲目失败: %w", err)
	}
	defer rows.Close()

	var out []TrackView
	for rows.Next() {
		v, err := scanTrackView(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

// CountTracks 返回符合条件的曲目总数，供分页使用。
func (s *Store) CountTracks(f TrackFilter) (int, error) {
	query, args, err := s.buildTrackQuery(f, true)
	if err != nil {
		return 0, err
	}
	var n int
	if err := s.db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// buildTrackQuery 构造查询语句。
func (s *Store) buildTrackQuery(f TrackFilter, countOnly bool) (string, []any, error) {
	var (
		joins  []string
		wheres []string
		args   []any
	)

	if f.CollectionID != 0 {
		joins = append(joins, "JOIN collection_item ci ON ci.track_id = t.id")
		wheres = append(wheres, "ci.collection_id = ?")
		args = append(args, f.CollectionID)
	}
	if f.TagID != 0 && f.TagID != f.CollectionID {
		joins = append(joins, "JOIN collection_item cti ON cti.track_id = t.id")
		wheres = append(wheres, "cti.collection_id = ?")
		args = append(args, f.TagID)
	}

	// 艺术家视图：默认取该艺术家关联的全部曲目（署名 ∪ 全部角色）。
	// 分面筛选时收窄——「署名」是一个分面，其余是具体角色。
	if f.ArtistID != 0 {
		switch f.Role {
		case "":
			wheres = append(wheres, `(EXISTS (SELECT 1 FROM track_credit tc
				WHERE tc.track_id = t.id AND tc.artist_id = ?)
				OR EXISTS (SELECT 1 FROM track_artist ta
				WHERE ta.track_id = t.id AND ta.artist_id = ?))`)
			args = append(args, f.ArtistID, f.ArtistID)
		case RoleCredited:
			wheres = append(wheres, `EXISTS (SELECT 1 FROM track_credit tc
				WHERE tc.track_id = t.id AND tc.artist_id = ?)`)
			args = append(args, f.ArtistID)
		default:
			wheres = append(wheres, `EXISTS (SELECT 1 FROM track_artist ta
				WHERE ta.track_id = t.id AND ta.artist_id = ? AND ta.role = ?)`)
			args = append(args, f.ArtistID, f.Role)
		}
	}

	if f.ReleaseID != 0 {
		wheres = append(wheres, "t.release_id = ?")
		args = append(args, f.ReleaseID)
	}
	if f.ReleaseGroupID != 0 {
		wheres = append(wheres, "r.release_group_id = ?")
		args = append(args, f.ReleaseGroupID)
	}

	// 快速搜索走 FTS
	if q := strings.TrimSpace(f.Query); q != "" {
		ids, err := s.SearchTrackIDs(q, 2000)
		if err != nil {
			return "", nil, err
		}
		if len(ids) == 0 {
			wheres = append(wheres, "1 = 0")
		} else {
			placeholders := make([]string, len(ids))
			for i, id := range ids {
				placeholders[i] = "?"
				args = append(args, id)
			}
			wheres = append(wheres, "t.id IN ("+strings.Join(placeholders, ",")+")")
		}
	}

	// 高级搜索的三个文本条件：留空的不参与筛选
	if v := strings.TrimSpace(f.Title); v != "" {
		wheres = append(wheres, "LOWER(t.title) LIKE ?")
		args = append(args, "%"+strings.ToLower(v)+"%")
	}
	if v := strings.TrimSpace(f.Artist); v != "" {
		// 署名原串、被署名的艺术家名、担任角色的艺术家名都算
		wheres = append(wheres,
			"(LOWER(COALESCE(t.credit,'')) LIKE ? OR EXISTS ("+
				"SELECT 1 FROM track_credit tcx JOIN artist acx ON acx.id = tcx.artist_id "+
				"WHERE tcx.track_id = t.id AND LOWER(acx.name) LIKE ?) OR EXISTS ("+
				"SELECT 1 FROM track_artist tax JOIN artist ax ON ax.id = tax.artist_id "+
				"WHERE tax.track_id = t.id AND LOWER(ax.name) LIKE ?))")
		like := "%" + strings.ToLower(v) + "%"
		args = append(args, like, like, like)
	}
	if v := strings.TrimSpace(f.Album); v != "" {
		wheres = append(wheres, "LOWER(COALESCE(r.title,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(v)+"%")
	}

	sb := strings.Builder{}
	if countOnly {
		sb.WriteString("SELECT COUNT(DISTINCT t.id) FROM track_read t")
	} else {
		sb.WriteString("SELECT DISTINCT " + trackSelectColumns + " FROM track_read t")
	}
	sb.WriteString(" LEFT JOIN release_read r ON r.id = t.release_id")
	for _, j := range joins {
		sb.WriteString(" " + j)
	}
	if len(wheres) > 0 {
		sb.WriteString(" WHERE " + strings.Join(wheres, " AND "))
	}

	if !countOnly {
		switch {
		case f.RecentOnly:
			sb.WriteString(" ORDER BY t.created_at DESC")
		case f.CollectionID != 0:
			// 歌单维护显式顺序
			sb.WriteString(" ORDER BY (SELECT ci2.position FROM collection_item ci2" +
				" WHERE ci2.collection_id = ? AND ci2.track_id = t.id), t.title")
			args = append(args, f.CollectionID)
		default:
			sb.WriteString(" ORDER BY t.title")
		}
		if f.Limit > 0 {
			sb.WriteString(" LIMIT ?")
			args = append(args, f.Limit)
			if f.Offset > 0 {
				sb.WriteString(" OFFSET ?")
				args = append(args, f.Offset)
			}
		}
	}
	return sb.String(), args, nil
}

// GetTrackView 读取单条曲目的完整视图。
func (s *Store) GetTrackView(id int64) (*TrackView, error) {
	v, err := scanTrackView(s.db.QueryRow(
		"SELECT "+trackSelectColumns+
			" FROM track_read t LEFT JOIN release_read r ON r.id = t.release_id WHERE t.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// UpdateBasicOnMatch 在匹配成功时更新曲名与署名。
//
// 未加锁的字段用上游规范值覆盖，已加锁的字段保持不变。
// 歌集名不在这里改——它归歌集，改它对该歌集全部曲目生效，
// 不该由某一首曲目的一次匹配悄悄触发。
func (s *Store) UpdateBasicOnMatch(trackID int64, title, credit *string) error {
	return s.UpdateTrackFields(trackID, title, credit, false)
}

// MoveTrackToRelease 把一首曲目挪到另一条歌集下。
//
// 「改歌集名」在实体模型下有三种语义（改这张歌集 / 挪到已有的另一张 /
// 新建一张），由调用方按用户的选择决定，这里只负责挪。挪走之后原歌集
// 若是本地歌集且再无曲目引用，顺带删掉——否则侧栏会积一堆空专辑。
func (s *Store) MoveTrackToRelease(trackID, releaseID int64) error {
	var oldID *int64
	if err := s.db.QueryRow(`SELECT release_id FROM track_read WHERE id = ?`, trackID).Scan(&oldID); err != nil {
		return err
	}
	err := s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE track SET release_id = ?, updated_at = ? WHERE id = ?`,
			nullableID(releaseID), time.Now().UTC(), trackID); err != nil {
			return err
		}
		return syncFTS(tx, trackID)
	})
	if err != nil {
		return err
	}
	if oldID != nil && *oldID != releaseID {
		return s.DeleteOrphanRelease(*oldID)
	}
	return nil
}

// ReleaseTrackCount 返回某歌集下的曲目数。
//
// 「改歌集名」时用它判断这次改动影响几首——只有一首时不必二次确认。
func (s *Store) ReleaseTrackCount(releaseID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM track_read WHERE release_id = ?`, releaseID).Scan(&n)
	return n, err
}

// FindReleaseByTitleCredit 按 (标题, 署名) 查找歌集；未命中返回 0。
//
// 「改歌集名」时用它判断库中是否已有同名歌集——有则可以关联过去，
// 没有则只能新建。
func (s *Store) FindReleaseByTitleCredit(title string, credit *string) (int64, error) {
	var id int64
	err := s.db.QueryRow(`
		SELECT id FROM release_read
		WHERE LOWER(title) = LOWER(?)
		  AND LOWER(COALESCE(credit,'')) = LOWER(COALESCE(?,''))
		ORDER BY (provider_ref IS NULL), id
		LIMIT 1`, title, credit).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}
