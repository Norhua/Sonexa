package store

import "testing"

// TestEnrichStateCountsPartitionAllRows 钉住三个计数刚好把全部行分完。
//
// 分不完的那一刻界面上的分母就永远到不了头，而用户读到的是「补全卡住了」。
func TestEnrichStateCountsPartitionAllRows(t *testing.T) {
	s := openTestStore(t)

	source, ref := "musicbrainz", "trk-1"
	release := "rel-1"
	// 有上游身份 + 有发行标识 → 待补
	pendingID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "待补", MatchSource: &source, MatchRef: &ref,
		Metadata: &MetadataInput{ReleaseMBID: &release},
	})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}
	// 有上游身份但没有发行标识 → 无从取起，记 absent（算已匹配）
	ref2 := "trk-2"
	absentID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "无从取起", MatchSource: &source, MatchRef: &ref2,
	})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}
	// 无需匹配入库：溯源置空
	skippedID, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "无需匹配"})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}

	for id, want := range map[int64]EnrichState{
		pendingID: EnrichPending, absentID: EnrichAbsent, skippedID: EnrichSkipped,
	} {
		got, err := s.EnrichStateOf(id)
		if err != nil {
			t.Fatalf("读状态失败: %v", err)
		}
		if got != want {
			t.Errorf("曲目 %d 的状态 = %q, 期望 %q", id, got, want)
		}
	}

	counts, err := s.EnrichStats()
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if counts.Matched != 1 || counts.Pending != 1 || counts.Skipped != 1 {
		t.Fatalf("计数 = %+v, 期望各 1 条", counts)
	}
	if sum := counts.Matched + counts.Pending + counts.Skipped; sum != 3 {
		t.Errorf("三个计数相加 = %d, 期望等于总行数 3", sum)
	}

	// 只有待补且带发行标识的那一条进工作集
	targets, err := s.TracksNeedingEnrichment(0)
	if err != nil {
		t.Fatalf("查询工作集失败: %v", err)
	}
	if len(targets) != 1 || targets[0].TrackID != pendingID {
		t.Errorf("工作集 = %+v, 期望只有待补那一条", targets)
	}
}
