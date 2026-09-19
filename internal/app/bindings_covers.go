package app

import (
	"context"
	"fmt"

	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

// coverLadder 按结果来源生成封面的尺寸降级链。
//
// 自动获取时来源跟随结果来源，不跨源取图：
//
//	来自 MusicBrainz → Cover Art Archive
//	来自 iTunes      → iTunes 图片地址（尺寸靠构造）
func (a *App) coverLadder(providerID, releaseRef, artworkURL string, releaseGroupRef *string) []string {
	switch provider.ProviderID(providerID) {
	case provider.MusicBrainz:
		rgRef := ""
		if releaseGroupRef != nil {
			rgRef = *releaseGroupRef
		}
		images := musicbrainz.CAAFrontLadder(releaseRef, rgRef)
		// 列表缩略图直接用 front-250，放在链首
		urls := []string{musicbrainz.CAAThumbnailURL(releaseRef)}
		for _, img := range images {
			urls = append(urls, img.URL)
		}
		urls = append(urls, musicbrainz.CAAIndexFallbackURLs(releaseRef, rgRef)...)
		return urls

	case provider.ITunes:
		// iTunes 只返回一个 artworkUrl100，大图靠构造地址（未文档化行为），
		// 因此需要基准地址才能生成降级链——它来自 search 响应。
		//
		// 四档从高到低，选中时先 HEAD 验证再采用，不可用自动退档。
		if artworkURL == "" {
			return nil
		}
		images := itunes.ArtworkLadder(artworkURL)
		urls := make([]string, 0, len(images))
		for _, img := range images {
			urls = append(urls, img.URL)
		}
		return urls
	}
	return nil
}

// CoverCandidate 是详情抽屉手动重新获取时的一条候选。
type CoverCandidate struct {
	Provider    string           `json:"provider"`
	SourceRef   string           `json:"sourceRef"`
	Title       *string          `json:"title"`
	Artist      *string          `json:"artist"`
	Variants    []covers.Variant `json:"variants"`
	Constructed bool             `json:"constructed"`
	Confidence  float64          `json:"confidence"`
}

// SearchCovers 向所有上游查询封面，供详情抽屉的手动重新获取使用。
//
// 用曲名/艺术家/专辑名向全部上游查询，结果按置信度排序；
// 同一结果返回的多个规格收拢为一条并标注可选规格。
func (a *App) SearchCovers(title, artist, album string) ([]CoverCandidate, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return nil, fmt.Errorf("%s", gate.Reason)
	}

	found, err := covers.SearchAllProviders(context.Background(), a.registry,
		provider.Query{Title: title, Artist: artist, Album: album, Limit: 10})
	if err != nil {
		return nil, err
	}

	out := make([]CoverCandidate, 0, len(found))
	for _, c := range found {
		out = append(out, CoverCandidate{
			Provider: string(c.Provider), SourceRef: c.SourceRef,
			Title: c.Title, Artist: c.Artist, Variants: c.Variants,
			Constructed: c.Constructed, Confidence: c.Confidence,
		})
	}
	return out, nil
}

// AdoptCover 确认采用某张封面。
//
// 按内容哈希落盘并记录来源、原始地址、尺寸与获取时间。
// 构造型来源（iTunes）的档位不可得时自动退到下一档。
func (a *App) AdoptCover(trackID int64, providerID, url string, fallbackURLs []string) error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.coverSvc == nil {
		return fmt.Errorf("封面服务未就绪")
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return fmt.Errorf("%s", gate.Reason)
	}

	ladder := append([]string{url}, fallbackURLs...)
	adopted, err := a.coverSvc.AdoptFromLadder(context.Background(),
		provider.ProviderID(providerID), ladder)
	if err != nil {
		return fmt.Errorf("采用封面失败: %w", err)
	}

	if err := a.store.SetCover(trackID, store.CoverRecord{
		Path:      adopted.Path,
		Source:    adopted.Source,
		SourceURL: adopted.SourceURL,
		Width:     adopted.Width,
		Height:    adopted.Height,
		FetchedAt: adopted.FetchedAt,
	}); err != nil {
		return err
	}
	a.notifyLibraryChanged()
	return nil
}

// ITunesArtworkLadder 由 artworkUrl100 构造四档尺寸。
//
// 大图尺寸靠构造地址属未文档化行为，因此只提供有限档位，
// 且选中时先验证可用性，不可用自动退档。
func (a *App) ITunesArtworkLadder(artworkURL string) []string {
	images := itunes.ArtworkLadder(artworkURL)
	out := make([]string, 0, len(images))
	for _, img := range images {
		out = append(out, img.URL)
	}
	return out
}

// coverThumbSize 是封面统一使用的尺寸。
//
// 缓存键包含尺寸，列表与详情因此必须用同一个值：换一个尺寸就是另一个键，
// 等于为同一张图再出网取一次。呈现大小交给 <img> 的显示尺寸。
const coverThumbSize = covers.DefaultThumbSize

// releaseRefOf 安全地取出行上的发行标识；缺失时返回空串。
func releaseRefOf(r CandidateRow) string {
	if r.ReleaseRef == nil {
		return ""
	}
	return *r.ReleaseRef
}

// CoverSource 是一张封面的来源标识，与 /cover 地址里的参数一一对应。
type CoverSource struct {
	Provider string `json:"provider"`
	Ref      string `json:"ref"`
	Size     int    `json:"size"`
}

// CoverOutcomes 返回若干封面最近一次取图的结果。
//
// 界面要区分「上游没有这张封面」（显示 nil）与「取图出错」（显示失败图标），
// 而 <img> 的 error 事件读不到状态码——两种失败在前端看起来一模一样。
// 因此由后端把结果记下来，前端只在 <img> 失败时来问一次，且整批一起问。
func (a *App) CoverOutcomes(sources []CoverSource) []string {
	a.mu.RLock()
	svc := a.coverSvc
	a.mu.RUnlock()

	out := make([]string, len(sources))
	if svc == nil {
		for i := range out {
			out[i] = string(covers.OutcomeError)
		}
		return out
	}

	keys := make([]string, len(sources))
	for i, src := range sources {
		size := src.Size
		if size <= 0 {
			size = coverThumbSize
		}
		keys[i] = covers.CacheKey(src.Provider+":"+src.Ref, size)
	}
	for i, o := range svc.Outcomes(keys) {
		// 没记录过就当作出错：这个查询只在 <img> 已经失败之后才发起，
		// 「失败了但查不到成因」最接近的呈现是失败，而不是「上游没有」。
		if o == covers.OutcomeUnknown {
			o = covers.OutcomeError
		}
		out[i] = string(o)
	}
	return out
}
