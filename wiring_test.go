package main

// 本文件在 main 包内，因此能构造真实的 App 并调用真正的绑定方法。
//
// 放在 main 包是刻意的：此前把验收测试写在 internal/acceptance 里、
// 由测试自己手工调用 store 原语，结果测的是「零件能不能用」而不是
// 「应用有没有把它们接起来」——艺术家子系统整块没接进生产路径，
// 而测试全绿。测试必须打到真实的生产入口上。

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/provider/musicbrainz"
	"github.com/norhua/sonexa/internal/store"
)

// newTestApp 构造一个指向临时目录、且已配置联系方式的 App。
func newTestApp(t *testing.T) (*App, func()) {
	t.Helper()
	dir := t.TempDir()

	// 直接装配，绕开 os.UserConfigDir —— 测试不应写用户的真实配置目录
	app := NewApp("test")
	app.ctx = context.Background()

	paths := config.Paths{
		AppDir:   dir,
		DBPath:   filepath.Join(dir, "test.db"),
		CoverDir: filepath.Join(dir, "covers"),
	}
	if err := paths.Ensure(); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	app.paths = paths

	settings, err := config.NewStore(dir)
	if err != nil {
		t.Fatalf("载入设置失败: %v", err)
	}
	// 配置联系方式，使出网前置条件满足（本测试不出网，只是要过检查）
	cur := settings.Get()
	cur.MusicBrainz.Contact = "test@example.com"
	if err := settings.Save(cur); err != nil {
		t.Fatalf("保存设置失败: %v", err)
	}
	app.settings = settings

	st, err := store.Open(paths.DBPath)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	app.store = st

	app.limiters = provider.NewLimiters()
	app.rebuildProviders(settings.Get())
	app.coverSvc = newTestCoverService(t, paths.CoverDir, settings.Get())
	app.rebuildRunner(settings.Get())

	return app, func() { st.Close() }
}

// TestApplyDetailToTrackWiresArtistSubsystem 是审计发现的回归测试。
//
// 它调用**真正的生产方法** ApplyDetailToTrack，而不是手工调用 store 原语，
// 因此若该方法被跳过、或它的任一步骤被漏掉，本测试会失败。
func TestApplyDetailToTrackWiresArtistSubsystem(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// 用金样例详情响应（生产路径就是从这类响应解析的）
	body, err := os.ReadFile(filepath.Join("testdata", "golden",
		"responses", "musicbrainz", "release_detail.json"))
	if err != nil {
		t.Fatalf("读取金样例失败: %v", err)
	}
	detail, err := musicbrainz.ParseDetailJSON(body, "trk-cd-jp-02")
	if err != nil {
		t.Fatalf("解析详情失败: %v", err)
	}

	const credit = "涼宮ハルヒ(CV:平野綾)"
	trackID, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "God knows...", Artist: strPtrOrNil(credit),
		MatchSource: strPtrOrNil("musicbrainz"),
		MatchRef:    strPtrOrNil("aaaaaaaa-1111-2222-3333-444444444444"),
		Metadata: &store.MetadataInput{
			TrackCredit:      strPtrOrNil(credit),
			RecordingMBID:    strPtrOrNil("rec-0001-god-knows"),
			ReleaseMBID:      strPtrOrNil("rel-cd-jp"),
			ReleaseGroupMBID: strPtrOrNil("rg-haruhi"),
		},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	// 调真正的生产方法
	if err := app.ApplyDetailToTrack(context.Background(), trackID, "musicbrainz", detail); err != nil {
		t.Fatalf("ApplyDetailToTrack 失败: %v", err)
	}

	// 曲目级署名应已落库（供后续解析）
	meta, err := app.store.GetMetadata(trackID)
	if err != nil || meta == nil {
		t.Fatalf("读取丰富信息失败: %v", err)
	}
	if meta.CreditSpansJSON == nil {
		t.Fatal("credit_spans_json 未写入——可点击区间没有生成")
	}
	if meta.Label == nil || *meta.Label != "Lantis" {
		t.Errorf("厂牌 = %v, 期望 Lantis", meta.Label)
	}

	// 艺术家与关系应已建立，且两个名字都能检索到这首歌
	for _, q := range []string{"平野綾", "涼宮ハルヒ"} {
		hits, err := app.store.SearchTrackIDs(q, 10)
		if err != nil {
			t.Fatalf("检索 %q 失败: %v", q, err)
		}
		found := false
		for _, id := range hits {
			if id == trackID {
				found = true
			}
		}
		if !found {
			t.Errorf("检索 %q 未命中——艺术家关联没有真正落到索引上", q)
		}
	}

	// 作曲者来自 Work 层级，也应被写入并检索到
	if hits, _ := app.store.SearchTrackIDs("田中公平", 10); len(hits) == 0 {
		t.Error("检索作曲者未命中——Work 层级的作曲关系没有落到曲目上")
	}

	// GetTrackDetail 是前端的真实入口，它应能给出两段可点击区间与分组
	d, err := app.GetTrackDetail(trackID)
	if err != nil {
		t.Fatalf("GetTrackDetail 失败: %v", err)
	}
	if len(d.CreditSpans) != 2 {
		t.Errorf("可点击区间 = %d 段, 期望 2 段", len(d.CreditSpans))
	}
	for _, sp := range d.CreditSpans {
		for _, forbidden := range []string{"(", ")", "CV", ":"} {
			if len(sp.Text) > 0 && containsText(sp.Text, forbidden) {
				t.Errorf("可点击片段 %q 不应包含 %q", sp.Text, forbidden)
			}
		}
	}
	// 参与者区应含分组的作曲与作词，且不含「角色」分组
	var hasComposer, hasLyricist, hasCharacterGroup bool
	for _, g := range d.Participants {
		switch g.Role {
		case "composer":
			hasComposer = true
		case "lyricist":
			hasLyricist = true
		case "character":
			hasCharacterGroup = true
		}
	}
	if !hasComposer || !hasLyricist {
		t.Errorf("参与者分组缺少作曲或作词: %+v", d.Participants)
	}
	if hasCharacterGroup {
		t.Error("character 不应单独成组——它只出现在署名原串的可点击片段中")
	}

	// 艺术家视图：两个艺术家都应含这首歌
	artists, err := app.ListArtists()
	if err != nil {
		t.Fatalf("ListArtists 失败: %v", err)
	}
	for _, want := range []string{"涼宮ハルヒ", "平野綾"} {
		var id int64
		for _, a := range artists {
			if a.Name == want {
				id = a.ID
			}
		}
		if id == 0 {
			t.Errorf("未建立艺术家 %q——CV 拆分没有执行", want)
			continue
		}
		tracks, err := app.store.ListTracks(store.TrackFilter{ArtistID: id})
		if err != nil {
			t.Fatalf("查询 %q 的曲目失败: %v", want, err)
		}
		if len(tracks) == 0 {
			t.Errorf("%q 的艺术家视图为空", want)
		}
	}
}

func containsText(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
