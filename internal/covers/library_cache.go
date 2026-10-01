package covers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

func (l *LibraryImages) Configure(maxBytes int64, ttl time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maxBytes, l.ttl = maxBytes, ttl
	return l.pruneLocked(false)
}

func (l *LibraryImages) Clear() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.pruneLocked(true)
}

func (l *LibraryImages) expireLocked(file store.CoverFile) error {
	if file.Protected {
		return nil
	}
	if err := l.files.Remove(file.RelativePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return l.store.MarkCoverFileMissing(file.ID)
}

func (l *LibraryImages) pruneLocked(clear bool) error {
	files, err := l.store.AllCoverFiles()
	if err != nil {
		return err
	}
	var total int64
	kept := []store.CoverFile{}
	for _, file := range files {
		if file.Protected || !file.Available {
			continue
		}
		if clear || (l.ttl > 0 && time.Since(file.FetchedAt) > l.ttl) {
			if err := l.expireLocked(file); err != nil {
				return err
			}
			continue
		}
		total += file.ByteSize
		kept = append(kept, file)
	}
	for _, file := range kept {
		if l.maxBytes <= 0 || total <= l.maxBytes {
			break
		}
		if err := l.expireLocked(file); err != nil {
			return err
		}
		total -= file.ByteSize
	}
	return nil
}

// MoveRoot 将写操作阻塞在同一锁上。复制校验完成、设置原子保存成功之后才换根。
// 旧根保留用于回滚；已缺失的保护文件会使迁移失败而不是静默丢图。
func (l *LibraryImages) MoveRoot(dir string, commit func() error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	old := l.files
	if filepath.Clean(dir) == filepath.Clean(old.root.Name()) {
		return commit()
	}
	next, err := NewAlbumFiles(dir)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = next.Close()
		}
	}()
	files, err := l.store.AllCoverFiles()
	if err != nil {
		return err
	}
	for _, file := range files {
		if !file.Available && !file.Protected {
			continue
		}
		data, err := old.Read(file.RelativePath)
		if err != nil {
			return fmt.Errorf("迁移封面 %s: %w", file.RelativePath, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.ContentHash {
			return fmt.Errorf("迁移封面校验不匹配")
		}
		if err := copyExact(next, file.RelativePath, data); err != nil {
			return err
		}
	}
	for _, draft := range l.drafts {
		data, err := old.Read(draft.relative)
		if err != nil {
			return err
		}
		if err := copyExact(next, draft.relative, data); err != nil {
			return err
		}
	}
	if err := commit(); err != nil {
		return err
	}
	l.files = next
	committed = true
	return old.Close()
}

func copyExact(files *AlbumFiles, relative string, data []byte) error {
	if !safeRelativePath(relative) {
		return fmt.Errorf("非法迁移路径")
	}
	if existing, err := files.Read(relative); err == nil {
		a, b := sha256.Sum256(existing), sha256.Sum256(data)
		if a != b {
			return fmt.Errorf("目标目录已有不同内容: %s", relative)
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	name := filepath.FromSlash(relative)
	if err := files.root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	file, err := files.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = files.root.Remove(name)
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	written, err := files.Read(relative)
	if err != nil {
		return err
	}
	if sha256.Sum256(written) != sha256.Sum256(data) {
		return fmt.Errorf("迁移落盘校验失败")
	}
	return nil
}
