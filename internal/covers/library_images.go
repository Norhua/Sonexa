package covers

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/norhua/sonexa/internal/store"
)

// LibraryImages 串行化文件落盘和引用提交。调用方必须在网络/文件读取完成后
// 交付字节；持锁期间不出网，不删除任何尚被引用的用户选择。
type LibraryImages struct {
	mu    sync.Mutex
	files *AlbumFiles
	store *store.Store
}

func NewLibraryImages(st *store.Store, dir string) (*LibraryImages, error) {
	files, err := NewAlbumFiles(dir)
	if err != nil {
		return nil, err
	}
	return &LibraryImages{files: files, store: st}, nil
}

func (l *LibraryImages) Close() error { return l.files.Close() }

func (l *LibraryImages) SaveVariant(releaseID int64, variantID int64, data []byte, adopt bool, revision int64) (*store.CoverFile, *store.CoverSelection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	variant, err := l.store.GetCoverVariant(variantID)
	if err != nil || variant == nil {
		return nil, nil, fmt.Errorf("封面规格不存在: %w", err)
	}
	image, err := l.store.GetCoverImage(variant.ImageID)
	if err != nil || image == nil {
		return nil, nil, fmt.Errorf("封面图片不存在: %w", err)
	}
	key, err := l.store.CoverCatalogKeyForImage(image.ID)
	if err != nil {
		return nil, nil, err
	}
	if adopt && releaseID <= 0 {
		return nil, nil, fmt.Errorf("没有所属歌集，无法采用封面")
	}
	saved, err := l.files.Save(FileIdentity{ReleaseID: releaseID, Source: key.Source, ObjectRef: key.ObjectType + ":" + key.SourceRef,
		ImageID: image.SourceImageID, Type: image.PrimaryType, Original: variant.Spec == "original"}, data)
	if err != nil {
		return nil, nil, err
	}
	file := store.CoverFile{VariantID: variantID, RelativePath: saved.RelativePath, ContentHash: saved.Info.Hash, ByteSize: saved.Info.Bytes,
		Width: saved.Info.Width, Height: saved.Info.Height, Format: saved.Info.Format}
	if releaseID > 0 {
		file.ReleaseID = &releaseID
	}
	var selection *store.CoverSelection
	if adopt {
		selection, err = l.store.AdoptCoverFile(file, revision)
	} else {
		file.ID, err = l.store.SaveCoverFile(file)
	}
	if err != nil {
		// 本次新建的路径尚未提交引用；重复采用复用文件时绝不能删除它。
		if saved.Created {
			_ = l.files.Remove(saved.RelativePath)
		}
		return nil, nil, err
	}
	if adopt {
		files, err := l.store.ListReleaseCoverFiles(releaseID)
		if err != nil {
			return nil, selection, err
		}
		for _, candidate := range files {
			if candidate.VariantID == variantID {
				file = candidate
				break
			}
		}
	}
	return &file, selection, nil
}

func (l *LibraryImages) Read(file store.CoverFile) ([]byte, error) {
	data, err := l.files.Read(file.RelativePath)
	if errors.Is(err, os.ErrNotExist) {
		_ = l.store.MarkCoverFileMissing(file.ID)
	}
	return data, err
}

// ReuseReleasePreviews 在已入库歌集需要图片时惰性复制同身份预览，不发网络请求。
// 无需侵入导入事务；调用它不会创建歌集或改变用户采用关系。
func (l *LibraryImages) ReuseReleasePreviews(releaseID int64) error {
	keys, err := l.store.CatalogsForRelease(releaseID)
	if err != nil {
		return err
	}
	existing, err := l.store.ListReleaseCoverFiles(releaseID)
	if err != nil {
		return err
	}
	have := map[int64]bool{}
	for _, file := range existing {
		if file.Available {
			have[file.VariantID] = true
		}
	}
	for _, key := range keys {
		catalog, err := l.store.GetCoverCatalog(key)
		if err != nil {
			return err
		}
		if catalog == nil {
			continue
		}
		previews, err := l.store.ListCatalogPreviewFiles(catalog.ID)
		if err != nil {
			return err
		}
		for _, preview := range previews {
			if !preview.Available || have[preview.VariantID] {
				continue
			}
			data, err := l.Read(preview)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return err
			}
			if _, _, err := l.SaveVariant(releaseID, preview.VariantID, data, false, 0); err != nil {
				return err
			}
			have[preview.VariantID] = true
		}
	}
	return nil
}

// ImportLocal 只处理用户选择器交付的字节，绝不让客户端指定网络 URL。
func (l *LibraryImages) ImportLocal(releaseID int64, data []byte, revision int64) (*store.CoverSelection, error) {
	info, err := InspectImage(data)
	if err != nil {
		return nil, err
	}
	key := store.CoverCatalogKey{Source: "local", ObjectType: "image", SourceRef: info.Hash}
	if _, err := l.store.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: info.Hash, PrimaryType: "Front", Types: []string{"Front"}, Front: true,
		Variants: []store.CoverVariant{{Spec: "original"}}}}); err != nil {
		return nil, err
	}
	catalog, err := l.store.GetCoverCatalog(key)
	if err != nil {
		return nil, err
	}
	_, selection, err := l.SaveVariant(releaseID, catalog.Images[0].Variants[0].ID, data, true, revision)
	return selection, err
}

// MigrateLegacy 可重复运行；先落盘并采用再标 complete。中断后已有采用关系
// 使重试只完成标记，不再覆盖用户之后选的新图。旧文件始终保留以便回滚。
func (l *LibraryImages) MigrateLegacy(rootDir string) []error {
	pending, err := l.store.PendingLegacyCovers()
	if err != nil {
		return []error{err}
	}
	var failures []error
	for _, item := range pending {
		err := l.migrateLegacyOne(rootDir, item)
		if err != nil {
			_ = l.store.SetLegacyCoverMigration(item.ReleaseID, false, err.Error())
			failures = append(failures, fmt.Errorf("歌集 %d 封面迁移: %w", item.ReleaseID, err))
		}
	}
	return failures
}

func (l *LibraryImages) migrateLegacyOne(rootDir string, legacy store.LegacyCover) error {
	selected, err := l.store.GetCoverSelection(legacy.ReleaseID)
	if err != nil {
		return err
	}
	if selected != nil {
		return l.store.SetLegacyCoverMigration(legacy.ReleaseID, true, "")
	}
	relative, err := filepath.Rel(rootDir, legacy.Path)
	if err != nil {
		return err
	}
	data, err := l.files.Read(filepath.ToSlash(relative))
	if err != nil {
		return err
	}
	info, err := InspectImage(data)
	if err != nil {
		return err
	}
	key := store.CoverCatalogKey{Source: legacyCoverSource(legacy.SourceURL), ObjectType: "legacy-image", SourceRef: info.Hash}
	if _, err := l.store.SaveCoverCatalog(key, []store.CoverImage{{SourceImageID: info.Hash, PrimaryType: "Front", Types: []string{"Front"}, Front: true,
		OriginalURL: legacy.SourceURL, Variants: []store.CoverVariant{{Spec: "original", URL: legacy.SourceURL}}}}); err != nil {
		return err
	}
	catalog, err := l.store.GetCoverCatalog(key)
	if err != nil {
		return err
	}
	if _, _, err := l.SaveVariant(legacy.ReleaseID, catalog.Images[0].Variants[0].ID, data, true, 0); err != nil {
		return err
	}
	return l.store.SetLegacyCoverMigration(legacy.ReleaseID, true, "")
}

func legacyCoverSource(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "unknown"
	}
	host := strings.ToLower(u.Hostname())
	for _, source := range []struct{ domain, name string }{
		{"coverartarchive.org", "caa"}, {"archive.org", "caa"}, {"mzstatic.com", "itunes"}, {"y.qq.com", "qq"}, {"music.126.net", "163"},
	} {
		if host == source.domain || strings.HasSuffix(host, "."+source.domain) {
			return source.name
		}
	}
	return "unknown"
}
