package store

import (
	"errors"
	"testing"
	"time"
)

// TestFuzzyScopeExcludesMatchedTracks 覆盖 8.3：
// 文本模糊比对的作用域仅限没有匹配来源的曲目。
func TestFuzzyScopeExcludesMatchedTracks(t *testing.T) {
	s := openTestStore(t)

	// 一首有溯源的曲目
	matched, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "God knows...", Credit: ptr("平野綾"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-1"),
		Metadata: &MetadataInput{DurationMS: ptr(332000)},
	})
	if err != nil {
		t.Fatalf("创建已匹配曲目失败: %v", err)
	}

	// 一首无溯源的曲目（「无需匹配」入库的那种）
	unmatched, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "ハレ晴レユカイ", Credit: ptr("平野綾"),
		Metadata: &MetadataInput{DurationMS: ptr(242000)},
	})
	if err != nil {
		t.Fatalf("创建未匹配曲目失败: %v", err)
	}

	// 比对已匹配曲目的文本：不应命中——它有标识可比，不参与模糊比对
	got, err := s.FindUnmatchedByText("godknows", "平野綾", ptr(332000))
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if got == matched {
		t.Error("已有溯源的曲目不应参与文本模糊比对——否则误判会扩散到全库")
	}
	if got != 0 {
		t.Errorf("不应命中任何曲目, 得到 %d", got)
	}

	// 比对未匹配曲目的文本：应命中
	got, err = s.FindUnmatchedByText("はれ晴れゆかい", "平野綾", ptr(242000))
	if err != nil {
		t.Fatalf("比对失败: %v", err)
	}
	if got != unmatched {
		t.Errorf("命中 %d, 期望未匹配曲目 %d", got, unmatched)
	}
}

// TestFuzzyRequiresDurationWithin3s 覆盖第三步的时长判据。
func TestFuzzyRequiresDurationWithin3s(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "That Girl", Credit: ptr("Natalia Lesz"),
		Metadata: &MetadataInput{DurationMS: ptr(221000)},
	})

	// 时长差 2s：命中
	if got, _ := s.FindUnmatchedByText("thatgirl", "natalialesz", ptr(223000)); got != id {
		t.Errorf("时长差 2s 应命中, 得到 %d", got)
	}
	// 时长差 20s：不命中
	if got, _ := s.FindUnmatchedByText("thatgirl", "natalialesz", ptr(241000)); got != 0 {
		t.Errorf("时长差 20s 不应命中, 得到 %d", got)
	}
}

// TestDuplicateAddIsSilentRedirect 覆盖 8.4 与 F14：
// 重复加入同一上游结果不产生第二条曲目，也不向用户报错。
func TestDuplicateAddIsSilentRedirect(t *testing.T) {
	s := openTestStore(t)
	input := NewTrackInput{
		Title: "God knows...", Credit: ptr("平野綾"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-dup"),
	}

	first, err := s.CreateTrackWithProvenance(input)
	if err != nil {
		t.Fatalf("首次加入失败: %v", err)
	}

	second, err := s.CreateTrackWithProvenance(input)
	var exists *ErrAlreadyExists
	if !errors.As(err, &exists) {
		t.Fatalf("重复加入应返回 ErrAlreadyExists, 得到 %v", err)
	}
	if second != first {
		t.Errorf("应跳转到已有曲目 %d, 得到 %d", first, second)
	}
	if exists.TrackID != first {
		t.Errorf("错误中的曲目 id = %d, 期望 %d", exists.TrackID, first)
	}

	var count int
	s.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&count)
	if count != 1 {
		t.Errorf("曲目数 = %d, 期望 1（不产生第二条）", count)
	}
}

// TestConcurrentAddSameResult 覆盖并发竞态：
// 两个路径几乎同时加入同一条上游结果时不报错。
func TestConcurrentAddSameResult(t *testing.T) {
	s := openTestStore(t)
	input := NewTrackInput{
		Title: "並发曲目", MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-race"),
	}

	const n = 8
	results := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			_, err := s.CreateTrackWithProvenance(input)
			results <- err
		}()
	}

	var created, redirected int
	for i := 0; i < n; i++ {
		err := <-results
		var exists *ErrAlreadyExists
		switch {
		case err == nil:
			created++
		case errors.As(err, &exists):
			redirected++
		default:
			t.Errorf("并发写入出现非预期错误: %v", err)
		}
	}

	if created != 1 {
		t.Errorf("成功创建 %d 次, 期望 1 次", created)
	}
	if redirected != n-1 {
		t.Errorf("静默跳转 %d 次, 期望 %d 次", redirected, n-1)
	}

	var count int
	s.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&count)
	if count != 1 {
		t.Errorf("曲目数 = %d, 期望 1", count)
	}
}

// TestResolveTracklistThreeStates 覆盖 8.5 与 15.8：
// 已入库 / 已有其他版本 / 未入库三种状态。
func TestResolveTracklistThreeStates(t *testing.T) {
	s := openTestStore(t)

	// 用户从 CD 版入库了第 2 首
	cdTrackID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title:       "God knows...",
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-cd-02"),
		Metadata: &MetadataInput{RecordingRef: ptr("rec-god-knows")},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	// 又入库了第 1 首（同版本）
	ownedID, _ := s.CreateTrackWithProvenance(NewTrackInput{
		Title:       "冒険でしょでしょ?",
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-digital-01"),
	})

	// 当前专辑视图显示的是数字版清单
	releaseID, _ := s.UpsertRelease(&Release{
		Provider: ptr("musicbrainz"), ProviderRef: ptr("rel-digital"), Title: "数字版",
		TracklistState: TracklistComplete,
	})
	if err := s.ReplaceTracklist(releaseID, []ReleaseTrack{
		// 第 1 首：track_ref 直接命中 → 已入库
		{DiscNumber: 1, TrackNumber: 1, TrackRef: ptr("trk-digital-01"), Title: "冒険でしょでしょ?"},
		// 第 2 首：track_ref 不同（数字版），但 recording 相同 → 已有其他版本
		{DiscNumber: 1, TrackNumber: 2, TrackRef: ptr("trk-digital-02"),
			RecordingRef: ptr("rec-god-knows"), Title: "God knows..."},
		// 第 3 首：都未命中 → 未入库（灰色行）
		{DiscNumber: 1, TrackNumber: 3, TrackRef: ptr("trk-digital-03"), Title: "ハレ晴レユカイ"},
	}, TracklistComplete); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	got, err := s.ResolveTracklist(releaseID, "musicbrainz")
	if err != nil {
		t.Fatalf("解析清单失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("清单位置数 = %d, 期望 3", len(got))
	}

	if got[0].State != PositionOwned || got[0].TrackID != ownedID {
		t.Errorf("第 1 首 = %s/%d, 期望 owned/%d", got[0].State, got[0].TrackID, ownedID)
	}
	// 关键：CD 版入库而清单为数字版时，该位置不被标成缺失
	if got[1].State != PositionOtherVersion {
		t.Errorf("第 2 首 = %s, 期望 other_version——CD 版入库时不应被标成缺失", got[1].State)
	}
	if got[1].TrackID != cdTrackID {
		t.Errorf("第 2 首指向 %d, 期望跳转到已有的 %d", got[1].TrackID, cdTrackID)
	}
	if got[2].State != PositionMissing {
		t.Errorf("第 3 首 = %s, 期望 missing（灰色行）", got[2].State)
	}
}

// TestSetAndClearProvenance 覆盖 8.2 的赋值与置空时机。
func TestSetAndClearProvenance(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.CreateTrack(&Track{Title: "曲目"})

	// matchedAt 直接读列：它是溯源的一部分，但不进详情读出结构
	matchedAt := func() *time.Time {
		t.Helper()
		var at *time.Time
		if err := s.DB().QueryRow(`SELECT matched_at FROM track WHERE id = ?`, id).Scan(&at); err != nil {
			t.Fatalf("读取匹配时间失败: %v", err)
		}
		return at
	}

	// 赋值
	if err := s.SetProvenance(id, "musicbrainz", "trk-1"); err != nil {
		t.Fatalf("写入溯源失败: %v", err)
	}
	m, _ := s.GetMetadata(id)
	if m == nil || m.MatchSource == nil || *m.MatchSource != "musicbrainz" {
		t.Fatalf("溯源来源 = %+v", m)
	}
	if m.MatchRef == nil || *m.MatchRef != "trk-1" {
		t.Errorf("溯源标识 = %v", m.MatchRef)
	}
	if matchedAt() == nil {
		t.Error("匹配时间应被写入")
	}

	// 覆盖为新值（编辑页重新匹配）
	if err := s.SetProvenance(id, "itunes", "jp:123"); err != nil {
		t.Fatalf("覆盖溯源失败: %v", err)
	}
	m, _ = s.GetMetadata(id)
	if *m.MatchSource != "itunes" || *m.MatchRef != "jp:123" {
		t.Errorf("覆盖后 = %s/%s", *m.MatchSource, *m.MatchRef)
	}

	// 置空（「无需匹配」或手动清空）
	if err := s.ClearProvenance(id); err != nil {
		t.Fatalf("清空溯源失败: %v", err)
	}
	m, _ = s.GetMetadata(id)
	if m.MatchSource != nil || m.MatchRef != nil || matchedAt() != nil {
		t.Errorf("清空后仍有溯源: source=%v ref=%v at=%v", m.MatchSource, m.MatchRef, matchedAt())
	}
	// 字段值本身不变
	track, _ := s.GetTrack(id)
	if track.Title != "曲目" {
		t.Error("清空溯源不应改变字段值")
	}
}

// TestNoMatchNeededTrack 覆盖「无需匹配」入库：
// 按原值入库、丰富信息为空、溯源置空，且不阻碍后续操作。
func TestNoMatchNeededTrack(t *testing.T) {
	s := openTestStore(t)

	id, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "无名曲", Credit: ptr("未知艺术家"),
	})
	if err != nil {
		t.Fatalf("无需匹配入库失败: %v", err)
	}

	m, err := s.GetMetadata(id)
	if err != nil {
		t.Fatalf("读取丰富信息失败: %v", err)
	}
	if m.MatchSource != nil || m.MatchRef != nil {
		t.Error("「无需匹配」入库的曲目溯源应为空")
	}
	// 丰富信息整体为空，但读取不报错
	if m.Year != nil || m.Genre != nil || m.Label != nil {
		t.Error("丰富信息应整体为空")
	}

	// 多条无溯源曲目可以并存（部分唯一索引不约束它们）
	for i := 0; i < 3; i++ {
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{Title: "另一首无名曲"}); err != nil {
			t.Fatalf("第 %d 条无溯源曲目写入失败: %v", i+1, err)
		}
	}
}
