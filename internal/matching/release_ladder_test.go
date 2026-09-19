package matching

import (
	"testing"

	"github.com/norhua/sonexa/internal/provider"
)

// TestLadderPrefersOfficial 覆盖「排除非正式发行」。
func TestLadderPrefersOfficial(t *testing.T) {
	cfg := DefaultLadderConfig()
	got := cfg.SelectRelease([]provider.Candidate{
		{MatchRef: "a", ReleaseStatus: sptr("Bootleg"), MediumFormat: sptr("CD")},
		{MatchRef: "b", ReleaseStatus: sptr("Official"), MediumFormat: sptr("Digital Media")},
		{MatchRef: "c", ReleaseStatus: sptr("Promotion"), MediumFormat: sptr("CD")},
	})
	if got.MatchRef != "b" {
		t.Errorf("选中 %q, 期望 b（Official 优先于更好的载体）", got.MatchRef)
	}
}

// TestLadderMediumPriority 覆盖载体格式优先级 CD > SACD > Vinyl > Cassette > Digital Media。
func TestLadderMediumPriority(t *testing.T) {
	cfg := DefaultLadderConfig()
	official := sptr("Official")
	got := cfg.SelectRelease([]provider.Candidate{
		{MatchRef: "digital", ReleaseStatus: official, MediumFormat: sptr("Digital Media")},
		{MatchRef: "vinyl", ReleaseStatus: official, MediumFormat: sptr("Vinyl")},
		{MatchRef: "cd", ReleaseStatus: official, MediumFormat: sptr("CD")},
	})
	if got.MatchRef != "cd" {
		t.Errorf("选中 %q, 期望 cd", got.MatchRef)
	}
}

// TestLadderPreferredCountry 覆盖偏好国家列表。
func TestLadderPreferredCountry(t *testing.T) {
	cfg := LadderConfig{
		PreferredCountries: []string{"JP", "US"},
		MediumPriority:     []string{"CD"},
	}
	official, cd := sptr("Official"), sptr("CD")
	got := cfg.SelectRelease([]provider.Candidate{
		{MatchRef: "de", ReleaseStatus: official, MediumFormat: cd, Country: sptr("DE")},
		{MatchRef: "jp", ReleaseStatus: official, MediumFormat: cd, Country: sptr("JP")},
		{MatchRef: "us", ReleaseStatus: official, MediumFormat: cd, Country: sptr("US")},
	})
	if got.MatchRef != "jp" {
		t.Errorf("选中 %q, 期望 jp（偏好列表首位）", got.MatchRef)
	}
}

// TestLadderEarliestFirstRelease 覆盖首次发行日最早者优先。
func TestLadderEarliestFirstRelease(t *testing.T) {
	cfg := DefaultLadderConfig()
	official, cd, jp := sptr("Official"), sptr("CD"), sptr("JP")
	got := cfg.SelectRelease([]provider.Candidate{
		{MatchRef: "later", ReleaseStatus: official, MediumFormat: cd, Country: jp,
			FirstReleaseDate: sptr("2009-05-27")},
		{MatchRef: "earlier", ReleaseStatus: official, MediumFormat: cd, Country: jp,
			FirstReleaseDate: sptr("2006-07-05")},
	})
	if got.MatchRef != "earlier" {
		t.Errorf("选中 %q, 期望 earlier", got.MatchRef)
	}
}

// TestLadderPrefersHavingCover 覆盖有封面者优先。
func TestLadderPrefersHavingCover(t *testing.T) {
	cfg := DefaultLadderConfig()
	official, cd, jp, date := sptr("Official"), sptr("CD"), sptr("JP"), sptr("2006-07-05")
	got := cfg.SelectRelease([]provider.Candidate{
		{MatchRef: "nocover", ReleaseStatus: official, MediumFormat: cd, Country: jp,
			FirstReleaseDate: date, HasCover: false},
		{MatchRef: "withcover", ReleaseStatus: official, MediumFormat: cd, Country: jp,
			FirstReleaseDate: date, HasCover: true},
	})
	if got.MatchRef != "withcover" {
		t.Errorf("选中 %q, 期望 withcover", got.MatchRef)
	}
}

// TestLadderLexicographicTiebreak 覆盖 4.6 的第 6 级：
// 前五级全同时以标识字典序决出唯一一个，而不是任取其一。
func TestLadderLexicographicTiebreak(t *testing.T) {
	cfg := DefaultLadderConfig()
	official, cd, jp, date := sptr("Official"), sptr("CD"), sptr("JP"), sptr("2006-07-05")
	identical := func(ref string) provider.Candidate {
		return provider.Candidate{
			MatchRef: ref, ReleaseStatus: official, MediumFormat: cd,
			Country: jp, FirstReleaseDate: date, HasCover: true,
		}
	}
	got := cfg.SelectRelease([]provider.Candidate{
		identical("ffff"), identical("aaaa"), identical("mmmm"),
	})
	if got.MatchRef != "aaaa" {
		t.Errorf("选中 %q, 期望 aaaa（字典序最小）", got.MatchRef)
	}
}

// TestLadderIsDeterministic 覆盖「结果可复现」：
// 对同一组候选重复执行优选，每次都选出同一个发行。
func TestLadderIsDeterministic(t *testing.T) {
	cfg := DefaultLadderConfig()
	official, cd, jp, date := sptr("Official"), sptr("CD"), sptr("JP"), sptr("2006-07-05")

	// 构造一组在前五级上完全相同的候选
	candidates := []provider.Candidate{}
	for _, ref := range []string{"zzz", "aaa", "mmm", "bbb", "yyy"} {
		candidates = append(candidates, provider.Candidate{
			MatchRef: ref, ReleaseStatus: official, MediumFormat: cd,
			Country: jp, FirstReleaseDate: date, HasCover: true,
		})
	}

	first := cfg.SelectRelease(candidates).MatchRef
	for i := 0; i < 50; i++ {
		// 每轮打乱输入顺序，结果必须不变
		shuffled := make([]provider.Candidate, len(candidates))
		copy(shuffled, candidates)
		shuffled[0], shuffled[len(shuffled)-1] = shuffled[len(shuffled)-1], shuffled[0]
		if i%2 == 0 {
			shuffled[1], shuffled[3] = shuffled[3], shuffled[1]
		}
		if got := cfg.SelectRelease(shuffled).MatchRef; got != first {
			t.Fatalf("第 %d 轮选出 %q, 首轮为 %q——排序不确定", i, got, first)
		}
	}
}

// TestGroupByReleaseGroup 覆盖 4.7 的前置：按 release group 分组。
func TestGroupByReleaseGroup(t *testing.T) {
	rg1, rg2 := sptr("rg-1"), sptr("rg-2")
	groups := GroupByReleaseGroup([]provider.Candidate{
		{MatchRef: "a", ReleaseGroupRef: rg1},
		{MatchRef: "b", ReleaseGroupRef: rg1},
		{MatchRef: "c", ReleaseGroupRef: rg2},
		{MatchRef: "d"}, // 无 release group：单独成组
	})
	if len(groups) != 3 {
		t.Fatalf("分组数 = %d, 期望 3: %v", len(groups), groups)
	}
	if len(groups["rg-1"]) != 2 {
		t.Errorf("rg-1 组有 %d 条, 期望 2", len(groups["rg-1"]))
	}
	if len(groups["rg-2"]) != 1 {
		t.Errorf("rg-2 组有 %d 条, 期望 1", len(groups["rg-2"]))
	}
}

// TestSortReleasesSharesLadder 覆盖 4.8：
// 排序与择优是同一实现，排序后的首位必等于 SelectRelease 的结果。
func TestSortReleasesSharesLadder(t *testing.T) {
	cfg := DefaultLadderConfig()
	candidates := []provider.Candidate{
		{MatchRef: "bootleg", ReleaseStatus: sptr("Bootleg"), MediumFormat: sptr("CD")},
		{MatchRef: "digital", ReleaseStatus: sptr("Official"), MediumFormat: sptr("Digital Media")},
		{MatchRef: "cd", ReleaseStatus: sptr("Official"), MediumFormat: sptr("CD")},
	}
	selected := cfg.SelectRelease(candidates)

	sorted := make([]provider.Candidate, len(candidates))
	copy(sorted, candidates)
	cfg.SortReleases(sorted)

	if sorted[0].MatchRef != selected.MatchRef {
		t.Errorf("排序首位 %q 与择优结果 %q 不一致——两处应共用同一阶梯",
			sorted[0].MatchRef, selected.MatchRef)
	}
}
