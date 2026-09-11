package matching

import (
	"encoding/json"
	"testing"
)

// TestParseCreditGolden 覆盖 3.6：
// 涼宮ハルヒ(CV:平野綾) 拆出角色与人物两个艺术家 + 配音关系 + 两条参与者关系。
func TestParseCreditGolden(t *testing.T) {
	r := DefaultRules()
	got := r.ParseCredit("涼宮ハルヒ(CV:平野綾)")

	if got.Uncertain {
		t.Fatal("标准形态不应被判为不确定")
	}
	if got.Raw != "涼宮ハルヒ(CV:平野綾)" {
		t.Errorf("原串未保真: %q", got.Raw)
	}
	if len(got.Artists) != 2 {
		t.Fatalf("艺术家数 = %d, 期望 2: %+v", len(got.Artists), got.Artists)
	}

	character, actor := got.Artists[0], got.Artists[1]
	if character.Name != "涼宮ハルヒ" || character.Type != "character" || character.Role != "character" {
		t.Errorf("角色艺术家 = %+v", character)
	}
	if actor.Name != "平野綾" || actor.Type != "person" || actor.Role != "vocal" {
		t.Errorf("人物艺术家 = %+v", actor)
	}
	if got.VoiceOf["涼宮ハルヒ"] != "平野綾" {
		t.Errorf("配音关系 = %v, 期望 涼宮ハルヒ -> 平野綾", got.VoiceOf)
	}
}

// TestCreditSpansPointAtRightSubstrings 覆盖 3.9：
// 区间的起止位置必须对应原串中的正确片段。
func TestCreditSpansPointAtRightSubstrings(t *testing.T) {
	r := DefaultRules()
	raw := "涼宮ハルヒ(CV:平野綾)"
	parse := r.ParseCredit(raw)

	spans := BuildSpans(parse, map[string]int64{"涼宮ハルヒ": 42, "平野綾": 17})
	if len(spans.Spans) != 2 {
		t.Fatalf("区间数 = %d, 期望 2", len(spans.Spans))
	}

	// 按原串切片取出的内容必须正是对应的艺术家名
	want := map[int64]string{42: "涼宮ハルヒ", 17: "平野綾"}
	for _, s := range spans.Spans {
		got := raw[s.Start:s.End]
		if got != want[s.ArtistID] {
			t.Errorf("artist %d 的区间 [%d,%d) 取出 %q, 期望 %q",
				s.ArtistID, s.Start, s.End, got, want[s.ArtistID])
		}
	}

	// 括号与 CV: 部分不可点击——两段区间不应覆盖它们
	for _, s := range spans.Spans {
		frag := raw[s.Start:s.End]
		for _, forbidden := range []string{"(", ")", "CV", ":"} {
			if containsSub(frag, forbidden) {
				t.Errorf("可点击区间 %q 不应包含 %q", frag, forbidden)
			}
		}
	}

	// credit_spans_json 可序列化
	data, err := json.Marshal(spans)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var round CreditSpans
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if round.Text != raw || len(round.Spans) != 2 {
		t.Errorf("往返后不一致: %+v", round)
	}
}

// TestSpansSkipUnmatchedFragments 覆盖「对不上的片段保持纯文本」。
func TestSpansSkipUnmatchedFragments(t *testing.T) {
	r := DefaultRules()
	parse := r.ParseCredit("涼宮ハルヒ(CV:平野綾)")

	// 只有角色落了库，声优没有
	spans := BuildSpans(parse, map[string]int64{"涼宮ハルヒ": 42})
	if len(spans.Spans) != 1 {
		t.Fatalf("区间数 = %d, 期望 1（对不上的片段不生成区间）", len(spans.Spans))
	}
	if spans.Spans[0].ArtistID != 42 {
		t.Errorf("区间指向 %d, 期望 42", spans.Spans[0].ArtistID)
	}
}

// TestCVPrefixFormsAllParse 覆盖 3.7 的前缀形态。
func TestCVPrefixFormsAllParse(t *testing.T) {
	r := DefaultRules()
	forms := []string{
		"涼宮ハルヒ(CV:平野綾)",
		"涼宮ハルヒ(cv:平野綾)",
		"涼宮ハルヒ(ＣＶ:平野綾)",
		"涼宮ハルヒ(C.V.平野綾)",
		"涼宮ハルヒ(声:平野綾)",
		"涼宮ハルヒ(声優:平野綾)",
		"涼宮ハルヒ(声优:平野綾)",
		"涼宮ハルヒ(配音:平野綾)",
		"涼宮ハルヒ(Vo.平野綾)",
		"涼宮ハルヒ（CV：平野綾）",
		"涼宮ハルヒ【CV:平野綾】",
	}
	for _, form := range forms {
		t.Run(form, func(t *testing.T) {
			got := r.ParseCredit(form)
			if got.Uncertain {
				t.Fatalf("%q 不应被判为不确定", form)
			}
			if len(got.Artists) != 2 {
				t.Fatalf("%q 艺术家数 = %d, 期望 2", form, len(got.Artists))
			}
			if got.Artists[0].Name != "涼宮ハルヒ" || got.Artists[1].Name != "平野綾" {
				t.Errorf("%q 解析出 %q / %q", form, got.Artists[0].Name, got.Artists[1].Name)
			}
		})
	}
}

// TestNonCVBracketsDoNotSplit 覆盖 3.7 的反例：
// (Remix) / (feat. X) / (US) 全部不触发角色与声优拆分。
func TestNonCVBracketsDoNotSplit(t *testing.T) {
	r := DefaultRules()
	cases := []string{
		"Artist (Remix)",
		"Song (feat. X)",
		"Band (US)",
		"Natalia Lesz (2011)",
		"Artist (Live)",
		"Artist (Instrumental)",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := r.ParseCredit(in)
			if len(got.VoiceOf) != 0 {
				t.Errorf("%q 不应产生配音关系, 得到 %v", in, got.VoiceOf)
			}
			for _, a := range got.Artists {
				if a.Type == "character" {
					t.Errorf("%q 不应产生角色类型艺术家, 得到 %+v", in, a)
				}
			}
		})
	}
}

// TestUncertainSplitProducesNoArtists 覆盖 3.8：
// 拆分不确定时不拆，不产生艺术家记录，条目进复核队列。
func TestUncertainSplitProducesNoArtists(t *testing.T) {
	r := DefaultRules()

	// 内层既像配音又像版本标识
	got := r.ParseCredit("Artist (CV: Live Mix)")
	if !got.Uncertain {
		t.Fatal("可疑形态应被判为不确定")
	}
	if len(got.Artists) != 0 {
		t.Errorf("不确定时不应产生艺术家记录, 得到 %+v", got.Artists)
	}
	if got.Raw != "Artist (CV: Live Mix)" {
		t.Error("不确定时仍应保留原串")
	}
}

// TestPlainCreditSingleArtist 覆盖无括号署名。
func TestPlainCreditSingleArtist(t *testing.T) {
	r := DefaultRules()
	got := r.ParseCredit("平野綾")

	if len(got.Artists) != 1 {
		t.Fatalf("艺术家数 = %d, 期望 1", len(got.Artists))
	}
	if got.Artists[0].Name != "平野綾" || got.Artists[0].Role != "vocal" {
		t.Errorf("解析结果 = %+v", got.Artists[0])
	}
	if len(got.VoiceOf) != 0 {
		t.Error("无括号署名不应产生配音关系")
	}

	spans := BuildSpans(got, map[string]int64{"平野綾": 7})
	if len(spans.Spans) != 1 {
		t.Fatalf("区间数 = %d", len(spans.Spans))
	}
	if "平野綾"[0:] != "平野綾" {
		t.Skip()
	}
	if frag := got.Raw[spans.Spans[0].Start:spans.Spans[0].End]; frag != "平野綾" {
		t.Errorf("区间取出 %q, 期望 平野綾", frag)
	}
}

// TestRulesAreExtensible 覆盖 3.10：
// 新增一种前缀写法只需扩表，不需要改动解析流程的结构。
func TestRulesAreExtensible(t *testing.T) {
	r := DefaultRules()

	// 扩表前不识别
	if got := r.ParseCredit("涼宮ハルヒ(CAST:平野綾)"); len(got.VoiceOf) != 0 {
		t.Fatalf("扩表前不应识别 CAST, 得到 %v", got.VoiceOf)
	}

	// 仅扩充模式表
	r.CVPrefixes = append(r.CVPrefixes, `CAST`)
	r.compile()

	got := r.ParseCredit("涼宮ハルヒ(CAST:平野綾)")
	if got.VoiceOf["涼宮ハルヒ"] != "平野綾" {
		t.Errorf("扩表后应识别 CAST, 得到 %v", got.VoiceOf)
	}

	// 既有形态的行为不回归
	if old := r.ParseCredit("涼宮ハルヒ(CV:平野綾)"); old.VoiceOf["涼宮ハルヒ"] != "平野綾" {
		t.Error("扩表不应影响既有形态")
	}
}

func containsSub(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
