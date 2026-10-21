package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/domain"
)

type LyricsDraft struct {
	Token string `json:"token"`
	Hash  string `json:"hash"`
	Size  int    `json:"size"`
}
type preparedLyrics struct {
	path, hash string
	size       int
}

// StageLyrics 只写临时文件，尚不产生任何正式文件引用。
func (s *Store) StageLyrics(ctx context.Context, text string) (LyricsDraft, error) {
	if err := ctx.Err(); err != nil {
		return LyricsDraft{}, err
	}
	if !utf8.ValidString(text) {
		return LyricsDraft{}, fmt.Errorf("歌词必须是 UTF-8 文本")
	}
	if err := os.MkdirAll(filepath.Join(s.lyricsRoot, "drafts"), 0o700); err != nil {
		return LyricsDraft{}, err
	}
	f, err := os.CreateTemp(filepath.Join(s.lyricsRoot, "drafts"), "lyrics-")
	if err != nil {
		return LyricsDraft{}, err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			_ = os.Remove(f.Name())
		}
	}()
	if _, err := f.WriteString(text); err != nil {
		return LyricsDraft{}, err
	}
	if err := f.Sync(); err != nil {
		return LyricsDraft{}, err
	}
	if err := ctx.Err(); err != nil {
		return LyricsDraft{}, err
	}
	hash := sha256.Sum256([]byte(text))
	ok = true
	return LyricsDraft{Token: filepath.Base(f.Name()), Hash: hex.EncodeToString(hash[:]), Size: len(text)}, nil
}
func (s *Store) DiscardLyrics(draft LyricsDraft) error {
	if !validDraftToken(draft.Token) {
		return fmt.Errorf("歌词草稿标识无效")
	}
	err := os.Remove(filepath.Join(s.lyricsRoot, "drafts", draft.Token))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
func validDraftToken(token string) bool {
	return strings.HasPrefix(token, "lyrics-") && filepath.Base(token) == token && !strings.ContainsAny(token, "/\\")
}

// prepareLyrics 在 SQL 提交前准备不可变最终路径；调用方持有 fileMu 至提交和回收完成。
func (s *Store) prepareLyrics(ctx context.Context, trackID int64, draft LyricsDraft) (preparedLyrics, error) {
	if !validDraftToken(draft.Token) {
		return preparedLyrics{}, fmt.Errorf("歌词草稿标识无效")
	}
	data, err := os.ReadFile(filepath.Join(s.lyricsRoot, "drafts", draft.Token))
	if err != nil {
		return preparedLyrics{}, err
	}
	h := sha256.Sum256(data)
	hash := hex.EncodeToString(h[:])
	if hash != draft.Hash || len(data) != draft.Size || !utf8.Valid(data) {
		return preparedLyrics{}, fmt.Errorf("歌词草稿校验失败")
	}
	if err := ctx.Err(); err != nil {
		return preparedLyrics{}, err
	}
	ext := ".lrc"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "[xlrc:") || strings.HasPrefix(line, "[>") {
			ext = ".xlrc"
			break
		}
	}
	rel := fmt.Sprintf("current/%d-%s%s", trackID, hash, ext)
	path := filepath.Join(s.lyricsRoot, rel)
	if old, err := os.ReadFile(path); err == nil {
		oldHash := sha256.Sum256(old)
		if oldHash != h {
			return preparedLyrics{}, fmt.Errorf("歌词最终文件校验不匹配")
		}
		return preparedLyrics{path: rel, hash: hash, size: len(data)}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return preparedLyrics{}, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".writing-")
	if err != nil {
		return preparedLyrics{}, err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return preparedLyrics{}, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return preparedLyrics{}, err
	}
	if err := f.Close(); err != nil {
		return preparedLyrics{}, err
	}
	if err := ctx.Err(); err != nil {
		return preparedLyrics{}, err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return preparedLyrics{}, err
	}
	return preparedLyrics{path: rel, hash: hash, size: len(data)}, nil
}
func registerLyricsTx(tx *sql.Tx, p preparedLyrics, source string) (int64, error) {
	if _, err := tx.Exec("INSERT INTO lyrics_file(relative_path,content_hash,byte_size,source,created_at) VALUES(?,?,?,?,?) ON CONFLICT(relative_path) DO NOTHING", p.path, p.hash, p.size, source, time.Now().UTC()); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow("SELECT id FROM lyrics_file WHERE relative_path=? AND content_hash=?", p.path, p.hash).Scan(&id)
	return id, err
}
func (s *Store) readLyrics(id int64) (*string, error) {
	var relative, hash string
	if err := s.db.QueryRow("SELECT relative_path,content_hash FROM lyrics_file WHERE id=?", id).Scan(&relative, &hash); err != nil {
		return nil, err
	}
	if filepath.IsAbs(relative) || strings.Contains(relative, "..") {
		return nil, fmt.Errorf("歌词文件路径无效")
	}
	data, err := os.ReadFile(filepath.Join(s.lyricsRoot, relative))
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(data)
	if hex.EncodeToString(h[:]) != hash {
		return nil, fmt.Errorf("歌词文件校验失败")
	}
	text := string(data)
	return &text, nil
}

func (s *Store) SaveLyrics(ctx context.Context, trackID int64, draft *LyricsDraft, revision int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.fileMu.Lock()
	defer s.fileMu.Unlock()
	var prepared preparedLyrics
	var err error
	if draft != nil {
		prepared, err = s.prepareLyrics(ctx, trackID, *draft)
		if err != nil {
			return err
		}
	}
	err = s.withTx(func(tx *sql.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var current int64
		if err := tx.QueryRow("SELECT revision FROM track WHERE id=?", trackID).Scan(&current); err != nil {
			return err
		}
		if current != revision {
			return ErrRevisionConflict
		}
		field := rawField{State: domain.Empty}
		if draft != nil {
			id, err := registerLyricsTx(tx, prepared, "user")
			if err != nil {
				return err
			}
			field = rawKnown(id)
		}
		return writeCurrentTx(tx, "track", trackID, fieldMap{"lyricsFileId": field}, "", true, nil)
	})
	if err == nil && draft != nil {
		_ = s.DiscardLyrics(*draft)
	}
	_ = s.collectLyrics()
	return err
}

// collectLyrics 仅回收无当前/快照引用的最终文件。编辑草稿由会话释放，重启才清理残留。
func (s *Store) collectLyrics() error {
	if _, err := s.db.Exec(`DELETE FROM lyrics_file WHERE id NOT IN (SELECT lyrics_file_id FROM track_current WHERE lyrics_file_id IS NOT NULL UNION SELECT lyrics_file_id FROM track_snapshot WHERE lyrics_file_id IS NOT NULL)`); err != nil {
		return err
	}
	rows, err := s.db.Query("SELECT relative_path FROM lyrics_file")
	if err != nil {
		return err
	}
	live := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return err
		}
		live[p] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(filepath.Join(s.lyricsRoot, "current"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() && !live["current/"+entry.Name()] {
			if err := os.Remove(filepath.Join(s.lyricsRoot, "current", entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
