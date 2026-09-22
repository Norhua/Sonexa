package covers

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// Candidate 是详情抽屉手动重新获取时的一条封面候选。
//
// 同一结果返回的多个规格被收拢为一条并标注可选规格，
// 而不是列成 4 条重复项。
type Candidate struct {
	Provider  provider.ProviderID `json:"provider"`
	SourceRef string              `json:"sourceRef"`
	Title     *string             `json:"title"`
	Artist    *string             `json:"artist"`
	// Variants 是该结果的可选规格，按从高到低排列。
	Variants []Variant `json:"variants"`
	// Constructed 为真表示规格靠构造地址得到，选中时需先验证可用性。
	Constructed bool    `json:"constructed"`
	Confidence  float64 `json:"confidence"`
}

// Variant 是一档可选规格。
type Variant struct {
	Type   string `json:"type"` // Front | Back | Booklet | ...
	URL    string `json:"url"`
	Label  string `json:"label"`
	Width  *int   `json:"width"`
	Height *int   `json:"height"`
}

// SearchAllProviders 向所有上游查询封面，结果按置信度排序。
//
// 详情抽屉的手动重新获取用曲名/艺术家/专辑名向**全部**上游查询——
// 某首来自 MusicBrainz 的曲目没有封面时，iTunes 的结果也应出现在候选中。
func SearchAllProviders(
	ctx context.Context,
	registry *provider.Registry,
	q provider.Query,
) ([]Candidate, error) {
	providers := registry.List()
	type result struct {
		groups []provider.CoverGroup
		err    error
	}
	results := make([]result, len(providers))

	// 各上游并发查询，慢的不阻塞快的
	done := make(chan int, len(providers))
	for i, p := range providers {
		go func(idx int, prov provider.Provider) {
			if err := prov.IsConfigured(); err != nil {
				results[idx] = result{err: err}
				done <- idx
				return
			}
			groups, err := prov.SearchCovers(ctx, q)
			results[idx] = result{groups: groups, err: err}
			done <- idx
		}(i, p)
	}
	for range providers {
		<-done
	}

	var out []Candidate
	for _, r := range results {
		// 单个上游失败不影响其余上游的结果
		if r.err != nil {
			continue
		}
		for _, g := range r.groups {
			out = append(out, collapseVariants(g))
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	return out, nil
}

// collapseVariants 把同一结果的多个规格收拢为一条候选。
func collapseVariants(g provider.CoverGroup) Candidate {
	c := Candidate{
		Provider:    g.Provider,
		SourceRef:   g.SourceRef,
		Title:       g.Title,
		Artist:      g.Artist,
		Constructed: g.Constructed,
		Confidence:  g.Confidence,
	}
	seen := map[string]bool{}
	for _, img := range g.Images {
		if seen[img.URL] {
			continue
		}
		seen[img.URL] = true
		c.Variants = append(c.Variants, Variant{
			Type:   img.Type,
			URL:    img.URL,
			Label:  variantLabel(img),
			Width:  img.Width,
			Height: img.Height,
		})
	}
	return c
}

// variantLabel 由地址推断规格标签，如 front-1200 → 1200px。
func variantLabel(img provider.CoverImage) string {
	if img.Width != nil && img.Height != nil {
		return fmt.Sprintf("%d×%d", *img.Width, *img.Height)
	}
	base := filepath.Base(img.URL)
	for _, marker := range []string{"3000", "1200", "600", "500", "250", "100"} {
		if strings.Contains(base, marker) {
			return marker + "px"
		}
	}
	return img.Type
}

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
