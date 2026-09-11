package matching

import "testing"

// classifyOne 分类串中的第一个括号段，便于表驱动测试。
func classifyOne(t *testing.T, r *Rules, s string) Classification {
	t.Helper()
	segs := FindBracketSegments(s)
	if len(segs) == 0 {
		t.Fatalf("%q 中未找到括号段", s)
	}
	return r.Classify(segs[0])
}

// TestClassifyGolden 覆盖 3.3 的四分类与判定顺序。
func TestClassifyGolden(t *testing.T) {
	r := DefaultRules()
	cases := []struct {
		name string
		in   string
		want BracketKind
	}{
		// [1] 角色与声优
		{"CV 标准形态", "涼宮ハルヒ(CV:平野綾)", KindCV},
		{"CV 全角括号", "涼宮ハルヒ（CV：平野綾）", KindCV},
		{"声優 前缀", "涼宮ハルヒ(声優:平野綾)", KindCV},

		// [2] 版本标识——必须保留
		{"Remix 不被剥离", "That Girl (DJLIU Remix)", KindVersion},
		{"Live 版本", "God knows... (Live)", KindVersion},
		{"TV Size", "ハレ晴レユカイ (TV Size)", KindVersion},
		{"Instrumental", "God knows... (Instrumental)", KindVersion},
		{"Off Vocal", "残酷な天使のテーゼ (Off Vocal)", KindVersion},
		{"カラオケ", "残酷な天使のテーゼ (カラオケ)", KindVersion},

		// 附加艺术家：从曲名剥离并并入艺术家
		{"feat. 并入艺术家", "Song (feat. Someone)", KindFeature},
		{"ft. 并入艺术家", "Song (ft. Someone)", KindFeature},

		// [3] 翻译提示词
		{"翻译提示词", "夕立（中文名：驟雨）", KindTranslation},
		{"Romaji 提示词", "残酷な天使のテーゼ (Romaji: Zankoku na Tenshi no These)", KindTranslation},

		// 歧义消解
		{"国家码歧义消解", "Nirvana (US)", KindDisambiguation},

		// [4] 文字系统差异
		{"日文外层 + 汉字内层生成变体", "夕立（驟雨）", KindTranslation},
		{"拉丁外层 + 假名内层", "3L（スリーエル）", KindTranslation},

		// [5] 保守兜底
		{"无法分类时兜底", "Song (XYZQ)", KindFallback},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := classifyOne(t, r, c.in)
			if got.Kind != c.want {
				t.Errorf("Classify(%q) = %q, 期望 %q（内层 %q）", c.in, got.Kind, c.want, got.Segment.Inner)
			}
		})
	}
}

// TestVersionKeywordBeatsScriptDifference 覆盖 spec 的
// 「版本关键词优先于文字系统差异」：
// 内层为拉丁字母版本关键词、外层为日文时按版本标识处理，不生成剥离变体。
func TestVersionKeywordBeatsScriptDifference(t *testing.T) {
	r := DefaultRules()
	got := classifyOne(t, r, "ハレ晴レユカイ (Extended Mix)")
	if got.Kind != KindVersion {
		t.Errorf("分类 = %q, 期望 version——版本关键词必须先于文字系统差异生效", got.Kind)
	}
}

// TestThatGirlNotStripped 是 spec 反复强调的反例：
// 盲目剥离会把 That Girl (DJLIU Remix) 搜成 That Girl，匹配到完全不相关的歌。
func TestThatGirlNotStripped(t *testing.T) {
	r := DefaultRules()
	variants := r.GenerateVariants("That Girl (DJLIU Remix)", "Liu")

	for _, v := range variants {
		if v.Title == "That Girl" {
			t.Fatalf("版本标识被剥离，产生了裸曲名变体: %+v", variants)
		}
	}
	if len(variants) != 1 {
		t.Errorf("版本标识不应生成变体, 得到 %d 个: %+v", len(variants), variants)
	}
	if variants[0].Title != "That Girl (DJLIU Remix)" {
		t.Errorf("曲名 = %q, 期望保留括号", variants[0].Title)
	}
}

// TestTranslationGeneratesVariants 覆盖 spec 的「翻译异写生成变体」：
// 夕立（驟雨）生成含 夕立 与 驟雨 的查询变体。
func TestTranslationGeneratesVariants(t *testing.T) {
	r := DefaultRules()
	variants := r.GenerateVariants("夕立（驟雨）", "")

	titles := map[string]bool{}
	for _, v := range variants {
		titles[v.Title] = true
	}
	for _, want := range []string{"夕立（驟雨）", "夕立", "驟雨"} {
		if !titles[want] {
			t.Errorf("变体中缺少 %q, 实际为 %v", want, titles)
		}
	}
}

// TestDominantScript 覆盖文字系统判定，含「日文汉字与中文汉字同属 CJK 区」
// 需靠外层是否含假名辅助判定这一点。
func TestDominantScript(t *testing.T) {
	cases := []struct {
		in   string
		want Script
	}{
		{"That Girl", ScriptLatin},
		{"ハルヒ", ScriptKana},
		{"涼宮ハルヒ", ScriptKana}, // 含假名即判为日文，尽管汉字更多
		{"驟雨", ScriptHan},
		{"안녕", ScriptHangul},
		{"Привет", ScriptCyrillic},
		{"", ScriptUnknown},
		{"123", ScriptUnknown},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := DominantScript(c.in); got != c.want {
				t.Errorf("DominantScript(%q) = %q, 期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestBracketFormsCovered 覆盖 3.2：各括号形态与分隔符同等适用。
func TestBracketFormsCovered(t *testing.T) {
	r := DefaultRules()
	forms := []string{
		"涼宮ハルヒ(CV:平野綾)",
		"涼宮ハルヒ（CV：平野綾）",
		"涼宮ハルヒ[CV:平野綾]",
		"涼宮ハルヒ［CV：平野綾］",
		"涼宮ハルヒ【CV:平野綾】",
		"涼宮ハルヒ(CV.平野綾)",
		"涼宮ハルヒ（CV。平野綾）",
		"涼宮ハルヒ(cv:平野綾)",
		"涼宮ハルヒ(ＣＶ:平野綾)",
		"涼宮ハルヒ(C.V.平野綾)",
		"涼宮ハルヒ(配音:平野綾)",
		"涼宮ハルヒ(Vo.平野綾)",
	}
	for _, form := range forms {
		t.Run(form, func(t *testing.T) {
			got := classifyOne(t, r, form)
			if got.Kind != KindCV {
				t.Errorf("%q 分类 = %q, 期望 cv", form, got.Kind)
			}
			if got.Payload != "平野綾" {
				t.Errorf("%q 提取出 %q, 期望 平野綾", form, got.Payload)
			}
		})
	}
}

// TestFindBracketSegments 验证段的位置与内容。
func TestFindBracketSegments(t *testing.T) {
	segs := FindBracketSegments("涼宮ハルヒ(CV:平野綾)")
	if len(segs) != 1 {
		t.Fatalf("段数 = %d, 期望 1", len(segs))
	}
	if segs[0].Inner != "CV:平野綾" {
		t.Errorf("内层 = %q", segs[0].Inner)
	}
	if segs[0].Outer != "涼宮ハルヒ" {
		t.Errorf("外层 = %q", segs[0].Outer)
	}

	// 无括号
	if segs := FindBracketSegments("That Girl"); len(segs) != 0 {
		t.Errorf("无括号时段数 = %d, 期望 0", len(segs))
	}
	// 多段
	if segs := FindBracketSegments("A (X) B (Y)"); len(segs) != 2 {
		t.Errorf("多段时段数 = %d, 期望 2", len(segs))
	}
}

func TestStripBrackets(t *testing.T) {
	cases := []struct{ in, want string }{
		{"夕立（驟雨）", "夕立"},
		{"That Girl (DJLIU Remix)", "That Girl"},
		{"A (X) B (Y)", "A B"},
		{"无括号", "无括号"},
	}
	for _, c := range cases {
		if got := StripBrackets(c.in); got != c.want {
			t.Errorf("StripBrackets(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}
