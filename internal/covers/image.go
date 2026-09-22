package covers

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"

	_ "golang.org/x/image/webp"
)

const MaxImageBytes int64 = 32 << 20
const MaxImagePixels int64 = 64_000_000

type ImageInfo struct {
	Width  int
	Height int
	Format string
	Hash   string
	Bytes  int64
}

func (i ImageInfo) Extension() string {
	if i.Format == "jpeg" {
		return ".jpg"
	}
	return "." + i.Format
}

func ReadImageBytes(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxImageBytes {
		return nil, fmt.Errorf("封面图片超过 32 MiB 上限")
	}
	return data, nil
}

// InspectImage 先检查像素预算再完整解码，防止只凭头部接受截断文件或解码炸弹。
func InspectImage(data []byte) (ImageInfo, error) {
	if len(data) == 0 || int64(len(data)) > MaxImageBytes {
		return ImageInfo{}, fmt.Errorf("封面图片为空或超过 32 MiB 上限")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "jpeg" && format != "png" && format != "webp") {
		return ImageInfo{}, fmt.Errorf("封面必须是有效的 JPEG、PNG 或静态 WebP 图片")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width) > MaxImagePixels/int64(cfg.Height) {
		return ImageInfo{}, fmt.Errorf("封面图片超过 6400 万像素上限")
	}
	if err := rejectAnimation(data, format); err != nil {
		return ImageInfo{}, err
	}
	decoded, decodedFormat, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return ImageInfo{}, fmt.Errorf("封面图片损坏或不完整: %w", err)
	}
	if decodedFormat != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return ImageInfo{}, fmt.Errorf("封面图片尺寸不一致")
	}
	sum := sha256.Sum256(data)
	return ImageInfo{Width: cfg.Width, Height: cfg.Height, Format: format, Hash: hex.EncodeToString(sum[:]), Bytes: int64(len(data))}, nil
}

func rejectAnimation(data []byte, format string) error {
	if format == "png" {
		for offset := 8; offset+12 <= len(data); {
			n := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
			if n+12 > uint64(len(data)-offset) {
				return fmt.Errorf("PNG 数据块不完整")
			}
			if string(data[offset+4:offset+8]) == "acTL" {
				return fmt.Errorf("暂不支持动画封面")
			}
			offset += int(n) + 12
		}
	}
	if format == "webp" {
		for offset := 12; offset+8 <= len(data); {
			n := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
			if n+8 > uint64(len(data)-offset) {
				return fmt.Errorf("WebP 数据块不完整")
			}
			if string(data[offset:offset+4]) == "ANIM" || string(data[offset:offset+4]) == "ANMF" ||
				(string(data[offset:offset+4]) == "VP8X" && n > 0 && data[offset+8]&2 != 0) {
				return fmt.Errorf("暂不支持动画封面")
			}
			offset += int(n+n%2) + 8
		}
	}
	return nil
}
