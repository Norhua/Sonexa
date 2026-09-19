package matching

import "testing"

// TestSearchCoreStripsExportNoise 覆盖检索主干的两层噪音剥离。
//
// 导出表格与上游之间隔着译名/读音注释与截断省略号；带着它们做短语检索
// 必然一无所获，而上游的引号短语是**子短语**匹配，用主干去查只会查到更多。
func TestSearchCoreStripsExportNoise(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"译名注释", "綿雪の夢（繁雪之梦）", "綿雪の夢"},
		{"读音注释", "3L（スリーエル）", "3L"},
		{"另一种写法", "原田瞳（原田ひとみ）", "原田瞳"},
		{"截断省略号", "冴えない彼女の育てかた 第4巻 特典C...", "冴えない彼女の育てかた 第4巻 特典C"},
		{"截断切在注释中间", "心の羽根（ドラゴンボール改ver.）（心...", "心の羽根"},
		{"无噪音时原样", "3253212", "3253212"},

		// 版本标记是这条录音身份的一部分，剥掉就换了一首歌——必须带着它去查，
		// 否则上游返回的几百条裸 That Girl 里根本没有那条 remix
		{"版本标记不剥", "That Girl (DJLIU Remix)", "That Girl (DJLIU Remix)"},
		{"off vocal 不剥", "Tetra. (off vocal)", "Tetra. (off vocal)"},

		// 整个字段就是一段括号内容时不能剥成空串
		{"全是括号内容", "（インスト）", "（インスト）"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SearchCore(c.in); got != c.want {
				t.Errorf("SearchCore(%q) = %q, 期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestIsTruncated 覆盖截断识别：截断的字段不该被要求「完全相等」——
// 它缺的那一截不是差异，是根本没给出来。
func TestIsTruncated(t *testing.T) {
	for _, s := range []string{"特典C...", "心の羽根（心…", "abc…"} {
		if !IsTruncated(s) {
			t.Errorf("IsTruncated(%q) = false, 期望 true", s)
		}
	}
	for _, s := range []string{"3253212", "Tetra.", "God knows..."[:6]} {
		if IsTruncated(s) {
			t.Errorf("IsTruncated(%q) = true, 期望 false", s)
		}
	}
}

// TestAlternatesExtractsOtherSpellings 覆盖「括号里装着另一种写法」。
func TestAlternatesExtractsOtherSpellings(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"原田瞳（原田ひとみ）", []string{"原田ひとみ"}},
		{"3L（スリーエル）", []string{"スリーエル"}},
		{"霞ヶ丘詩羽(CV.茅野愛衣)", []string{"茅野愛衣"}},
		{"Kirara Magic", nil},
		// 版本标记不是「另一种写法」
		{"That Girl (DJLIU Remix)", nil},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got := Alternates(c.in)
			if len(got) != len(c.want) {
				t.Fatalf("Alternates(%q) = %v, 期望 %v", c.in, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("Alternates(%q)[%d] = %q, 期望 %q", c.in, i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestCompareTitleRejectsVersionMismatch 是这次修复的核心断言：
// 版本标记不一致的两条录音，无论其余部分多像都不是同一首歌。
//
// 修复前用的是「词元包含」，`{tetra}` ⊆ `{tetra, offvocal}` 判成模糊匹配，
// 于是 Tetra. 被匹配到了 off vocal 那一版——这正是用户报告的「选错了版本」。
func TestCompareTitleRejectsVersionMismatch(t *testing.T) {
	w := DefaultWeights()
	cases := []struct {
		name             string
		query, candidate string
		want             MatchDegree
	}{
		{"同名同版本", "Tetra.", "Tetra.", MatchExact},
		{"off vocal 不是同一首", "Tetra.", "Tetra. (off vocal)", MatchNone},
		{"movie ver. 不是同一首（破折号写法）", "Tetra.", "Tetra. -movie ver.-", MatchNone},
		{"remix 不是原曲", "That Girl (DJLIU Remix)", "That Girl", MatchNone},
		{"原曲不是 remix", "That Girl", "That Girl (DJLIU Remix)", MatchNone},
		{"off vocal ver. 不是原曲", "世界中の雨（世间之雨）", "世界中の雨 (off vocal ver.)", MatchNone},

		// 译名/读音这类平台附加的注释可以剥
		{"译名可剥", "綿雪の夢（繁雪之梦）", "綿雪の夢", MatchExact},
		{"译名可剥（反向）", "ツバサ", "ツバサ（羽翼）", MatchExact},
		// feat. 标的是参与者不是版本：上游的录音名不含它
		{"feat. 不是版本标记", "By the Seaside (feat. Kitsui Akira)", "By the Seaside", MatchExact},
		// 括号里是上游用的那个写法（中文平台常写成「译名（原名）」）
		{"取内层写法", "夕立（驟雨）", "驟雨", MatchExact},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := w.CompareTitle(c.query, c.candidate); got != c.want {
				t.Errorf("CompareTitle(%q, %q) = %v, 期望 %v", c.query, c.candidate, got, c.want)
			}
		})
	}
}

// TestVersionMarkersFindsDashForm 确保版本标记不止在括号里找。
func TestVersionMarkersFindsDashForm(t *testing.T) {
	if len(VersionMarkers("Tetra. -movie ver.-")) == 0 {
		t.Error("破折号写法的版本标记未被识别")
	}
	if len(VersionMarkers("Tetra.")) != 0 {
		t.Error("干净曲名不该有版本标记")
	}
	// 词边界：ver 作为子串会命中 over / silver / never
	for _, s := range []string{"Never Gonna Give You Up", "Silver Bullet", "Discover"} {
		if len(VersionMarkers(s)) != 0 {
			t.Errorf("%q 被误判为含版本标记: %v", s, VersionMarkers(s))
		}
	}
}
