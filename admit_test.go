package main

import (
	"context"
	"reflect"
	"testing"

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
		Title: s("God knows..."), ArtistName: s("平野綾"), AlbumTitle: s("涼宮ハルヒの詰合"),
		PrimaryType: s("Album"), DurationMS: i(272000), TrackNumber: i(2), TrackTotal: i(4),
		DiscNumber: i(1), Year: i(2006), MediumFormat: s("CD"), Country: s("JP"),
		ReleaseStatus: s("Official"), FirstReleaseDate: s("2006-07-05"),
		Label: s("Lantis"), CatalogNumber: s("LACM-4262"),
		ReleaseRef: s("rel-" + ref), ReleaseGroupRef: s("rg-1"), RecordingRef: s("rec-1"), ISRC: s("JPB600600001"),
	}
}

// TestAllAdmissionPathsWriteIdenticalTracks 覆盖用户要求 2：
// 导入自动入库、复核「接受」、搜索页「加入曲库」写出的曲目没有任何区别。
//
// 先前三处各写各的：导入多写了发行状态与碟号，搜索页少写了它们，
// 厂牌与目录号三处都没写——同一条候选从不同入口进来，库里长得不一样。
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
		&recordingSearcher{hit: []provider.Candidate{c}}, provider.DefaultRetryPolicy(), false, nil)
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
	// 齐全的候选进来，齐全的列就该在
	if metaA.Label == nil || metaA.DiscNumber == nil || metaA.ReleaseStatus == nil || metaA.FirstReleaseDate == nil {
		t.Errorf("候选带的字段没写全: %+v", metaA)
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
	if track.Artist != nil || track.Album != nil {
		t.Errorf("接受候选后署名/歌集 = %v/%v, 期望留空而不是拿表格值去补",
			deref(track.Artist), deref(track.Album))
	}
	meta := metaOf(t, app, id)
	if meta.ProvenanceJSON != nil {
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
		if tr.Artist != nil {
			t.Errorf("导入入库后署名 = %q, 期望留空", *tr.Artist)
		}
	}
}

func metaOf(t *testing.T, app *App, id int64) store.TrackMetadata {
	t.Helper()
	m, err := app.store.GetMetadata(id)
	if err != nil || m == nil {
		t.Fatalf("读丰富信息失败: %v", err)
	}
	// 三条路径的曲目 id、match_ref 与发行标识必然不同，比对其余列
	m.TrackID, m.MatchRef, m.ReleaseMBID, m.MatchedAt = 0, nil, nil, nil
	return *m
}
