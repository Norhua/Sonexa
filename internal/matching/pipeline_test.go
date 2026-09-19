package matching

import (
	"context"
	"errors"
	"testing"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/provider"
)

// stubSearcher 按查询曲名返回预设候选，并记录每次查询。
type stubSearcher struct {
	byTitle map[string][]provider.Candidate
	queries []provider.Query
	err     error
}

func (s *stubSearcher) SearchTracks(_ context.Context, q provider.Query) ([]provider.Candidate, error) {
	s.queries = append(s.queries, q)
	if s.err != nil {
		return nil, s.err
	}
	return s.byTitle[q.Title], nil
}

func highConfCandidate(ref, title, artist, album string, rg *string) provider.Candidate {
	return provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: ref,
		Title: sptr(title), ArtistName: sptr(artist), AlbumTitle: sptr(album),
		ReleaseGroupRef: rg, ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD"),
	}
}

func testEngine() *Engine {
	return NewEngine(config.Defaults().Matching)
}

// TestProgressiveStopsAtFirstHighConfidence 覆盖 11.4：
// 首个阶梯产出可用候选即停，多数行只消耗 1 次请求。
//
// 第一级查的是**主干**（`夕立`）而不是原串：上游的引号短语是子短语匹配，
// 主干的结果集是原串结果集的超集，多发一次原串只是浪费一个 1 req/s 的名额。
func TestProgressiveStopsAtFirstHighConfidence(t *testing.T) {
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{
		"夕立": {highConfCandidate("trk-1", "夕立", "3L", "专辑", sptr("rg-1"))},
	}}

	e := testEngine()
	res, err := e.Run(context.Background(), s, nil, Input{
		Title: "夕立（驟雨）", Artist: "3L", Album: "专辑",
	})
	if err != nil {
		t.Fatalf("匹配失败: %v", err)
	}

	if len(s.queries) != 1 {
		t.Errorf("发出 %d 次查询, 期望 1 次（首个变体高置信即停）", len(s.queries))
	}
	if res.Decision.Outcome != OutcomeImportUnique {
		t.Errorf("分流 = %q, 期望 import_unique", res.Decision.Outcome)
	}
	if len(res.TriedVariants) != 1 {
		t.Errorf("已尝试变体 = %v, 期望只有 1 个", res.TriedVariants)
	}
}

// TestProgressiveFallsThroughVariants 覆盖 11.4：
// 首个阶梯无结果时依次查询后续阶梯。
func TestProgressiveFallsThroughVariants(t *testing.T) {
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{
		// 主干那一级无结果，取内层（译名）那一级才命中
		"驟雨": {highConfCandidate("trk-1", "驟雨", "3L", "专辑", sptr("rg-1"))},
	}}

	e := testEngine()
	res, err := e.Run(context.Background(), s, nil, Input{
		Title: "夕立（驟雨）", Artist: "3L", Album: "专辑",
	})
	if err != nil {
		t.Fatalf("匹配失败: %v", err)
	}

	if len(s.queries) < 2 {
		t.Errorf("发出 %d 次查询, 期望至少 2 次（首个变体无结果应继续）", len(s.queries))
	}
	if res.Decision.Outcome != OutcomeImportUnique {
		t.Errorf("分流 = %q, 期望 import_unique", res.Decision.Outcome)
	}
	// 已尝试变体供复核队列显示
	if len(res.TriedVariants) < 2 {
		t.Errorf("已尝试变体 = %v, 期望记录多个", res.TriedVariants)
	}
}

// TestExhaustiveRunsAllVariants 覆盖 11.4：
// 设置切为全量后每行执行全部变体。
func TestExhaustiveRunsAllVariants(t *testing.T) {
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{
		"夕立（驟雨）": {highConfCandidate("trk-1", "夕立（驟雨）", "3L", "专辑", sptr("rg-1"))},
	}}

	m := config.Defaults().Matching
	m.VariantStrategy = config.VariantExhaustive
	e := NewEngine(m)

	if _, err := e.Run(context.Background(), s, nil, Input{
		Title: "夕立（驟雨）", Artist: "3L（スリーエル）", Album: "专辑",
	}); err != nil {
		t.Fatalf("匹配失败: %v", err)
	}

	if len(s.queries) < 3 {
		t.Errorf("全量策略发出 %d 次查询, 期望执行全部变体", len(s.queries))
	}
}

// TestPoolDedupesByMatchRefKeepingBestScore 覆盖 11.5：
// 同一候选在多个变体下出现时只出现一次，分值取最高。
func TestPoolDedupesByMatchRefKeepingBestScore(t *testing.T) {
	// 同一 match_ref 在两个变体下出现，第二个变体的匹配更好
	weak := provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: "trk-same",
		Title: sptr("夕立"), ArtistName: sptr("别的艺术家"), AlbumTitle: sptr("别的专辑"),
	}
	strong := provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: "trk-same",
		Title: sptr("夕立"), ArtistName: sptr("3L"), AlbumTitle: sptr("专辑"),
	}
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{
		"夕立（驟雨）": {weak},
		"夕立":     {strong},
	}}

	m := config.Defaults().Matching
	m.VariantStrategy = config.VariantExhaustive
	e := NewEngine(m)

	res, err := e.Run(context.Background(), s, nil, Input{
		Title: "夕立（驟雨）", Artist: "3L", Album: "专辑",
	})
	if err != nil {
		t.Fatalf("匹配失败: %v", err)
	}

	count := 0
	var found ScoredCandidate
	for _, sc := range res.Scored {
		if sc.Candidate.MatchRef == "trk-same" {
			count++
			found = sc
		}
	}
	if count != 1 {
		t.Fatalf("同一候选出现 %d 次, 期望去重为 1 次", count)
	}
	// 取跨变体最高分
	if !found.Score.HighConf {
		t.Errorf("应保留分值更高的那次评分, 得到 %+v", found.Score)
	}
	if found.MatchedVariant == "" {
		t.Error("应记录取得该分的变体")
	}
}

// TestFourWayDispatch 覆盖 11.6 的四分支分流。
func TestFourWayDispatch(t *testing.T) {
	rg1, rg2 := sptr("rg-1"), sptr("rg-2")

	t.Run("唯一高置信直接入库", func(t *testing.T) {
		s := &stubSearcher{byTitle: map[string][]provider.Candidate{
			"曲": {highConfCandidate("a", "曲", "艺术家", "专辑", rg1)},
		}}
		res, _ := testEngine().Run(context.Background(), s, nil,
			Input{Title: "曲", Artist: "艺术家", Album: "专辑"})
		if res.Decision.Outcome != OutcomeImportUnique {
			t.Errorf("分流 = %q", res.Decision.Outcome)
		}
		if res.Decision.Selected == nil {
			t.Error("应选出入库的候选")
		}
	})

	t.Run("同组多条经阶梯入库", func(t *testing.T) {
		cd := highConfCandidate("cd", "曲", "艺术家", "专辑", rg1)
		digital := highConfCandidate("digital", "曲", "艺术家", "专辑", rg1)
		digital.MediumFormat = sptr("Digital Media")

		s := &stubSearcher{byTitle: map[string][]provider.Candidate{
			"曲": {digital, cd},
		}}
		res, _ := testEngine().Run(context.Background(), s, nil,
			Input{Title: "曲", Artist: "艺术家", Album: "专辑"})

		if res.Decision.Outcome != OutcomeImportLadder {
			t.Errorf("分流 = %q, 期望 import_ladder", res.Decision.Outcome)
		}
		if res.Decision.Selected == nil || res.Decision.Selected.MatchRef != "cd" {
			t.Errorf("选中 %+v, 期望 CD 版", res.Decision.Selected)
		}
	})

	t.Run("跨组进复核", func(t *testing.T) {
		s := &stubSearcher{byTitle: map[string][]provider.Candidate{
			"曲": {
				highConfCandidate("a", "曲", "艺术家", "专辑", rg1),
				highConfCandidate("b", "曲", "艺术家", "专辑", rg2),
			},
		}}
		res, _ := testEngine().Run(context.Background(), s, nil,
			Input{Title: "曲", Artist: "艺术家", Album: "专辑"})

		if res.Decision.Outcome != OutcomeReviewCrossGroup {
			t.Errorf("分流 = %q, 期望 review_cross_group", res.Decision.Outcome)
		}
		if res.Decision.Selected != nil {
			t.Error("跨组时不应自动选择")
		}
	})

	t.Run("无结果进复核", func(t *testing.T) {
		s := &stubSearcher{byTitle: map[string][]provider.Candidate{}}
		res, _ := testEngine().Run(context.Background(), s, nil,
			Input{Title: "不存在的曲", Artist: "艺术家", Album: "专辑"})

		if res.Decision.Outcome != OutcomeReviewNoMatch {
			t.Errorf("分流 = %q, 期望 review_no_match", res.Decision.Outcome)
		}
	})

	t.Run("全部低置信进复核", func(t *testing.T) {
		// 曲名匹配但署名与歌集都只是模糊
		low := provider.Candidate{
			Provider: provider.MusicBrainz, MatchRef: "low",
			Title: sptr("曲"), ArtistName: sptr("艺术家 与其他人"), AlbumTitle: sptr("专辑 特别版"),
		}
		s := &stubSearcher{byTitle: map[string][]provider.Candidate{"曲": {low}}}
		res, _ := testEngine().Run(context.Background(), s, nil,
			Input{Title: "曲", Artist: "艺术家", Album: "专辑"})

		if res.Decision.Outcome != OutcomeReviewNoMatch {
			t.Errorf("分流 = %q, 期望 review_no_match", res.Decision.Outcome)
		}
	})
}

// TestTitleMismatchNeverEntersPool 覆盖 4.2 在管线中的表现。
func TestTitleMismatchNeverEntersPool(t *testing.T) {
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{
		"曲": {{
			Provider: provider.MusicBrainz, MatchRef: "unrelated",
			Title: sptr("完全不相关的歌"), ArtistName: sptr("艺术家"),
		}},
	}}
	res, _ := testEngine().Run(context.Background(), s, nil,
		Input{Title: "曲", Artist: "艺术家"})

	if len(res.Scored) != 0 {
		t.Errorf("候选池含 %d 条, 期望曲名不匹配的候选被丢弃", len(res.Scored))
	}
}

// TestSearchErrorPropagates 确保出网错误被传出，供上层退避重排。
func TestSearchErrorPropagates(t *testing.T) {
	wantErr := errors.New("503 Service Unavailable")
	s := &stubSearcher{err: wantErr}

	_, err := testEngine().Run(context.Background(), s, nil, Input{Title: "曲"})
	if !errors.Is(err, wantErr) {
		t.Errorf("错误 = %v, 期望传出原始错误", err)
	}
}

// TestLogLinesAreDistinguishable 覆盖 11.10 与 6.3：
// 六种分支提示文案各自可区分。
func TestLogLinesAreDistinguishable(t *testing.T) {
	in := Input{Title: "That Girl (DJLIU Remix)", Artist: "Liu", Album: "That Girl"}
	sc := &ScoredCandidate{
		Candidate: provider.Candidate{
			Title: sptr("That Girl"), ArtistName: sptr("Natalia Lesz"), AlbumTitle: sptr("That Girl"),
		},
		Score: Score{Total: 95},
	}

	unique := LogLine(OutcomeImportUnique, in, sc)
	ladder := LogLine(OutcomeImportLadder, in, sc)
	crossGroup := LogLine(OutcomeReviewCrossGroup, in, nil)
	noMatch := LogLine(OutcomeReviewNoMatch, in, nil)

	// 唯一高置信与经阶梯选出必须可区分
	if unique == ladder {
		t.Error("唯一高置信与经优选阶梯入库的提示应可区分")
	}
	if !containsSub(ladder, "实体release") {
		t.Errorf("经阶梯入库的提示应标出它是优选出的发行: %q", ladder)
	}
	if !containsSub(unique, "置信率") {
		t.Errorf("入库提示应含置信率: %q", unique)
	}
	if !containsSub(crossGroup, "[进入复核]") {
		t.Errorf("跨组提示 = %q", crossGroup)
	}
	if !containsSub(noMatch, "[匹配失败]") {
		t.Errorf("无结果提示 = %q", noMatch)
	}
	// 提示包含足以辨认该条目的输入信息
	for _, line := range []string{crossGroup, noMatch} {
		if !containsSub(line, "That Girl (DJLIU Remix)") {
			t.Errorf("提示应含可辨认的输入信息: %q", line)
		}
	}

	dup := DuplicateLogLine(in, "库中: That Girl · Liu")
	if !containsSub(dup, "输入信息") || !containsSub(dup, "当前曲目库信息") {
		t.Errorf("疑似重复提示应同时输出两边信息: %q", dup)
	}

	errLine := ErrorLogLine(errors.New("503"))
	if !containsSub(errLine, "[发生错误]") || !containsSub(errLine, "返回队列") {
		t.Errorf("错误提示 = %q", errLine)
	}
}

// TestSummaryLine 覆盖 11.10 的完成统计文案。
func TestSummaryLine(t *testing.T) {
	got := SummaryLine(180, 20, 10, 4, 5)
	for _, want := range []string{"本次导入 180 条", "进入复核 20 条", "未匹配 10 条", "失败 4 条"} {
		if !containsSub(got, want) {
			t.Errorf("统计文案缺少 %q: %q", want, got)
		}
	}
	if !containsSub(got, "不再返回队列") {
		t.Errorf("应说明失败条目不再自动返回队列: %q", got)
	}
}

// TestNoCorroborationNeverEntersPool 覆盖用户报告 1：
// 曲名相符但署名与歌集全不相干的候选不进候选池，该条目的结论是「无结果」。
//
// 阶梯也不能停在这一级：只过曲名那一道闸不算「有候选」，
// 否则更宽的一级永远试不到。
func TestNoCorroborationNeverEntersPool(t *testing.T) {
	// 真实返回的形态：43 条同名录音，没有一条的署名或歌集对得上
	sameName := []provider.Candidate{
		{Provider: provider.MusicBrainz, MatchRef: "a",
			Title: sptr("つばさ"), ArtistName: sptr("熊木杏里"), AlbumTitle: sptr("君の名前")},
		{Provider: provider.MusicBrainz, MatchRef: "b",
			Title: sptr("ツバサ"), ArtistName: sptr("アンダーグラフ"), AlbumTitle: sptr("UNDER GRAPH")},
	}
	s := &stubSearcher{byTitle: map[string][]provider.Candidate{"ツバサ": sameName}}

	res, err := testEngine().Run(context.Background(), s, nil, Input{
		Title: "ツバサ（羽翼）", Artist: "4円（ヨエン）", Album: "side-c",
	})
	if err != nil {
		t.Fatalf("匹配失败: %v", err)
	}

	if len(res.Scored) != 0 {
		t.Errorf("候选池含 %d 条, 期望一处旁证都没有的候选全部落闸", len(res.Scored))
	}
	if res.Decision.Outcome != OutcomeReviewNoMatch {
		t.Errorf("分流 = %q, 期望 review_no_match", res.Decision.Outcome)
	}
	// 阶梯必须走完：锚定级只过曲名那一道，不足以让它停下
	if len(s.queries) < 2 {
		t.Errorf("发出 %d 次查询, 期望继续试更宽的一级", len(s.queries))
	}
}
