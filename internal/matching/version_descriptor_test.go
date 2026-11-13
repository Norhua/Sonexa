package matching

import "testing"

// TestSplitVersionDescriptor 覆盖「曲名里哪一段是版本描述」。
//
// 版本描述的**写法**在同一条录音的各处并不一致（`short Ver.` /
// `(Short version)`），而主干是各种写法共有的那一段。上游据此决定检索时
// 拿哪一段当必须项——这里只负责把两段分开。
func TestSplitVersionDescriptor(t *testing.T) {
	cases := []struct {
		name, in         string
		base, descriptor string
		ok               bool
	}{
		// 用户报告的那一行：版本词没有括号，一直延续到结尾
		{"结尾未加括号的版本词串", "星の在り処 Harmonica short Ver.", "星の在り処 Harmonica", "short Ver.", true},
		{"全角空格分隔", "星の在り処　Harmonica long Ver.", "星の在り処　Harmonica long", "Ver.", true},
		{"结尾版本词", "君の知らない物語 TV Size", "君の知らない物語", "TV Size", true},
		{"版本词后带序号", "Song Remix 2", "Song", "Remix 2", true},
		{"紧贴日文的版本词", "物語Remix", "物語", "Remix", true},
		{"破折号包裹", "Song - Instrumental -", "Song", "Instrumental", true},

		{"括号里的版本段", "That Girl (DJLIU Remix)", "That Girl", "DJLIU Remix", true},
		{"括号里的 off vocal", "Tetra. (off vocal)", "Tetra.", "off vocal", true},
		{"只剥版本段，其余括号留在主干", "Song (Live) (Remastered)", "Song (Remastered)", "Live", true},

		// 版本词是曲名本身：后面还跟着别的词，不拆
		{"版本词在开头", "Live and Let Die", "Live and Let Die", "", false},
		{"版本词在中间", "Video Killed the Radio Star", "Video Killed the Radio Star", "", false},
		// 词边界：ver / live 作为子串不算
		{"子串不算版本词", "Discover", "Discover", "", false},
		{"子串不算版本词（结尾）", "Silver", "Silver", "", false},

		// 没有主干可言
		{"整个曲名就是版本词", "Live", "Live", "", false},
		{"整个曲名就是一段括号", "（インスト）", "（インスト）", "", false},

		{"无版本描述", "Tetra.", "Tetra.", "", false},
		// feat. 标的是参与者，译名是另一种写法——都不是版本描述
		{"feat. 不是版本描述", "By the Seaside (feat. Kitsui Akira)", "By the Seaside (feat. Kitsui Akira)", "", false},
		{"译名不是版本描述", "綿雪の夢（繁雪之梦）", "綿雪の夢（繁雪之梦）", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			base, descriptor, ok := SplitVersionDescriptor(c.in)
			if base != c.base || descriptor != c.descriptor || ok != c.ok {
				t.Errorf("SplitVersionDescriptor(%q) = (%q, %q, %v), 期望 (%q, %q, %v)",
					c.in, base, descriptor, ok, c.base, c.descriptor, c.ok)
			}
		})
	}
}

// TestSplitVersionDescriptorBaseIsSubphrase 守住检索放宽赖以成立的前提：
// 主干必须是原曲名的一段——拿它做必须项，结果集才是原串结果集的超集。
func TestSplitVersionDescriptorBaseIsSubphrase(t *testing.T) {
	for _, title := range []string{
		"星の在り処 Harmonica short Ver.", "That Girl (DJLIU Remix)", "Tetra. -movie ver.-",
		"君の知らない物語 TV Size", "心の羽根（ドラゴンボール改ver.）", "Song - Instrumental -",
	} {
		base, _, ok := SplitVersionDescriptor(title)
		if !ok {
			t.Errorf("SplitVersionDescriptor(%q) 未拆分", title)
			continue
		}
		nb, nt := Normalize(base), Normalize(title)
		if len(nb) >= len(nt) || nt[:len(nb)] != nb {
			t.Errorf("主干 %q 不是 %q 的前段", base, title)
		}
	}
}

// TestSplitVersionDescriptorDoesNotRelaxIdentity 确认拆分只服务于检索：
// 版本不同的两个曲名主干相同，但 CompareTitle 照旧判成不匹配。
func TestSplitVersionDescriptorDoesNotRelaxIdentity(t *testing.T) {
	short, long := "星の在り処 Harmonica short Ver.", "星の在り処 Harmonica long Ver."
	if got := DefaultWeights().CompareTitle(short, long); got != MatchNone {
		t.Errorf("CompareTitle(%q, %q) = %v, 期望不匹配", short, long, got)
	}
}
