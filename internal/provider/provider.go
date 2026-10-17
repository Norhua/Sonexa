// Package provider 定义元数据契约、注册表与共享传输。
//
// 元数据业务消费 MetadataProvider；歌词与封面搜索的能力契约由各自消费侧定义。
// 本包不依赖业务服务、internal/matching 或具体上游；具体上游可以依赖
// 这些消费侧契约与纯函数，依赖方向不能反转。
package provider

import (
	"context"
	"time"

	"github.com/norhua/sonexa/internal/domain"
)

// ProviderID 是上游的稳定标识，如 "musicbrainz" / "itunes"。
type ProviderID = domain.ProviderID

// 已实现的上游标识。
const (
	MusicBrainz ProviderID = "musicbrainz"
	ITunes      ProviderID = "itunes"
	QQMusic     ProviderID = "qq"
)

// Upstream 仅声明稳定身份，可被各消费侧的单一能力接口嵌入。
type Upstream interface {
	ID() ProviderID
	DisplayName() string
}

// MetadataProvider 是音乐元数据上游的统一契约，不要求歌词或封面搜索能力。
//
// 接口不做「最小公倍数」式的统一：MB 与 iTunes 的字段交集很小，
// 若只暴露交集，MB 的演唱者、作曲、厂牌、ISRC、载体、国家全部无处安放。
// 因此所有字段用可空类型，由 Capabilities() 声明各自能提供什么。
type MetadataProvider interface {
	Upstream
	// Capabilities 声明本上游能提供哪些字段与能力。
	Capabilities() Capabilities
	// RateLimit 声明速率限制：每 per 时间内至多 n 次请求。
	RateLimit() (n int, per time.Duration)
	// IsConfigured 在前置条件未满足时返回可直接展示给用户的错误说明。
	IsConfigured() error
	// SearchTracks 按条件搜索，返回 track 粒度的候选。
	SearchTracks(ctx context.Context, q Query) ([]Candidate, error)
	Refs() RefCodec
	SnapshotFromCandidate(Candidate) (*domain.Snapshot, error)
	Complete(context.Context, domain.DetailTarget) (*domain.Snapshot, error)
	LoadReleaseSummary(context.Context, domain.ReleaseIdentity) (*domain.ReleaseSummary, error)
	// FetchDetail 拉取一条候选的完整详情（含该发行的完整曲目清单）。
	FetchDetail(ctx context.Context, ref Ref) (*Detail, error)
}
