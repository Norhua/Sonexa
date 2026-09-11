package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// syncFTS 在事务内刷新某曲目的全文检索索引。
//
// 索引内容：曲名、艺术家串、专辑名、曲目级署名原串，以及该曲目全部关联艺术家名
// 的拼接。最后一项是「搜索 平野綾 与 涼宮ハルヒ 都能命中同一首歌」的实现基础。
//
// 必须与曲目/关系的写入在同一事务内调用——否则改了关系却没刷索引时，
// 搜声优名会搜不到，且这种漂移很难被发现。
func syncFTS(tx *sql.Tx, trackID int64) error {
	if err := deleteFTS(tx, trackID); err != nil {
		return err
	}

	var row ftsRow
	err := tx.QueryRow(`
		SELECT title, artist, album, track_credit, artist_names
		FROM track_fts_source WHERE rowid = ?`, trackID).Scan(
		&row.Title, &row.Artist, &row.Album, &row.TrackCredit, &row.ArtistNames)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // 曲目已不存在（如删除路径），无需重建索引
	}
	if err != nil {
		return fmt.Errorf("读取 FTS 内容失败: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO track_fts(rowid, title, artist, album, track_credit, artist_names)
		VALUES (?,?,?,?,?,?)`,
		trackID, row.Title, row.Artist, row.Album, row.TrackCredit, row.ArtistNames); err != nil {
		return fmt.Errorf("写入 FTS 行失败: %w", err)
	}
	if _, err := tx.Exec(`
		INSERT INTO track_fts_shadow (track_id, title, artist, album, track_credit, artist_names)
		VALUES (?,?,?,?,?,?)`,
		trackID, row.Title, row.Artist, row.Album, row.TrackCredit, row.ArtistNames); err != nil {
		return fmt.Errorf("记录 FTS 影子行失败: %w", err)
	}
	return nil
}

// deleteFTS 在事务内移除某曲目的索引行。索引中尚无该行时是无操作。
//
// contentless 表的 'delete' 命令要求提供的列值与当初索引进去的完全一致，
// 否则索引会损坏。这些值取自影子表，因此与调用时机无关——
// 数据本身是否已被改写都不影响删除的正确性。
func deleteFTS(tx *sql.Tx, trackID int64) error {
	var old ftsRow
	err := tx.QueryRow(`
		SELECT title, artist, album, track_credit, artist_names
		FROM track_fts_shadow WHERE track_id = ?`, trackID).Scan(
		&old.Title, &old.Artist, &old.Album, &old.TrackCredit, &old.ArtistNames)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取 FTS 影子行失败: %w", err)
	}

	if _, err := tx.Exec(`
		INSERT INTO track_fts(track_fts, rowid, title, artist, album, track_credit, artist_names)
		VALUES ('delete', ?, ?, ?, ?, ?, ?)`,
		trackID, old.Title, old.Artist, old.Album, old.TrackCredit, old.ArtistNames); err != nil {
		return fmt.Errorf("删除旧 FTS 行失败: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM track_fts_shadow WHERE track_id = ?`, trackID); err != nil {
		return fmt.Errorf("清除 FTS 影子行失败: %w", err)
	}
	return nil
}

// ftsRow 是一行索引内容。
type ftsRow struct {
	Title       string
	Artist      string
	Album       string
	TrackCredit string
	ArtistNames string
}

// RebuildFTS 重建全部曲目的检索索引。
func (s *Store) RebuildFTS() error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO track_fts(track_fts) VALUES('delete-all')`); err != nil {
			return fmt.Errorf("清空 FTS 索引失败: %w", err)
		}
		if _, err := tx.Exec(`DELETE FROM track_fts_shadow`); err != nil {
			return err
		}
		if _, err := tx.Exec(`
			INSERT INTO track_fts(rowid, title, artist, album, track_credit, artist_names)
			SELECT rowid, title, artist, album, track_credit, artist_names FROM track_fts_source`); err != nil {
			return fmt.Errorf("重建 FTS 索引失败: %w", err)
		}
		if _, err := tx.Exec(`
			INSERT INTO track_fts_shadow (track_id, title, artist, album, track_credit, artist_names)
			SELECT rowid, title, artist, album, track_credit, artist_names FROM track_fts_source`); err != nil {
			return err
		}
		return nil
	})
}

// ftsQuery 把用户输入转为 FTS5 的查询串。
// 用户输入的是自然文本而非 FTS 语法，因此逐词加引号，避免特殊字符被当作操作符。
func ftsQuery(input string) string {
	fields := strings.Fields(input)
	quoted := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		if f == "" {
			continue
		}
		quoted = append(quoted, `"`+f+`"`)
	}
	return strings.Join(quoted, " ")
}
