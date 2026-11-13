package musicbrainz

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/norhua/sonexa/internal/config"
	"github.com/norhua/sonexa/internal/matching"
	"github.com/norhua/sonexa/internal/provider"
)

// 用户报告的那一行：完整曲名搜不到目标，只填 `星の在り処` 反而第一条就是。
var harmonicaQuery = provider.Query{
	Title:  "星の在り処 Harmonica short Ver.",
	Artist: "Falcom Sound Team J.D.K.",
	Album:  "オリジナルサウンドトラック 「英雄伝説 空の軌跡」",
}

// targetRecording 是目标录音，它在上游的名字是 `星の在り処 (Harmonica Short version)`。
const targetRecording = "b919f281-7f86-4be5-abb4-4cefef472040"

// TestVersionDescriptorIsNotRequiredPhrase 是这次修复的核心断言：
// 版本描述不得落在必须命中的短语里。
//
// `recording:` 只索引录音自己的名字，而目标录音名是
// `星の在り処 (Harmonica Short version)`——`Ver.` ≠ `version`，
// 完整曲名作为必须短语时这条录音在上游就被挡掉了。实测（真实 MB）：
//
//	recording:"星の在り処 Harmonica short Ver." AND (…)   → 2 条，没有目标
//	+recording:"星の在り処 Harmonica" +(…) …可选项…      → 6 条，目标在其中
func TestVersionDescriptorIsNotRequiredPhrase(t *testing.T) {
	got := buildLuceneQuery(harmonicaQuery)
	want := `+recording:"星の在り処 Harmonica"` +
		` +(release:"オリジナルサウンドトラック 「英雄伝説 空の軌跡」" OR artist:"Falcom Sound Team J.D.K.")` +
		` recording:"星の在り処 Harmonica short Ver."` +
		` recording:("short" "Ver.")`
	if got != want {
		t.Errorf("查询串 =\n  %s\n期望 =\n  %s", got, want)
	}
	for _, clause := range strings.Split(got, " +") {
		if strings.HasPrefix(strings.TrimPrefix(clause, "+"), `recording:"星の在り処 Harmonica short Ver."`) {
			t.Error("完整曲名不得作为必须项——版本描述的写法在录音名与曲目名之间并不一致")
		}
	}
}

// TestVersionRelaxedQueryShapes 覆盖放宽形态在各级阶梯下的拼法。
func TestVersionRelaxedQueryShapes(t *testing.T) {
	cases := []struct {
		name string
		q    provider.Query
		want string
	}{
		{
			"仅曲名一级不带锚点",
			provider.Query{Title: harmonicaQuery.Title, Artist: "A", Album: "Al", TitleOnly: true},
			`+recording:"星の在り処 Harmonica" recording:"星の在り処 Harmonica short Ver." recording:("short" "Ver.")`,
		},
		{
			"单个锚点不加括号",
			provider.Query{Title: "雪 (Live)", Album: "Al"},
			`+recording:"雪" +release:"Al" recording:"雪 \(Live\)" recording:("Live")`,
		},
		{
			// 裸 `That Girl` 实测有 2838 条录音：版本词必须留在查询里把那条 remix 排上来
			"括号里的版本段仍参与排序",
			provider.Query{Title: "That Girl (DJLIU Remix)", Artist: "Liu"},
			`+recording:"That Girl" +artist:"Liu" recording:"That Girl \(DJLIU Remix\)" recording:("DJLIU" "Remix")`,
		},
		{
			"版本词逐个加引号，运算符字样不被当成语法",
			provider.Query{Title: "Song (OR Remix)"},
			`+recording:"Song" recording:"Song \(OR Remix\)" recording:("OR" "Remix")`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildLuceneQuery(c.q); got != c.want {
				t.Errorf("查询串 =\n  %s\n期望 =\n  %s", got, c.want)
			}
		})
	}
}

// TestVersionRelaxedQueryCapsTerms 确保版本词有上限，理由同艺术家备选写法。
func TestVersionRelaxedQueryCapsTerms(t *testing.T) {
	got := buildLuceneQuery(provider.Query{Title: "Song (a b c d e f g h i Remix)"})
	group := got[strings.LastIndex(got, "recording:(")+len("recording:("):]
	if n := strings.Count(group, `"`) / 2; n > maxVersionTerms {
		t.Errorf("版本词 = %d 个, 上限应为 %d:\n  %s", n, maxVersionTerms, got)
	}
}

// TestPlainTitleQueryUnchanged 守住没有版本描述的曲名：查询串与此前逐字相同。
//
// 它同时是缓存键的一部分——这类行占绝大多数，不该因为这次修复全部重新出网。
func TestPlainTitleQueryUnchanged(t *testing.T) {
	cases := []struct {
		q    provider.Query
		want string
	}{
		{provider.Query{Title: "星の在り処", Artist: "A", Album: "Al"}, `recording:"星の在り処" AND (release:"Al" OR artist:"A")`},
		{provider.Query{Title: "Live and Let Die", Album: "Al"}, `recording:"Live and Let Die" AND release:"Al"`},
		{provider.Query{Title: "Tetra.", TitleOnly: true}, `recording:"Tetra."`},
	}
	for _, c := range cases {
		if got := buildLuceneQuery(c.q); got != c.want {
			t.Errorf("查询串 = %s, 期望 %s", got, c.want)
		}
	}
}

// TestFullTitleFindsTargetRecording 用真实响应回放整条检索路径。
//
// 夹具是放宽后的查询在真实 MusicBrainz 上的返回（6 条录音）。断言的是用户
// 关心的结论：填完整曲名，目标专辑上的那一条进了候选池并且排第一；
// 同一主干下的 `long Ver.` 是另一版录音，不得混进来。
func TestFullTitleFindsTargetRecording(t *testing.T) {
	fixture, err := os.ReadFile("testdata/search-version-descriptor.json")
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := buildLuceneQuery(harmonicaQuery)

	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		queries = append(queries, query)
		if query != wantQuery {
			// 旧形态的严格短语在真实上游返回不到目标录音；这里直接当作无结果
			w.Write([]byte(`{"count":0,"recordings":[]}`))
			return
		}
		w.Write(fixture)
	}))
	defer srv.Close()

	limiters := provider.NewLimiters()
	limiters.Configure(provider.MusicBrainz, 100, time.Second)
	p := New(provider.NewClient(limiters, nopCache{}, "ua"), Config{Contact: "c", ServerURL: srv.URL}, nil)

	res, err := matching.NewEngine(config.Defaults().Matching).Run(context.Background(), p, matching.Input{
		Title: harmonicaQuery.Title, Artist: harmonicaQuery.Artist, Album: harmonicaQuery.Album,
	})
	if err != nil {
		t.Fatalf("匹配失败: %v", err)
	}
	if len(queries) != 1 {
		t.Errorf("发出 %d 次查询, 期望 1 次: %q", len(queries), queries)
	}
	if len(res.Scored) == 0 {
		t.Fatal("候选池为空")
	}

	top := res.Scored[0]
	if album := deref(top.Candidate.AlbumTitle); album != harmonicaQuery.Album {
		t.Errorf("首位候选的专辑 = %q, 期望 %q", album, harmonicaQuery.Album)
	}
	if top.Score.TitleMatch != matching.MatchExact || top.Score.AlbumMatch != matching.MatchExact ||
		top.Score.CreditMatch != matching.MatchExact || top.Score.ReviewOnly {
		t.Errorf("首位候选应三项精确且不是仅供复核: %+v", top.Score)
	}
	// 目标录音在上游的名字与输入不同——这正是旧查询搜不到它的原因
	if rec := deref(top.Candidate.RecordingRef); rec != targetRecording {
		t.Errorf("首位候选的录音 = %q, 期望 %q", rec, targetRecording)
	}
	for _, sc := range res.Scored {
		if strings.Contains(strings.ToLower(deref(sc.Candidate.Title)), "long") {
			t.Errorf("另一版录音混进了候选池: %q", deref(sc.Candidate.Title))
		}
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
