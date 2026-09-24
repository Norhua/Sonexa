package matching

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/norhua/sonexa/internal/provider"
)

// mbidPattern 是 MusicBrainz 标识的格式：标准 UUID。
var mbidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// itunesRefPattern 是 iTunes 标识的格式：{country}:{trackId}。
var itunesRefPattern = regexp.MustCompile(`^[a-z]{2}:\d+$`)

// qqRefPattern 是 QQ 音乐标识的格式：歌曲 MID。
var qqRefPattern = regexp.MustCompile(`^[0-9A-Za-z]{10,20}$`)

// ValidateMatchRef 校验匹配标识的格式。
//
// 取值方案按上游确定：
//
//	musicbrainz  track MBID（不可用 recording MBID——同一录音出现在几十张
//	             发行上，用它会把 CD 版与数字版判为同一条）
//	itunes       {country}:{trackId}（同一首歌在不同地区 trackId 不同）
//	qq           歌曲 MID（song id 是数字，MID 才是详情与歌词接口的稳定标识）
//
// 用户在编辑页手动填入标识时需通过此校验。
func ValidateMatchRef(source provider.ProviderID, ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return fmt.Errorf("标识不能为空")
	}
	switch source {
	case provider.MusicBrainz:
		if !mbidPattern.MatchString(ref) {
			return fmt.Errorf("MusicBrainz 标识应为 track MBID（形如 xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx）")
		}
		return nil
	case provider.ITunes:
		if !itunesRefPattern.MatchString(ref) {
			return fmt.Errorf("iTunes 标识应为「地区:曲目编号」（形如 jp:534038851）")
		}
		return nil
	case provider.QQMusic:
		if !qqRefPattern.MatchString(ref) {
			return fmt.Errorf("QQ 音乐标识应为歌曲 MID（形如 003RcjkV1XjQQy）")
		}
		return nil
	default:
		return fmt.Errorf("未知的上游 %q", source)
	}
}

// MakeITunesRef 构造 iTunes 的匹配标识。
func MakeITunesRef(country string, trackID int64) string {
	return strings.ToLower(country) + ":" + strconv.FormatInt(trackID, 10)
}

// LocalMatchKind 是候选与库中曲目的对应关系。
type LocalMatchKind string

const (
	// LocalNone 纯上游候选：库中不存在。
	LocalNone LocalMatchKind = "none"
	// LocalByRef 按来源与标识精确命中。
	LocalByRef LocalMatchKind = "by_ref"
	// LocalByISRC 经 ISRC 桥接命中（跨上游）。
	LocalByISRC LocalMatchKind = "by_isrc"
	// LocalSuspected 文本模糊比对判定的疑似重复，需用户确认。
	LocalSuspected LocalMatchKind = "suspected"
)

// LocalMatch 是候选合并判定的结果。
type LocalMatch struct {
	Kind LocalMatchKind
	// TrackID 是命中的库中曲目；未命中时为 0。
	TrackID int64
}

// Badges 返回该结果应显示的徽章。
//
//	[本地][MB]    标识或 ISRC 命中
//	[本地?][MB]   文本模糊比对的疑似命中，问号表示需用户确认
func (m LocalMatch) Badges(upstream provider.ProviderID) []string {
	switch m.Kind {
	case LocalByRef, LocalByISRC:
		return []string{"本地", string(upstream)}
	case LocalSuspected:
		return []string{"本地?", string(upstream)}
	default:
		return []string{string(upstream)}
	}
}

// Note 返回该结果的附加说明。
func (m LocalMatch) Note() string {
	switch m.Kind {
	case LocalByISRC:
		return "经 ISRC 匹配"
	case LocalSuspected:
		return "疑似重复，需确认"
	default:
		return ""
	}
}

// Action 返回该行应显示的动作。
func (m LocalMatch) Action() string {
	if m.Kind == LocalNone {
		return "加入曲库"
	}
	return "合并"
}

// LocalLookup 是候选合并判定所需的库内查询能力，由 store 实现。
type LocalLookup interface {
	// FindByMatchRef 按来源与标识精确查找。
	FindByMatchRef(source, ref string) (int64, error)
	// FindByISRC 按 ISRC 查找（跨上游桥接）。
	FindByISRC(isrc string) (int64, error)
	// FindUnmatchedByText 在**没有匹配来源**的曲目中做标准化文本比对。
	// 作用域被压到这个小集合上，比全库模糊去重可靠得多。
	FindUnmatchedByText(normTitle, normArtist string, durationMS *int) (int64, error)
}

// ResolveLocal 按四步顺序判定候选是否已存在于曲库。
//
//	[1] 按来源与标识精确查找
//	[2] 未命中且候选带 ISRC 时按 ISRC 查找（跨上游桥接）
//	[3] 仍未命中时做标准化文本比对，但作用域仅限没有匹配来源的曲目，
//	    且要求标准化曲名相等、标准化主艺术家相等、时长差在 3 秒以内
//	[4] 都未命中则为纯上游候选
//
// 第三步的作用域限制很关键：已有溯源的曲目有标识可比，不该参与文本模糊比对，
// 否则误判会扩散到全库。
func ResolveLocal(lookup LocalLookup, c provider.Candidate) (LocalMatch, error) {
	// [1] 标识
	if c.MatchRef != "" {
		id, err := lookup.FindByMatchRef(string(c.Provider), c.MatchRef)
		if err != nil {
			return LocalMatch{}, err
		}
		if id != 0 {
			return LocalMatch{Kind: LocalByRef, TrackID: id}, nil
		}
	}

	// [2] ISRC 桥接
	if c.ISRC != nil && *c.ISRC != "" {
		id, err := lookup.FindByISRC(*c.ISRC)
		if err != nil {
			return LocalMatch{}, err
		}
		if id != 0 {
			return LocalMatch{Kind: LocalByISRC, TrackID: id}, nil
		}
	}

	// [3] 文本模糊比对，作用域仅限无溯源的曲目
	if c.Title != nil && c.ArtistName != nil {
		normTitle := Normalize(*c.Title)
		normArtist := Normalize(*c.ArtistName)
		if normTitle != "" && normArtist != "" {
			id, err := lookup.FindUnmatchedByText(normTitle, normArtist, c.DurationMS)
			if err != nil {
				return LocalMatch{}, err
			}
			if id != 0 {
				return LocalMatch{Kind: LocalSuspected, TrackID: id}, nil
			}
		}
	}

	// [4] 纯上游
	return LocalMatch{Kind: LocalNone}, nil
}

// DurationWithinTolerance 报告两个时长是否在 3 秒以内。
// 第三步的判据之一。
func DurationWithinTolerance(a, b int) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= 3000
}
