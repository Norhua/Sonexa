package app

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/domain"
	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
	"github.com/norhua/sonexa/internal/store"
)

// fullCandidate 是一条带齐全部字段的候选，用来核对哪条入库路径漏写了哪一列。
func fullCandidate(ref string) provider.Candidate {
	s := func(v string) *string { return &v }
	i := func(v int) *int { return &v }
	return provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: ref,
		Title: s("God knows..."), ArtistName: s("平野綾"), AlbumTitle: s("涼宮ハルヒの詰合"), AlbumCredit: s("平野綾"),
		Labels:            []domain.Label{{Name: "Lantis", CatalogNumber: "LACM-4262"}},
		Media:             []domain.Medium{{Number: 1, TrackTotal: domain.Known(4), Format: domain.Known("CD")}},
		ReleaseTrackTotal: i(4), MediumTrackTotal: i(4),
		Identifiers: []domain.Identifier{{Kind: "isrc", Value: "JPB600600001"}},
		PrimaryType: s("Album"), DurationMS: i(272000), TrackNumber: i(2), TrackTotal: i(4),
		DiscNumber: i(1), Year: i(2006), MediumFormat: s("CD"), Country: s("JP"),
		ReleaseStatus: s("Official"), FirstReleaseDate: s("2006-07-05"),
		Label: s("Lantis"), CatalogNumber: s("LACM-4262"),
		ReleaseRef: s("rel-common"), ReleaseGroupRef: s("rg-1"), RecordingRef: s("rec-1"), ISRC: s("JPB600600001"),
	}
}

// TestAllAdmissionPathsWriteIdenticalTracks 覆盖用户要求 2：
// 导入自动入库、复核「接受」、搜索页「加入曲库」、专辑视图灰色行「加入曲库」
// 写出的曲目没有任何区别。
//
// 先前几处各写各的：导入多写了发行状态与碟号，搜索页少写了它们，
// 厂牌与目录号处处都没写——同一条候选从不同入口进来，库里长得不一样。
//
// 歌集升格为实体之后，比对的口径没变但落点变了：发行级事实
// （类型、载体、国家、厂牌、目录号、音轨数、封面）存在 release 上一份，
// 由 GetMetadata 的 join 带出。四条路径写出的曲目相同，
// 意味着它们挂上的歌集也带着同一组事实。
func TestAllAdmissionPathsWriteIdenticalTracks(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// 路径 A：搜索页加入曲库（经 CandidateRow 往返）
	rowA := rowFrom(matching.ScoredCandidate{Candidate: fullCandidate("a")})
	idA, err := app.AddToLibrary(rowA)
	if err != nil {
		t.Fatalf("搜索页入库失败: %v", err)
	}

	// 路径 B：复核队列接受
	itemID := enqueueOneItem(t, app, "God knows...", "表格里的署名", "表格里的歌集")
	rowB := rowFrom(matching.ScoredCandidate{Candidate: fullCandidate("b")})
	idB, err := app.AcceptCandidate(itemID, rowB)
	if err != nil {
		t.Fatalf("复核接受失败: %v", err)
	}

	// 路径 C：导入自动入库（唯一高置信）
	c := fullCandidate("c")
	runner := importer.NewRunner(app.store, matching.NewEngine(app.settings.Get().Matching),
		&recordingSearcher{hit: []provider.Candidate{c}}, provider.DefaultRetryPolicy(), false, nil, app.library)
	batchID, err := app.store.CreateBatch(&store.ImportBatch{Filename: "t.csv", TotalRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	title, artist := "God knows...", "平野綾"
	if err := app.store.EnqueueItems(batchID, []store.ImportItem{{RawRow: "{}", InTitle: &title, InArtist: &artist}}); err != nil {
		t.Fatal(err)
	}
	if err := runner.Start(context.Background(), batchID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "导入跑完", func() bool { return !runner.IsRunning(batchID) })
	idC, err := app.store.FindByMatchRef("musicbrainz", "c")
	if err != nil || idC == 0 {
		t.Fatalf("导入未入库: %v", err)
	}

	metaA, metaB, metaC := metaOf(t, app, idA), metaOf(t, app, idB), metaOf(t, app, idC)
	if !reflect.DeepEqual(metaA, metaB) {
		t.Errorf("搜索页与复核接受写出的丰富信息不同:\n A=%+v\n B=%+v", metaA, metaB)
	}
	if !reflect.DeepEqual(metaA, metaC) {
		t.Errorf("搜索页与导入自动入库写出的丰富信息不同:\n A=%+v\n C=%+v", metaA, metaC)
	}
	// 齐全的候选进来，齐全的列就该在。厂牌、发行状态与首次发行日期
	// 现在存在歌集上——断言它们，就是断言歌集侧真的被写了。
	if metaA.Label == nil || metaA.DiscNumber == nil || metaA.ReleaseStatus == nil || metaA.FirstReleaseDate == nil {
		t.Errorf("候选带的字段没写全: %+v", metaA)
	}
	// 歌集必须真的建出来：发行级事实只存在 release 上一份，
	// 曲目没有归属就意味着它们被丢掉了。（metaOf 抹掉了各路径必然不同的
	// 歌集行 id，所以这里直接读曲目。）
	for name, id := range map[string]int64{"搜索页": idA, "复核接受": idB, "导入": idC} {
		track, err := app.store.GetTrack(id)
		if err != nil || track == nil {
			t.Fatalf("读取 %s 入库的曲目失败: %v", name, err)
		}
		if track.ReleaseID == nil {
			t.Errorf("%s 入库的曲目没有挂上歌集——发行级事实无处可放", name)
		}
		if track.Album == nil || *track.Album != "涼宮ハルヒの詰合" {
			t.Errorf("%s 入库的曲目歌集名 = %v, 期望候选带的那个", name, track.Album)
		}
	}

	// 路径 D：灰色行必须从具体发行清单读取，保留位置及专辑事实。
	release, _ := app.store.GetReleaseByRef("musicbrainz", "rel-common")
	snapshot, _ := app.store.GetReleaseSnapshot(release.ID)
	snapshot.Tracklist = domain.Known([]domain.TracklistEntry{{DiscNumber: 1, TrackNumber: 3, Track: domain.Known(domain.Identity{Provider: provider.MusicBrainz, Ref: "d"}), Title: domain.Known("God knows..."), Credit: domain.Known(domain.Credit{Text: "平野綾"}), DurationMS: domain.Known(272000), Recording: domain.Known(domain.Identity{Provider: provider.MusicBrainz, Ref: "rec-1"})}})
	snapshot.TracklistCompleteness = domain.Complete
	rev, _ := app.store.ReleaseRevision(release.ID)
	if err := app.store.ApplyReleaseSnapshot(release.ID, &domain.Snapshot{Release: snapshot}, rev); err != nil {
		t.Fatal(err)
	}
	idD, err := app.AddGhostRowToLibrary(release.ID, 1, 3)
	if err != nil {
		t.Fatal(err)
	}
	metaD := metaOf(t, app, idD)
	if metaD.Title != metaA.Title || !reflect.DeepEqual(metaD.Credit, metaA.Credit) || !reflect.DeepEqual(metaD.DurationMS, metaA.DurationMS) || !reflect.DeepEqual(metaD.Label, metaA.Label) || metaD.TrackNumber == nil || *metaD.TrackNumber != 3 {
		t.Fatalf("灰色行丢失上下文: %+v", metaD)
	}
	trackD, _ := app.store.GetTrack(idD)
	stateD, _ := app.store.EnrichStateOf(idD)
	if trackD.ReleaseID == nil || *trackD.ReleaseID != release.ID || stateD != store.EnrichPending {
		t.Fatalf("灰色行状态=%+v", trackD)
	}

}

// TestAdmissionNeverBorrowsImportValues 覆盖用户要求 1：
// 上游没给的字段留空，不用表格里的原始输入去补。
func TestAdmissionNeverBorrowsImportValues(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	// 上游只给了曲名
	bare := provider.Candidate{Provider: provider.MusicBrainz, MatchRef: "bare"}
	bareTitle := "曲"
	bare.Title = &bareTitle

	// 复核队列：表格里写着署名与歌集
	itemID := enqueueOneItem(t, app, "曲", "表格署名", "表格歌集")
	id, err := app.AcceptCandidate(itemID, rowFrom(matching.ScoredCandidate{Candidate: bare}))
	if err != nil {
		t.Fatal(err)
	}
	track, _ := app.store.GetTrack(id)
	// 歌集名来自所属歌集：上游没给歌集，曲目就不该挂在任何歌集上
	if track.Credit != nil || track.Album != nil || track.ReleaseID != nil {
		t.Errorf("接受候选后署名/歌集 = %v/%v, 期望留空而不是拿表格值去补",
			deref(track.Credit), deref(track.Album))
	}
	meta := metaOf(t, app, id)
	if meta.ProvenanceJSON != nil && strings.Contains(*meta.ProvenanceJSON, "import") {
		t.Errorf("不该再有 import 来源的溯源: %s", *meta.ProvenanceJSON)
	}

	// 导入自动入库：同样不补
	runner := importer.NewRunner(app.store, matching.NewEngine(app.settings.Get().Matching),
		&recordingSearcher{hit: []provider.Candidate{{Provider: provider.MusicBrainz, MatchRef: "bare2", Title: &bareTitle}}},
		provider.DefaultRetryPolicy(), false, nil)
	batchID, _ := app.store.CreateBatch(&store.ImportBatch{Filename: "t.csv", TotalRows: 1})
	title, artist := "曲", "表格署名"
	_ = app.store.EnqueueItems(batchID, []store.ImportItem{{RawRow: "{}", InTitle: &title, InArtist: &artist}})
	_ = runner.Start(context.Background(), batchID)
	waitFor(t, "导入跑完", func() bool { return !runner.IsRunning(batchID) })
	if id2, _ := app.store.FindByMatchRef("musicbrainz", "bare2"); id2 != 0 {
		tr, _ := app.store.GetTrack(id2)
		if tr.Credit != nil {
			t.Errorf("导入入库后署名 = %q, 期望留空", *tr.Credit)
		}
	}
}

func metaOf(t *testing.T, app *App, id int64) store.TrackMetadata {
	t.Helper()
	m, err := app.store.GetMetadata(id)
	if err != nil || m == nil {
		t.Fatalf("读丰富信息失败: %v", err)
	}
	// 各路径的曲目 id、match_ref 与发行标识必然不同（候选各带各的），
	// 歌集行 id 随之不同；比对其余列。
	m.TrackID, m.MatchRef, m.ReleaseRef, m.ReleaseID = 0, nil, nil, nil
	return *m
}

func TestCandidateRowPreservesNewSnapshotFields(t *testing.T) {
	candidate := fullCandidate("rich")
	candidate.GroupTitle = strPtrOrNil("组标题")
	candidate.GroupCredit = strPtrOrNil("组署名")
	candidate.ReleaseIdentifiers = []domain.Identifier{{Kind: "barcode", Value: "123456"}, {Kind: "barcode", Value: "654321"}}
	row := rowFrom(matching.ScoredCandidate{Candidate: candidate})
	restored := row.toCandidate()
	if !reflect.DeepEqual(candidate, restored) {
		t.Fatalf("候选往返丢失字段:\n%+v\n%+v", candidate, restored)
	}
}
