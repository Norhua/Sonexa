package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// WriteDetailColumns 把详情中的署名原串与上游标识写入丰富信息。
//
// 这些字段不走编辑界面那套「填充/保持」：它们是上游事实的直接映射，
// 由 ApplyDetailToTrack 在拉取详情时写入。
func (s *Store) WriteDetailColumns(trackID int64, d *provider.Detail) error {
	if d == nil {
		return nil
	}
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO track_metadata (track_id, updated_at) VALUES (?, ?)
			 ON CONFLICT(track_id) DO NOTHING`, trackID, now); err != nil {
			return err
		}
		_, err := tx.Exec(`
			UPDATE track_metadata SET
				track_credit       = COALESCE(?, track_credit),
				album_credit       = COALESCE(?, album_credit),
				title              = COALESCE(?, title),
				album              = COALESCE(?, album),
				recording_mbid     = COALESCE(?, recording_mbid),
				release_mbid       = COALESCE(?, release_mbid),
				release_group_mbid = COALESCE(?, release_group_mbid),
				work_mbid          = COALESCE(?, work_mbid),
				isrc               = COALESCE(?, isrc),
				track_total        = COALESCE(?, track_total),
				disc_total         = COALESCE(?, disc_total),
				release_date       = COALESCE(?, release_date),
				first_release_date = COALESCE(?, first_release_date),
				secondary_types    = COALESCE(?, secondary_types),
				release_status     = COALESCE(?, release_status),
				track_number_text  = COALESCE(?, track_number_text),
				updated_at         = ?
			WHERE track_id = ?`,
			d.TrackCredit, d.AlbumCredit, d.Title, d.AlbumTitle,
			d.RecordingRef, d.ReleaseRef, d.ReleaseGroupRef, d.WorkRef, d.ISRC,
			d.TrackTotal, d.DiscTotal, d.ReleaseDate, d.FirstReleaseDate,
			secondaryTypesJSON(d.SecondaryTypes), d.ReleaseStatus, d.TrackNumberText,
			now, trackID)
		return err
	})
}

// secondaryTypesJSON 把次要类型数组序列化为 JSON。
func secondaryTypesJSON(types []string) *string {
	if len(types) == 0 {
		return nil
	}
	data, err := json.Marshal(types)
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
}

// WriteCreditSpans 写入署名的可点击区间。
//
// 区间在解析署名时一并生成，不在渲染时反推——
// 渲染时已经没有解析上下文了。
func (s *Store) WriteCreditSpans(trackID int64, spans any) error {
	data, err := json.Marshal(spans)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO track_metadata (track_id, credit_spans_json, updated_at) VALUES (?,?,?)
			 ON CONFLICT(track_id) DO UPDATE SET
				credit_spans_json = excluded.credit_spans_json,
				updated_at        = excluded.updated_at`,
			trackID, string(data), now); err != nil {
			return err
		}
		return nil
	})
}

// MarkTrackForReview 标记某曲目需要人工确认。
//
// 「拆分不确定时不拆」的落点：保留原串、不建立猜测性的艺术家记录，
// 而是把该曲目标记出来交由用户处置。
func (s *Store) MarkTrackForReview(trackID int64, reason string) error {
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`INSERT INTO track_metadata (track_id, comment, updated_at) VALUES (?,?,?)
			 ON CONFLICT(track_id) DO UPDATE SET
				comment    = excluded.comment,
				updated_at = excluded.updated_at`,
			trackID, "[需确认] "+reason, now); err != nil {
			return err
		}
		return nil
	})
}

// TracksNeedingReview 返回被标记为需人工确认的曲目。
func (s *Store) TracksNeedingReview() ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT track_id FROM track_metadata WHERE comment LIKE '[需确认]%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// RecordEnrichFailure 记录一次丰富失败的原因。
//
// 丰富失败不回滚入库：曲目已经在库里，缺的丰富信息之后可以再补，
// 但原因要留下，便于界面提示与排查。
func (s *Store) RecordEnrichFailure(trackID int64, reason string) error {
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO track_metadata (track_id, comment, updated_at) VALUES (?,?,?)
			 ON CONFLICT(track_id) DO UPDATE SET
				comment    = excluded.comment,
				updated_at = excluded.updated_at`,
			trackID, "[丰富失败] "+reason, now)
		return err
	})
}
