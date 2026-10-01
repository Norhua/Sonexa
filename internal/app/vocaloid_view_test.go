package app

import (
	"context"
	"testing"

	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// TestVocaloidAndCreditedComposerReachArtistView 覆盖用户第六轮报告的
// 前两个问题，走的是生产路径（applyDetailArtists）而不是直接调 store 原语。
//
// 场景：一首曲目署名只有 DECO*27（作词作曲），上游关系里初音ミク 担任演唱、
// 另有一位未署名的编曲者。
//
//	DECO*27   署名 → 应出现在艺术家列表
//	初音ミク   演唱 → 应出现（哪怕没有出现在署名串里）
//	未署名编曲 两条都不满足 → 不应出现
func TestVocaloidAndCreditedComposerReachArtistView(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	credit := "DECO*27"
	trackID, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "愛言葉", Credit: &credit,
		MatchSource: strPtrOrNil("musicbrainz"), MatchRef: strPtrOrNil("trk-aikotoba"),
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	// 上游详情：署名是 DECO*27，关系里有演唱（初音ミク）、作曲作词（DECO*27）、
	// 以及一位未署名的编曲者。
	detail := &provider.Detail{
		Provider:    provider.MusicBrainz,
		MatchRef:    "trk-aikotoba",
		TrackCredit: &credit,
		Artists: []provider.DetailArtist{
			{Name: "初音ミク", Role: "vocal", Type: strPtrOrNil("character")},
			{Name: "DECO*27", Role: "composer"},
			{Name: "DECO*27", Role: "lyricist"},
			{Name: "某位编曲者", Role: "arranger"},
		},
	}
	if err := app.ApplyDetailToTrack(context.Background(), trackID, "musicbrainz", detail); err != nil {
		t.Fatalf("应用详情失败: %v", err)
	}

	artists, err := app.store.ListArtists()
	if err != nil {
		t.Fatalf("查询艺术家列表失败: %v", err)
	}
	inList := map[string]bool{}
	for _, a := range artists {
		inList[a.Name] = true
	}

	if !inList["DECO*27"] {
		t.Error("署名的 DECO*27 应出现在艺术家列表中")
	}
	if !inList["初音ミク"] {
		t.Error("担任演唱的初音ミク应出现在艺术家列表中（问题 2）")
	}
	if inList["某位编曲者"] {
		t.Error("未署名的编曲者不应出现在艺术家列表中（问题 1）")
	}

	// 进入 DECO*27 的视图后，他的作曲作词关系仍在——
	// 列表成员判定与视图内容是两件事。
	var decoID int64
	for _, a := range artists {
		if a.Name == "DECO*27" {
			decoID = a.ID
		}
	}
	facets, err := app.store.RoleFacets(decoID)
	if err != nil {
		t.Fatalf("查询角色分面失败: %v", err)
	}
	byRole := map[string]int{}
	for _, f := range facets {
		byRole[f.Role] = f.Count
	}
	if byRole[string(store.RoleComposer)] != 1 {
		t.Errorf("DECO*27 的作曲分面 = %d, 期望 1", byRole[string(store.RoleComposer)])
	}
	if byRole[string(store.RoleLyricist)] != 1 {
		t.Errorf("DECO*27 的作词分面 = %d, 期望 1", byRole[string(store.RoleLyricist)])
	}
	// 他没有演唱这首歌：署名不等于演唱，这正是此前被写错的那一处
	if byRole[string(store.RoleVocal)] != 0 {
		t.Errorf("DECO*27 的演唱分面 = %d, 期望 0（他只是被署名，不是演唱者）",
			byRole[string(store.RoleVocal)])
	}
}
