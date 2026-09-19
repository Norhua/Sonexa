package store

import "testing"

// TestFTSMatchesRelatedArtists 覆盖 2.10 与验收 F3：
// 署名为 涼宮ハルヒ(CV:平野綾) 的曲目，搜角色名与搜声优名都要命中。
func TestFTSMatchesRelatedArtists(t *testing.T) {
	s := openTestStore(t)

	// 署名原串直接存在 track 上——曲目是单表，不再有第二份署名
	credit := "涼宮ハルヒ(CV:平野綾)"
	trackID, err := s.CreateTrack(&Track{Title: "God knows...", Credit: &credit})
	if err != nil {
		t.Fatalf("创建曲目失败: %v", err)
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
	// 署名分解与参与角色分开写：前者回答「谁出现在署名串里」，
	// 后者回答「谁在这首歌上担任什么」。两张表都进 FTS 索引。
	if err := s.SetTrackCredits(trackID, []CreditEntry{
		{ArtistID: charID, CreditedName: ptr("涼宮ハルヒ"), SpanStart: ptr(0), SpanEnd: ptr(6)},
	}); err != nil {
		t.Fatalf("写入署名分解失败: %v", err)
	}
	if err := s.SetTrackArtists(trackID, []TrackArtist{
		{ArtistID: charID, Role: RoleCharacter},
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

// TestFTSRefreshedOnAlbumRename 覆盖歌集改名后的检索。
//
// 歌集名进 FTS 的 album 列，而歌集名只存在 release 上一份——改名要连带
// 刷新该歌集的每一首曲目，否则搜专辑名会搜到旧名字。
func TestFTSRefreshedOnAlbumRename(t *testing.T) {
	s := openTestStore(t)

	var trackIDs []int64
	for _, title := range []string{"第一首", "第二首"} {
		id, err := s.CreateTrack(&Track{Title: title, Album: ptr("旧专辑名")})
		if err != nil {
			t.Fatalf("创建曲目失败: %v", err)
		}
		trackIDs = append(trackIDs, id)
	}
	if ids, _ := s.SearchTrackIDs("旧专辑名", 10); len(ids) != 2 {
		t.Fatalf("改名前应命中两首, 得到 %v", ids)
	}

	track, _ := s.GetTrack(trackIDs[0])
	if track.ReleaseID == nil {
		t.Fatal("曲目应已挂到本地歌集上")
	}
	if err := s.UpdateReleaseFields(*track.ReleaseID,
		map[string]string{"title": "新专辑名"}, nil, nil); err != nil {
		t.Fatalf("改歌集名失败: %v", err)
	}

	if ids, _ := s.SearchTrackIDs("新专辑名", 10); len(ids) != 2 {
		t.Errorf("改名后搜新名字得到 %v, 期望两首都命中", ids)
	}
	if ids, _ := s.SearchTrackIDs("旧专辑名", 10); len(ids) != 0 {
		t.Errorf("改名后搜旧名字得到 %v, 期望不再命中", ids)
	}
}

func ptr[T any](v T) *T { return &v }
