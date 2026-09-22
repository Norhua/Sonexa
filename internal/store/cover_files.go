package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
)

var ErrCoverSelectionChanged = errors.New("专辑封面已被其他操作更新，请重新选择")

// CoverFile 只存缓存根目录内相对路径。ReleaseID 为空时是未采用的预览，
// 不能加入歌集的自动选图范围或受到用户采用保护。
type CoverFile struct {
	ID           int64     `json:"id"`
	VariantID    int64     `json:"variantId"`
	ReleaseID    *int64    `json:"releaseId"`
	RelativePath string    `json:"-"`
	ContentHash  string    `json:"-"`
	ByteSize     int64     `json:"byteSize"`
	Width        int       `json:"width"`
	Height       int       `json:"height"`
	Format       string    `json:"format"`
	FetchedAt    time.Time `json:"-"`
	LastUsedAt   time.Time `json:"-"`
	Protected    bool      `json:"-"`
	Available    bool      `json:"available"`
}

type CoverSelection struct {
	ReleaseID int64     `json:"releaseId"`
	ImageID   int64     `json:"imageId"`
	VariantID int64     `json:"variantId"`
	Revision  int64     `json:"revision"`
	AdoptedAt time.Time `json:"-"`
}

func saveCoverFileTx(tx *sql.Tx, file CoverFile) (int64, error) {
	if file.RelativePath == "" || path.IsAbs(file.RelativePath) || path.Clean(file.RelativePath) != file.RelativePath ||
		file.RelativePath == "." || strings.HasPrefix(file.RelativePath, "../") || strings.ContainsAny(file.RelativePath, "\\:\x00") {
		return 0, fmt.Errorf("封面文件路径必须是受控目录内的相对路径")
	}
	if file.ContentHash == "" || file.Width <= 0 || file.Height <= 0 || file.ByteSize <= 0 ||
		(file.Format != "jpeg" && file.Format != "png" && file.Format != "webp") {
		return 0, fmt.Errorf("封面文件缺少有效的校验信息")
	}
	if file.FetchedAt.IsZero() {
		file.FetchedAt = time.Now().UTC()
	}
	if file.LastUsedAt.IsZero() {
		file.LastUsedAt = file.FetchedAt
	}
	// 更新已知身份，而不是按文件名碰撞替换另一张图片。
	var id int64
	err := tx.QueryRow(`SELECT id FROM cover_file WHERE variant_id=? AND release_id IS ?`, file.VariantID, file.ReleaseID).Scan(&id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if errors.Is(err, sql.ErrNoRows) {
		result, err := tx.Exec(`INSERT INTO cover_file(variant_id,release_id,relative_path,content_hash,byte_size,width,height,format,fetched_at,last_used_at,protected,available)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,1)`, file.VariantID, file.ReleaseID, file.RelativePath, file.ContentHash, file.ByteSize,
			file.Width, file.Height, file.Format, file.FetchedAt.UTC(), file.LastUsedAt.UTC(), file.Protected)
		if err != nil {
			return 0, err
		}
		id, err = result.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		_, err = tx.Exec(`UPDATE cover_file SET relative_path=?,content_hash=?,byte_size=?,width=?,height=?,format=?,fetched_at=?,last_used_at=?,
			protected=MAX(protected,?),available=1 WHERE id=?`, file.RelativePath, file.ContentHash, file.ByteSize,
			file.Width, file.Height, file.Format, file.FetchedAt.UTC(), file.LastUsedAt.UTC(), file.Protected, id)
		if err != nil {
			return 0, err
		}
	}
	_, err = tx.Exec(`UPDATE cover_variant SET width=?,height=?,format=?,state='verified',last_error='' WHERE id=?`, file.Width, file.Height, file.Format, file.VariantID)
	return id, err
}

func (s *Store) SaveCoverFile(file CoverFile) (int64, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		id, err = saveCoverFileTx(tx, file)
		return err
	})
	return id, err
}

// AdoptCoverFile 在图片已校验落盘后一次提交文件记录与歌集采用关系。
// expectedRevision=0 表示尚无选择；迟到的采用不得覆盖另一次用户选择。
func (s *Store) AdoptCoverFile(file CoverFile, expectedRevision int64) (*CoverSelection, error) {
	if file.ReleaseID == nil || *file.ReleaseID <= 0 {
		return nil, fmt.Errorf("没有所属歌集，无法采用封面")
	}
	selection := &CoverSelection{ReleaseID: *file.ReleaseID, VariantID: file.VariantID, Revision: expectedRevision + 1, AdoptedAt: time.Now().UTC()}
	err := s.withTx(func(tx *sql.Tx) error {
		// 先写以取得 SQLite 写锁，再检查版本，避免并发读升级导致 BUSY。
		if _, err := tx.Exec(`UPDATE release SET updated_at=updated_at WHERE id=?`, selection.ReleaseID); err != nil {
			return err
		}
		var exists int
		if err := tx.QueryRow(`SELECT 1 FROM release WHERE id=?`, selection.ReleaseID).Scan(&exists); err != nil {
			return err
		}
		var revision int64
		err := tx.QueryRow(`SELECT revision FROM release_cover_selection WHERE release_id=?`, selection.ReleaseID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision != expectedRevision {
			return ErrCoverSelectionChanged
		}
		if err := tx.QueryRow(`SELECT image_id FROM cover_variant WHERE id=?`, file.VariantID).Scan(&selection.ImageID); err != nil {
			return err
		}
		file.Protected = true
		if _, err := saveCoverFileTx(tx, file); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO release_cover_selection(release_id,image_id,variant_id,revision,adopted_at) VALUES(?,?,?,?,?)
			ON CONFLICT(release_id) DO UPDATE SET image_id=excluded.image_id,variant_id=excluded.variant_id,
			revision=excluded.revision,adopted_at=excluded.adopted_at`, selection.ReleaseID, selection.ImageID, selection.VariantID, selection.Revision, selection.AdoptedAt)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE release SET updated_at=? WHERE id=?`, selection.AdoptedAt, selection.ReleaseID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return selection, nil
}

func (s *Store) GetCoverSelection(releaseID int64) (*CoverSelection, error) {
	var selection CoverSelection
	err := s.db.QueryRow(`SELECT release_id,image_id,variant_id,revision,adopted_at FROM release_cover_selection WHERE release_id=?`, releaseID).
		Scan(&selection.ReleaseID, &selection.ImageID, &selection.VariantID, &selection.Revision, &selection.AdoptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &selection, nil
}

const coverFileColumns = `id,variant_id,release_id,relative_path,content_hash,byte_size,width,height,format,fetched_at,last_used_at,protected,available`

func scanCoverFile(row interface{ Scan(...any) error }) (*CoverFile, error) {
	var file CoverFile
	err := row.Scan(&file.ID, &file.VariantID, &file.ReleaseID, &file.RelativePath, &file.ContentHash, &file.ByteSize,
		&file.Width, &file.Height, &file.Format, &file.FetchedAt, &file.LastUsedAt, &file.Protected, &file.Available)
	return &file, err
}

func (s *Store) GetCoverFile(id int64) (*CoverFile, error) {
	file, err := scanCoverFile(s.db.QueryRow(`SELECT `+coverFileColumns+` FROM cover_file WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return file, err
}

func (s *Store) ListReleaseCoverFiles(releaseID int64) ([]CoverFile, error) {
	return s.queryCoverFiles(`SELECT `+coverFileColumns+` FROM cover_file WHERE release_id=? ORDER BY id`, releaseID)
}

func (s *Store) ListCatalogPreviewFiles(catalogID int64) ([]CoverFile, error) {
	return s.queryCoverFiles(`SELECT `+coverFileColumns+` FROM cover_file WHERE release_id IS NULL AND variant_id IN
		(SELECT v.id FROM cover_variant v JOIN cover_image i ON i.id=v.image_id WHERE i.catalog_id=?) ORDER BY id`, catalogID)
}

func (s *Store) PreviewFilesForVariant(variantID int64) ([]CoverFile, error) {
	return s.queryCoverFiles(`SELECT `+coverFileColumns+` FROM cover_file WHERE release_id IS NULL AND variant_id=?`, variantID)
}

func (s *Store) queryCoverFiles(query string, args ...any) ([]CoverFile, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	files := []CoverFile{}
	for rows.Next() {
		file, err := scanCoverFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *file)
	}
	return files, rows.Err()
}

// MarkCoverFileMissing 保留已验证像素和采用身份，只撤销文件命中状态。
func (s *Store) MarkCoverFileMissing(id int64) error {
	_, err := s.db.Exec(`UPDATE cover_file SET available=0 WHERE id=?`, id)
	return err
}
