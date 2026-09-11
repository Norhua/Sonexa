package store

import (
	"testing"
	"time"
)

// TestSetCoverWritesMetadata 覆盖 9.11：
// 确认采用后 track_metadata 的封面字段被正确写入。
func TestSetCoverWritesMetadata(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.CreateTrack(&Track{Title: "God knows..."})

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
	if m.CoverSource == nil || *m.CoverSource != "musicbrainz" {
		t.Errorf("封面来源 = %v", m.CoverSource)
	}
	if m.CoverSourceURL == nil {
		t.Error("应记录原始地址")
	}
	if m.CoverWidth == nil || *m.CoverWidth != 1200 {
		t.Errorf("宽度 = %v", m.CoverWidth)
	}
	if m.CoverHeight == nil || *m.CoverHeight != 1200 {
		t.Errorf("高度 = %v", m.CoverHeight)
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
	if *m.CoverSource != "itunes" || *m.CoverWidth != 3000 {
		t.Errorf("更换后 = %s/%d", *m.CoverSource, *m.CoverWidth)
	}
}
