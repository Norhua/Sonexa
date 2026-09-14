package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/itunes"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ProviderInfo 描述一个已注册的上游，供界面动态渲染。
//
// 所有「选择上游」的位置都从这里渲染，禁止硬编码 provider 名称或数量。
type ProviderInfo struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"displayName"`
	Capabilities []string `json:"capabilities"`
	// Configured 为假时相关入口禁用，Reason 给出可展示的原因。
	Configured bool   `json:"configured"`
	Reason     string `json:"reason"`
	// NeedsCountry 为真时该上游的界面需要地区下拉。
	NeedsCountry bool `json:"needsCountry"`
}

// ListProviders 返回全部已注册的上游。
func (a *App) ListProviders() ([]ProviderInfo, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	out := []ProviderInfo{}
	for _, p := range a.registry.List() {
		info := ProviderInfo{
			ID:           string(p.ID()),
			DisplayName:  p.DisplayName(),
			Capabilities: p.Capabilities().Names(),
			Configured:   true,
			NeedsCountry: p.ID() == provider.ITunes,
		}
		if err := p.IsConfigured(); err != nil {
			info.Configured = false
			info.Reason = err.Error()
		}
		out = append(out, info)
	}
	return out, nil
}

// OutboundGate 描述出网前置条件的状态。
//
// MusicBrainz 联系方式未填写时全局禁用出网——导入、上游搜索、封面获取。
// iTunes 虽不需要联系方式，但混着可用会让用户误以为 MB 也已配好，
// 因此同样禁用，且三处入口给出同一句原因说明。
type OutboundGate struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	// SettingsAnchor 供界面「跳转设置」使用。
	SettingsAnchor string `json:"settingsAnchor"`
}

// OutboundDisabledReason 是全部被禁用入口共用的文案。
const OutboundDisabledReason = "需要先在设置中填写 MusicBrainz 联系方式，才能使用导入、上游搜索与封面获取等出网功能。"

// CheckOutbound 返回出网前置条件的状态。
func (a *App) CheckOutbound() OutboundGate {
	if a.settings == nil {
		return OutboundGate{Allowed: false, Reason: OutboundDisabledReason, SettingsAnchor: "upstream"}
	}
	if strings.TrimSpace(a.settings.Get().MusicBrainz.Contact) == "" {
		return OutboundGate{Allowed: false, Reason: OutboundDisabledReason, SettingsAnchor: "upstream"}
	}
	return OutboundGate{Allowed: true}
}

// GetSettings 返回当前设置。
func (a *App) GetSettings() (config.Settings, error) {
	if a.settings == nil {
		return config.Defaults(), fmt.Errorf("设置尚未载入")
	}
	return a.settings.Get(), nil
}

// SaveSettings 保存设置并即时生效。
//
// 填写联系方式后出网入口立即可用，无需重启应用。
func (a *App) SaveSettings(next config.Settings) error {
	if err := a.ready(); err != nil {
		return err
	}
	if err := a.settings.Save(next); err != nil {
		return err
	}

	a.mu.Lock()
	// 上游配置改动后重建注册表与令牌桶
	a.rebuildProviders(next)
	a.rebuildRunner(next)
	// 封面并发数改动后就地调整工作池
	if a.coverSvc != nil {
		a.coverSvc.Pool().SetConcurrency(next.Cache.CoverConcurrency)
	}
	a.mu.Unlock()

	// 封面缓存目录改动后重建缓存与工作池——
	// 此前只在启动时建一次，改目录要重启才生效
	a.mu.RLock()
	oldDir := a.paths.CoverDir
	a.mu.RUnlock()
	newPaths := a.paths.WithCoverDir(next.Cache.CoverDir)
	if newPaths.CoverDir != oldDir {
		if err := newPaths.Ensure(); err != nil {
			return fmt.Errorf("创建新的封面缓存目录失败: %w", err)
		}
		svc, err := newCoverService(newPaths, next, a.version)
		if err != nil {
			return fmt.Errorf("重建封面缓存失败: %w", err)
		}
		a.mu.Lock()
		a.paths = newPaths
		a.coverSvc = a.attachLadderBuilder(svc)
		a.mu.Unlock()
	}

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, EventSettingsSaved, next)
	}
	return nil
}

// DefaultSettings 返回全部默认值，供设置页的「恢复默认」使用。
func (a *App) DefaultSettings() config.Settings { return config.Defaults() }

// CountryOption 是地区下拉的一项。
type CountryOption struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ListCountries 返回地区下拉的候选。
func (a *App) ListCountries() []CountryOption {
	codes := itunes.SupportedCountries()
	out := make([]CountryOption, 0, len(codes))
	for _, c := range codes {
		out = append(out, CountryOption{Code: c, Name: itunes.CountryDisplayName(c)})
	}
	return out
}

// CountryHint 是文字系统探测给出的建议。
type CountryHint struct {
	Country  string `json:"country"`
	Message  string `json:"message"`
	Detected bool   `json:"detected"`
}

// SuggestCountry 依据输入文本的文字系统给出地区建议。
//
// 探测结果仅作为建议并在界面上明确提示；用户手动选择优先，不被反复覆盖。
func (a *App) SuggestCountry(text string) CountryHint {
	s := itunes.DetectCountry(text)
	return CountryHint{Country: s.Country, Message: s.Message(), Detected: s.Detected}
}

// AboutInfo 是「关于」页的内容。
type AboutInfo struct {
	Version string `json:"version"`
	// Attributions 是数据来源署名。
	Attributions []Attribution `json:"attributions"`
}

// Attribution 是一条数据来源署名。
type Attribution struct {
	Name string `json:"name"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

// About 返回版本号与数据来源署名。
func (a *App) About() AboutInfo {
	return AboutInfo{
		Version: a.version,
		Attributions: []Attribution{
			{
				Name: "MusicBrainz",
				Text: "曲目元数据来自 MusicBrainz，其数据以 CC0 公共领域贡献协议发布。" +
					"本应用按其要求在每个请求中携带规范的 User-Agent。",
				URL: "https://musicbrainz.org",
			},
			{
				Name: "Cover Art Archive",
				Text: "封面图片来自 Cover Art Archive。图片版权各异，本地缓存仅供自用，不随导出分发。",
				URL:  "https://coverartarchive.org",
			},
			{
				Name: "Apple / iTunes",
				Text: "部分元数据来自 iTunes Search API。依据 Apple 的条款，" +
					"这些数据仅为性能目的短期缓存，不作为长期事实来源留存；其图片同样不随导出分发。",
				URL: "https://www.apple.com/itunes/",
			},
		},
	}
}

// ClearCache 清除封面缓存与上游响应缓存。
//
// 曲库数据与已确认采用的封面不受影响。
func (a *App) ClearCache() error {
	if err := a.ready(); err != nil {
		return err
	}
	if a.coverSvc != nil {
		if err := a.coverSvc.Pool().Cache().Clear(); err != nil {
			return fmt.Errorf("清除封面缓存失败: %w", err)
		}
	}
	if err := a.store.ClearCache(); err != nil {
		return fmt.Errorf("清除上游缓存失败: %w", err)
	}
	return nil
}

// CompleteOnboarding 标记首次启动引导已完成。
func (a *App) CompleteOnboarding() error {
	if err := a.ready(); err != nil {
		return err
	}
	cur := a.settings.Get()
	cur.OnboardingDone = true
	return a.SaveSettings(cur)
}

// CheckReachable 探测上游是否可达。
//
// 用于离线态提示。探测本身走一次 HEAD，失败即认为处于离线；
// 但只在用户触发或应用启动时调用，不做后台轮询。
func (a *App) CheckReachable() bool {
	if err := a.ready(); err != nil {
		return true // 后端未就绪不是离线
	}
	if !a.CheckOutbound().Allowed {
		return true // 未配置时不出网，不算离线
	}
	base := "https://musicbrainz.org"
	if cur := a.settings.Get(); cur.MusicBrainz.ServerURL != "" {
		base = cur.MusicBrainz.ServerURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, base, nil)
	if err != nil {
		return false
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}
