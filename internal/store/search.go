package store

import "fmt"

// SearchTrackIDs 在全文索引中检索，返回命中的曲目 id（按相关度排序）。
//
// 命中范围：曲名、艺术家串、专辑名、曲目级署名原串、全部关联艺术家名。
// 索引包含所有本地实体，本入口在 LIMIT 之前限定音乐库。
func (s *Store) SearchTrackIDs(query string, limit int) ([]int64, error) {
	q := ftsQuery(query)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 500
	}
	rows, err := s.db.Query(
		`SELECT track_fts.rowid FROM track_fts JOIN track t ON t.id=track_fts.rowid WHERE track_fts MATCH ? AND t.in_library=1 ORDER BY rank LIMIT ?`, q, limit)
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
