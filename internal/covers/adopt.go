package covers

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// Adopted 是一张已采用封面的落盘记录。
type Adopted struct {
	Path      string
	Source    string
	SourceURL string
	Width     *int
	Height    *int
	FetchedAt time.Time
}

// Adopt 取得指定规格的图片、按内容哈希落盘并返回其记录。
//
// 用户在详情抽屉确认采用后调用；这些文件不受 TTL 与「清除缓存」影响。
// 对构造型来源（iTunes），选中的档位先经 HEAD 验证再采用，
// 不可用时由调用方退到下一档。
func (s *Service) Adopt(ctx context.Context, prov provider.ProviderID, url string) (*Adopted, error) {
	data, err := s.pool.fetchOne(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("取图失败: %w", err)
	}

	path, err := s.pool.cache.Adopt(data, extOf(url))
	if err != nil {
		return nil, err
	}

	width, height := imageDimensions(data)
	return &Adopted{
		Path:      path,
		Source:    string(prov),
		SourceURL: url,
		Width:     width,
		Height:    height,
		FetchedAt: time.Now().UTC(),
	}, nil
}

// AdoptFromLadder 依次尝试各档，采用第一个可用的。
// 供构造型来源的「选中时先验证、失败自动退档」使用。
func (s *Service) AdoptFromLadder(ctx context.Context, prov provider.ProviderID, urls []string) (*Adopted, error) {
	for _, url := range urls {
		got, err := s.Adopt(ctx, prov, url)
		if err == nil {
			return got, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, ErrNotFound
}

func extOf(url string) string {
	base := filepath.Base(url)
	if i := strings.IndexByte(base, '?'); i >= 0 {
		base = base[:i]
	}
	if ext := filepath.Ext(base); ext != "" {
		return ext
	}
	return ".jpg"
}

// imageDimensions 使用与导入相同的完整解码校验；无法判定时返回 nil。
func imageDimensions(data []byte) (*int, *int) {
	info, err := InspectImage(data)
	if err != nil {
		return nil, nil
	}
	return &info.Width, &info.Height
}
