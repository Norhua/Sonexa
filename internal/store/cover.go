package store

import (
	"database/sql"
	"time"
)

// CoverRecord 是一张已采用封面的落盘记录。
type CoverRecord struct {
	Path      string
	Source    string
	SourceURL string
	Width     *int
	Height    *int
	FetchedAt time.Time
}

// SetCover 把已采用的封面写入曲目的丰富信息。
//
// 用户在详情抽屉确认采用后调用：图按内容哈希落盘（文件不入库），
// 这里只记录路径、来源、原始地址、尺寸与获取时间。
func (s *Store) SetCover(trackID int64, c CoverRecord) error {
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			INSERT INTO track_metadata (
				track_id, cover_path, cover_source, cover_source_url,
				cover_width, cover_height, cover_fetched_at, updated_at
			) VALUES (?,?,?,?,?,?,?,?)
			ON CONFLICT(track_id) DO UPDATE SET
				cover_path       = excluded.cover_path,
				cover_source     = excluded.cover_source,
				cover_source_url = excluded.cover_source_url,
				cover_width      = excluded.cover_width,
				cover_height     = excluded.cover_height,
				cover_fetched_at = excluded.cover_fetched_at,
				updated_at       = excluded.updated_at`,
			trackID, c.Path, c.Source, c.SourceURL, c.Width, c.Height, c.FetchedAt, now)
		return err
	})
}

// SetReleaseCover 记录发行级封面。
func (s *Store) SetReleaseCover(releaseID int64, path, sourceURL string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE release SET cover_path = ?, cover_source_url = ?, updated_at = ? WHERE id = ?`,
			path, sourceURL, time.Now().UTC(), releaseID)
		return err
	})
}
