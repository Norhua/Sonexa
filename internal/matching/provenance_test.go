package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

// TestValidateMatchRef 覆盖 8.1：手填非法标识被拒绝。
func TestValidateMatchRef(t *testing.T) {
	cases := []struct {
		name   string
		source provider.ProviderID
		ref    string
		ok     bool
	}{
		{"合法 MBID", provider.MusicBrainz, "c8b0a1f2-3d4e-5f60-9a8b-7c6d5e4f3a2b", true},
		{"MBID 大写也合法", provider.MusicBrainz, "C8B0A1F2-3D4E-5F60-9A8B-7C6D5E4F3A2B", true},
		{"非 UUID 被拒", provider.MusicBrainz, "not-a-uuid", false},
		{"iTunes 标识不能用于 MB", provider.MusicBrainz, "jp:534038851", false},
		{"空串被拒", provider.MusicBrainz, "", false},
		{"合法 iTunes 标识", provider.ITunes, "jp:534038851", true},
		{"iTunes 缺地区被拒", provider.ITunes, "534038851", false},
		{"iTunes 地区需两位小写", provider.ITunes, "JP:534038851", false},
		{"MBID 不能用于 iTunes", provider.ITunes, "c8b0a1f2-3d4e-5f60-9a8b-7c6d5e4f3a2b", false},
		{"未知上游被拒", provider.ProviderID("unknown"), "anything", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateMatchRef(c.source, c.ref)
			if c.ok && err != nil {
				t.Errorf("应通过校验, 得到 %v", err)
			}
			if !c.ok && err == nil {
				t.Error("应被拒绝")
			}
			// 被拒时错误信息要说明格式要求
			if !c.ok && err != nil && err.Error() == "" {
				t.Error("错误信息应说明格式要求")
			}
		})
	}
}

// fakeLookup 记录各步是否被调用，便于断言判定顺序与作用域。
type fakeLookup struct {
	byRef       map[string]int64
	byISRC      map[string]int64
	byText      int64
	textCalled  bool
	isrcCalled  bool
}

func (f *fakeLookup) FindByMatchRef(source, ref string) (int64, error) {
	return f.byRef[source+"|"+ref], nil
}
func (f *fakeLookup) FindByISRC(isrc string) (int64, error) {
	f.isrcCalled = true
	return f.byISRC[isrc], nil
}
func (f *fakeLookup) FindUnmatchedByText(title, artist string, d *int) (int64, error) {
	f.textCalled = true
	return f.byText, nil
}

// TestResolveLocalFourSteps 覆盖 8.3 的四步判定顺序。
func TestResolveLocalFourSteps(t *testing.T) {
	t.Run("第一步标识命中", func(t *testing.T) {
		f := &fakeLookup{byRef: map[string]int64{"musicbrainz|trk-1": 42}}
		got, err := ResolveLocal(f, provider.Candidate{
			Provider: provider.MusicBrainz, MatchRef: "trk-1",
			Title: sptr("God knows"), ArtistName: sptr("平野綾"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Kind != LocalByRef || got.TrackID != 42 {
			t.Errorf("结果 = %+v, 期望 by_ref/42", got)
		}
		// 标识命中后不该再走后续步骤
		if f.isrcCalled || f.textCalled {
			t.Error("标识命中后不应继续 ISRC 或文本比对")
		}
		if got.Action() != "合并" {
			t.Errorf("动作 = %q, 期望 合并", got.Action())
		}
	})

	t.Run("第二步 ISRC 桥接", func(t *testing.T) {
		f := &fakeLookup{
			byRef:  map[string]int64{},
			byISRC: map[string]int64{"JPPL00600123": 7},
		}
		got, _ := ResolveLocal(f, provider.Candidate{
			Provider: provider.ITunes, MatchRef: "jp:1",
			ISRC: sptr("JPPL00600123"),
		})
		if got.Kind != LocalByISRC || got.TrackID != 7 {
			t.Errorf("结果 = %+v, 期望 by_isrc/7", got)
		}
		if got.Note() != "经 ISRC 匹配" {
			t.Errorf("标注 = %q", got.Note())
		}
	})

	t.Run("第三步文本模糊比对", func(t *testing.T) {
		f := &fakeLookup{byRef: map[string]int64{}, byISRC: map[string]int64{}, byText: 9}
		got, _ := ResolveLocal(f, provider.Candidate{
			Provider: provider.MusicBrainz, MatchRef: "trk-x",
			Title: sptr("God knows"), ArtistName: sptr("平野綾"),
		})
		if got.Kind != LocalSuspected || got.TrackID != 9 {
			t.Errorf("结果 = %+v, 期望 suspected/9", got)
		}
		// 疑似必须带问号徽章，需用户确认
		badges := got.Badges(provider.MusicBrainz)
		if badges[0] != "本地?" {
			t.Errorf("徽章 = %v, 疑似应带问号", badges)
		}
	})

	t.Run("第四步纯上游", func(t *testing.T) {
		f := &fakeLookup{byRef: map[string]int64{}, byISRC: map[string]int64{}}
		got, _ := ResolveLocal(f, provider.Candidate{
			Provider: provider.MusicBrainz, MatchRef: "trk-y",
			Title: sptr("未知曲"), ArtistName: sptr("未知艺术家"),
		})
		if got.Kind != LocalNone {
			t.Errorf("结果 = %+v, 期望 none", got)
		}
		if got.Action() != "加入曲库" {
			t.Errorf("动作 = %q, 期望 加入曲库", got.Action())
		}
		badges := got.Badges(provider.MusicBrainz)
		if len(badges) != 1 {
			t.Errorf("纯上游候选徽章 = %v, 期望只有一个", badges)
		}
	})
}

// TestFuzzyMatchDoesNotRunWhenRefHits 覆盖 8.3 的关键约束：
// 模糊比对不扩散到已有溯源的曲目。
func TestFuzzyMatchDoesNotRunWhenRefHits(t *testing.T) {
	f := &fakeLookup{byRef: map[string]int64{"musicbrainz|trk-1": 1}}
	ResolveLocal(f, provider.Candidate{
		Provider: provider.MusicBrainz, MatchRef: "trk-1",
		Title: sptr("t"), ArtistName: sptr("a"),
	})
	if f.textCalled {
		t.Error("标识已命中时不应执行文本模糊比对")
	}
}

// TestDurationTolerance 覆盖第三步的时长判据：|Δ| ≤ 3s。
func TestDurationTolerance(t *testing.T) {
	cases := []struct {
		a, b int
		want bool
	}{
		{220000, 220000, true},
		{220000, 222000, true},  // Δ=2s
		{220000, 223000, true},  // Δ=3s 边界
		{220000, 223001, false}, // Δ=3.001s
		{223000, 220000, true},  // 反向
		{220000, 240000, false},
	}
	for _, c := range cases {
		if got := DurationWithinTolerance(c.a, c.b); got != c.want {
			t.Errorf("DurationWithinTolerance(%d,%d) = %v, 期望 %v", c.a, c.b, got, c.want)
		}
	}
}

// TestMakeITunesRef 确保标识构造统一。
func TestMakeITunesRef(t *testing.T) {
	if got := MakeITunesRef("JP", 534038851); got != "jp:534038851" {
		t.Errorf("标识 = %q, 期望 jp:534038851（地区转小写）", got)
	}
	if err := ValidateMatchRef(provider.ITunes, MakeITunesRef("jp", 1)); err != nil {
		t.Errorf("构造出的标识应通过校验: %v", err)
	}
}
