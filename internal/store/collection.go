package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CollectionKind 区分歌单与标签。两者是同一套模型。
type CollectionKind string

const (
	KindPlaylist CollectionKind = "playlist"
	KindTag      CollectionKind = "tag"
)

// Collection 是一个歌单或标签。
type Collection struct {
	ID        int64          `json:"id"`
	Name      string         `json:"name"`
	Kind      CollectionKind `json:"kind"`
	Color     *string        `json:"color"`
	SortOrder *int           `json:"sortOrder"`
	// TrackCount 是其中的曲目数，供侧栏显示。
	TrackCount int `json:"trackCount"`
}

// ErrDuplicateName 表示同种类内已有同名条目。
var ErrDuplicateName = errors.New("同名条目已存在")

// CreateCollection 新建歌单或标签。
//
// 同名约束在种类内生效：同名的歌单与标签可以并存。
func (s *Store) CreateCollection(name string, kind CollectionKind, color *string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("名称不能为空")
	}

	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			`INSERT INTO collection (name, kind, color, created_at) VALUES (?,?,?,?)`,
			name, string(kind), color, time.Now().UTC())
		if err != nil {
			if isUniqueViolation(err) {
				return fmt.Errorf("%w: %s「%s」", ErrDuplicateName, kindDisplay(kind), name)
			}
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

func kindDisplay(k CollectionKind) string {
	if k == KindTag {
		return "标签"
	}
	return "歌单"
}

// ListCollections 列出某种类的全部条目，带曲目数。
func (s *Store) ListCollections(kind CollectionKind) ([]Collection, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.kind, c.color, c.sort_order,
		       (SELECT COUNT(*) FROM collection_item ci WHERE ci.collection_id = c.id)
		FROM collection c WHERE c.kind = ?
		ORDER BY COALESCE(c.sort_order, 1 << 30), c.name`, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Collection{}
	for rows.Next() {
		var c Collection
		var k string
		if err := rows.Scan(&c.ID, &c.Name, &k, &c.Color, &c.SortOrder, &c.TrackCount); err != nil {
			return nil, err
		}
		c.Kind = CollectionKind(k)
		out = append(out, c)
	}
	return out, rows.Err()
}

// RenameCollection 重命名。
func (s *Store) RenameCollection(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("名称不能为空")
	}
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE collection SET name = ? WHERE id = ?`, name, id)
		if err != nil && isUniqueViolation(err) {
			return fmt.Errorf("%w: 「%s」", ErrDuplicateName, name)
		}
		return err
	})
}

// DeleteCollection 删除歌单或标签。曲目本身不受影响。
func (s *Store) DeleteCollection(id int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM collection WHERE id = ?`, id)
		return err
	})
}

// AddToCollection 把曲目加入歌单或标签。
//
// 同一套接口服务两种种类，仅目标的 kind 不同。
// 已有该归属的曲目不产生重复归属。
func (s *Store) AddToCollection(collectionID int64, trackIDs []int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		// 歌单需要显式顺序，追加到末尾
		var nextPos int
		if err := tx.QueryRow(
			`SELECT COALESCE(MAX(position), -1) + 1 FROM collection_item WHERE collection_id = ?`,
			collectionID).Scan(&nextPos); err != nil {
			return err
		}

		now := time.Now().UTC()
		for i, trackID := range trackIDs {
			if _, err := tx.Exec(`
				INSERT INTO collection_item (collection_id, track_id, position, added_at)
				VALUES (?,?,?,?)
				ON CONFLICT(collection_id, track_id) DO NOTHING`,
				collectionID, trackID, nextPos+i, now); err != nil {
				return fmt.Errorf("加入失败: %w", err)
			}
		}
		return nil
	})
}

// RemoveFromCollection 把曲目从歌单或标签中移出。
//
// 移出不等于删除：曲目仍存在于全部曲目中。
func (s *Store) RemoveFromCollection(collectionID int64, trackIDs []int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		for _, trackID := range trackIDs {
			if _, err := tx.Exec(
				`DELETE FROM collection_item WHERE collection_id = ? AND track_id = ?`,
				collectionID, trackID); err != nil {
				return err
			}
		}
		return nil
	})
}

// ReorderCollection 按给定顺序重排歌单。
//
// 标签忽略顺序，只有歌单需要这个操作。
func (s *Store) ReorderCollection(collectionID int64, orderedTrackIDs []int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		for pos, trackID := range orderedTrackIDs {
			if _, err := tx.Exec(
				`UPDATE collection_item SET position = ? WHERE collection_id = ? AND track_id = ?`,
				pos, collectionID, trackID); err != nil {
				return err
			}
		}
		return nil
	})
}

// TagsOfTrack 返回某曲目的全部标签，供详情抽屉以 chips 显示。
//
// 歌单不在抽屉显示——抽屉只显示标签。
func (s *Store) TagsOfTrack(trackID int64) ([]Collection, error) {
	rows, err := s.db.Query(`
		SELECT c.id, c.name, c.kind, c.color, c.sort_order, 0
		FROM collection c
		JOIN collection_item ci ON ci.collection_id = c.id
		WHERE ci.track_id = ? AND c.kind = ?
		ORDER BY c.name`, trackID, string(KindTag))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Collection{}
	for rows.Next() {
		var c Collection
		var k string
		if err := rows.Scan(&c.ID, &c.Name, &k, &c.Color, &c.SortOrder, &c.TrackCount); err != nil {
			return nil, err
		}
		c.Kind = CollectionKind(k)
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCollection 按 id 读取。
func (s *Store) GetCollection(id int64) (*Collection, error) {
	var c Collection
	var k string
	err := s.db.QueryRow(`
		SELECT c.id, c.name, c.kind, c.color, c.sort_order,
		       (SELECT COUNT(*) FROM collection_item ci WHERE ci.collection_id = c.id)
		FROM collection c WHERE c.id = ?`, id).Scan(
		&c.ID, &c.Name, &k, &c.Color, &c.SortOrder, &c.TrackCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.Kind = CollectionKind(k)
	return &c, nil
}
