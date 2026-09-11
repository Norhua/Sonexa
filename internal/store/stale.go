package store

import "time"

// StaleConfig 是清单过期判定的配置。
type StaleConfig struct {
	// ITunesTTL 是 iTunes 来源清单的有效期，默认 30 天。
	ITunesTTL time.Duration
}

// DefaultStaleConfig 返回默认配置。
func DefaultStaleConfig() StaleConfig {
	return StaleConfig{ITunesTTL: 30 * 24 * time.Hour}
}

// IsStale 判定某发行的清单是否已过期。
//
// 这是**唯一**的过期判定入口：所有读取清单的路径都必须经过它，
// 而不是各处自己比 fetched_at。iTunes 的 TTL 合规要求很容易被忘记，
// 尤其 release 表里 MB 与 iTunes 数据混存时。
//
//	iTunes 来源：按 fetched_at 设 TTL，过期即视为清单状态为无（none）
//	MusicBrainz 来源：数据为 CC0，可长期留存，不因时间而作废
func (cfg StaleConfig) IsStale(r *Release) bool {
	if r == nil {
		return true
	}
	// MB 数据为 CC0，不受 TTL 约束
	if r.Provider != "itunes" {
		return false
	}
	if r.FetchedAt == nil {
		return true
	}
	ttl := cfg.ITunesTTL
	if ttl <= 0 {
		ttl = DefaultStaleConfig().ITunesTTL
	}
	return time.Since(*r.FetchedAt) > ttl
}

// EffectiveTracklistState 返回经过期判定后的清单状态。
//
// 过期的 iTunes 清单被视为 none，在下次打开该专辑时重新拉取，
// 而不是直接使用过期数据。
func (cfg StaleConfig) EffectiveTracklistState(r *Release) TracklistState {
	if r == nil {
		return TracklistNone
	}
	if cfg.IsStale(r) {
		return TracklistNone
	}
	return r.TracklistState
}
