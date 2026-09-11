package store

import (
	"database/sql"
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

	Year         *int    `json:"year"`
	TrackNumber  *int    `json:"trackNumber"`
	TrackTotal   *int    `json:"trackTotal"`
	DiscNumber   *int    `json:"discNumber"`
	DurationMS   *int    `json:"durationMs"`
	Genre        *string `json:"genre"`
	MediumFormat *string `json:"mediumFormat"`
	Country      *string `json:"country"`
	PrimaryType  *string `json:"primaryType"`
	CoverPath    *string `json:"coverPath"`
	CoverKey     *string `json:"coverKey"`
	MatchSource  *string `json:"matchSource"`
	MatchRef     *string `json:"matchRef"`

	CreatedAt time.Time `json:"createdAt"`
}

// TrackFilter 描述一次曲目查询的条件。
type TrackFilter struct {
	// CollectionID 非零时限定在该歌单/标签内。
	CollectionID int64 `json:"collectionId"`
	// ArtistID 非零时限定为该艺术家参与的曲目。
	ArtistID int64 `json:"artistId"`
	// Role 非空时进一步限定参与角色（艺术家视图的分面筛选）。
	Role string `json:"role"`
	// ReleaseGroupRef / ReleaseRef 非空时限定为该专辑的曲目。
	ReleaseGroupRef string `json:"releaseGroupRef"`
	ReleaseRef      string `json:"releaseRef"`
	// DerivedAlbumKey 非空时按派生分组键限定。
	DerivedAlbumKey string `json:"derivedAlbumKey"`
	// RecentOnly 为真时只取最近添加。
	RecentOnly bool `json:"recentOnly"`

	// 高级搜索条件
	Title        string `json:"title"`
	Artist       string `json:"artist"`
	Album        string `json:"album"`
	YearFrom     *int   `json:"yearFrom"`
	YearTo       *int   `json:"yearTo"`
	TagID        int64  `json:"tagId"`
	MediumFormat string `json:"mediumFormat"`
	Country      string `json:"country"`
	PrimaryType  string `json:"primaryType"`
	// Query 是快速搜索的关键词，走 FTS。
	Query string `json:"query"`

	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

// trackSelectColumns 是曲目视图的公共列。
const trackSelectColumns = `
	t.id, t.title, t.artist, t.album,
	t.title_locked, t.artist_locked, t.album_locked,
	m.year, m.track_number, m.track_total, m.disc_number, m.duration_ms,
	m.genre, m.medium_format, m.country, m.primary_type,
	m.cover_path, m.match_source, m.match_ref, t.created_at`

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
		var v TrackView
		var titleLocked, artistLocked, albumLocked int
		if err := rows.Scan(&v.ID, &v.Title, &v.Artist, &v.Album,
			&titleLocked, &artistLocked, &albumLocked,
			&v.Year, &v.TrackNumber, &v.TrackTotal, &v.DiscNumber, &v.DurationMS,
			&v.Genre, &v.MediumFormat, &v.Country, &v.PrimaryType,
			&v.CoverPath, &v.MatchSource, &v.MatchRef, &v.CreatedAt); err != nil {
			return nil, err
		}
		v.TitleLocked = titleLocked != 0
		v.ArtistLocked = artistLocked != 0
		v.AlbumLocked = albumLocked != 0
		out = append(out, v)
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
	if f.ArtistID != 0 {
		joins = append(joins, "JOIN track_artist ta ON ta.track_id = t.id")
		wheres = append(wheres, "ta.artist_id = ?")
		args = append(args, f.ArtistID)
		if f.Role != "" {
			wheres = append(wheres, "ta.role = ?")
			args = append(args, f.Role)
		}
	}
	if f.ReleaseGroupRef != "" {
		wheres = append(wheres, "m.release_group_mbid = ?")
		args = append(args, f.ReleaseGroupRef)
	}
	if f.ReleaseRef != "" {
		wheres = append(wheres, "m.release_mbid = ?")
		args = append(args, f.ReleaseRef)
	}
	if f.DerivedAlbumKey != "" {
		wheres = append(wheres,
			"m.release_group_mbid IS NULL AND m.release_mbid IS NULL AND "+
				"LOWER(COALESCE(t.album,'')) || '\x1f' || LOWER(COALESCE(m.album_credit, t.artist, '')) = ?")
		args = append(args, f.DerivedAlbumKey)
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
		wheres = append(wheres,
			"(LOWER(COALESCE(t.artist,'')) LIKE ? OR EXISTS ("+
				"SELECT 1 FROM track_artist tax JOIN artist ax ON ax.id = tax.artist_id "+
				"WHERE tax.track_id = t.id AND LOWER(ax.name) LIKE ?))")
		like := "%" + strings.ToLower(v) + "%"
		args = append(args, like, like)
	}
	if v := strings.TrimSpace(f.Album); v != "" {
		wheres = append(wheres, "LOWER(COALESCE(t.album,'')) LIKE ?")
		args = append(args, "%"+strings.ToLower(v)+"%")
	}
	if f.YearFrom != nil {
		wheres = append(wheres, "m.year >= ?")
		args = append(args, *f.YearFrom)
	}
	if f.YearTo != nil {
		wheres = append(wheres, "m.year <= ?")
		args = append(args, *f.YearTo)
	}
	// 载体、国家、类型仅对本地搜索生效
	if f.MediumFormat != "" {
		wheres = append(wheres, "m.medium_format = ?")
		args = append(args, f.MediumFormat)
	}
	if f.Country != "" {
		wheres = append(wheres, "m.country = ?")
		args = append(args, f.Country)
	}
	if f.PrimaryType != "" {
		wheres = append(wheres, "m.primary_type = ?")
		args = append(args, f.PrimaryType)
	}

	sb := strings.Builder{}
	if countOnly {
		sb.WriteString("SELECT COUNT(DISTINCT t.id) FROM track t")
	} else {
		sb.WriteString("SELECT DISTINCT " + trackSelectColumns + " FROM track t")
	}
	sb.WriteString(" LEFT JOIN track_metadata m ON m.track_id = t.id")
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
	views, err := s.ListTracks(TrackFilter{Limit: 1})
	_ = views
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(
		"SELECT "+trackSelectColumns+
			" FROM track t LEFT JOIN track_metadata m ON m.track_id = t.id WHERE t.id = ?", id)

	var v TrackView
	var titleLocked, artistLocked, albumLocked int
	err = row.Scan(&v.ID, &v.Title, &v.Artist, &v.Album,
		&titleLocked, &artistLocked, &albumLocked,
		&v.Year, &v.TrackNumber, &v.TrackTotal, &v.DiscNumber, &v.DurationMS,
		&v.Genre, &v.MediumFormat, &v.Country, &v.PrimaryType,
		&v.CoverPath, &v.MatchSource, &v.MatchRef, &v.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v.TitleLocked = titleLocked != 0
	v.ArtistLocked = artistLocked != 0
	v.AlbumLocked = albumLocked != 0
	return &v, nil
}

// UpdateBasicOnMatch 在匹配成功时更新基本信息。
//
// 未加锁的字段用上游规范值覆盖（保持基本与丰富信息一致），
// 已加锁的字段保持不变。
func (s *Store) UpdateBasicOnMatch(trackID int64, title, artist, album *string) error {
	return s.withTx(func(tx *sql.Tx) error {
		var titleLocked, artistLocked, albumLocked int
		if err := tx.QueryRow(
			`SELECT title_locked, artist_locked, album_locked FROM track WHERE id = ?`,
			trackID).Scan(&titleLocked, &artistLocked, &albumLocked); err != nil {
			return err
		}

		sets := []string{"updated_at = ?"}
		args := []any{time.Now().UTC()}
		if title != nil && titleLocked == 0 {
			sets = append(sets, "title = ?")
			args = append(args, *title)
		}
		if artist != nil && artistLocked == 0 {
			sets = append(sets, "artist = ?")
			args = append(args, *artist)
		}
		if album != nil && albumLocked == 0 {
			sets = append(sets, "album = ?")
			args = append(args, *album)
		}
		args = append(args, trackID)

		if _, err := tx.Exec(
			"UPDATE track SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			return err
		}
		return syncFTS(tx, trackID)
	})
}
