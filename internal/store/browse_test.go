package store

import "testing"

// TestAlbumGroupingMergesVersions 覆盖 15.4：
// CD 版与数字版属同一发行组时合为一条，而不是裂成两条。
func TestAlbumGroupingMergesVersions(t *testing.T) {
	s := openTestStore(t)

	// 两首同专辑曲目，一首匹配 CD 版、一首匹配数字版，同属一个发行组
	for _, tc := range []struct{ ref, relRef string }{
		{"trk-cd-01", "rel-cd"},
		{"trk-digital-02", "rel-digital"},
	} {
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{
			Title: "曲目-" + tc.ref, Album: ptr("涼宮ハルヒの詰合"),
			MatchSource: ptr("musicbrainz"), MatchRef: ptr(tc.ref),
			Metadata: &MetadataInput{
				ReleaseMBID:      ptr(tc.relRef),
				ReleaseGroupMBID: ptr("rg-haruhi"),
				Album:            ptr("涼宮ハルヒの詰合"),
			},
		}); err != nil {
			t.Fatalf("入库失败: %v", err)
		}
	}

	// 两个 release 都登记在同一发行组下
	for _, tc := range []struct {
		ref, format string
		total       int
	}{
		{"rel-cd", "CD", 12},
		{"rel-digital", "Digital Media", 12},
	} {
		if _, err := s.UpsertRelease(&Release{
			Provider: "musicbrainz", ProviderRef: tc.ref,
			ReleaseGroupRef: ptr("rg-haruhi"), Title: "涼宮ハルヒの詰合",
			ReleaseStatus: ptr("Official"), MediumFormat: ptr(tc.format),
			TrackTotal: ptr(tc.total), TracklistState: TracklistComplete,
		}); err != nil {
			t.Fatalf("写入发行失败: %v", err)
		}
	}

	albums, err := s.ListAlbums()
	if err != nil {
		t.Fatalf("查询专辑失败: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("专辑条目数 = %d, 期望 1（不同版本应合为一条）: %+v", len(albums), albums)
	}

	a := albums[0]
	if a.Level != GroupByReleaseGroup {
		t.Errorf("分组层级 = %q, 期望 release_group", a.Level)
	}
	if a.OwnedCount != 2 {
		t.Errorf("已入库 = %d, 期望 2", a.OwnedCount)
	}
	if a.TotalCount == nil || *a.TotalCount != 12 {
		t.Errorf("清单总数 = %v, 期望 12", a.TotalCount)
	}
	if a.CountLabel() != "2/12" {
		t.Errorf("计数呈现 = %q, 期望 2/12", a.CountLabel())
	}
	// 代表 release 经优选阶梯选出：CD 优于 Digital Media
	if a.RepresentativeRef == nil || *a.RepresentativeRef != "rel-cd" {
		t.Errorf("代表 release = %v, 期望 rel-cd（CD 优于数字版）", a.RepresentativeRef)
	}
}

// TestDerivedAlbumGroupHasNoTotal 覆盖 15.6：
// 派生分组不显示总数，呈现为 `3/-`。
func TestDerivedAlbumGroupHasNoTotal(t *testing.T) {
	s := openTestStore(t)

	// 三首手动录入/「无需匹配」入库的曲目，没有任何上游标识
	for i := 0; i < 3; i++ {
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{
			Title: "无名曲" + string(rune('A'+i)), Artist: ptr("某艺术家"), Album: ptr("そんな私に"),
		}); err != nil {
			t.Fatalf("入库失败: %v", err)
		}
	}

	albums, err := s.ListAlbums()
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("专辑条目数 = %d, 期望 1", len(albums))
	}

	a := albums[0]
	if a.Level != GroupDerived {
		t.Errorf("分组层级 = %q, 期望 derived", a.Level)
	}
	if a.OwnedCount != 3 {
		t.Errorf("已入库 = %d, 期望 3", a.OwnedCount)
	}
	// 没有上游事实就不假装知道这张专辑有几首
	if a.TotalCount != nil {
		t.Errorf("派生分组不应有清单总数, 得到 %v", a.TotalCount)
	}
	if a.CountLabel() != "3/-" {
		t.Errorf("计数呈现 = %q, 期望 3/-", a.CountLabel())
	}
}

// TestAlbumDisappearsWhenLastTrackDeleted 覆盖 15.5：
// 曲目删光后条目消失，而 release 行保留作缓存。
func TestAlbumDisappearsWhenLastTrackDeleted(t *testing.T) {
	s := openTestStore(t)

	trackID, _ := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "唯一一首", Album: ptr("某专辑"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-1"),
		Metadata: &MetadataInput{ReleaseMBID: ptr("rel-1"), ReleaseGroupMBID: ptr("rg-1")},
	})
	releaseID, _ := s.UpsertRelease(&Release{
		Provider: "musicbrainz", ProviderRef: "rel-1",
		ReleaseGroupRef: ptr("rg-1"), Title: "某专辑",
		TrackTotal: ptr(10), TracklistState: TracklistComplete,
	})

	if albums, _ := s.ListAlbums(); len(albums) != 1 {
		t.Fatalf("删除前专辑数 = %d, 期望 1", len(albums))
	}

	if err := s.DeleteTrack(trackID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 专辑从列表中消失（至少拥有一首才出现）
	albums, _ := s.ListAlbums()
	if len(albums) != 0 {
		t.Errorf("删光后专辑数 = %d, 期望 0", len(albums))
	}
	// 但 release 行保留作缓存
	rel, _ := s.GetReleaseByRef("musicbrainz", "rel-1")
	if rel == nil {
		t.Error("release 行应保留作缓存")
	}
	var tracklistRows int
	s.DB().QueryRow(`SELECT COUNT(*) FROM release_track WHERE release_id = ?`, releaseID).Scan(&tracklistRows)
	_ = tracklistRows
}

// TestRoleFacetsIncludeZeroCounts 覆盖 15.2：
// 「全部 14」「演唱 12」「作曲 0」「作词 2」——计数为零的分面也要列出。
func TestRoleFacetsIncludeZeroCounts(t *testing.T) {
	s := openTestStore(t)
	artistID, _ := s.UpsertArtist(&Artist{Name: "平野綾"})

	// 12 首演唱、2 首作词、0 首作曲
	for i := 0; i < 12; i++ {
		id, _ := s.CreateTrack(&Track{Title: "演唱曲" + itoa(i)})
		s.AddTrackArtist(id, TrackArtist{ArtistID: artistID, Role: RoleVocal})
	}
	for i := 0; i < 2; i++ {
		id, _ := s.CreateTrack(&Track{Title: "作词曲" + itoa(i)})
		s.AddTrackArtist(id, TrackArtist{ArtistID: artistID, Role: RoleLyricist})
	}

	facets, err := s.RoleFacets(artistID)
	if err != nil {
		t.Fatalf("查询分面失败: %v", err)
	}

	byRole := map[string]RoleFacet{}
	for _, f := range facets {
		byRole[f.Role] = f
	}

	if got := byRole[""].Count; got != 14 {
		t.Errorf("全部 = %d, 期望 14", got)
	}
	if got := byRole["vocal"].Count; got != 12 {
		t.Errorf("演唱 = %d, 期望 12", got)
	}
	if got := byRole["lyricist"].Count; got != 2 {
		t.Errorf("作词 = %d, 期望 2", got)
	}
	// 计数为零的分面必须在列表中
	composer, ok := byRole["composer"]
	if !ok {
		t.Fatal("作曲分面应被列出，即使计数为零")
	}
	if composer.Count != 0 {
		t.Errorf("作曲 = %d, 期望 0", composer.Count)
	}
	if composer.Label != "作曲" {
		t.Errorf("作曲分面标签 = %q", composer.Label)
	}
}

// TestArtistViewFiltersByRole 覆盖 15.2 的「在同一张表内收窄」。
func TestArtistViewFiltersByRole(t *testing.T) {
	s := openTestStore(t)
	artistID, _ := s.UpsertArtist(&Artist{Name: "平野綾"})

	vocalID, _ := s.CreateTrack(&Track{Title: "演唱的歌"})
	s.AddTrackArtist(vocalID, TrackArtist{ArtistID: artistID, Role: RoleVocal})
	lyricID, _ := s.CreateTrack(&Track{Title: "作词的歌"})
	s.AddTrackArtist(lyricID, TrackArtist{ArtistID: artistID, Role: RoleLyricist})

	// 不带 role：全部
	all, err := s.ListTracks(TrackFilter{ArtistID: artistID})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("全部 = %d 首, 期望 2", len(all))
	}

	// 带 role：收窄
	vocals, _ := s.ListTracks(TrackFilter{ArtistID: artistID, Role: "vocal"})
	if len(vocals) != 1 || vocals[0].ID != vocalID {
		t.Errorf("演唱分面 = %+v, 期望只有演唱的那首", vocals)
	}
}

// TestCharacterArtistTitle 覆盖 15.3：
// 角色艺术家的视图标题渲染为「涼宮ハルヒ (CV: 平野綾)」。
func TestCharacterArtistTitle(t *testing.T) {
	s := openTestStore(t)

	charID, _ := s.UpsertArtist(&Artist{Name: "涼宮ハルヒ", Type: ptr("character")})
	vaID, _ := s.UpsertArtist(&Artist{Name: "平野綾", Type: ptr("person")})
	s.LinkArtists(charID, vaID, LinkVoiceOf)

	trackID, _ := s.CreateTrack(&Track{Title: "God knows..."})
	s.SetTrackArtists(trackID, []TrackArtist{
		{ArtistID: charID, Role: RoleCharacter},
		{ArtistID: vaID, Role: RoleVocal},
	})

	artists, err := s.ListArtists()
	if err != nil {
		t.Fatalf("查询艺术家失败: %v", err)
	}

	var character *ArtistListItem
	for i := range artists {
		if artists[i].ID == charID {
			character = &artists[i]
		}
	}
	if character == nil {
		t.Fatal("未找到角色艺术家")
	}
	if character.VoiceActor == nil || *character.VoiceActor != "平野綾" {
		t.Errorf("配音关系 = %v", character.VoiceActor)
	}
	if got := character.DisplayTitle(); got != "涼宮ハルヒ (CV: 平野綾)" {
		t.Errorf("视图标题 = %q, 期望 涼宮ハルヒ (CV: 平野綾)", got)
	}

	// 非角色艺术家的标题就是名字本身
	for i := range artists {
		if artists[i].ID == vaID {
			if got := artists[i].DisplayTitle(); got != "平野綾" {
				t.Errorf("人物艺术家标题 = %q", got)
			}
		}
	}
}

// TestCollectionsUnifiedModel 覆盖 13.4：同一套接口服务歌单与标签。
func TestCollectionsUnifiedModel(t *testing.T) {
	s := openTestStore(t)

	playlistID, err := s.CreateCollection("我的收藏", KindPlaylist, nil)
	if err != nil {
		t.Fatalf("建歌单失败: %v", err)
	}
	tagID, err := s.CreateCollection("我的收藏", KindTag, nil)
	if err != nil {
		t.Fatalf("同名标签应被允许: %v", err)
	}

	// 同名同种类被拒
	if _, err := s.CreateCollection("我的收藏", KindPlaylist, nil); err == nil {
		t.Error("同名歌单应被拒绝")
	}

	trackID, _ := s.CreateTrack(&Track{Title: "曲目"})

	// 同一个加入接口，仅目标种类不同
	if err := s.AddToCollection(playlistID, []int64{trackID}); err != nil {
		t.Fatalf("加入歌单失败: %v", err)
	}
	if err := s.AddToCollection(tagID, []int64{trackID}); err != nil {
		t.Fatalf("加入标签失败: %v", err)
	}

	// 重复加入不产生重复归属
	if err := s.AddToCollection(tagID, []int64{trackID}); err != nil {
		t.Fatalf("重复加入失败: %v", err)
	}
	var n int
	s.DB().QueryRow(
		`SELECT COUNT(*) FROM collection_item WHERE collection_id = ? AND track_id = ?`,
		tagID, trackID).Scan(&n)
	if n != 1 {
		t.Errorf("归属数 = %d, 期望 1（不产生重复）", n)
	}

	// 详情抽屉只显示标签，不显示歌单
	tags, err := s.TagsOfTrack(trackID)
	if err != nil {
		t.Fatalf("查询标签失败: %v", err)
	}
	if len(tags) != 1 {
		t.Fatalf("标签数 = %d, 期望 1", len(tags))
	}
	if tags[0].Kind != KindTag {
		t.Errorf("抽屉应只显示标签, 得到 %q", tags[0].Kind)
	}

	// 移出不等于删除
	if err := s.RemoveFromCollection(playlistID, []int64{trackID}); err != nil {
		t.Fatalf("移出失败: %v", err)
	}
	if track, _ := s.GetTrack(trackID); track == nil {
		t.Error("移出歌单不应删除曲目")
	}
	if tags, _ := s.TagsOfTrack(trackID); len(tags) != 1 {
		t.Error("移出歌单不应影响标签归属")
	}
}

// TestPlaylistOrderPersists 覆盖 13.5：拖拽排序持久化。
func TestPlaylistOrderPersists(t *testing.T) {
	s := openTestStore(t)
	playlistID, _ := s.CreateCollection("排序测试", KindPlaylist, nil)

	var ids []int64
	for i := 0; i < 5; i++ {
		id, _ := s.CreateTrack(&Track{Title: "第" + itoa(i+1) + "首"})
		ids = append(ids, id)
	}
	s.AddToCollection(playlistID, ids)

	// 把第 5 首拖到第 1 位
	reordered := []int64{ids[4], ids[0], ids[1], ids[2], ids[3]}
	if err := s.ReorderCollection(playlistID, reordered); err != nil {
		t.Fatalf("重排失败: %v", err)
	}

	got, err := s.ListTracks(TrackFilter{CollectionID: playlistID})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("曲目数 = %d", len(got))
	}
	if got[0].ID != ids[4] {
		t.Errorf("首位 = %d, 期望被拖上来的 %d", got[0].ID, ids[4])
	}
	if got[1].ID != ids[0] {
		t.Errorf("第二位 = %d, 期望 %d", got[1].ID, ids[0])
	}
}

// TestLockedFieldsSurviveMatch 覆盖 13.2：
// 匹配时覆盖未锁字段、不动已锁字段。
func TestLockedFieldsSurviveMatch(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.CreateTrack(&Track{
		Title: "用户改过的曲名", Artist: ptr("艺术家"), Album: ptr("旧专辑"),
	})

	// 用户手动修改曲名 → 加锁
	if err := s.UpdateTrackFields(id, ptr("用户设定的曲名"), nil, nil, true); err != nil {
		t.Fatalf("手动修改失败: %v", err)
	}

	// 匹配成功：未锁字段被上游规范值覆盖，已锁字段保持不变
	if err := s.UpdateBasicOnMatch(id,
		ptr("上游的曲名"), ptr("上游的艺术家"), ptr("上游的专辑")); err != nil {
		t.Fatalf("匹配更新失败: %v", err)
	}

	got, _ := s.GetTrack(id)
	if got.Title != "用户设定的曲名" {
		t.Errorf("曲名 = %q, 期望保持用户设定的值", got.Title)
	}
	if got.Artist == nil || *got.Artist != "上游的艺术家" {
		t.Errorf("艺术家 = %v, 期望被上游值覆盖", got.Artist)
	}
	if got.Album == nil || *got.Album != "上游的专辑" {
		t.Errorf("专辑 = %v, 期望被上游值覆盖", got.Album)
	}
	if !got.TitleLocked {
		t.Error("曲名应仍处于加锁状态")
	}
}
