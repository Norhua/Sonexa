package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrTitleRequired 表示试图创建没有曲名的曲目。
var ErrTitleRequired = errors.New("曲名是必填项")

// Track 是基本信息：每首曲目必有，字段集合稳定。
type Track struct {
	ID           int64
	Title        string
	Artist       *string
	Album        *string
	TitleLocked  bool
	ArtistLocked bool
	AlbumLocked  bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CreateTrack 创建一首曲目并同步 FTS 索引。丰富信息可以整体为空。
func (s *Store) CreateTrack(t *Track) (int64, error) {
	if t.Title == "" {
		return 0, ErrTitleRequired
	}
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC()
		res, err := tx.Exec(`
			INSERT INTO track (title, artist, album, title_locked, artist_locked, album_locked, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?)`,
			t.Title, t.Artist, t.Album,
			boolToInt(t.TitleLocked), boolToInt(t.ArtistLocked), boolToInt(t.AlbumLocked),
			now, now)
		if err != nil {
			return fmt.Errorf("创建曲目失败: %w", err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return err
		}
		return syncFTS(tx, id)
	})
	return id, err
}

// GetTrack 按 id 读取基本信息。
func (s *Store) GetTrack(id int64) (*Track, error) {
	var t Track
	var titleLocked, artistLocked, albumLocked int
	err := s.db.QueryRow(`
		SELECT id, title, artist, album, title_locked, artist_locked, album_locked, created_at, updated_at
		FROM track WHERE id = ?`, id).Scan(
		&t.ID, &t.Title, &t.Artist, &t.Album,
		&titleLocked, &artistLocked, &albumLocked, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.TitleLocked, t.ArtistLocked, t.AlbumLocked = titleLocked != 0, artistLocked != 0, albumLocked != 0
	return &t, nil
}

// UpdateTrackFields 更新基本信息并同步 FTS。byUser 为真时对被改动的字段加锁。
func (s *Store) UpdateTrackFields(id int64, title, artist, album *string, byUser bool) error {
	return s.withTx(func(tx *sql.Tx) error {
		sets := []string{}
		args := []any{}
		if title != nil {
			if *title == "" {
				return ErrTitleRequired
			}
			sets = append(sets, "title = ?")
			args = append(args, *title)
			if byUser {
				sets = append(sets, "title_locked = 1")
			}
		}
		if artist != nil {
			sets = append(sets, "artist = ?")
			args = append(args, *artist)
			if byUser {
				sets = append(sets, "artist_locked = 1")
			}
		}
		if album != nil {
			sets = append(sets, "album = ?")
			args = append(args, *album)
			if byUser {
				sets = append(sets, "album_locked = 1")
			}
		}
		if len(sets) == 0 {
			return nil
		}
		sets = append(sets, "updated_at = ?")
		args = append(args, time.Now().UTC(), id)

		q := "UPDATE track SET " + joinComma(sets) + " WHERE id = ?"
		if _, err := tx.Exec(q, args...); err != nil {
			return fmt.Errorf("更新曲目失败: %w", err)
		}
		return syncFTS(tx, id)
	})
}

// DeleteTrack 删除一首曲目。
//
// 级联清理丰富信息、歌单与标签归属、艺术家关联（靠 FK ON DELETE CASCADE），
// 但不动 release / release_track——两者本就没有外键，该位置自然回落为灰色行。
// 这正是期望行为：用户删掉一首歌，专辑视图应显示「这张专辑你还缺这首」。
func (s *Store) DeleteTrack(id int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		if err := deleteFTS(tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM track WHERE id = ?`, id); err != nil {
			return fmt.Errorf("删除曲目失败: %w", err)
		}
		return nil
	})
}

// withTx 在事务中执行 fn，出错时回滚。
// 全部写操作集中在 store 层经由它进行，便于统一处理锁争用。
func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}
