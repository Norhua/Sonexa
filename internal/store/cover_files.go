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

// CoverSelection 是歌集的封面采用关系。Cleared 表示用户清除了封面：
// 图片身份为空，自动获取不再为该歌集补图，直到用户重新采用。
type CoverSelection struct {
	ReleaseID int64     `json:"releaseId"`
	ImageID   int64     `json:"imageId"`
	VariantID int64     `json:"variantId"`
	Revision  int64     `json:"revision"`
	Cleared   bool      `json:"cleared"`
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
	// 首条数据库语句必须是写入：WAL 下先 SELECT 会建立读快照，
	// 与目录刷新并发时升级写锁可能立即 SQLITE_BUSY，busy_timeout 无法等待。
	// 规格验证与文件记录在同一事务中，后续失败会一起回滚。
	if _, err := tx.Exec(`UPDATE cover_variant SET width=?,height=?,format=?,state='verified',last_error='' WHERE id=?`, file.Width, file.Height, file.Format, file.VariantID); err != nil {
		return 0, fmt.Errorf("登记封面规格验证失败: %w", err)
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
	return id, nil
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
	var selection *CoverSelection
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		selection, err = adoptCoverFileTx(tx, file, expectedRevision)
		return err
	})
	if err != nil {
		return nil, err
	}
	return selection, nil
}

func adoptCoverFileTx(tx *sql.Tx, file CoverFile, expectedRevision int64) (*CoverSelection, error) {
	if file.ReleaseID == nil || *file.ReleaseID <= 0 {
		return nil, fmt.Errorf("没有所属歌集，无法采用封面")
	}
	selection := &CoverSelection{ReleaseID: *file.ReleaseID, VariantID: file.VariantID, Revision: expectedRevision + 1, AdoptedAt: time.Now().UTC()}

	// 先写以取得 SQLite 写锁，再检查版本，避免并发读升级导致 BUSY。
	if _, err := tx.Exec(`UPDATE release SET updated_at=updated_at WHERE id=?`, selection.ReleaseID); err != nil {
		return nil, err
	}
	var exists int
	if err := tx.QueryRow(`SELECT 1 FROM release_read WHERE id=?`, selection.ReleaseID).Scan(&exists); err != nil {
		return nil, err
	}
	var revision int64
	err := tx.QueryRow(`SELECT revision FROM release_cover_selection WHERE release_id=?`, selection.ReleaseID).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if revision != expectedRevision {
		return nil, ErrCoverSelectionChanged
	}
	if file.VariantID == 0 {
		catalog, err := ensureCoverCatalogTx(tx, CoverCatalogKey{Source: "local", ObjectType: "image", SourceRef: file.ContentHash})
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec("INSERT INTO cover_image(catalog_id,source_image_id,types_json,primary_type,is_front) VALUES(?,?,?, 'Front',1) ON CONFLICT(catalog_id,source_image_id) DO NOTHING", catalog, file.ContentHash, `["Front"]`); err != nil {
			return nil, err
		}
		var imageID int64
		if err := tx.QueryRow("SELECT id FROM cover_image WHERE catalog_id=? AND source_image_id=?", catalog, file.ContentHash).Scan(&imageID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec("INSERT INTO cover_variant(image_id,spec) VALUES(?,'original') ON CONFLICT(image_id,spec) DO NOTHING", imageID); err != nil {
			return nil, err
		}
		if err := tx.QueryRow("SELECT id FROM cover_variant WHERE image_id=? AND spec='original'", imageID).Scan(&file.VariantID); err != nil {
			return nil, err
		}
		selection.VariantID = file.VariantID
	}

	if err := tx.QueryRow(`SELECT image_id FROM cover_variant WHERE id=?`, file.VariantID).Scan(&selection.ImageID); err != nil {
		return nil, err
	}
	file.Protected = true
	if _, err := saveCoverFileTx(tx, file); err != nil {
		return nil, err
	}
	_, err = tx.Exec(`INSERT INTO release_cover_selection(release_id,image_id,variant_id,revision,adopted_at) VALUES(?,?,?,?,?)
			ON CONFLICT(release_id) DO UPDATE SET image_id=excluded.image_id,variant_id=excluded.variant_id,
			revision=excluded.revision,adopted_at=excluded.adopted_at`, selection.ReleaseID, selection.ImageID, selection.VariantID, selection.Revision, selection.AdoptedAt)
	if err != nil {
		return nil, err
	}
	_, err = tx.Exec(`UPDATE release SET revision=revision+1,updated_at=? WHERE id=?`, selection.AdoptedAt, selection.ReleaseID)
	return selection, err
}

func (s *Store) GetCoverSelection(releaseID int64) (*CoverSelection, error) {
	var selection CoverSelection
	var imageID, variantID sql.NullInt64
	err := s.db.QueryRow(`SELECT release_id,image_id,variant_id,revision,adopted_at FROM release_cover_selection WHERE release_id=?`, releaseID).
		Scan(&selection.ReleaseID, &imageID, &variantID, &selection.Revision, &selection.AdoptedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	selection.ImageID, selection.VariantID, selection.Cleared = imageID.Int64, variantID.Int64, !imageID.Valid
	return &selection, nil
}

// ClearCoverSelection 删除歌集的全部封面文件记录，并把采用关系标记为已清除（版本加一）。
// 返回被删除的记录，由调用方在提交后删除磁盘文件。
func (s *Store) ClearCoverSelection(releaseID, expectedRevision int64) (*CoverSelection, []CoverFile, error) {
	selection := &CoverSelection{ReleaseID: releaseID, Revision: expectedRevision + 1, Cleared: true, AdoptedAt: time.Now().UTC()}
	var removed []CoverFile
	err := s.withTx(func(tx *sql.Tx) error {
		// 先写以取得 SQLite 写锁，再检查版本（同 AdoptCoverFile）。
		result, err := tx.Exec(`UPDATE release SET updated_at=updated_at WHERE id=?`, releaseID)
		if err != nil {
			return err
		}
		if n, _ := result.RowsAffected(); n == 0 {
			return fmt.Errorf("歌集不存在")
		}
		var revision int64
		err = tx.QueryRow(`SELECT revision FROM release_cover_selection WHERE release_id=?`, releaseID).Scan(&revision)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if revision != expectedRevision {
			return ErrCoverSelectionChanged
		}
		rows, err := tx.Query(`SELECT `+coverFileColumns+` FROM cover_file WHERE release_id=?`, releaseID)
		if err != nil {
			return err
		}
		for rows.Next() {
			file, err := scanCoverFile(rows)
			if err != nil {
				rows.Close()
				return err
			}
			removed = append(removed, *file)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM cover_file WHERE release_id=?`, releaseID); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO release_cover_selection(release_id,image_id,variant_id,revision,adopted_at) VALUES(?,NULL,NULL,?,?)
			ON CONFLICT(release_id) DO UPDATE SET image_id=NULL,variant_id=NULL,revision=excluded.revision,adopted_at=excluded.adopted_at`,
			releaseID, selection.Revision, selection.AdoptedAt)
		if err != nil {
			return err
		}
		// 封面属于共享发行字段，改变它必须令发行保存计划失效。
		_, err = tx.Exec(`UPDATE release SET revision=revision+1,updated_at=? WHERE id=?`,
			selection.AdoptedAt, releaseID)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	return selection, removed, nil
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
