package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrTitleRequired 表示试图创建没有曲名的曲目。
var ErrTitleRequired = errors.New("曲名是必填项")

// Track 是曲目的基本形态。
//
// 曲目是单表：此前 track 与 track_metadata 各存一份曲名、专辑名与署名，
// 写入路径两边都写而「哪一份为准」没有定论。现在曲名必存在（title NOT NULL），
// 其余可空，「丰富信息可整体为空」由列的可空性表达。
//
// Album 由所属歌集带出，不是本表的列。
type Track struct {
	ID        int64           `json:"id"`
	ReleaseID *int64          `json:"releaseId"`
	Title     string          `json:"title"`
	Credit    *string         `json:"credit"`
	Album     *string         `json:"album"`
	Locked    map[string]bool `json:"locked"`
	CreatedAt time.Time       `json:"-"`
	UpdatedAt time.Time       `json:"-"`
}

// CreateTrack 创建一首曲目并同步 FTS 索引。
//
// 有歌集名时曲目会被挂到一条本地歌集上——没有上游标识的曲目也要有归属，
// 否则专辑视图要为它们单独走一条派生分组的代码路径。
func (s *Store) CreateTrack(t *Track) (int64, error) {
	if t.Title == "" {
		return 0, ErrTitleRequired
	}
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		releaseID := int64(0)
		if t.ReleaseID != nil {
			releaseID = *t.ReleaseID
		} else if t.Album != nil && strings.TrimSpace(*t.Album) != "" {
			var err error
			releaseID, err = findOrCreateLocalReleaseTx(tx, *t.Album, t.Credit)
			if err != nil {
				return err
			}
		}

		lockedJSON, err := marshalLocks(t.Locked)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		res, err := tx.Exec(`
			INSERT INTO track (release_id, title, credit, locked_json, created_at, updated_at)
			VALUES (?,?,?,?,?,?)`,
			nullableID(releaseID), t.Title, t.Credit, lockedJSON, now, now)
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

func marshalLocks(locks map[string]bool) (*string, error) {
	if len(locks) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(locks)
	if err != nil {
		return nil, err
	}
	s := string(data)
	return &s, nil
}

// GetTrack 按 id 读取曲目，含由歌集带出的歌集名。
func (s *Store) GetTrack(id int64) (*Track, error) {
	var t Track
	var lockedJSON *string
	err := s.db.QueryRow(`
		SELECT t.id, t.release_id, t.title, t.credit, r.title, t.locked_json,
		       t.created_at, t.updated_at
		FROM track t LEFT JOIN release r ON r.id = t.release_id
		WHERE t.id = ?`, id).Scan(
		&t.ID, &t.ReleaseID, &t.Title, &t.Credit, &t.Album, &lockedJSON,
		&t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Locked = ParseLocks(lockedJSON)
	return &t, nil
}

// UpdateTrackFields 更新曲名与署名并同步 FTS。byUser 为真时对被改动的字段加锁。
//
// 歌集名不在这里改：它归歌集，一处改动对该歌集的全部曲目生效，
// 走 UpdateReleaseFields / MoveTrackToRelease。
func (s *Store) UpdateTrackFields(id int64, title, credit *string, byUser bool) error {
	return s.withTx(func(tx *sql.Tx) error {
		var lockedJSON *string
		if err := tx.QueryRow(`SELECT locked_json FROM track WHERE id = ?`, id).Scan(&lockedJSON); err != nil {
			return err
		}
		locks := ParseLocks(lockedJSON)

		sets := []string{}
		args := []any{}
		if title != nil {
			if *title == "" {
				return ErrTitleRequired
			}
			sets = append(sets, "title = ?")
			args = append(args, *title)
			if byUser {
				locks["title"] = true
			}
		}
		if credit != nil {
			sets = append(sets, "credit = ?")
			args = append(args, *credit)
			if byUser {
				locks["credit"] = true
			}
		}
		if len(sets) == 0 {
			return nil
		}
		if byUser {
			encoded, err := marshalLocks(locks)
			if err != nil {
				return err
			}
			sets = append(sets, "locked_json = ?")
			args = append(args, encoded)
		}
		sets = append(sets, "updated_at = ?")
		args = append(args, time.Now().UTC(), id)

		if _, err := tx.Exec(
			"UPDATE track SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			return fmt.Errorf("更新曲目失败: %w", err)
		}
		return syncFTS(tx, id)
	})
}

// DeleteTrack 删除一首曲目。
//
// 级联清理署名分解、艺术家关联、歌单与标签归属（靠 FK ON DELETE CASCADE），
// 但不动 release_track——清单与曲目本就没有外键，该位置自然回落为灰色行。
// 这正是期望行为：用户删掉一首歌，专辑视图应显示「这张专辑你还缺这首」。
//
// 所属歌集若是本地歌集且再无曲目引用，顺带删除——上游来源的保留，
// 它同时是清单缓存。
func (s *Store) DeleteTrack(id int64) error {
	var releaseID *int64
	if err := s.db.QueryRow(`SELECT release_id FROM track WHERE id = ?`, id).Scan(&releaseID); err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if err := s.withTx(func(tx *sql.Tx) error {
		if err := deleteFTS(tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM track WHERE id = ?`, id); err != nil {
			return fmt.Errorf("删除曲目失败: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if releaseID != nil {
		return s.DeleteOrphanLocalRelease(*releaseID)
	}
	return nil
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
