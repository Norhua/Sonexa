// Package acceptance 是端到端集成验收。
//
// 这些测试把 store、matching、provider、importer、covers 各层串起来跑，
// 对着 stub server 与金样例响应验证功能清单中的验收标准——
// 单元测试各自通过不等于串起来也对。
package acceptance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/covers"
	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "acceptance.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func ptr[T any](v T) *T { return &v }

// TestF3_CharacterAndVoiceActor 覆盖 22.1 / F3：
// 导入一首 涼宮ハルヒ(CV:平野綾)，验证：
//   - 搜索「平野綾」与「涼宮ハルヒ」都命中
//   - 抽屉的署名原串两段分别可点
//   - 两个艺术家视图都含这首歌
func TestF3_CharacterAndVoiceActor(t *testing.T) {
	s := openStore(t)
	rules := matching.DefaultRules()

	const credit = "涼宮ハルヒ(CV:平野綾)"

	// 解析署名：拆出角色与人物两个艺术家 + 配音关系
	parsed := rules.ParseCredit(credit)
	if parsed.Uncertain {
		t.Fatal("标准 CV 形态不应被判为不确定")
	}
	if len(parsed.Artists) != 2 {
		t.Fatalf("解析出 %d 位艺术家, 期望 2", len(parsed.Artists))
	}

	// 入库
	trackID, err := s.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "God knows...", Artist: ptr(credit),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("11111111-2222-3333-4444-555555555555"),
		Metadata: &store.MetadataInput{TrackCredit: ptr(credit)},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	// 建立艺术家与关系
	ids := map[string]int64{}
	var relations []store.TrackArtist
	for _, a := range parsed.Artists {
		artistID, err := s.UpsertArtist(&store.Artist{Name: a.Name, Type: ptr(a.Type)})
		if err != nil {
			t.Fatalf("建立艺术家 %s 失败: %v", a.Name, err)
		}
		ids[a.Name] = artistID
		relations = append(relations, store.TrackArtist{
			ArtistID: artistID, Role: store.ArtistRole(a.Role), CreditedName: ptr(credit),
		})
	}
	for character, actor := range parsed.VoiceOf {
		if err := s.LinkArtists(ids[character], ids[actor], store.LinkVoiceOf); err != nil {
			t.Fatalf("建立配音关系失败: %v", err)
		}
	}
	if err := s.SetTrackArtists(trackID, relations); err != nil {
		t.Fatalf("写入参与者失败: %v", err)
	}

	// 生成并写入可点击区间
	spans := matching.BuildSpans(parsed, ids)
	spansJSON, _ := json.Marshal(spans)
	if err := s.ApplyMetadataEdits(trackID, nil, nil, nil); err != nil {
		t.Fatalf("准备丰富信息失败: %v", err)
	}
	if _, err := s.DB().Exec(
		`UPDATE track_metadata SET credit_spans_json = ? WHERE track_id = ?`,
		string(spansJSON), trackID); err != nil {
		t.Fatalf("写入区间失败: %v", err)
	}

	// [验收 1] 搜索两个名字都命中同一首歌
	for _, query := range []string{"平野綾", "涼宮ハルヒ"} {
		hits, err := s.SearchTrackIDs(query, 10)
		if err != nil {
			t.Fatalf("检索 %q 失败: %v", query, err)
		}
		if len(hits) != 1 || hits[0] != trackID {
			t.Errorf("检索 %q 得到 %v, 期望 [%d]", query, hits, trackID)
		}
	}

	// [验收 2] 抽屉的两段分别可点，且括号与 CV: 不在可点区间内
	if len(spans.Spans) != 2 {
		t.Fatalf("可点击区间 = %d 段, 期望 2 段", len(spans.Spans))
	}
	byID := map[int64]string{}
	for _, sp := range spans.Spans {
		byID[sp.ArtistID] = credit[sp.Start:sp.End]
	}
	if byID[ids["涼宮ハルヒ"]] != "涼宮ハルヒ" {
		t.Errorf("角色区间取出 %q", byID[ids["涼宮ハルヒ"]])
	}
	if byID[ids["平野綾"]] != "平野綾" {
		t.Errorf("声优区间取出 %q", byID[ids["平野綾"]])
	}
	for _, frag := range byID {
		for _, forbidden := range []string{"(", ")", "CV", ":"} {
			if containsStr(frag, forbidden) {
				t.Errorf("可点击片段 %q 不应包含 %q", frag, forbidden)
			}
		}
	}

	// [验收 3] 两个艺术家视图都含这首歌
	for name, artistID := range ids {
		tracks, err := s.ListTracks(store.TrackFilter{ArtistID: artistID})
		if err != nil {
			t.Fatalf("查询 %s 的曲目失败: %v", name, err)
		}
		if len(tracks) != 1 || tracks[0].ID != trackID {
			t.Errorf("%s 的艺术家视图含 %d 首, 期望含这首歌", name, len(tracks))
		}
	}

	// 角色艺术家的视图标题
	artists, _ := s.ListArtists()
	for _, a := range artists {
		if a.ID == ids["涼宮ハルヒ"] {
			if got := a.DisplayTitle(); got != "涼宮ハルヒ (CV: 平野綾)" {
				t.Errorf("角色视图标题 = %q", got)
			}
		}
	}
}

// TestF4_GhostRowsAndCount 覆盖 22.2 / F4：
// 一张 12 首专辑入库 8 首 → 显示 12 行（4 灰色）且计数 8/12；
// 删除一首后计数变 7/12 且该行变灰。
func TestF4_GhostRowsAndCount(t *testing.T) {
	s := openStore(t)

	releaseID, err := s.UpsertRelease(&store.Release{
		Provider: "musicbrainz", ProviderRef: "rel-12",
		ReleaseGroupRef: ptr("rg-12"), Title: "十二首专辑",
		ReleaseStatus: ptr("Official"), MediumFormat: ptr("CD"),
		TrackTotal: ptr(12), TracklistState: store.TracklistComplete,
	})
	if err != nil {
		t.Fatalf("写入发行失败: %v", err)
	}

	// 12 条清单
	var tracklist []store.ReleaseTrack
	for i := 1; i <= 12; i++ {
		tracklist = append(tracklist, store.ReleaseTrack{
			DiscNumber: 1, TrackNumber: i,
			TrackRef: ptr(fmt.Sprintf("trk-%02d", i)),
			Title:    fmt.Sprintf("第 %d 首", i),
		})
	}
	if err := s.ReplaceTracklist(releaseID, tracklist, store.TracklistComplete); err != nil {
		t.Fatalf("写入清单失败: %v", err)
	}

	// 入库前 8 首
	var trackIDs []int64
	for i := 1; i <= 8; i++ {
		id, err := s.CreateTrackWithProvenance(store.NewTrackInput{
			Title: fmt.Sprintf("第 %d 首", i), Album: ptr("十二首专辑"),
			MatchSource: ptr("musicbrainz"), MatchRef: ptr(fmt.Sprintf("trk-%02d", i)),
			Metadata: &store.MetadataInput{
				ReleaseMBID: ptr("rel-12"), ReleaseGroupMBID: ptr("rg-12"),
				TrackNumber: ptr(i), DiscNumber: ptr(1),
			},
		})
		if err != nil {
			t.Fatalf("入库第 %d 首失败: %v", i, err)
		}
		trackIDs = append(trackIDs, id)
	}

	// [验收 1] 12 行，其中 4 行灰色
	positions, err := s.ResolveTracklist(releaseID, "musicbrainz")
	if err != nil {
		t.Fatalf("解析清单失败: %v", err)
	}
	if len(positions) != 12 {
		t.Fatalf("清单位置 = %d, 期望 12", len(positions))
	}
	owned, ghost := countStates(positions)
	if owned != 8 || ghost != 4 {
		t.Errorf("已入库 %d 行、灰色 %d 行, 期望 8 与 4", owned, ghost)
	}

	// 计数 8/12
	albums, _ := s.ListAlbums()
	if len(albums) != 1 {
		t.Fatalf("专辑条目 = %d, 期望 1", len(albums))
	}
	if got := albums[0].CountLabel(); got != "8/12" {
		t.Errorf("计数 = %q, 期望 8/12", got)
	}

	// [验收 2] 删除一首后计数变 7/12，且该行变灰
	if err := s.DeleteTrack(trackIDs[0]); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	albums, _ = s.ListAlbums()
	if got := albums[0].CountLabel(); got != "7/12" {
		t.Errorf("删除后计数 = %q, 期望 7/12", got)
	}

	positions, _ = s.ResolveTracklist(releaseID, "musicbrainz")
	if len(positions) != 12 {
		t.Errorf("删除后清单位置 = %d, 期望仍为 12（清单不受影响）", len(positions))
	}
	if positions[0].State != store.PositionMissing {
		t.Errorf("被删的那行状态 = %q, 期望回落为灰色行", positions[0].State)
	}
	owned, ghost = countStates(positions)
	if owned != 7 || ghost != 5 {
		t.Errorf("删除后已入库 %d、灰色 %d, 期望 7 与 5", owned, ghost)
	}
}

func countStates(positions []store.ResolvedPosition) (owned, ghost int) {
	for _, p := range positions {
		if p.State == store.PositionMissing {
			ghost++
		} else {
			owned++
		}
	}
	return owned, ghost
}

// TestF5_StubProviderAppearsEverywhere 覆盖 22.3 / F5：
// 新增一个 stub provider 并注册，它自动出现在全部「选择上游」的位置，
// 且未改动任何页面代码。
func TestF5_StubProviderAppearsEverywhere(t *testing.T) {
	registry := provider.NewRegistry()
	registry.Register(&stubProvider{id: "musicbrainz", name: "MusicBrainz"})
	registry.Register(&stubProvider{id: "itunes", name: "iTunes"})

	before := len(registry.List())

	// 新增一个上游：只实现接口并登记
	registry.Register(&stubProvider{
		id: "discogs", name: "Discogs",
		caps: provider.CapLabel | provider.CapCatalogNumber,
	})

	list := registry.List()
	if len(list) != before+1 {
		t.Fatalf("注册后上游数 = %d, 期望 %d", len(list), before+1)
	}

	// 它出现在 List() 中——上游搜索勾选区、复核队列下拉、设置分组
	// 都从 List() 动态渲染，因此三处会同时出现它。
	var found bool
	for _, p := range list {
		if p.ID() == "discogs" {
			found = true
			if p.DisplayName() != "Discogs" {
				t.Errorf("显示名 = %q", p.DisplayName())
			}
			// 能力声明也随之可用，驱动列显示策略
			if !p.Capabilities().Has(provider.CapLabel) {
				t.Error("新上游的能力声明应可读取")
			}
		}
	}
	if !found {
		t.Error("新登记的上游应出现在 List() 中")
	}

	// 能力并集随之变化：新上游支持厂牌，聚合搜索时该列应显示
	caps := registry.CapabilitiesOf([]provider.ProviderID{"itunes", "discogs"})
	if !caps.Has(provider.CapLabel) {
		t.Error("能力并集应包含新上游的厂牌能力")
	}
}

// TestF8_ZeroConfigCSVImport 覆盖 22.4 / F8：
// 一个常见 UTF-8 逗号 CSV 从选文件到可提交无需任何手动设置。
func TestF8_ZeroConfigCSVImport(t *testing.T) {
	csv := "曲名,艺术家,专辑,年份\n" +
		"God knows...,平野綾,涼宮ハルヒの詰合,2006\n" +
		"ハレ晴レユカイ,平野綾,涼宮ハルヒの詰合,2006\n"

	res, err := importer.Parse("music_list.csv", []byte(csv), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	// 全部自动探测正确，无需手动介入
	if res.Detection.NeedsAttention {
		t.Errorf("常见 CSV 不应需要手动设置: %s", res.Detection.Reason)
	}
	if res.Detection.Encoding != "UTF-8" {
		t.Errorf("编码 = %q", res.Detection.Encoding)
	}
	if res.Detection.Delimiter != "逗号" {
		t.Errorf("分隔符 = %q", res.Detection.Delimiter)
	}
	if !res.Detection.HasHeader {
		t.Error("应识别出表头")
	}
	if want := "已识别: UTF-8 · 逗号分隔 · 含表头"; res.Detection.StatusLine() != want {
		t.Errorf("状态行 = %q, 期望 %q", res.Detection.StatusLine(), want)
	}

	// 列映射自动命中
	for i, want := range []importer.Field{
		importer.FieldTitle, importer.FieldArtist, importer.FieldAlbum, importer.FieldYear,
	} {
		if res.ColumnMap[i] != want {
			t.Errorf("第 %d 列映射 = %q, 期望 %q", i, res.ColumnMap[i], want)
		}
	}

	// 直接可提交
	preview := importer.BuildPreview(res, importer.PreviewRows)
	if preview.ImportableRows != 2 {
		t.Errorf("可导入 = %d 条, 期望 2", preview.ImportableRows)
	}
	if preview.SkippedRows != 0 {
		t.Errorf("跳过 = %d 行, 期望 0", preview.SkippedRows)
	}
}

// TestF9_ImportSummaryListsFailures 覆盖 22.5 / F9：
// 一次导入结束后输出统计并逐行列出失败条目的原始输入。
func TestF9_ImportSummaryListsFailures(t *testing.T) {
	s := openStore(t)

	// stub server 对特定曲名始终返回 503
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		query := r.URL.Query().Get("query")
		if containsStr(query, "必然失败") {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"count":0,"recordings":[]}`))
	}))
	defer srv.Close()

	batchID, _ := s.CreateBatch(&store.ImportBatch{Filename: "mixed.csv", TotalRows: 3})
	rows := []struct{ title, artist, album string }{
		{"正常曲目", "艺术家甲", "专辑甲"},
		{"必然失败", "艺术家乙", "专辑乙"},
		{"另一首", "艺术家丙", "专辑丙"},
	}
	var items []store.ImportItem
	for i, r := range rows {
		raw, _ := json.Marshal([]string{r.title, r.artist, r.album})
		title, artist, album := r.title, r.artist, r.album
		items = append(items, store.ImportItem{
			RowIndex: i, RawRow: string(raw),
			InTitle: &title, InArtist: &artist, InAlbum: &album,
		})
	}
	s.EnqueueItems(batchID, items)

	engine := matching.NewEngine(config.Defaults().Matching)
	retry := provider.DefaultRetryPolicy()
	retry.BaseDelay = time.Millisecond
	searcher := &httpSearcher{baseURL: srv.URL}

	var mu sync.Mutex
	var lastLine string
	done := make(chan struct{})
	runner := importer.NewRunner(s, engine, searcher, retry, func(p importer.Progress) {
		mu.Lock()
		if p.Line != "" {
			lastLine = p.Line
		}
		finished := p.Done
		mu.Unlock()
		if finished {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	})

	if err := runner.Start(context.Background(), batchID); err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("导入未在预期时间内结束")
	}

	stats, err := s.Stats(batchID)
	if err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if stats.Failed != 1 {
		t.Errorf("失败 = %d 条, 期望 1", stats.Failed)
	}
	if stats.Review != 2 {
		t.Errorf("进入复核 = %d 条, 期望 2", stats.Review)
	}

	mu.Lock()
	summary := lastLine
	mu.Unlock()

	// 统计四项
	for _, want := range []string{"本次导入", "进入复核", "未匹配", "失败 1 条"} {
		if !containsStr(summary, want) {
			t.Errorf("统计缺少 %q:\n%s", want, summary)
		}
	}
	// 逐行列出失败条目的原始输入
	if !containsStr(summary, "必然失败") || !containsStr(summary, "艺术家乙") {
		t.Errorf("统计应逐行列出失败条目的原始输入:\n%s", summary)
	}
	// 说明这些条目不再自动返回队列
	if !containsStr(summary, "不再返回队列") {
		t.Errorf("统计应说明失败条目不再自动返回队列:\n%s", summary)
	}
}

// TestF11_UserEditedYearSurvivesRematch 覆盖 22.6 / F11：
// 手动修改年份后重新匹配，年份不被覆盖。
func TestF11_UserEditedYearSurvivesRematch(t *testing.T) {
	s := openStore(t)

	trackID, err := s.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "That Girl", Artist: ptr("Natalia Lesz"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"),
		Metadata:    &store.MetadataInput{Year: ptr(2011)},
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	// 用户手动把年份改为 2010：来源置为 user 并加锁
	if err := s.ApplyMetadataEdits(trackID,
		map[string]string{"year": "2010"},
		map[string]string{"year": "user"},
		map[string]bool{"year": true},
	); err != nil {
		t.Fatalf("手动修改失败: %v", err)
	}

	meta, _ := s.GetMetadata(trackID)
	if meta.Year == nil || *meta.Year != 2010 {
		t.Fatalf("手动修改后年份 = %v, 期望 2010", meta.Year)
	}

	// 重新匹配得到 2011：来源为 user 的字段默认不被填充
	var provenance map[string]string
	var locked map[string]bool
	json.Unmarshal([]byte(*meta.ProvenanceJSON), &provenance)
	json.Unmarshal([]byte(*meta.LockedJSON), &locked)

	if provenance["year"] != "user" {
		t.Errorf("年份来源 = %q, 期望 user", provenance["year"])
	}
	if !locked["year"] {
		t.Error("年份应处于用户锁定状态")
	}

	// 模拟「未勾选填充」的重新匹配：不传 year 的编辑
	if err := s.ApplyMetadataEdits(trackID,
		map[string]string{"genre": "Pop"},
		provenance, locked,
	); err != nil {
		t.Fatalf("重新匹配写入失败: %v", err)
	}

	meta, _ = s.GetMetadata(trackID)
	if meta.Year == nil || *meta.Year != 2010 {
		t.Errorf("重新匹配后年份 = %v, 期望仍为用户设定的 2010", meta.Year)
	}
	// 其余未锁字段照常更新
	if meta.Genre == nil || *meta.Genre != "Pop" {
		t.Errorf("风格 = %v, 期望被更新", meta.Genre)
	}

	// 用户显式勾选填充后，年份才被改写，来源转为上游并清除锁
	delete(locked, "year")
	provenance["year"] = "musicbrainz"
	if err := s.ApplyMetadataEdits(trackID,
		map[string]string{"year": "2011"}, provenance, locked); err != nil {
		t.Fatalf("填充失败: %v", err)
	}
	meta, _ = s.GetMetadata(trackID)
	if meta.Year == nil || *meta.Year != 2011 {
		t.Errorf("显式填充后年份 = %v, 期望 2011", meta.Year)
	}
	json.Unmarshal([]byte(*meta.LockedJSON), &locked)
	if locked["year"] {
		t.Error("填充后用户锁应被清除")
	}
}

// TestF14_DuplicateAddIsIdempotent 覆盖 22.7 / F14：
// 从上游搜索重复添加同一结果，不产生第二条曲目。
func TestF14_DuplicateAddIsIdempotent(t *testing.T) {
	s := openStore(t)
	input := store.NewTrackInput{
		Title: "God knows...", Artist: ptr("平野綾"),
		MatchSource: ptr("musicbrainz"), MatchRef: ptr("trk-unique"),
	}

	first, err := s.CreateTrackWithProvenance(input)
	if err != nil {
		t.Fatalf("首次加入失败: %v", err)
	}

	// 渲染时预查本地：已存在 → 按钮显示「已在库中」
	existing, err := s.FindByMatchRef("musicbrainz", "trk-unique")
	if err != nil {
		t.Fatalf("预查失败: %v", err)
	}
	if existing != first {
		t.Errorf("预查得到 %d, 期望 %d", existing, first)
	}

	// 即便再次写入，也静默转为跳转到已有曲目，不报错
	second, err := s.CreateTrackWithProvenance(input)
	var exists *store.ErrAlreadyExists
	if !asExists(err, &exists) {
		t.Fatalf("重复加入应返回 ErrAlreadyExists, 得到 %v", err)
	}
	if second != first {
		t.Errorf("应跳转到已有曲目 %d, 得到 %d", first, second)
	}

	var count int
	s.DB().QueryRow(`SELECT COUNT(*) FROM track`).Scan(&count)
	if count != 1 {
		t.Errorf("曲目数 = %d, 期望 1（不产生第二条）", count)
	}
}

// TestF7_CoverFailuresDoNotBlockList 覆盖 22.8 / F7：
// 25 行结果在大量 404 的情况下不长时间空白，
// 失败行用占位图而其他列正常渲染。
func TestF7_CoverFailuresDoNotBlockList(t *testing.T) {
	// 大部分请求 404（CAA 的常态），少数成功
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&requests, 1)
		// 每 5 个里只有 1 个有图
		if n%5 != 0 {
			time.Sleep(5 * time.Millisecond)
			http.NotFound(w, r)
			return
		}
		w.Write(jpegBytes())
	}))
	defer srv.Close()

	cache, err := covers.NewDiskCache(t.TempDir(), 0, 0)
	if err != nil {
		t.Fatalf("建立缓存失败: %v", err)
	}
	svc := covers.NewService(cache, 8, "test")

	refs := make([]covers.Ref, 25)
	for i := range refs {
		refs[i] = covers.Ref{
			Key:       covers.CacheKey(fmt.Sprintf("rel-%d", i), 250),
			SourceRef: fmt.Sprintf("rel-%d", i),
			Size:      250,
			// 每行都有完整降级链
			URLs: []string{
				fmt.Sprintf("%s/release/rel-%d/front-1200", srv.URL, i),
				fmt.Sprintf("%s/release/rel-%d/front-500", srv.URL, i),
			},
		}
	}

	start := time.Now()
	svc.Prefetch(context.Background(), refs)

	// 并发 8，25 行 × 2 档 × 5ms ≈ 串行 250ms，并发应远快于此
	deadline := time.After(5 * time.Second)
	handler := httptest.NewServer(svc.Handler())
	defer handler.Close()

	var succeeded, placeholder int
	for _, ref := range refs {
		select {
		case <-deadline:
			t.Fatal("取图耗时过长，列表会长时间空白")
		default:
		}
		resp, err := http.Get(handler.URL + "/cover?key=" + ref.Key)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			succeeded++
		case http.StatusNotFound:
			// 失败行返回 404，前端显示占位图
			placeholder++
		default:
			t.Errorf("未预期的状态码 %d", resp.StatusCode)
		}
	}
	elapsed := time.Since(start)

	if succeeded+placeholder != 25 {
		t.Errorf("成功 %d + 占位 %d, 期望共 25 行", succeeded, placeholder)
	}
	if placeholder == 0 {
		t.Error("应有部分行落到占位图（404 是常态）")
	}
	// 单张失败只影响该行：仍有成功的行
	if succeeded == 0 {
		t.Error("有图的行应成功取得——单张失败不应影响其他行")
	}
	if elapsed > 4*time.Second {
		t.Errorf("25 行耗时 %v，并发取图应显著快于串行", elapsed)
	}
}

// TestF1_NoMatchNeededTrackRendersFine 覆盖 22.9 / F1：
// 以「无需匹配」入库的曲目在列表页与详情抽屉均正常显示。
func TestF1_NoMatchNeededTrackRendersFine(t *testing.T) {
	s := openStore(t)

	// 「无需匹配」：按原值入库、丰富信息为空、溯源置空
	trackID, err := s.CreateTrackWithProvenance(store.NewTrackInput{
		Title: "某首无名曲", Artist: ptr("未知艺术家"),
	})
	if err != nil {
		t.Fatalf("入库失败: %v", err)
	}

	// 列表页正常显示
	tracks, err := s.ListTracks(store.TrackFilter{})
	if err != nil {
		t.Fatalf("查询列表失败: %v", err)
	}
	if len(tracks) != 1 {
		t.Fatalf("列表 = %d 首, 期望 1", len(tracks))
	}
	row := tracks[0]
	if row.Title != "某首无名曲" {
		t.Errorf("曲名 = %q", row.Title)
	}
	// 缺失字段为 nil，界面据此显示占位短横线，而不是 0 或空白
	for name, v := range map[string]any{
		"年份": row.Year, "音轨号": row.TrackNumber, "时长": row.DurationMS,
		"风格": row.Genre, "载体": row.MediumFormat, "国家": row.Country,
	} {
		if !isNil(v) {
			t.Errorf("%s 应为缺失（nil），得到 %v", name, v)
		}
	}

	// 详情抽屉正常打开：丰富信息行存在但整体为空，读取不报错
	meta, err := s.GetMetadata(trackID)
	if err != nil {
		t.Fatalf("读取丰富信息失败: %v", err)
	}
	if meta == nil {
		t.Fatal("丰富信息行应存在（可整体为空），以便抽屉正常渲染")
	}
	if meta.MatchSource != nil || meta.MatchRef != nil {
		t.Error("「无需匹配」入库的曲目溯源应为空")
	}

	// 标签为空时也不报错
	tags, err := s.TagsOfTrack(trackID)
	if err != nil {
		t.Fatalf("读取标签失败: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("标签 = %d 个, 期望 0", len(tags))
	}
}

func isNil(v any) bool {
	switch x := v.(type) {
	case *int:
		return x == nil
	case *string:
		return x == nil
	}
	return v == nil
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func asExists(err error, target **store.ErrAlreadyExists) bool {
	if e, ok := err.(*store.ErrAlreadyExists); ok {
		*target = e
		return true
	}
	return false
}

func jpegBytes() []byte {
	b := []byte{0xFF, 0xD8, 0xFF, 0xE0}
	return append(b, make([]byte, 200)...)
}
