package store

import "testing"

// TestFTSMatchesRelatedArtists 覆盖 2.10 与验收 F3：
// 署名为 涼宮ハルヒ(CV:平野綾) 的曲目，搜角色名与搜声优名都要命中。
func TestFTSMatchesRelatedArtists(t *testing.T) {
	s := openTestStore(t)

	credit := "涼宮ハルヒ(CV:平野綾)"
	trackID, err := s.CreateTrack(&Track{Title: "God knows...", Artist: &credit})
	if err != nil {
		t.Fatalf("创建曲目失败: %v", err)
	}
	if _, err := s.DB().Exec(
		`INSERT INTO track_metadata (track_id, track_credit, updated_at)
		 VALUES (?, ?, datetime('now'))`, trackID, credit); err != nil {
		t.Fatalf("写入丰富信息失败: %v", err)
	}

	character := ptr("character")
	person := ptr("person")
	charID, err := s.UpsertArtist(&Artist{Name: "涼宮ハルヒ", Type: character})
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	vaID, err := s.UpsertArtist(&Artist{Name: "平野綾", Type: person})
	if err != nil {
		t.Fatalf("创建声优失败: %v", err)
	}
	if err := s.LinkArtists(charID, vaID, LinkVoiceOf); err != nil {
		t.Fatalf("建立配音关系失败: %v", err)
	}
	if err := s.SetTrackArtists(trackID, []TrackArtist{
		{ArtistID: charID, Role: RoleCharacter, CreditedName: &credit},
		{ArtistID: vaID, Role: RoleVocal},
	}); err != nil {
		t.Fatalf("写入参与者失败: %v", err)
	}

	for _, q := range []string{"平野綾", "涼宮ハルヒ", "God knows"} {
		ids, err := s.SearchTrackIDs(q, 10)
		if err != nil {
			t.Fatalf("检索 %q 失败: %v", q, err)
		}
		if len(ids) != 1 || ids[0] != trackID {
			t.Errorf("检索 %q 得到 %v, 期望 [%d]", q, ids, trackID)
		}
	}
}

// TestFTSRefreshedOnArtistChange 覆盖「艺术家关联变更后检索」：
// 新增一位作曲艺术家后，立即用其姓名检索即可命中，无需重建索引。
func TestFTSRefreshedOnArtistChange(t *testing.T) {
	s := openTestStore(t)

	trackID, err := s.CreateTrack(&Track{Title: "冒険でしょでしょ?"})
	if err != nil {
		t.Fatalf("创建曲目失败: %v", err)
	}
	if ids, _ := s.SearchTrackIDs("田中公平", 10); len(ids) != 0 {
		t.Fatalf("尚未关联作曲者时不应命中, 得到 %v", ids)
	}

	composerID, err := s.UpsertArtist(&Artist{Name: "田中公平"})
	if err != nil {
		t.Fatalf("创建作曲者失败: %v", err)
	}
	if err := s.AddTrackArtist(trackID, TrackArtist{ArtistID: composerID, Role: RoleComposer}); err != nil {
		t.Fatalf("关联作曲者失败: %v", err)
	}

	ids, err := s.SearchTrackIDs("田中公平", 10)
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(ids) != 1 || ids[0] != trackID {
		t.Errorf("关联作曲者后检索得到 %v, 期望 [%d]", ids, trackID)
	}
}

// TestFTSDropsDeletedTrack 确保删除曲目后其索引行一并消失。
func TestFTSDropsDeletedTrack(t *testing.T) {
	s := openTestStore(t)

	trackID, err := s.CreateTrack(&Track{Title: "ハレ晴レユカイ"})
	if err != nil {
		t.Fatalf("创建曲目失败: %v", err)
	}
	if ids, _ := s.SearchTrackIDs("ハレ晴レユカイ", 10); len(ids) != 1 {
		t.Fatalf("创建后应命中, 得到 %v", ids)
	}
	if err := s.DeleteTrack(trackID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if ids, _ := s.SearchTrackIDs("ハレ晴レユカイ", 10); len(ids) != 0 {
		t.Errorf("删除后不应命中, 得到 %v", ids)
	}
}

// TestRejectsUnknownRole 覆盖「未知角色的处理」：
// 不引入取值域之外的新角色值。
func TestRejectsUnknownRole(t *testing.T) {
	s := openTestStore(t)
	trackID, _ := s.CreateTrack(&Track{Title: "t"})
	artistID, _ := s.UpsertArtist(&Artist{Name: "a"})

	err := s.AddTrackArtist(trackID, TrackArtist{ArtistID: artistID, Role: ArtistRole("mastering")})
	if err == nil {
		t.Error("取值域之外的角色应被拒绝")
	}
}

func ptr[T any](v T) *T { return &v }
