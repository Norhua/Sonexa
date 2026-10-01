package covers

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/norhua/sonexa/internal/store"
)

// LibraryImages 串行化文件落盘和引用提交。调用方必须在网络/文件读取完成后
// 交付字节；持锁期间不出网，不删除任何尚被引用的用户选择。
type LibraryImages struct {
	mu          sync.Mutex
	files       *AlbumFiles
	store       *store.Store
	maxBytes    int64
	ttl         time.Duration
	catalogKeys func(*store.Release) []store.CoverCatalogKey
	drafts      map[string]stagedArtwork
}

func NewLibraryImages(st *store.Store, dir string) (*LibraryImages, error) {
	files, err := NewAlbumFiles(dir)
	if err != nil {
		return nil, err
	}
	library := &LibraryImages{files: files, store: st}
	if err := library.discardAllArtworkLocked(); err != nil {
		files.Close()
		return nil, err
	}
	return library, nil
}

func (l *LibraryImages) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	_ = l.discardAllArtworkLocked()
	return l.files.Close()
}

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
	title := ""
	if releaseID > 0 {
		release, err := l.store.GetRelease(releaseID)
		if err != nil {
			return nil, nil, err
		}
		if release != nil {
			title = release.Title
		}
	}
	saved, err := l.files.Save(FileIdentity{ReleaseID: releaseID, ReleaseTitle: title, Source: key.Source, ObjectRef: key.ObjectType + ":" + key.SourceRef,
		ImageID: image.SourceImageID, Type: image.PrimaryType, Original: variant.Spec == "original", VariantID: variant.ID}, data)
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
	if !adopt {
		persisted, readErr := l.store.GetCoverFile(file.ID)
		if readErr != nil {
			return nil, nil, readErr
		}
		file = *persisted
	}
	if err := l.pruneLocked(false); err != nil {
		return nil, selection, err
	}
	return &file, selection, nil
}

func (l *LibraryImages) Read(file store.CoverFile) ([]byte, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !file.Protected && l.ttl > 0 && time.Since(file.FetchedAt) > l.ttl {
		if err := l.expireLocked(file); err != nil {
			return nil, err
		}
		return nil, os.ErrNotExist
	}
	data, err := l.files.Read(file.RelativePath)
	if errors.Is(err, os.ErrNotExist) {
		_ = l.store.MarkCoverFileMissing(file.ID)
	}
	if err == nil {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != file.ContentHash {
			return nil, fmt.Errorf("封面文件校验失败")
		}
		err = l.store.TouchCoverFile(file.ID)
	}
	return data, err
}

// ReuseReleasePreviews 在已入库歌集需要图片时惰性复制同身份预览，不发网络请求。
// 无需侵入导入事务；调用它不会创建歌集或改变用户采用关系。
func (l *LibraryImages) ReuseReleasePreviews(releaseID int64) error {
	release, err := l.store.GetRelease(releaseID)
	if err != nil {
		return err
	}
	if release == nil {
		return nil
	}
	l.mu.Lock()
	resolver := l.catalogKeys
	l.mu.Unlock()
	if resolver == nil {
		return nil
	}
	keys := resolver(release)
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
		previews, err := l.store.AutoPreviewFiles(catalog.ID)
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

// ClearRelease 清除歌集封面：删除该歌集目录下的全部已保存文件并记录「已清除」。
// 数据库先提交，文件删除失败只留下无引用的孤儿文件，不会让记录指向缺失文件。
func (l *LibraryImages) ClearRelease(releaseID, revision int64) (*store.CoverSelection, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	selection, removed, err := l.store.ClearCoverSelection(releaseID, revision)
	if err != nil {
		return nil, err
	}
	dirs := map[string]bool{}
	var failure error
	for _, file := range removed {
		if err := l.files.Remove(file.RelativePath); err != nil && !errors.Is(err, os.ErrNotExist) {
			failure = err
		}
		dirs[path.Dir(file.RelativePath)] = true
	}
	// 目录非空（如有未登记文件）时删除会失败，保留即可。
	for dir := range dirs {
		if strings.HasPrefix(dir, "albums/") {
			_ = l.files.Remove(dir)
		}
	}
	if failure != nil {
		return selection, fmt.Errorf("封面已清除，但部分文件删除失败: %w", failure)
	}
	return selection, nil
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
