package matching

import "github.com/norhua/sonexa/internal/provider"

// SongFacts 是判断「两个条目是不是同一首歌」所需的全部事实。
// 由调用方从清单条目、曲目快照或本地曲目行构造。
type SongFacts struct {
	Provider     provider.ProviderID
	RecordingRef string
	Title        string
	Credit       string
	DurationMS   *int
}

// SongIdentity 是两个条目身份比较的结果。
type SongIdentity string

const (
	// SongSameRecording：两侧录音标识都有值、来源相同且标识相同——已证明是同一首歌。
	SongSameRecording SongIdentity = "recording"
	// SongSameText：文本级相同（曲名精确、时长与署名不冲突）——疑似同一首歌。
	SongSameText SongIdentity = "text"
	// SongDifferent：可比较且确定不是同一首歌。
	SongDifferent SongIdentity = "different"
	// SongUnknown：事实不足，给不出结论。
	SongUnknown SongIdentity = "unknown"
)

// CompareSong 先比录音级，再退到文本级。
//
// 录间级比较只在「双方都有录音标识且来源相同」时成立：来源不同时标识空间不同，
// 相同数值不构成证据。同来源且双方都有标识却不同，即视为不同歌曲，不再做文本级
// 比较——重制版、不同混音在上游是不同录音，退到文本级反而会把它们混为一谈。
//
// 文本级是启发式，只用于「有人在场」的交互路径与展示；自动决策只接受录音级。
// 原因是当前自动导入默认只使用 MusicBrainz，而 MusicBrainz 为每条曲目提供可追溯的
// 录音标识，所以自动路径上的两侧都有录音标识，用不到文本级。如果将来实现多来源
// 自动导入（例如 MusicBrainz 无结果时自动切换其他上游搜索），其他来源没有录音标识，
// 届时必须先实现判断文本级可信度的逻辑，明确哪些文本级相同可以自动入库，
// 再放宽这条限制。
func CompareSong(a, b SongFacts) SongIdentity {
	if a.Provider != "" && a.Provider == b.Provider && a.RecordingRef != "" && b.RecordingRef != "" {
		if a.RecordingRef == b.RecordingRef {
			return SongSameRecording
		}
		return SongDifferent
	}
	if a.Title == "" || b.Title == "" {
		return SongUnknown
	}
	w := DefaultWeights()
	if w.CompareTitle(a.Title, b.Title) != MatchExact {
		return SongDifferent
	}
	if a.DurationMS != nil && b.DurationMS != nil && !DurationWithinTolerance(*a.DurationMS, *b.DurationMS) {
		return SongDifferent
	}
	if a.Credit != "" && b.Credit != "" && w.CompareCredit(a.Credit, b.Credit) == MatchNone {
		return SongDifferent
	}
	return SongSameText
}

// TextLevelComparable 报告两个条目能否给出文本级结论。任一侧录音标识缺失时，
// 结果只可能是文本级或未知；覆盖结论据此区分「已证明不存在」与「只能按文本判断」。
func TextLevelComparable(a, b SongFacts) bool {
	return a.Title != "" && b.Title != ""
}
