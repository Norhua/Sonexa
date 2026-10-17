package provider

import (
	"context"
	"errors"
	"github.com/norhua/sonexa/internal/domain"
	"testing"
	"time"
)

// stubProvider 是一个用于测试的最小上游实现。
type stubProvider struct {
	id         ProviderID
	name       string
	caps       Capabilities
	rateN      int
	ratePer    time.Duration
	configured error
}

func (s *stubProvider) ID() ProviderID             { return s.id }
func (s *stubProvider) DisplayName() string        { return s.name }
func (s *stubProvider) Capabilities() Capabilities { return s.caps }
func (s *stubProvider) RateLimit() (int, time.Duration) {
	if s.rateN == 0 {
		return 1, time.Second
	}
	return s.rateN, s.ratePer
}
func (s *stubProvider) IsConfigured() error { return s.configured }
func (s *stubProvider) SearchTracks(context.Context, Query) ([]Candidate, error) {
	return nil, nil
}
func (s *stubProvider) FetchDetail(context.Context, Ref) (*Detail, error) { return nil, nil }

var _ MetadataProvider = (*stubProvider)(nil)

// TestRegistryListsRegistered 覆盖 5.3：
// 注册一个 stub provider 后它出现在 List() 中。
func TestRegistryListsRegistered(t *testing.T) {
	r := NewRegistry()
	if r.Len() != 0 {
		t.Fatalf("空注册表 Len = %d", r.Len())
	}

	r.Register(&stubProvider{id: "stub", name: "Stub 上游"})
	if r.Len() != 1 {
		t.Fatalf("注册后 Len = %d, 期望 1", r.Len())
	}

	list := r.List()
	if len(list) != 1 || list[0].ID() != "stub" {
		t.Fatalf("List = %v", list)
	}
	if list[0].DisplayName() != "Stub 上游" {
		t.Errorf("显示名 = %q", list[0].DisplayName())
	}

	got, err := r.Get("stub")
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if got.ID() != "stub" {
		t.Errorf("Get 返回 %q", got.ID())
	}
	if _, err := r.Get("nonexistent"); err == nil {
		t.Error("取用未登记的上游应返回错误")
	}
}

func TestMetadataRegistryStillReplacesDuplicateID(t *testing.T) {
	r := NewRegistry()
	first := &stubProvider{id: "same", name: "first"}
	second := &stubProvider{id: "same", name: "second"}
	r.Register(first)
	r.Register(second)
	got, err := r.Get("same")
	if err != nil || got != second || r.Len() != 1 {
		t.Fatalf("元数据覆盖语义改变: got=%v len=%d err=%v", got, r.Len(), err)
	}
}

// TestRegistryListIsStable 确保 List 顺序稳定且可由优先级设置控制——
// 这是「新增上游零页面代码改动」的前提。
func TestRegistryListIsStable(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubProvider{id: "itunes", name: "iTunes"})
	r.Register(&stubProvider{id: "musicbrainz", name: "MusicBrainz"})
	r.Register(&stubProvider{id: "zzz", name: "ZZZ"})

	// 无优先级时按标识字典序，重复调用顺序一致
	first := ids(r.List())
	for i := 0; i < 10; i++ {
		if got := ids(r.List()); !equalIDs(got, first) {
			t.Fatalf("第 %d 次 List 顺序变化: %v vs %v", i, got, first)
		}
	}

	// 设置优先级后按其排列
	r.SetPriority([]ProviderID{"musicbrainz", "itunes"})
	got := ids(r.List())
	want := []ProviderID{"musicbrainz", "itunes", "zzz"}
	if !equalIDs(got, want) {
		t.Errorf("优先级排序 = %v, 期望 %v", got, want)
	}
}

// TestIsConfiguredGatesOutbound 覆盖 5.5：
// 前置条件未满足时返回可直接展示的错误说明。
func TestIsConfiguredGatesOutbound(t *testing.T) {
	configured := &stubProvider{id: "ok"}
	if err := configured.IsConfigured(); err != nil {
		t.Errorf("已配置的上游不应报错: %v", err)
	}

	missing := &stubProvider{
		id:         "missing",
		configured: errors.New("请先在设置中填写 MusicBrainz 联系方式"),
	}
	err := missing.IsConfigured()
	if err == nil {
		t.Fatal("未配置的上游应返回错误")
	}
	if err.Error() == "" {
		t.Error("错误说明应可直接展示给用户")
	}
}

// TestCapabilitiesUnionDrivesColumns 覆盖 5.2 与 12.6：
// 列策略以当次结果涉及的上游集合为准。
func TestCapabilitiesUnionDrivesColumns(t *testing.T) {
	r := NewRegistry()
	r.Register(&stubProvider{
		id:   "musicbrainz",
		caps: CapLabel | CapCountry | CapMediumFormat | CapISRC | CapCoverArt,
	})
	r.Register(&stubProvider{
		id:   "itunes",
		caps: CapCoverArt | CapMultiSizeCover,
	})

	// 只涉及 iTunes：都不支持厂牌 → 隐藏该列
	only := r.CapabilitiesOf([]ProviderID{"itunes"})
	if only.Has(CapLabel) {
		t.Error("只搜 iTunes 时不应有厂牌能力")
	}
	// 聚合：部分支持 → 显示该列并在不支持的行填占位
	both := r.CapabilitiesOf([]ProviderID{"musicbrainz", "itunes"})
	if !both.Has(CapLabel) {
		t.Error("聚合时应有厂牌能力")
	}
	if !both.Has(CapMultiSizeCover) {
		t.Error("聚合应取并集")
	}
}

func TestCapabilityNames(t *testing.T) {
	caps := CapLabel | CapISRC
	names := caps.Names()
	if len(names) != 2 {
		t.Fatalf("能力名数 = %d: %v", len(names), names)
	}
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	if !found["Label"] || !found["ISRC"] {
		t.Errorf("能力名 = %v", names)
	}
}

func ids(ps []MetadataProvider) []ProviderID {
	out := make([]ProviderID, len(ps))
	for i, p := range ps {
		out[i] = p.ID()
	}
	return out
}

func equalIDs(a, b []ProviderID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (s *stubProvider) Refs() RefCodec { return nil }
func (s *stubProvider) SnapshotFromCandidate(c Candidate) (*domain.Snapshot, error) {
	return CandidateSnapshot(s.ID(), c)
}
func (s *stubProvider) Complete(context.Context, domain.DetailTarget) (*domain.Snapshot, error) {
	return nil, ErrUnsupported
}
func (s *stubProvider) LoadReleaseSummary(context.Context, domain.ReleaseIdentity) (*domain.ReleaseSummary, error) {
	return nil, ErrUnsupported
}

func TestSnapshotContractAndOptionalCapabilities(t *testing.T) {
	r := NewRegistry()
	p := &stubProvider{id: "test"}
	r.Register(p)
	album, artist := "Album", "Track Artist"
	c := Candidate{Provider: "test", MatchRef: "track", AlbumTitle: &album, ArtistName: &artist}
	s, err := p.SnapshotFromCandidate(c)
	if err != nil || s.Track.Credit.Value.Text != artist || s.Release != nil {
		t.Fatalf("candidate conversion: %#v %v", s, err)
	}
	c.Provider = "different"
	if _, err := p.SnapshotFromCandidate(c); err == nil {
		t.Fatal("wrong provider accepted")
	}
	if _, err := r.ListVersions(context.Background(), domain.Identity{Provider: "test", Ref: "group"}, Page{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
	if _, err := r.LoadArtworkCatalog(context.Background(), ArtworkTarget{Release: domain.Identity{Provider: "test", Ref: "release"}}, ArtworkIO{}); !errors.Is(err, ErrUnsupported) {
		t.Fatal(err)
	}
}
