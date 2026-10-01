package matching

import (
	"sort"
	"strings"

	"github.com/norhua/sonexa/internal/provider"
)

// LadderConfig 是发行优选阶梯的可配置项。
type LadderConfig struct {
	// PreferredCountries 是偏好国家列表，靠前者优先。
	PreferredCountries []string
	// MediumPriority 是载体格式优先级，靠前者优先。
	MediumPriority []string
	// DigitalFormats 是被视为**非实体**的载体格式。
	//
	// 「实体」这一侧不枚举：MusicBrainz 的载体分类有上百个（CD / SACD / Blu-spec CD /
	// Vinyl / Cassette / MiniDisc / DVD-Audio…），列出来必然漏，而漏掉一个实体格式
	// 会让本该入库的行落进复核。非实体的那一侧只有寥寥几个，枚举得完。
	DigitalFormats []string
}

// DefaultLadderConfig 返回默认阶梯配置。
func DefaultLadderConfig() LadderConfig {
	return LadderConfig{
		PreferredCountries: []string{"JP", "CN", "US", "GB", "XW"},
		MediumPriority:     []string{"CD", "SACD", "Vinyl", "Cassette", "Digital Media"},
		DigitalFormats:     []string{"Digital Media"},
	}
}

// PhysicalOnly 过滤出载体为实体发行的候选。
//
// 这是分流规则第 3 条的「先过滤 release 类型，优先选择为实体光碟的」那一步：
// 同一发行组内有多条高置信时，先只看实体发行，剩唯一一条才入库。
//
// 载体缺失（上游没给 media 信息）**不算实体**：那是「不知道」而不是「是」，
// 而这一步的产出会直接决定要不要替用户入库，不知道时不该算它通过。
func (cfg LadderConfig) PhysicalOnly(candidates []provider.Candidate) []provider.Candidate {
	out := make([]provider.Candidate, 0, len(candidates))
	for _, c := range candidates {
		if cfg.IsPhysical(c.MediumFormat) {
			out = append(out, c)
		}
	}
	return out
}

// IsPhysical 报告某载体格式是否为实体发行。
func (cfg LadderConfig) IsPhysical(format *string) bool {
	if format == nil || strings.TrimSpace(*format) == "" {
		return false
	}
	for _, d := range cfg.DigitalFormats {
		if strings.EqualFold(strings.TrimSpace(*format), d) {
			return false
		}
	}
	return true
}

// SelectRelease 从一组候选中按发行优选阶梯选出唯一一个。
//
// 阶梯顺序（每级相同再看下一级）：
//
//  1. release_status = Official           排除 Bootleg / Promotion / Pseudo-Release
//  2. medium_format 优先级                 CD > SACD > Vinyl > Cassette > Digital Media > 其他
//  3. country 命中偏好列表                  靠前者优先
//  4. release-group.first-release-date 最早
//  5. 有封面者优先
//  6. 上游标识（MatchRef）字典序             纯为确定性，保证结果可复现
//
// 排序完全确定：同样的输入必产生同样的结果。第 6 级的唯一目的是在前 5 级
// 全同时给出确定裁决，否则同样的输入会随遍历顺序产生不同结果。
//
// 这一套阶梯既用于匹配阶段的多候选优选，也用于专辑视图选「代表 release」——
// 两处对「哪个版本更有代表性」的判断没有理由不同，分成两套必然漂移。
func (cfg LadderConfig) SelectRelease(candidates []provider.Candidate) *provider.Candidate {
	if len(candidates) == 0 {
		return nil
	}
	best := candidates[0]
	for i := 1; i < len(candidates); i++ {
		if cfg.less(candidates[i], best) {
			best = candidates[i]
		}
	}
	return &best
}

// SortReleases 按阶梯对候选排序（原地），最优者在前。
func (cfg LadderConfig) SortReleases(candidates []provider.Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return cfg.less(candidates[i], candidates[j])
	})
}

// less 报告 a 是否比 b 更优（应排在前）。
func (cfg LadderConfig) less(a, b provider.Candidate) bool {
	// 1. Official 优先
	if oa, ob := isOfficial(a.ReleaseStatus), isOfficial(b.ReleaseStatus); oa != ob {
		return oa
	}
	// 2. 载体格式优先级（数值越小越优）
	if ma, mb := cfg.mediumRank(a.MediumFormat), cfg.mediumRank(b.MediumFormat); ma != mb {
		return ma < mb
	}
	// 3. 偏好国家（数值越小越优）
	if ca, cb := cfg.countryRank(a.Country), cfg.countryRank(b.Country); ca != cb {
		return ca < cb
	}
	// 4. 首次发行日最早
	if da, db := deref(a.FirstReleaseDate), deref(b.FirstReleaseDate); da != db {
		return earlierDate(da, db)
	}
	// 5. 有封面者优先
	if a.HasCover != b.HasCover {
		return a.HasCover
	}
	// 6. 上游标识字典序——纯为确定性
	if a.Provider != b.Provider {
		return a.Provider < b.Provider
	}
	return a.MatchRef < b.MatchRef
}

func isOfficial(status *string) bool {
	return status != nil && strings.EqualFold(*status, "Official")
}

// mediumRank 返回载体格式的优先级序号；不在列表中的排在最后。
func (cfg LadderConfig) mediumRank(format *string) int {
	if format == nil {
		return len(cfg.MediumPriority) + 1
	}
	for i, m := range cfg.MediumPriority {
		if strings.EqualFold(*format, m) {
			return i
		}
	}
	return len(cfg.MediumPriority)
}

// countryRank 返回国家的偏好序号；不在列表中的排在最后。
func (cfg LadderConfig) countryRank(country *string) int {
	if country == nil {
		return len(cfg.PreferredCountries) + 1
	}
	for i, c := range cfg.PreferredCountries {
		if strings.EqualFold(*country, c) {
			return i
		}
	}
	return len(cfg.PreferredCountries)
}

// earlierDate 报告 a 是否早于 b。空日期视为最晚。
func earlierDate(a, b string) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	return a < b // ISO 日期字符串可直接字典序比较
}

// GroupByReleaseGroup 把候选按 release group 分组。
// 无 release group 标识的候选各自单独成组（键为其 MatchRef）。
func GroupByReleaseGroup(candidates []provider.Candidate) map[string][]provider.Candidate {
	groups := map[string][]provider.Candidate{}
	for _, c := range candidates {
		key := deref(c.ReleaseGroupRef)
		if key == "" {
			key = "\x00solo:" + c.MatchRef
		}
		groups[key] = append(groups[key], c)
	}
	return groups
}
