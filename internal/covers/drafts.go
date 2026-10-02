package covers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/norhua/sonexa/internal/store"
)

// ArtworkDraft 是编辑暂存凭据。来源身份及文件路径只由服务保存，客户端不能改写。
type ArtworkDraft struct {
	Token  string `json:"token"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
}
type stagedArtwork struct {
	relative string
	info     ImageInfo
	identity FileIdentity
}

// StageArtwork 校验并暂存本地图片（variantID=0）或已读取的上游规格，尚不创建采用记录。
func (l *LibraryImages) StageArtwork(data []byte, variantID int64) (ArtworkDraft, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	info, err := InspectImage(data)
	if err != nil {
		return ArtworkDraft{}, err
	}
	identity := FileIdentity{Source: "local", ImageID: info.Hash, Type: "Front", Original: true, VariantID: variantID}
	if variantID != 0 {
		variant, err := l.store.GetCoverVariant(variantID)
		if err != nil {
			return ArtworkDraft{}, err
		}
		if variant == nil {
			return ArtworkDraft{}, fmt.Errorf("封面规格不存在")
		}
		image, err := l.store.GetCoverImage(variant.ImageID)
		if err != nil {
			return ArtworkDraft{}, err
		}
		if image == nil {
			return ArtworkDraft{}, fmt.Errorf("封面图片不存在")
		}
		key, err := l.store.CoverCatalogKeyForImage(image.ID)
		if err != nil {
			return ArtworkDraft{}, err
		}
		identity.Source, identity.ObjectRef, identity.ImageID, identity.Type, identity.Original = key.Source, key.ObjectType+":"+key.SourceRef, image.SourceImageID, image.PrimaryType, variant.Spec == "original"
	}
	var tokenBytes [24]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return ArtworkDraft{}, err
	}
	token := hex.EncodeToString(tokenBytes[:])
	relative := ".edit-drafts/" + token + info.Extension()
	if err := copyExact(l.files, relative, data); err != nil {
		return ArtworkDraft{}, err
	}
	l.drafts[token] = stagedArtwork{relative: relative, info: info, identity: identity}
	return ArtworkDraft{Token: token, Width: info.Width, Height: info.Height, Format: info.Format}, nil
}

func (l *LibraryImages) DiscardArtwork(token string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	draft, ok := l.drafts[token]
	if !ok {
		return nil
	}
	if err := l.files.Remove(draft.relative); err != nil && !os.IsNotExist(err) {
		return err
	}
	delete(l.drafts, token)
	return nil
}

// WithPreparedArtwork 完成不可变落盘后调用事务提交函数；整个期间阻止清缓存及切换目录。
// 提交失败保留草稿供重试，旧采用文件始终保持可读。
func (l *LibraryImages) WithPreparedArtwork(token string, releaseID int64, commit func(store.CoverFile) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	draft, ok := l.drafts[token]
	if !ok {
		return fmt.Errorf("封面草稿已失效，请重新选择")
	}
	if releaseID <= 0 {
		return fmt.Errorf("没有所属发行，无法保存封面")
	}
	data, err := l.files.Read(draft.relative)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != draft.info.Hash {
		return fmt.Errorf("封面草稿校验失败")
	}
	identity := draft.identity
	identity.ReleaseID = releaseID
	release, err := l.store.GetRelease(releaseID)
	if err != nil {
		return err
	}
	if release != nil {
		identity.ReleaseTitle = release.Title
	}
	saved, err := l.files.Save(identity, data)
	if err != nil {
		return err
	}
	file := store.CoverFile{ReleaseID: &releaseID, VariantID: identity.VariantID, RelativePath: saved.RelativePath, ContentHash: saved.Info.Hash, ByteSize: saved.Info.Bytes, Width: saved.Info.Width, Height: saved.Info.Height, Format: saved.Info.Format}
	if err := commit(file); err != nil {
		if saved.Created {
			_ = l.files.Remove(saved.RelativePath)
		}
		return err
	}
	_ = l.files.Remove(draft.relative)
	delete(l.drafts, token)
	return nil
}

func (l *LibraryImages) discardAllArtworkLocked() error {
	l.drafts = map[string]stagedArtwork{}
	return l.files.root.RemoveAll(filepath.FromSlash(".edit-drafts"))
}

// StageReleaseArtwork copies the currently adopted immutable image into an edit draft.
// The caller retains the source revision and rechecks it when committing the edit.
func (l *LibraryImages) StageReleaseArtwork(releaseID int64) (*ArtworkDraft, bool, error) {
	selection, err := l.store.GetCoverSelection(releaseID)
	if err != nil {
		return nil, false, err
	}
	if selection == nil {
		return nil, false, nil
	}
	if selection.Cleared {
		return nil, true, nil
	}
	files, err := l.store.ListReleaseCoverFiles(releaseID)
	if err != nil {
		return nil, false, err
	}
	for _, file := range files {
		if file.VariantID != selection.VariantID || !file.Available {
			continue
		}
		data, err := l.Read(file)
		if err != nil {
			return nil, false, err
		}
		draft, err := l.StageArtwork(data, file.VariantID)
		return &draft, false, err
	}
	return nil, false, fmt.Errorf("当前采用封面文件不可读")
}
