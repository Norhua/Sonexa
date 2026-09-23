package covers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// AlbumFiles 以 os.Root 限制文件操作范围，包括符号链接竞态。
// 所有图片名称由可信元数据生成，不接受用户提供的目录或远程 URL 作为文件名。
type AlbumFiles struct {
	mu   sync.Mutex
	root *os.Root
}

type FileIdentity struct {
	ReleaseID int64
	Source    string
	ObjectRef string
	ImageID   string
	Type      string
	Original  bool
	VariantID int64
}

type SavedImage struct {
	RelativePath string
	Info         ImageInfo
	Created      bool
}

func NewAlbumFiles(dir string) (*AlbumFiles, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &AlbumFiles{root: root}, nil
}

func (f *AlbumFiles) Close() error { return f.root.Close() }

func coverFileStem(identity FileIdentity, info ImageInfo) (string, error) {
	switch identity.Source {
	case "caa", "itunes", "qq", "163", "local", "unknown":
	default:
		return "", fmt.Errorf("未知封面来源")
	}
	if identity.ImageID == "" || identity.ReleaseID < 0 {
		return "", fmt.Errorf("封面图片身份无效")
	}
	kind := strings.TrimSpace(identity.Type)
	if kind == "" {
		kind = "Other"
	}
	for _, c := range kind {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == ' ') {
			return "", fmt.Errorf("封面类型包含非法字符")
		}
	}
	kind = strings.ReplaceAll(kind, " ", "_")
	spec := fmt.Sprintf("%d", info.Width)
	if info.Width != info.Height || identity.Original {
		spec = fmt.Sprintf("%dx%d", info.Width, info.Height)
	}
	if identity.Original {
		spec = "original-" + spec
	}
	return kind + "-" + identity.Source + "-" + spec, nil
}

func (f *AlbumFiles) Save(identity FileIdentity, data []byte) (*SavedImage, error) {
	info, err := InspectImage(data)
	if err != nil {
		return nil, err
	}
	stem, err := coverFileStem(identity, info)
	if err != nil {
		return nil, err
	}
	if identity.VariantID > 0 {
		stem += fmt.Sprintf("-v%d", identity.VariantID)
	}
	dir := fmt.Sprintf("albums/release-%d", identity.ReleaseID)
	if identity.ReleaseID == 0 {
		if identity.ObjectRef == "" {
			return nil, fmt.Errorf("预览图片缺少来源对象身份")
		}
		digest := sha256.Sum256([]byte(identity.ObjectRef))
		dir = "cache/sources/" + identity.Source + "-" + hex.EncodeToString(digest[:16])
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.root.MkdirAll(filepath.FromSlash(dir), 0o755); err != nil {
		return nil, err
	}
	relative := path.Join(dir, stem+info.Extension())
	if existing, err := f.Read(relative); err == nil {
		sum := sha256.Sum256(existing)
		if hex.EncodeToString(sum[:]) == info.Hash {
			return &SavedImage{RelativePath: relative, Info: info}, nil
		}
		// 同身份的内容更新也不得覆盖已经被采用的旧文件。
		digest := sha256.Sum256([]byte(identity.ImageID + "|" + info.Hash))
		relative = path.Join(dir, stem+"-"+hex.EncodeToString(digest[:8])+info.Extension())
		if same, err := f.Read(relative); err == nil {
			sum := sha256.Sum256(same)
			if hex.EncodeToString(sum[:]) != info.Hash {
				return nil, fmt.Errorf("封面文件名发生校验冲突")
			}
			return &SavedImage{RelativePath: relative, Info: info}, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	temporary := path.Join(dir, ".pending-"+hex.EncodeToString(random[:]))
	file, err := f.root.OpenFile(filepath.FromSlash(temporary), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	defer f.root.Remove(filepath.FromSlash(temporary))
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	written, err := f.Read(temporary)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(written)
	if hex.EncodeToString(sum[:]) != info.Hash {
		return nil, fmt.Errorf("封面落盘校验失败")
	}
	if err := f.root.Rename(filepath.FromSlash(temporary), filepath.FromSlash(relative)); err != nil {
		return nil, err
	}
	return &SavedImage{RelativePath: relative, Info: info, Created: true}, nil
}

func (f *AlbumFiles) Read(relative string) ([]byte, error) {
	if !safeRelativePath(relative) {
		return nil, fmt.Errorf("封面路径超出受控目录")
	}
	file, err := f.root.Open(filepath.FromSlash(relative))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("封面必须是普通文件")
	}
	return ReadImageBytes(file)
}

func (f *AlbumFiles) Remove(relative string) error {
	if !safeRelativePath(relative) {
		return fmt.Errorf("封面路径超出受控目录")
	}
	return f.root.Remove(filepath.FromSlash(relative))
}

func safeRelativePath(relative string) bool {
	return relative != "" && relative != "." && path.Clean(relative) == relative &&
		!path.IsAbs(relative) && !strings.HasPrefix(relative, "../") && !strings.ContainsAny(relative, "\\:\x00")
}
