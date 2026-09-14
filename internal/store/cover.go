package store

import (
	"database/sql"
	"fmt"
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

// SetCover 把已采用的封面写到该曲目所属的歌集上。
//
// 封面是歌集的属性，不是曲目的：此前逐曲目存一份，用户在一首歌上采用封面
// 之后，同专辑的其余曲目仍然没有图，而 release.cover_path 有列却没有任何
// 路径写入。现在采用一次即对整张歌集生效。
//
// 曲目没有归属歌集时返回错误——没有歌集就没有地方放这张封面，
// 静默丢弃会让用户以为采用成功了。
func (s *Store) SetCover(trackID int64, c CoverRecord) error {
	return s.withTx(func(tx *sql.Tx) error {
		var releaseID *int64
		if err := tx.QueryRow(`SELECT release_id FROM track WHERE id = ?`, trackID).Scan(&releaseID); err != nil {
			return err
		}
		if releaseID == nil {
			return fmt.Errorf("该曲目没有所属歌集，无法采用封面")
		}
		_, err := tx.Exec(`
			UPDATE release SET
				cover_path       = ?,
				cover_source_url = ?,
				cover_width      = ?,
				cover_height     = ?,
				cover_fetched_at = ?,
				updated_at       = ?
			WHERE id = ?`,
			c.Path, c.SourceURL, c.Width, c.Height, c.FetchedAt, time.Now().UTC(), *releaseID)
		return err
	})
}

// SetReleaseCover 记录歌集级封面。
func (s *Store) SetReleaseCover(releaseID int64, path, sourceURL string) error {
	return s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC()
		_, err := tx.Exec(`
			UPDATE release SET cover_path = ?, cover_source_url = ?, cover_fetched_at = ?, updated_at = ?
			WHERE id = ?`, path, sourceURL, now, now, releaseID)
		return err
	})
}
