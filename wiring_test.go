package main

// 本文件在 main 包内，因此能构造真实的 App 并调用真正的绑定方法。
//
// 放在 main 包是刻意的：此前把验收测试写在 internal/acceptance 里、
// 由测试自己手工调用 store 原语，结果测的是「零件能不能用」而不是
// 「应用有没有把它们接起来」——艺术家子系统整块没接进生产路径，
// 而测试全绿。测试必须打到真实的生产入口上。

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/covers"

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
	// 事件推送在测试里只记账：Wails 的 EventsEmit 在非生命周期 context 上
	// 会直接结束进程，任何走到推送的测试都会整包崩掉。
	app.emit = func(string, ...any) {}

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
	// 与生产同一条接线：降级链构造器必须挂上去。
	// 测试里另建一份不带构造器的服务，正是「封面全都加载不出来」这类 bug
	// 能在测试全绿的情况下发布的原因。
	app.coverSvc = app.attachLadderBuilder(newTestCoverService(t, paths.CoverDir, settings.Get()))
	app.rebuildRunner(settings.Get())
	// 同上：补全任务的四条依赖也走生产那一条接线
	app.attachEnrichWorker()

	return app, func() {
		app.enricher.Stop()
		st.Close()
	}
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

	// 署名原串就存在曲目的 credit 列上（合表后只此一份）；
	// 发行标识归歌集，因此要连歌集一起建出来——厂牌之类的发行级事实
	// 没有歌集就无处可写。
	const credit = "涼宮ハルヒ(CV:平野綾)"
	providerID := "musicbrainz"
	trackID, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "God knows...", Credit: strPtrOrNil(credit),
		Album: strPtrOrNil("涼宮ハルヒの詰合"), AlbumCredit: strPtrOrNil(credit),
		MatchSource: &providerID,
		MatchRef:    strPtrOrNil("aaaaaaaa-1111-2222-3333-444444444444"),
		Metadata: &store.MetadataInput{
			RecordingRef: strPtrOrNil("rec-0001-god-knows"),
		},
		Release: &store.ReleaseInput{
			Provider:        &providerID,
			ProviderRef:     strPtrOrNil("rel-cd-jp"),
			ReleaseGroupRef: strPtrOrNil("rg-haruhi"),
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
	// 署名分解落在 track_credit 表上（此前是 credit_spans_json 一列）：
	// 它既给出可点击区间，也回答「这位艺术家是否被署名」。
	entries, err := app.store.GetTrackCredits(trackID)
	if err != nil {
		t.Fatalf("读取署名分解失败: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("track_credit 未写入——可点击区间没有生成")
	}
	// 厂牌是发行级事实，存在歌集上，由 GetMetadata 的 join 带出
	if meta.Label == nil || *meta.Label != "Lantis" {
		t.Errorf("厂牌 = %v, 期望 Lantis——发行级字段没有落到歌集上", meta.Label)
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

	// GetTrackDetail 保留一个完整 CV 区间；参与者来自实际角色关系。
	d, err := app.GetTrackDetail(trackID)
	if err != nil {
		t.Fatalf("GetTrackDetail 失败: %v", err)
	}
	if len(d.CreditSpans) != 1 || d.CreditSpans[0].Text != credit {
		t.Errorf("CV 应保留一个完整署名区间: %+v", d.CreditSpans)
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

	// 艺术家视图：完整 CV 身份含这首歌，不再派生两个条目。
	artists, err := app.ListArtists()
	if err != nil {
		t.Fatalf("ListArtists 失败: %v", err)
	}
	// 未署名的作词作曲不进列表——这是用户报告「艺术家视图里出现一堆
	// 从没在署名里见过的名字」的那条。他们仍能被检索到（上面已断言），
	// 进入某位艺术家之后也仍展示全部角色：列表成员与视图内容是两件事。
	for _, unlisted := range []string{"田中公平", "森由里子", "須藤賢一"} {
		for _, a := range artists {
			if a.Name == unlisted {
				t.Errorf("艺术家列表出现了未署名的参与者 %q——列表成员判定应只认署名与演唱", unlisted)
			}
		}
	}
	for _, want := range []string{credit} {
		var id int64
		for _, a := range artists {
			if a.Name == want {
				id = a.ID
			}
		}
		if id == 0 {
			t.Errorf("未建立完整 CV 艺术家 %q", want)
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

// TestCoverSourceReachesFrontend 守住封面能显示这件事的**前半段**：
// 前端要拿到来源标识，才拼得出 /cover 地址。
//
// 这是用户报告「所有位置的专辑图片都无法加载」的一半成因：
// 资料库表格读的是 TrackView.CoverKey，而那个字段从声明出来就没有任何地方
// 给它赋过值，于是封面列永远走空态分支——一张图都不显示。
func TestCoverSourceReachesFrontend(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// 封面的来源标识（release / release group）现在归歌集：
	// 封面是歌集的属性，采用一次即对该歌集全部曲目生效。
	providerID := "musicbrainz"
	trackID, err := app.store.CreateTrackWithProvenance(store.NewTrackInput{
		Title:       "God knows...",
		Album:       strPtrOrNil("涼宮ハルヒの詰合"),
		MatchSource: &providerID,
		MatchRef:    strPtrOrNil("trk-cd-jp-02"),
		Release: &store.ReleaseInput{
			Provider:        &providerID,
			ProviderRef:     strPtrOrNil("rel-cd-jp"),
			ReleaseGroupRef: strPtrOrNil("rg-haruhi"),
		},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	// 列表：ListTracks 是曲目表格的真实入口
	page, err := app.ListTracks(store.TrackFilter{})
	if err != nil {
		t.Fatalf("ListTracks 失败: %v", err)
	}
	if len(page.Tracks) != 1 {
		t.Fatalf("曲目数 = %d, 期望 1", len(page.Tracks))
	}
	row := page.Tracks[0]
	if row.MatchSource == nil || row.ReleaseRef == nil {
		t.Fatalf("列表行缺少封面来源: matchSource=%v releaseRef=%v", row.MatchSource, row.ReleaseRef)
	}
	if *row.ReleaseRef != "rel-cd-jp" {
		t.Errorf("releaseRef = %q, 期望 rel-cd-jp", *row.ReleaseRef)
	}
	if row.ReleaseGroupRef == nil || *row.ReleaseGroupRef != "rg-haruhi" {
		t.Errorf("releaseGroupRef = %v, 期望 rg-haruhi——降级链最后一档要用它", row.ReleaseGroupRef)
	}

	// 详情抽屉：GetTrackDetail 是抽屉的真实入口
	detail, err := app.GetTrackDetail(trackID)
	if err != nil {
		t.Fatalf("GetTrackDetail 失败: %v", err)
	}
	if detail.CoverProvider != "musicbrainz" || detail.CoverRef != "rel-cd-jp" {
		t.Errorf("详情封面来源 = %q/%q, 期望 musicbrainz/rel-cd-jp",
			detail.CoverProvider, detail.CoverRef)
	}
	if detail.CoverGroupRef != "rg-haruhi" {
		t.Errorf("详情 releaseGroupRef = %q, 期望 rg-haruhi", detail.CoverGroupRef)
	}
}

// TestCoverHandlerNeedsNoRegistration 守住封面能显示这件事的**后半段**：
// handler 不再依赖「前端先调预取把降级链登记进来」。
//
// 这是另一半成因。此前 handler 只认登记过的缓存键，而详情抽屉与复核队列
// 从来不调预取，搜索页虽然调了却比 <img> 的请求晚到——三处都拿不到图。
//
// 断言分两步，都不出网：
//   - 降级链由 handler 就地构造，且指向该 release 在 Cover Art Archive 上的地址
//   - 预取暖好的字节能被后来的 <img> 请求命中，即两边算出的缓存键一致
func TestCoverHandlerNeedsNoRegistration(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	const releaseRef = "rel-cd-jp"
	urls := app.coverLadder("musicbrainz", releaseRef, "", strPtrOrNil("rg-haruhi"))
	if len(urls) == 0 {
		t.Fatal("降级链为空——handler 无处可取图")
	}
	if !strings.Contains(urls[0], releaseRef) {
		t.Errorf("降级链首档 = %q, 应指向该 release 的封面", urls[0])
	}

	// 预取算键用 BuildCoverRefs，取图算键在 handler 里。两边必须一致，
	// 否则预取白做：图会为同一行再下一遍。
	key := covers.CacheKey("musicbrainz:"+releaseRef, coverThumbSize)
	if err := app.coverSvc.Pool().Cache().Put(key, jpegHeader()); err != nil {
		t.Fatalf("预置缓存失败: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/cover?provider=musicbrainz&ref="+releaseRef+"&rg=rg-haruhi&size=250", nil)
	app.coverAssetHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200——handler 没有认出这个来源", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, 期望 image/jpeg", ct)
	}
}

// jpegHeader 是一段足以被识别为 JPEG 的字节。
func jpegHeader() []byte { return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10} }
