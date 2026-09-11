package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/norhua/sonexa/internal/matching"
)

// FindByMatchRef 按来源与标识精确查找曲目；未命中返回 0。
func (s *Store) FindByMatchRef(source, ref string) (int64, error) {
	var id int64
	err := s.db.QueryRow(
		`SELECT track_id FROM track_metadata WHERE match_source = ? AND match_ref = ?`,
		source, ref).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// FindByISRC 按 ISRC 查找曲目（跨上游桥接）；未命中返回 0。
func (s *Store) FindByISRC(isrc string) (int64, error) {
	if isrc == "" {
		return 0, nil
	}
	var id int64
	err := s.db.QueryRow(
		`SELECT track_id FROM track_metadata WHERE isrc = ? LIMIT 1`, isrc).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// FindUnmatchedByText 在没有匹配来源的曲目中做标准化文本比对。
//
// 作用域仅限 match_source IS NULL 的曲目——即「无需匹配」或手动录入的那些，
// 它们没有标识可比。已有溯源的曲目不参与，否则误判会扩散到全库。
//
// 判据：标准化曲名相等 AND 标准化主艺术家相等 AND |时长差| ≤ 3s。
func (s *Store) FindUnmatchedByText(normTitle, normArtist string, durationMS *int) (int64, error) {
	rows, err := s.db.Query(`
		SELECT t.id, t.title, t.artist, m.duration_ms
		FROM track t
		LEFT JOIN track_metadata m ON m.track_id = t.id
		WHERE m.match_source IS NULL OR m.track_id IS NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id       int64
			title    string
			artist   sql.NullString
			duration sql.NullInt64
		)
		if err := rows.Scan(&id, &title, &artist, &duration); err != nil {
			return 0, err
		}
		if matching.Normalize(title) != normTitle {
			continue
		}
		if !artist.Valid || matching.Normalize(artist.String) != normArtist {
			continue
		}
		// 两边都有时长时才比对；任一缺失则不以时长否决
		if durationMS != nil && duration.Valid {
			if !matching.DurationWithinTolerance(*durationMS, int(duration.Int64)) {
				continue
			}
		}
		return id, nil
	}
	return 0, rows.Err()
}

// 编译期断言：Store 必须满足候选合并判定所需的查询能力。
var _ matching.LocalLookup = (*Store)(nil)

// TracklistPosition 是清单中一个位置的呈现状态。
type TracklistPosition string

const (
	// PositionOwned 已入库：标识等值比对命中。
	PositionOwned TracklistPosition = "owned"
	// PositionOtherVersion 已有其他版本：标识未命中，但录音标识命中某曲目。
	PositionOtherVersion TracklistPosition = "other_version"
	// PositionMissing 未入库：都未命中，呈现为灰色行。
	PositionMissing TracklistPosition = "missing"
)

// ResolvedPosition 是一个清单位置的解析结果。
type ResolvedPosition struct {
	Track ReleaseTrack
	State TracklistPosition
	// TrackID 是对应的库中曲目；未入库时为 0。
	TrackID int64
}

// ResolveTracklist 判定某发行清单中每个位置的状态。
//
// 三种状态：
//
//	已入库        release_track.track_ref = track_metadata.match_ref
//	              AND release.provider = track_metadata.match_source
//	已有其他版本   上面未命中，但 recording_ref 命中某曲目的 recording_mbid
//	未入库        都未命中 → 灰色行
//
// 中间那个状态不能省：用户从 CD 版入库、专辑视图显示的是数字版清单时，
// 同一首歌的 track MBID 不同——只做等值 join 会把用户明明有的歌标成缺失。
func (s *Store) ResolveTracklist(releaseID int64, provider string) ([]ResolvedPosition, error) {
	tracks, err := s.GetTracklist(releaseID)
	if err != nil {
		return nil, err
	}

	out := make([]ResolvedPosition, 0, len(tracks))
	for _, t := range tracks {
		pos := ResolvedPosition{Track: t, State: PositionMissing}

		// 已入库：标识等值比对（要求来源一致）
		if t.TrackRef != nil && *t.TrackRef != "" {
			id, err := s.FindByMatchRef(provider, *t.TrackRef)
			if err != nil {
				return nil, err
			}
			if id != 0 {
				pos.State, pos.TrackID = PositionOwned, id
				out = append(out, pos)
				continue
			}
		}

		// 已有其他版本：recording 标识命中
		if t.RecordingRef != nil && *t.RecordingRef != "" {
			id, err := s.FindByRecordingRef(*t.RecordingRef)
			if err != nil {
				return nil, err
			}
			if id != 0 {
				pos.State, pos.TrackID = PositionOtherVersion, id
				out = append(out, pos)
				continue
			}
		}

		out = append(out, pos)
	}
	return out, nil
}

// FindByRecordingRef 按录音标识查找曲目；未命中返回 0。
//
// 用于「已有其他版本」判定——recording_mbid 已存在于 track_metadata，
// 这个判定是免费的。
func (s *Store) FindByRecordingRef(recordingRef string) (int64, error) {
	if recordingRef == "" {
		return 0, nil
	}
	var id int64
	err := s.db.QueryRow(
		`SELECT track_id FROM track_metadata WHERE recording_mbid = ? LIMIT 1`,
		recordingRef).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// SetProvenance 写入匹配来源、标识与匹配时间。
//
// 赋值时机：上游搜索页加入曲库、复核队列接受候选、导入自动入库、
// 编辑页重新匹配确认保存、用户手动填入标识（需先通过格式校验）。
func (s *Store) SetProvenance(trackID int64, source, ref string) error {
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO track_metadata (track_id, match_source, match_ref, matched_at, updated_at)
			VALUES (?,?,?,?,?)
			ON CONFLICT(track_id) DO UPDATE SET
				match_source = excluded.match_source,
				match_ref    = excluded.match_ref,
				matched_at   = excluded.matched_at,
				updated_at   = excluded.updated_at`,
			trackID, source, ref, now, now)
		return err
	})
}

// ClearProvenance 置空匹配来源、标识与匹配时间，字段值本身不变。
//
// 置空时机：复核队列的「无需匹配」、用户在编辑页手动清空。
func (s *Store) ClearProvenance(trackID int64) error {
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO track_metadata (track_id, match_source, match_ref, matched_at, updated_at)
			VALUES (?, NULL, NULL, NULL, ?)
			ON CONFLICT(track_id) DO UPDATE SET
				match_source = NULL,
				match_ref    = NULL,
				matched_at   = NULL,
				updated_at   = excluded.updated_at`,
			trackID, now)
		return err
	})
}

// ErrAlreadyExists 表示写入撞上了溯源的唯一约束。
// 调用方据此静默转为「跳转到已有曲目」，不向用户报错。
type ErrAlreadyExists struct {
	TrackID int64
}

func (e *ErrAlreadyExists) Error() string { return "该上游结果已在曲库中" }
