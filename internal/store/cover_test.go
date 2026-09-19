package store

import (
	"testing"
	"time"
)

// TestSetCoverWritesRelease 覆盖 9.11：
// 确认采用后封面字段被正确写入——写的是曲目所属的**歌集**。
//
// 封面是歌集的属性，不是曲目的：此前逐曲目存一份，用户在一首歌上采用封面
// 之后，同专辑的其余曲目仍然没有图。现在采用一次即对整张歌集生效。
func TestSetCoverWritesRelease(t *testing.T) {
	s := openTestStore(t)
	// 同一张歌集下的两首曲目
	id, _ := s.CreateTrack(&Track{Title: "God knows...", Album: ptr("涼宮ハルヒの詰合")})
	sibling, _ := s.CreateTrack(&Track{Title: "ハレ晴レユカイ", Album: ptr("涼宮ハルヒの詰合")})

	now := time.Now().UTC()
	err := s.SetCover(id, CoverRecord{
		Path:      "/covers/adopted/abc123.jpg",
		Source:    "musicbrainz",
		SourceURL: "https://coverartarchive.org/release/rel-1/front-1200",
		Width:     ptr(1200),
		Height:    ptr(1200),
		FetchedAt: now,
	})
	if err != nil {
		t.Fatalf("写入封面失败: %v", err)
	}

	m, err := s.GetMetadata(id)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if m.CoverPath == nil || *m.CoverPath != "/covers/adopted/abc123.jpg" {
		t.Errorf("封面路径 = %v", m.CoverPath)
	}
	if m.CoverSourceURL == nil {
		t.Error("应记录原始地址")
	}

	// 尺寸记在歌集上
	rel, err := s.GetRelease(*m.ReleaseID)
	if err != nil {
		t.Fatalf("读取歌集失败: %v", err)
	}
	if rel.CoverWidth == nil || *rel.CoverWidth != 1200 {
		t.Errorf("宽度 = %v", rel.CoverWidth)
	}
	if rel.CoverHeight == nil || *rel.CoverHeight != 1200 {
		t.Errorf("高度 = %v", rel.CoverHeight)
	}

	// 采用一次对同歌集的其余曲目一并生效
	other, _ := s.GetMetadata(sibling)
	if other.CoverPath == nil || *other.CoverPath != "/covers/adopted/abc123.jpg" {
		t.Errorf("同歌集其余曲目的封面 = %v, 期望一并生效", other.CoverPath)
	}

	// 更换封面时覆盖旧值
	if err := s.SetCover(id, CoverRecord{
		Path: "/covers/adopted/def456.jpg", Source: "itunes",
		SourceURL: "https://mzstatic/3000x3000bb.jpg",
		Width:     ptr(3000), Height: ptr(3000), FetchedAt: now,
	}); err != nil {
		t.Fatalf("更换封面失败: %v", err)
	}
	m, _ = s.GetMetadata(id)
	if m.CoverPath == nil || *m.CoverPath != "/covers/adopted/def456.jpg" {
		t.Errorf("更换后封面路径 = %v", m.CoverPath)
	}
	rel, _ = s.GetRelease(*m.ReleaseID)
	if rel.CoverWidth == nil || *rel.CoverWidth != 3000 {
		t.Errorf("更换后宽度 = %v", rel.CoverWidth)
	}
}

// TestSetCoverWithoutReleaseFails 覆盖没有归属歌集时的处置。
//
// 没有歌集就没有地方放这张封面，静默丢弃会让用户以为采用成功了。
func TestSetCoverWithoutReleaseFails(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.CreateTrack(&Track{Title: "没有歌集的曲目"})

	err := s.SetCover(id, CoverRecord{
		Path: "/covers/adopted/abc123.jpg", Source: "musicbrainz",
		SourceURL: "https://example/front", FetchedAt: time.Now().UTC(),
	})
	if err == nil {
		t.Error("曲目没有所属歌集时应报错，而不是静默丢弃")
	}
}
