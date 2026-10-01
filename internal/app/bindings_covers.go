package app

import (
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/provider"
)

func (a *App) checkCoverOutbound() error {
	if gate := a.CheckOutbound(); !gate.Allowed {
		return &covers.OutboundError{Reason: gate.Reason}
	}
	return nil
}

// coverLadder 按结果来源生成封面的尺寸降级链。
//
// 自动获取时来源跟随结果来源，不跨源取图：
//
//	来自 MusicBrainz → Cover Art Archive
//	来自 iTunes      → iTunes 图片地址（尺寸靠构造）
//	来自 QQ 音乐     → QQ 图片地址（由专辑 MID 构造）
func (a *App) coverLadder(providerID, releaseRef, artworkURL string, releaseGroupRef *string) []string {
	p, err := a.registry.Get(provider.ProviderID(providerID))
	if err != nil {
		return nil
	}
	source, ok := p.(provider.ArtworkStrategist)
	if !ok {
		return nil
	}
	rg := ""
	if releaseGroupRef != nil {
		rg = *releaseGroupRef
	}
	policy := source.ArtworkStrategy()
	if policy.LegacyLadder == nil {
		return nil
	}
	return policy.LegacyLadder(releaseRef, artworkURL, rg)
}

// coverThumbSize 仅供旧候选键兼容；实际选图使用 thumbnail/detail 用途。
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
