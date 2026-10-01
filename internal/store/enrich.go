package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// WriteDetailColumns 把详情中的署名原串与上游标识写入曲目及其歌集。
//
// 这些字段不走编辑界面那套「填充/保持」：它们是上游事实的直接映射，
// 由 ApplyDetailToTrack 在拉取详情时写入。
//
// 曲目侧与歌集侧分开写：署名、录音标识、作品标识、ISRC、音轨序号、歌词属于
// 这一首；碟数、音轨数、发行日期、类型、厂牌、目录号属于这张歌集。
func (s *Store) WriteDetailColumns(trackID int64, d *provider.Detail) error {
	revision, err := s.TrackRevision(trackID)
	if err != nil {
		return err
	}
	return s.ApplySnapshot(trackID, provider.DetailSnapshot(d), revision)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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

// MarkTrackForReview 标记某曲目需要人工确认。
//
// 「拆分不确定时不拆」的落点：保留原串、不建立猜测性的艺术家记录，
// 而是把该曲目标记出来交由用户处置。
//
// 写在 enrich_error 而不是 comment 上：comment 是用户自己的备注字段，
// 往里塞 `[需确认] …` 会把他写的内容冲掉。
func (s *Store) MarkTrackForReview(trackID int64, reason string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE track SET credit_review = ?, updated_at = ? WHERE id = ?`,
			"[需确认] "+reason, time.Now().UTC(), trackID)
		return err
	})
}

// TracksNeedingReview 返回被标记为需人工确认的曲目。
func (s *Store) TracksNeedingReview() ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT id FROM track_read WHERE credit_review IS NOT NULL`)
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
