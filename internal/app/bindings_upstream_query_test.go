package app

import (
	"context"
	"github.com/norhua/sonexa/internal/domain"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// recordingSearcher 记下每次收到的查询，返回一条固定候选。
type recordingSearcher struct {
	provider.Provider
	queries []provider.Query
	hit     []provider.Candidate
}

func (r *recordingSearcher) SearchTracks(_ context.Context, q provider.Query) ([]provider.Candidate, error) {
	r.queries = append(r.queries, q)
	return r.hit, nil
}
func (r *recordingSearcher) IsConfigured() error { return nil }

// TestMatchUpstreamSharesImportTextProcessing 覆盖手动检索与导入匹配走同一套
// 文本处理：用户在复核队列里「改搜」时填的往往正是从表格里复制过来的原串
// （`原田瞳（原田ひとみ）`）。带着译名/读音注释直接做短语检索必然落空，
// 而括号里那种写法恰恰是上游用的那个——它必须作为备选写法进查询。
func TestMatchUpstreamSharesImportTextProcessing(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	rs := &recordingSearcher{}
	_, _, err := app.matchUpstream(context.Background(), rs, matching.Input{
		Title: "綿雪の夢（繁雪之梦）", Artist: "原田瞳（原田ひとみ）",
		Album: "心の羽根（ドラゴンボール改ver.）（心...", Country: "JP",
	})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(rs.queries) == 0 {
		t.Fatal("没有发出任何查询")
	}
	q := rs.queries[0]
	if q.Title != "綿雪の夢" {
		t.Errorf("曲名 = %q, 期望剥成主干 綿雪の夢", q.Title)
	}
	if q.Artist != "原田瞳" {
		t.Errorf("艺术家 = %q, 期望剥成主干 原田瞳", q.Artist)
	}
	if q.Album != "心の羽根" {
		t.Errorf("歌集 = %q, 期望去掉截断与注释后的 心の羽根", q.Album)
	}
	if len(q.ArtistAlternates) == 0 || q.ArtistAlternates[0] != "原田ひとみ" {
		t.Errorf("备选写法 = %v, 期望含 原田ひとみ", q.ArtistAlternates)
	}
	if q.Country != "JP" {
		t.Errorf("地区 = %q, 应原样传下去", q.Country)
	}
}

// TestMatchUpstreamKeepsVersionMarker 确保版本标记不被当成噪音剥掉：
// 它是这条录音身份的一部分，剥掉就换了一首歌。
func TestMatchUpstreamKeepsVersionMarker(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	rs := &recordingSearcher{}
	_, _, _ = app.matchUpstream(context.Background(), rs, matching.Input{
		Title: "That Girl (DJLIU Remix)", Artist: "Liu", Album: "That Girl",
	})
	if len(rs.queries) == 0 || !strings.Contains(rs.queries[0].Title, "Remix") {
		t.Errorf("查询 = %+v, 版本标记不得剥掉", rs.queries)
	}
}

// TestManualSearchGatesOnFilledFieldsOnly 覆盖「验证署名和歌集名，除非传入的是空值」：
// 同一上游的搜索与导入用同一道闸。用户只填曲名时同名候选都能看到；
// 一旦填了署名，署名对不上的就不摆出来。
func TestManualSearchGatesOnFilledFieldsOnly(t *testing.T) {
	app, cleanup := newTestApp(t)
	defer cleanup()

	sameName := []provider.Candidate{
		{Provider: provider.MusicBrainz, MatchRef: "a",
			Title: sptr("つばさ"), ArtistName: sptr("熊木杏里"), AlbumTitle: sptr("君の名前")},
		{Provider: provider.MusicBrainz, MatchRef: "b",
			Title: sptr("ツバサ"), ArtistName: sptr("アンダーグラフ"), AlbumTitle: sptr("UNDER GRAPH")},
	}

	onlyTitle, _, _ := app.matchUpstream(context.Background(), &recordingSearcher{hit: sameName},
		matching.Input{Title: "ツバサ"})
	if len(onlyTitle) != 2 {
		t.Errorf("只填曲名时得到 %d 条, 期望 2 条——留空的字段不参与闸门", len(onlyTitle))
	}

	withArtist, _, _ := app.matchUpstream(context.Background(), &recordingSearcher{hit: sameName},
		matching.Input{Title: "ツバサ", Artist: "4円"})
	if len(withArtist) != 0 {
		t.Errorf("填了署名后得到 %d 条, 期望 0 条——署名对不上的不摆出来", len(withArtist))
	}

	onlyArtist, _, _ := app.matchUpstream(context.Background(), &recordingSearcher{hit: sameName},
		matching.Input{Artist: "熊木杏里"})
	if len(onlyArtist) != 1 {
		t.Errorf("只填艺术家得到 %d 条, 期望 1 条——曲名留空时曲名闸不参与", len(onlyArtist))
	}
}

func sptr(s string) *string { return &s }

func (s *recordingSearcher) ID() provider.ProviderID { return provider.MusicBrainz }
func (s *recordingSearcher) SnapshotFromCandidate(c provider.Candidate) (*domain.Snapshot, error) {
	return provider.CandidateSnapshot(s.ID(), c)
}
func (s *recordingSearcher) LoadReleaseSummary(context.Context, domain.ReleaseIdentity) (*domain.ReleaseSummary, error) {
	return nil, provider.ErrUnsupported
}
