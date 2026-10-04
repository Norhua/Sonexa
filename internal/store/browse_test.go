package store

import (
	"testing"

	"github.com/norhua/sonexa/internal/matching"
)

// TestAlbumGroupingMergesVersions 覆盖 15.4：
// CD 版与数字版属同一发行组时合为一条，而不是裂成两条。
func TestAlbumGroupingMergesVersions(t *testing.T) {
	s := openTestStore(t)

	// 两首同专辑曲目，一首匹配 CD 版、一首匹配数字版，同属一个发行组。
	// 发行级事实（载体、清单总数、类型）随曲目一起写到**歌集**上，
	// 不再逐曲目复制一份。
	for _, tc := range []struct{ ref, relRef, format string }{
		{"trk-cd-01", "rel-cd", "CD"},
		{"trk-digital-02", "rel-digital", "Digital Media"},
	} {
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{
			Title: "曲目-" + tc.ref, Album: ptr("涼宮ハルヒの詰合"),
			MatchSource: ptr("musicbrainz"), MatchRef: ptr(tc.ref),
			Release: &ReleaseInput{
				Provider: ptr("musicbrainz"), ProviderRef: ptr(tc.relRef),
				ReleaseGroupRef: ptr("rg-haruhi"),
				ReleaseStatus:   ptr("Official"), MediumFormat: ptr(tc.format),
				TrackTotal: ptr(12),
			},
		}); err != nil {
			t.Fatalf("入库失败: %v", err)
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
	// 合为一条的依据是发行组，而不是查询时现场拼的文本键
	if a.GroupID == 0 {
		t.Error("这条专辑应代表一个发行组")
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
	if a.ReleaseGroupRef == nil || *a.ReleaseGroupRef != "rg-haruhi" {
		t.Errorf("发行组标识 = %v, 期望 rg-haruhi", a.ReleaseGroupRef)
	}
	// 代表 release 经优选阶梯选出：CD 优于 Digital Media
	if a.RepresentativeRef == nil || *a.RepresentativeRef != "rel-cd" {
		t.Errorf("代表 release = %v, 期望 rel-cd（CD 优于数字版）", a.RepresentativeRef)
	}
}

// TestLocalAlbumHasNoTotal 覆盖 15.6：
// 没有上游标识的曲目按 (歌集名, 歌集署名) 归拢到一条**本地歌集**上，
// 而本地歌集没有上游清单，因此不显示总数，呈现为 `3/-`。
//
// 此前这一种靠查询时现场派生的文本键分组，于是专辑视图要为它单独走一条
// 代码路径，而歌集的字段（碟数、封面、类型）对它根本无处可放。给一条真正的
// release 行之后两者合流——但「不假装知道这张专辑有几首」这一条不变。
func TestLocalAlbumHasNoTotal(t *testing.T) {
	s := openTestStore(t)

	// 三首手动录入/「无需匹配」入库的曲目，没有任何上游标识
	var trackIDs []int64
	for i := 0; i < 3; i++ {
		id, err := s.CreateTrackWithProvenance(NewTrackInput{
			Title: "无名曲" + string(rune('A'+i)), Credit: ptr("某艺术家"),
			Album: ptr("そんな私に"), AlbumCredit: ptr("某艺术家"),
		})
		if err != nil {
			t.Fatalf("入库失败: %v", err)
		}
		trackIDs = append(trackIDs, id)
	}

	albums, err := s.ListAlbums()
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("专辑条目数 = %d, 期望 1", len(albums))
	}

	a := albums[0]
	// 本地发行也归本地组，但没有上游标识
	if a.GroupID == 0 {
		t.Errorf("本地发行应有本地组, 得到 %d", a.GroupID)
	}
	if a.RepresentativeRef != nil {
		t.Errorf("本地歌集没有上游标识, 得到 %v", a.RepresentativeRef)
	}
	if a.OwnedCount != 3 {
		t.Errorf("已入库 = %d, 期望 3", a.OwnedCount)
	}
	// 没有上游事实就不假装知道这张专辑有几首
	if a.TotalCount != nil {
		t.Errorf("本地歌集不应有清单总数, 得到 %v", a.TotalCount)
	}
	if a.CountLabel() != "3/-" {
		t.Errorf("计数呈现 = %q, 期望 3/-", a.CountLabel())
	}

	// 三首归拢到同一条歌集上，而不是各建一条
	for _, id := range trackIDs {
		track, _ := s.GetTrack(id)
		if track.ReleaseID == nil || *track.ReleaseID != a.ReleaseID {
			t.Errorf("曲目 %d 的歌集 = %v, 期望都归到 %d", id, track.ReleaseID, a.ReleaseID)
		}
	}
}

// TestAlbumDisappearsWhenLastTrackDeleted 覆盖 15.5：
// 曲目删光后条目消失，而 release 行保留作缓存。
func TestAlbumDisappearsWhenLastTrackDeleted(t *testing.T) {
	s := openTestStore(t)

	trackID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "唯一一首", Album: ptr("某专辑"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-1"),
		Release: &ReleaseInput{
			Provider: ptr("musicbrainz"), ProviderRef: ptr("rel-1"),
			ReleaseGroupRef: ptr("rg-1"), TrackTotal: ptr(10),
		},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	rel, _ := s.GetReleaseByRef("musicbrainz", "rel-1")
	if rel == nil {
		t.Fatal("歌集应已随曲目建立")
	}
	if err := s.ReplaceTracklist(rel.ID, []ReleaseTrack{
		{DiscNumber: 1, TrackNumber: 1, TrackRef: ptr("trk-1"), Title: "唯一一首"},
	}, TracklistComplete); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

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
	// 最后一首删除后清理发行及清单，不留下空成员。
	kept, _ := s.GetReleaseByRef("musicbrainz", "rel-1")
	if kept != nil {
		t.Fatal("无收藏发行应清理")
	}
	var tracklistRows int
	s.DB().QueryRow(`SELECT COUNT(*) FROM release_track WHERE release_id = ?`, rel.ID).Scan(&tracklistRows)
	if tracklistRows != 0 {
		t.Errorf("孤立清单行数 = %d, 期望 0", tracklistRows)
	}
}

// TestLocalAlbumDisappearsWithLastTrack 覆盖本地歌集的另一半：
// 上游歌集删光曲目后保留作缓存，本地歌集则顺带删除——
// 它没有清单可缓存，留着只会让侧栏积一堆空专辑。
func TestLocalAlbumDisappearsWithLastTrack(t *testing.T) {
	s := openTestStore(t)

	trackID, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "唯一一首", Album: ptr("本地专辑"), AlbumCredit: ptr("某艺术家"),
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	track, _ := s.GetTrack(trackID)
	if track.ReleaseID == nil {
		t.Fatal("曲目应已挂到本地歌集上")
	}
	releaseID := *track.ReleaseID

	if err := s.DeleteTrack(trackID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	got, err := s.GetRelease(releaseID)
	if err != nil {
		t.Fatalf("读取歌集失败: %v", err)
	}
	if got != nil {
		t.Error("再无曲目引用的本地歌集应被顺带删除")
	}
}

// TestRoleFacetsIncludeZeroCounts 覆盖 15.2：
// 「全部 14」「署名 12」「演唱 12」「作曲 0」「作词 2」——计数为零的分面也要列出。
func TestRoleFacetsIncludeZeroCounts(t *testing.T) {
	s := openTestStore(t)
	artistID, err := s.UpsertArtist(&Artist{Name: "平野綾"})
	if err != nil {
		t.Fatalf("建艺术家失败: %v", err)
	}

	// 12 首演唱且被署名、2 首只作词（未署名）、0 首作曲
	for i := 0; i < 12; i++ {
		id, err := s.CreateTrack(&Track{Title: "演唱曲" + itoa(i), Credit: ptr("平野綾")})
		if err != nil {
			t.Fatalf("建曲目失败: %v", err)
		}
		if err := s.AddTrackArtist(id, TrackArtist{ArtistID: artistID, Role: RoleVocal}); err != nil {
			t.Fatalf("关联演唱失败: %v", err)
		}
		if err := s.SetTrackCredits(id,
			[]CreditEntry{{ArtistID: artistID, CreditedName: ptr("平野綾")}}); err != nil {
			t.Fatalf("写入署名分解失败: %v", err)
		}
	}
	for i := 0; i < 2; i++ {
		id, err := s.CreateTrack(&Track{Title: "作词曲" + itoa(i)})
		if err != nil {
			t.Fatalf("建曲目失败: %v", err)
		}
		if err := s.AddTrackArtist(id, TrackArtist{ArtistID: artistID, Role: RoleLyricist}); err != nil {
			t.Fatalf("关联作词失败: %v", err)
		}
	}

	facets, err := s.RoleFacets(artistID)
	if err != nil {
		t.Fatalf("查询分面失败: %v", err)
	}

	byRole := map[string]RoleFacet{}
	for _, f := range facets {
		byRole[f.Role] = f
	}

	// 「全部」是署名 ∪ 全部角色，它可能大于任一单独分面
	if got := byRole[""].Count; got != 14 {
		t.Errorf("全部 = %d, 期望 14", got)
	}
	// 「署名」单列：它是艺术家列表成员判定的一半，用户应当能只看被署名的那些
	credited, ok := byRole[RoleCredited]
	if !ok {
		t.Fatal("署名分面应被列出")
	}
	if credited.Count != 12 {
		t.Errorf("署名 = %d, 期望 12（只作词的两首未署名）", credited.Count)
	}
	if credited.Label != "署名" {
		t.Errorf("署名分面标签 = %q", credited.Label)
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
//
// 「署名」是分面之一：它查 track_credit，其余分面查 track_artist。
func TestArtistViewFiltersByRole(t *testing.T) {
	s := openTestStore(t)
	artistID, err := s.UpsertArtist(&Artist{Name: "平野綾"})
	if err != nil {
		t.Fatalf("建艺术家失败: %v", err)
	}

	vocalID, err := s.CreateTrack(&Track{Title: "演唱的歌", Credit: ptr("平野綾")})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}
	if err := s.AddTrackArtist(vocalID, TrackArtist{ArtistID: artistID, Role: RoleVocal}); err != nil {
		t.Fatalf("关联演唱失败: %v", err)
	}
	if err := s.SetTrackCredits(vocalID,
		[]CreditEntry{{ArtistID: artistID, CreditedName: ptr("平野綾")}}); err != nil {
		t.Fatalf("写入署名分解失败: %v", err)
	}

	lyricID, err := s.CreateTrack(&Track{Title: "作词的歌"})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}
	if err := s.AddTrackArtist(lyricID, TrackArtist{ArtistID: artistID, Role: RoleLyricist}); err != nil {
		t.Fatalf("关联作词失败: %v", err)
	}

	// 不带 role：全部（署名 ∪ 全部角色）
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

	// 署名分面：只看被署名的那些，未署名的作词不在其中
	credited, _ := s.ListTracks(TrackFilter{ArtistID: artistID, Role: RoleCredited})
	if len(credited) != 1 || credited[0].ID != vocalID {
		t.Errorf("署名分面 = %+v, 期望只有被署名的那首", credited)
	}
}

// TestArtistListExcludesUncreditedContributors 覆盖艺术家列表的成员判定。
//
// 一位艺术家出现在列表里，当且仅当他**在某曲目的署名中出现**，
// 或**在某曲目上担任演唱**。未署名的作词作曲不满足任一条，因此不进列表——
// 此前无条件 JOIN track_artist 把这些人也列了出来，正是「艺术家视图
// 出现未署名的作词作曲」的成因。
func TestArtistListExcludesUncreditedContributors(t *testing.T) {
	s := openTestStore(t)

	// 建艺术家时逐个查错：写入失败会让回滚掉的 rowid 被下一条复用，
	// 于是两个变量指向同一位艺术家，而断言失败的位置离成因很远。
	newArtist := func(name string) int64 {
		t.Helper()
		id, err := s.UpsertArtist(&Artist{Name: name})
		if err != nil {
			t.Fatalf("建艺术家 %s 失败: %v", name, err)
		}
		return id
	}
	creditedID := newArtist("DECO*27")
	vocalID := newArtist("初音ミク")
	ghostID := newArtist("未署名的编曲")

	trackID, err := s.CreateTrack(&Track{Title: "愛言葉", Credit: ptr("DECO*27")})
	if err != nil {
		t.Fatalf("创建曲目失败: %v", err)
	}
	// 署名艺术家：只作词作曲，但出现在署名串里
	if err := s.SetTrackCredits(trackID,
		[]CreditEntry{{ArtistID: creditedID, CreditedName: ptr("DECO*27")}}); err != nil {
		t.Fatalf("写入署名分解失败: %v", err)
	}
	if err := s.SetTrackArtists(trackID, []TrackArtist{
		{ArtistID: creditedID, Role: RoleComposer},
		{ArtistID: vocalID, Role: RoleVocal},
		{ArtistID: ghostID, Role: RoleArranger},
	}); err != nil {
		t.Fatalf("写入参与者失败: %v", err)
	}

	artists, err := s.ListArtists()
	if err != nil {
		t.Fatalf("查询艺术家失败: %v", err)
	}

	inList := map[int64]bool{}
	for _, a := range artists {
		inList[a.ID] = true
	}
	if !inList[creditedID] {
		t.Error("被署名的艺术家应在列表中——哪怕他只作词作曲")
	}
	if !inList[vocalID] {
		t.Error("担任演唱的艺术家应在列表中——哪怕他没出现在署名串里")
	}
	if inList[ghostID] {
		t.Error("未署名且不演唱的参与者不应进入艺术家列表")
	}

	// 但进入某位艺术家之后，他的曲目列表仍展示全部角色：
	// 列表成员判定与视图内容是两件事
	tracks, _ := s.ListTracks(TrackFilter{ArtistID: ghostID})
	if len(tracks) != 1 {
		t.Errorf("按未署名参与者筛选 = %d 首, 期望 1（不在列表中不等于查不到）", len(tracks))
	}
}

// TestCharacterArtistTitle 覆盖 15.3：
// 角色艺术家的视图标题渲染为「涼宮ハルヒ (CV: 平野綾)」。
func TestCharacterArtistTitle(t *testing.T) {
	s := openTestStore(t)

	charID, err := s.UpsertArtist(&Artist{Name: "涼宮ハルヒ", Type: ptr("character")})
	if err != nil {
		t.Fatalf("建角色失败: %v", err)
	}
	vaID, err := s.UpsertArtist(&Artist{Name: "平野綾", Type: ptr("person")})
	if err != nil {
		t.Fatalf("建声优失败: %v", err)
	}
	if err := s.LinkArtists(charID, vaID, LinkVoiceOf); err != nil {
		t.Fatalf("建立配音关系失败: %v", err)
	}

	credit := "涼宮ハルヒ(CV:平野綾)"
	trackID, err := s.CreateTrack(&Track{Title: "God knows...", Credit: &credit})
	if err != nil {
		t.Fatalf("建曲目失败: %v", err)
	}
	// 角色出现在署名串里（进列表靠这一条），声优凭 vocal 关系进来
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
	var found bool
	for i := range artists {
		if artists[i].ID == vaID {
			found = true
			if got := artists[i].DisplayTitle(); got != "平野綾" {
				t.Errorf("人物艺术家标题 = %q", got)
			}
		}
	}
	if !found {
		t.Error("担任演唱的声优应在艺术家列表中")
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
//
// 三个布尔锁并入 locked_json 后判定不变，键为 title / credit / album。
// 歌集名不在这条路径上：它归歌集，一处改动对该歌集全部曲目生效，
// 不该由某一首曲目的一次匹配悄悄触发。
func TestLockedFieldsSurviveMatch(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.CreateTrack(&Track{
		Title: "用户改过的曲名", Credit: ptr("艺术家"), Album: ptr("旧专辑"),
	})

	// 用户手动修改曲名 → 加锁
	if err := s.UpdateTrackFields(id, ptr("用户设定的曲名"), nil, true); err != nil {
		t.Fatalf("手动修改失败: %v", err)
	}

	// 匹配成功：未锁字段被上游规范值覆盖，已锁字段保持不变
	if err := s.UpdateBasicOnMatch(id, ptr("上游的曲名"), ptr("上游的艺术家")); err != nil {
		t.Fatalf("匹配更新失败: %v", err)
	}

	got, _ := s.GetTrack(id)
	if got.Title != "用户设定的曲名" {
		t.Errorf("曲名 = %q, 期望保持用户设定的值", got.Title)
	}
	if got.Credit == nil || *got.Credit != "艺术家" {
		t.Errorf("署名 = %v, 本地无绑定字段应保持用户值", got.Credit)
	}
	if !got.Locked["title"] {
		t.Error("曲名应仍处于加锁状态")
	}
	if !got.Locked["credit"] {
		t.Error("本地无绑定的署名应是用户值")
	}
	// 歌集名归歌集，一次曲目匹配不该动它
	if got.Album == nil || *got.Album != "旧专辑" {
		t.Errorf("歌集名 = %v, 期望不被曲目匹配改动", got.Album)
	}

	// 加锁的署名同样不被上游覆盖
	if err := s.UpdateTrackFields(id, nil, ptr("用户设定的署名"), true); err != nil {
		t.Fatalf("手动修改署名失败: %v", err)
	}
	if err := s.UpdateBasicOnMatch(id, nil, ptr("上游的另一个艺术家")); err != nil {
		t.Fatalf("匹配更新失败: %v", err)
	}
	got, _ = s.GetTrack(id)
	if got.Credit == nil || *got.Credit != "用户设定的署名" {
		t.Errorf("署名 = %v, 期望保持用户设定的值", got.Credit)
	}
}

// TestRepresentativeReleaseHonoursUserLadder 覆盖 4.8 的完整含义：
// 专辑视图选「代表 release」与匹配阶段不仅共用同一个函数，
// 也必须共用同一套**配置**。
//
// 这是一处真实缺陷的回归测试：此前专辑视图写死了 DefaultLadderConfig()，
// 用户把偏好国家改为 DE 后，匹配阶段按新配置选、而专辑视图仍按默认配置选，
// 两处给出不同的代表 release——函数共用了，配置却各走一套，漂移照样发生。
//
// 配置现在由 SetLadderConfig 注入 store，两处取的是同一份。
func TestRepresentativeReleaseHonoursUserLadder(t *testing.T) {
	s := openTestStore(t)

	// 同一发行组下两个发行：一个 JP 一个 DE，其余判据完全相同。
	// 两个都各入库一首，使它们都是这张专辑的成员。
	for _, tc := range []struct{ ref, country string }{
		{"rel-jp", "JP"},
		{"rel-de", "DE"},
	} {
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{
			Title: "一首歌-" + tc.ref, Album: ptr("同一张专辑"),
			MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-" + tc.ref),
			Release: &ReleaseInput{
				Provider: ptr("musicbrainz"), ProviderRef: ptr(tc.ref),
				ReleaseGroupRef: ptr("rg-1"),
				ReleaseStatus:   ptr("Official"), MediumFormat: ptr("CD"),
				Country: ptr(tc.country), FirstReleaseDate: ptr("2006-07-05"),
				TrackTotal: ptr(10),
			},
		}); err != nil {
			t.Fatalf("入库失败: %v", err)
		}
	}

	// 默认配置：JP 排在偏好列表前面 → 选 JP
	defaults := matching.DefaultLadderConfig()
	s.SetLadderConfig(defaults)
	albums, err := s.ListAlbums()
	if err != nil {
		t.Fatalf("查询专辑失败: %v", err)
	}
	if len(albums) != 1 || albums[0].RepresentativeRef == nil {
		t.Fatalf("专辑条目异常: %+v", albums)
	}
	if got := *albums[0].RepresentativeRef; got != "rel-jp" {
		t.Errorf("默认配置下代表 release = %q, 期望 rel-jp", got)
	}

	// 用户把偏好国家改为 DE：专辑视图必须随之改选
	custom := matching.LadderConfig{
		PreferredCountries: []string{"DE"},
		MediumPriority:     defaults.MediumPriority,
		DigitalFormats:     defaults.DigitalFormats,
	}
	s.SetLadderConfig(custom)
	albums, err = s.ListAlbums()
	if err != nil {
		t.Fatalf("查询专辑失败: %v", err)
	}
	if albums[0].RepresentativeRef == nil {
		t.Fatalf("专辑条目异常: %+v", albums)
	}
	if got := *albums[0].RepresentativeRef; got != "rel-de" {
		t.Errorf("用户偏好 DE 时代表 release = %q, 期望 rel-de——"+
			"专辑视图必须与匹配阶段共用同一套阶梯配置", got)
	}

	// 同一套配置下，两处（匹配阶段与专辑视图）必须选出同一个
	var forMatching []matchingCandidate
	for _, c := range []struct{ ref, country string }{{"rel-jp", "JP"}, {"rel-de", "DE"}} {
		forMatching = append(forMatching, toLadderCandidate(Release{
			Provider: ptr("musicbrainz"), ProviderRef: ptr(c.ref),
			ReleaseGroupRef: ptr("rg-1"), ReleaseStatus: ptr("Official"),
			MediumFormat: ptr("CD"), Country: ptr(c.country),
			FirstReleaseDate: ptr("2006-07-05"),
		}))
	}
	matchStage := custom.SelectRelease(forMatching)
	if matchStage == nil || matchStage.MatchRef != *albums[0].RepresentativeRef {
		t.Errorf("匹配阶段选出 %v，专辑视图选出 %q——两处应一致",
			matchStage, *albums[0].RepresentativeRef)
	}
}

// 阶梯的「有封面」对库内歌集指显式采用：旧版写 cover_path，新版写采用记录。
// 两者都要计入，否则升级后新采用的封面不再参与代表选择。
func TestRepresentativeReleaseCountsAdoptedCover(t *testing.T) {
	s := openTestStore(t)
	ids := map[string]int64{}
	for _, ref := range []string{"rel-a", "rel-b"} {
		if _, err := s.CreateTrackWithProvenance(NewTrackInput{
			Title: "一首歌-" + ref, Album: ptr("同一张专辑"),
			MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-" + ref),
			Release: &ReleaseInput{
				Provider: ptr("musicbrainz"), ProviderRef: ptr(ref), ReleaseGroupRef: ptr("rg-cover"),
				ReleaseStatus: ptr("Official"), MediumFormat: ptr("CD"),
				Country: ptr("JP"), FirstReleaseDate: ptr("2006-07-05"),
			},
		}); err != nil {
			t.Fatal(err)
		}
		release, err := s.GetReleaseByRef("musicbrainz", ref)
		if err != nil || release == nil {
			t.Fatal(release, err)
		}
		ids[ref] = release.ID
	}
	groupID := *mustRelease(t, s, ids["rel-a"]).ReleaseGroupID
	representative := func() string {
		t.Helper()
		rep, err := s.RepresentativeRelease(groupID)
		if err != nil || rep == nil {
			t.Fatal(rep, err)
		}
		return *rep.ProviderRef
	}
	if got := representative(); got != "rel-a" {
		t.Fatalf("其余判据相同时按标识排序，代表应为 rel-a，实际 %s", got)
	}
	catalog := mustSaveCatalog(t, s, CoverCatalogKey{Source: "local", ObjectType: "local-image", SourceRef: "adopted"}, sampleCoverImages())
	file := sampleCoverFile(catalog.Images[0].Variants[0].ID, ids["rel-b"], "albums/release-2/Front-local-250.jpg")
	if _, err := s.AdoptCoverFile(file, 0); err != nil {
		t.Fatal(err)
	}
	if got := representative(); got != "rel-b" {
		t.Fatalf("rel-b 已采用封面，代表应为 rel-b，实际 %s", got)
	}
}

func mustRelease(t *testing.T, s *Store, id int64) *Release {
	t.Helper()
	release, err := s.GetRelease(id)
	if err != nil || release == nil || release.ReleaseGroupID == nil {
		t.Fatalf("歌集读取失败: %+v %v", release, err)
	}
	return release
}

// TestRepresentativeSurvivesUnownedPick 守 mergeOneGroup 的一处回退。
//
// 代表 release 由优选阶梯在**整个发行组**里选（偏好实体载体），而列表的
// 成员只包含「用户有曲目的」那几条。当阶梯选中的那一条上一首都不剩时，
// 合并逻辑此前静默退回 members[0]——于是列表上的 RepresentativeRef
// 与 RepresentativeRelease 给出的不是同一条，而前者正是清单读取的依据。
//
// 触发它只需要普通操作：CD 版与数字版各入库一首，再把 CD 版那首删掉。
// CD 的 release 行作为清单缓存保留，阶梯仍然选它。
func TestRepresentativeSurvivesUnownedPick(t *testing.T) {
	s := openTestStore(t)

	groupID, err := s.UpsertReleaseGroup(&ReleaseGroup{
		Provider: ptr("musicbrainz"), ProviderRef: ptr("rg-x"), Title: "某专辑",
	})
	if err != nil {
		t.Fatalf("建发行组失败: %v", err)
	}

	mk := func(ref, format string, total int) int64 {
		id, err := s.UpsertRelease(&Release{
			ReleaseGroupID: &groupID,
			Provider:       ptr("musicbrainz"), ProviderRef: ptr(ref),
			Title:         "某专辑",
			MediumFormat:  ptr(format),
			ReleaseStatus: ptr("Official"),
			TrackTotal:    ptr(total),
		})
		if err != nil {
			t.Fatalf("建歌集 %s 失败: %v", ref, err)
		}
		return id
	}
	cdID := mk("rel-cd", "CD", 12)
	digitalID := mk("rel-digital", "Digital Media", 12)

	// 阶梯应当偏好实体载体
	rep, err := s.RepresentativeRelease(groupID)
	if err != nil || rep == nil {
		t.Fatalf("选代表失败: %v", err)
	}
	if rep.ID != cdID {
		t.Fatalf("阶梯应选 CD 版, 得到 release %d", rep.ID)
	}

	// 用户只有数字版那一首（CD 版那首已被删，release 行作为清单缓存保留）
	if _, err := s.CreateTrackWithProvenance(NewTrackInput{
		Title: "第一首", Album: ptr("某专辑"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-d1"),
		Release: &ReleaseInput{
			Provider: ptr("musicbrainz"), ProviderRef: ptr("rel-digital"),
			ReleaseGroupRef: ptr("rg-x"),
		},
	}); err != nil {
		t.Fatalf("入库失败: %v", err)
	}
	_ = digitalID

	albums, err := s.ListAlbums()
	if err != nil {
		t.Fatalf("查询专辑失败: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("专辑条目 = %d, 期望 1（同组合为一条）", len(albums))
	}
	if albums[0].RepresentativeRef == nil || *albums[0].RepresentativeRef != "rel-cd" {
		t.Errorf("代表 release = %v, 期望 rel-cd（与 RepresentativeRelease 一致）",
			albums[0].RepresentativeRef)
	}
	if albums[0].OwnedCount != 1 {
		t.Errorf("已入库 = %d, 期望 1", albums[0].OwnedCount)
	}
}
