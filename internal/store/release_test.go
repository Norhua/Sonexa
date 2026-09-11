package store

import "testing"

// TestUpsertReleaseUpdatesNotInserts 覆盖 2.7：
// 重复写入同一 (provider, provider_ref) 走更新而非插入。
func TestUpsertReleaseUpdatesNotInserts(t *testing.T) {
	s := openTestStore(t)

	first, err := s.UpsertRelease(&Release{
		Provider: "musicbrainz", ProviderRef: "rel-1",
		Title: "涼宮ハルヒの詰合", TracklistState: TracklistNone,
	})
	if err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}

	country := "JP"
	second, err := s.UpsertRelease(&Release{
		Provider: "musicbrainz", ProviderRef: "rel-1",
		Title: "涼宮ハルヒの詰合", Country: &country, TracklistState: TracklistComplete,
	})
	if err != nil {
		t.Fatalf("再次写入失败: %v", err)
	}
	if first != second {
		t.Errorf("id 变化 %d -> %d，说明走了插入而非更新", first, second)
	}

	var rows int
	s.DB().QueryRow(`SELECT COUNT(*) FROM release`).Scan(&rows)
	if rows != 1 {
		t.Errorf("发行行数 = %d, 期望 1", rows)
	}

	got, err := s.GetReleaseByRef("musicbrainz", "rel-1")
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Country == nil || *got.Country != "JP" {
		t.Error("更新应补上此前缺失的国家字段")
	}
	if got.TracklistState != TracklistComplete {
		t.Errorf("清单状态 = %q, 期望 complete", got.TracklistState)
	}
}

// TestTracklistStateNeverDowngrades 确保只写 release 行的调用
// 不会把已有的完整清单状态退回 none。
func TestTracklistStateNeverDowngrades(t *testing.T) {
	s := openTestStore(t)

	id, _ := s.UpsertRelease(&Release{
		Provider: "musicbrainz", ProviderRef: "rel-2", Title: "专辑",
		TracklistState: TracklistComplete,
	})
	// 导入自动入库只写 release 行，状态为 none
	if _, err := s.UpsertRelease(&Release{
		Provider: "musicbrainz", ProviderRef: "rel-2", Title: "专辑",
		TracklistState: TracklistNone,
	}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	got, _ := s.GetReleaseByRef("musicbrainz", "rel-2")
	if got.ID != id {
		t.Error("应更新同一行")
	}
	if got.TracklistState != TracklistComplete {
		t.Errorf("清单状态 = %q, 期望保持 complete", got.TracklistState)
	}
}

// TestReplaceTracklistOrdersByDiscThenTrack 覆盖清单按碟号与音轨号排序。
func TestReplaceTracklistOrdersByDiscThenTrack(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.UpsertRelease(&Release{
		Provider: "musicbrainz", ProviderRef: "rel-3", Title: "双碟专辑",
		TracklistState: TracklistNone,
	})

	if err := s.ReplaceTracklist(id, []ReleaseTrack{
		{DiscNumber: 2, TrackNumber: 1, Title: "碟二第一首"},
		{DiscNumber: 1, TrackNumber: 2, Title: "碟一第二首"},
		{DiscNumber: 1, TrackNumber: 1, Title: "碟一第一首"},
	}, TracklistComplete); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	got, err := s.GetTracklist(id)
	if err != nil {
		t.Fatalf("读取清单失败: %v", err)
	}
	want := []string{"碟一第一首", "碟一第二首", "碟二第一首"}
	if len(got) != len(want) {
		t.Fatalf("清单行数 = %d, 期望 %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Title != w {
			t.Errorf("第 %d 行 = %q, 期望 %q", i, got[i].Title, w)
		}
	}

	// 替换为更短的清单时旧行必须消失，不能残留
	if err := s.ReplaceTracklist(id, []ReleaseTrack{
		{DiscNumber: 1, TrackNumber: 1, Title: "唯一一首"},
	}, TracklistComplete); err != nil {
		t.Fatalf("替换清单失败: %v", err)
	}
	got, _ = s.GetTracklist(id)
	if len(got) != 1 {
		t.Errorf("替换后清单行数 = %d, 期望 1", len(got))
	}
}
