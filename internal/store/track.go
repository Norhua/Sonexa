package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/norhua/sonexa/internal/domain"
	"time"
)

// ErrTitleRequired 表示试图创建没有曲名的曲目。
var ErrTitleRequired = errors.New("曲名是必填项")

// Track 是曲目当前值的读取投影；基本信息可空，Album 由所属发行带出。
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
	return s.CreateTrackWithProvenance(NewTrackInput{ExistingReleaseID: t.ReleaseID, Title: t.Title, Credit: t.Credit, Album: t.Album, AlbumCredit: t.Credit})
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
		FROM track_read t LEFT JOIN release_read r ON r.id = t.release_id
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
		fields := fieldMap{"title": rawFromPointer(title), "credit": rawFromPointer(credit)}
		if title != nil && *title == "" {
			fields["title"] = rawField{State: domain.Empty}
		}
		var source *string
		if err := tx.QueryRow("SELECT match_source FROM track WHERE id=?", id).Scan(&source); err != nil {
			return err
		}
		if err := writeCurrentTx(tx, "track", id, fields, deref(source), byUser, nil); err != nil {
			return err
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
// 删除最后一首后同时清理无收藏发行；还有其他收藏时保留清单及封面。
func (s *Store) DeleteTrack(id int64) error {
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	err := s.withTx(func(tx *sql.Tx) error {
		var releaseID *int64
		if err := tx.QueryRow("SELECT release_id FROM track WHERE id=?", id).Scan(&releaseID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if err := deleteFTS(tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM track WHERE id=?", id); err != nil {
			return fmt.Errorf("删除曲目失败: %w", err)
		}
		if releaseID != nil {
			return deleteOrphanReleaseTx(tx, *releaseID)
		}
		return nil
	})
	if err == nil {
		_ = s.collectLyrics()
	}
	return err
}

// withTx 在事务中执行 fn，出错时回滚。

func (s *Store) withTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	if s.beforeCommit != nil {
		if err := s.beforeCommit(tx); err != nil {
			return err
		}
	}
	return tx.Commit()
}
