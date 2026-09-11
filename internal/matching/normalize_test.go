package matching

import "testing"

// TestNormalizeGolden 覆盖 3.1 的金样例：
// 全角半角、大小写、片假名/平假名、Ⅲ→III。
func TestNormalizeGolden(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"全角转半角并转小写", "ＴＨＡＴ　ＧＩＲＬ", "thatgirl"},
		{"半角大写转小写", "That Girl", "thatgirl"},
		{"去空白", "God knows...", "godknows"},
		{"罗马数字统一", "ファイナルファンタジーⅢ", "ふぁいなるふぁんたじーiii"},
		{"罗马数字与字母写法一致", "Ⅲ", "iii"},
		{"片假名转平假名", "ハルヒ", "はるひ"},
		{"标点被去除", "冒険でしょでしょ?", "冒険でしょでしょ"},
		{"全角标点被去除", "冒険でしょでしょ？", "冒険でしょでしょ"},
		{"中日文保留", "涼宮ハルヒ", "涼宮はるひ"},
		{"空串", "", ""},
		{"仅标点", "!!!", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Normalize(c.in); got != c.want {
				t.Errorf("Normalize(%q) = %q, 期望 %q", c.in, got, c.want)
			}
		})
	}
}

// TestNormalizeEquivalence 覆盖 spec 的两个 Scenario：
// 全角半角大小写统一、假名统一后两者相等。
func TestNormalizeEquivalence(t *testing.T) {
	pairs := []struct {
		name string
		a, b string
	}{
		{"全角大写与半角小写", "ＴＨＡＴ　ＧＩＲＬ", "that girl"},
		{"片假名与平假名", "ハルヒ", "はるひ"},
		{"罗马数字与拉丁字母", "Ⅲ", "III"},
		{"空白差异", "God  knows", "Godknows"},
	}
	for _, p := range pairs {
		t.Run(p.name, func(t *testing.T) {
			if Normalize(p.a) != Normalize(p.b) {
				t.Errorf("Normalize(%q)=%q 与 Normalize(%q)=%q 应相等",
					p.a, Normalize(p.a), p.b, Normalize(p.b))
			}
		})
	}
}

// TestNormalizeKeepsBrackets 覆盖「标准化不动括号」：
// 括号处理是独立的、有语义判定的步骤。
func TestNormalizeKeepsBrackets(t *testing.T) {
	got := NormalizeKeepBrackets("That Girl (DJLIU Remix)")
	if want := "thatgirl(djliuremix)"; got != want {
		t.Errorf("NormalizeKeepBrackets = %q, 期望 %q", got, want)
	}
	// Normalize 本身把括号当作标点去除，但不移除括号内的内容
	if got := Normalize("That Girl (DJLIU Remix)"); got != "thatgirldjliuremix" {
		t.Errorf("Normalize 不应丢失括号内容, 得到 %q", got)
	}
}

// TestTokens 确保词元按空白切分后各自标准化——保留词边界是
// 「词元集合包含」这一模糊匹配判据的前提。
func TestTokens(t *testing.T) {
	got := Tokens("That Girl (DJLIU Remix)")
	want := []string{"that", "girl", "djliu", "remix"}
	if len(got) != len(want) {
		t.Fatalf("Tokens = %v, 期望 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Tokens[%d] = %q, 期望 %q", i, got[i], want[i])
		}
	}
}
