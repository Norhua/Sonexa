// Package provider 定义上游接口、注册表与限流。
//
// 所有需要上游信息的地方都面对这里的同一套接口，使具体上游可增删替换。
// 本包（父包）不依赖任何具体 provider 实现，也不依赖 internal/matching——
// 子包 musicbrainz / itunes 可以依赖 matching（如文字系统探测），
// 但反向依赖会成环，必须避免。
package provider

import (
	"context"
	"time"
)

// ProviderID 是上游的稳定标识，如 "musicbrainz" / "itunes"。
type ProviderID string

// 已实现的上游标识。
const (
	MusicBrainz ProviderID = "musicbrainz"
	ITunes      ProviderID = "itunes"
)

// Provider 是所有上游必须实现的统一接口。
//
// 接口不做「最小公倍数」式的统一：MB 与 iTunes 的字段交集很小，
// 若只暴露交集，MB 的演唱者、作曲、厂牌、ISRC、载体、国家全部无处安放。
// 因此所有字段用可空类型，由 Capabilities() 声明各自能提供什么。
type Provider interface {
	// ID 返回稳定标识。
	ID() ProviderID
	// DisplayName 返回界面上显示的名称。
	DisplayName() string
	// Capabilities 声明本上游能提供哪些字段与能力。
	Capabilities() Capabilities
	// RateLimit 声明速率限制：每 per 时间内至多 n 次请求。
	RateLimit() (n int, per time.Duration)
	// IsConfigured 在前置条件未满足时返回可直接展示给用户的错误说明。
	IsConfigured() error
	// SearchTracks 按条件搜索，返回 track 粒度的候选。
	SearchTracks(ctx context.Context, q Query) ([]Candidate, error)
	// FetchDetail 拉取一条候选的完整详情（含该发行的完整曲目清单）。
	FetchDetail(ctx context.Context, ref Ref) (*Detail, error)
	// SearchCovers 按条件搜索封面，供详情抽屉的手动重新获取使用。
	SearchCovers(ctx context.Context, q Query) ([]CoverGroup, error)
}
