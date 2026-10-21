package lyricdoc

import (
	"sort"
	"strings"
)

const precisionToleranceMS int64 = 10

// alignPrecision runs only in the producer's builder, after commonTimeline.
// It changes auxiliary line starts, never originals, word times or line ends.
// Positions refer to canonical channel lines; diagnostic times use the common
// serialized axis (including its global offset), so negative effective times
// remain representable without clamping. No source identity is consulted.
func alignPrecision(channels []channelTrack) (bool, error) {
	changed := false
	original := channels[0].channel.Track
	indices := []int{}
	times := []int64{}
	for i, line := range original.Lines {
		if line.StartMS != nil && strings.TrimSpace(line.Text) != "" {
			indices = append(indices, i)
			times = append(times, *line.StartMS)
		}
	}
	lower := func(t int64) int { return sort.Search(len(times), func(i int) bool { return times[i] >= t }) }
	upper := func(t int64) int { return sort.Search(len(times), func(i int) bool { return times[i] > t }) }
	for _, entry := range channels {
		ch := entry.channel
		if (entry.key != "translation" && entry.key != "romanization") || ch.Status != "available" || ch.Track == nil {
			continue
		}
		lines := ch.Track.Lines
		matches := make([]int, len(lines))
		// Range additions count ALL possible claimants, including ambiguous
		// ones, without constructing a quadratic candidate-pair matrix.
		claims := make([]int, len(times)+1)
		for i, line := range lines {
			matches[i] = -1
			if line.Independent || line.ParentID != "" || line.StartMS == nil || strings.TrimSpace(line.Text) == "" {
				continue
			}
			t := *line.StartMS
			lo, hi := lower(t-precisionToleranceMS), upper(t+precisionToleranceMS)
			claims[lo]++
			claims[hi]--
			exactLo, exactHi := lower(t), upper(t)
			if exactHi > exactLo {
				// Exact associations keep their existing semantics, including
				// multiple translation lines. Same-time originals are ambiguous.
				continue
			}
			if hi-lo == 1 {
				matches[i] = lo
			}
		}
		for i := 1; i < len(claims); i++ {
			claims[i] += claims[i-1]
		}
		for i, match := range matches {
			if match >= 0 && (claims[match] != 1 || lines[i].EndMS != nil && times[match] > *lines[i].EndMS) {
				matches[i] = -1
			}
		}
		// Protect order against both original and proposed neighbour times.
		// Rejected proposals cannot introduce a later crossing, and empty
		// timed lines remain barriers rather than being silently skipped.
		planned := func(i int) int64 {
			if matches[i] >= 0 {
				return times[matches[i]]
			}
			return *lines[i].StartMS
		}
		rejected := make([]bool, len(lines))
		previous := -1
		for i, line := range lines {
			if line.StartMS == nil {
				continue
			}
			if previous >= 0 {
				if matches[previous] >= 0 && planned(previous) > min(*line.StartMS, planned(i)) {
					rejected[previous] = true
				}
				if matches[i] >= 0 && planned(i) < max(*lines[previous].StartMS, planned(previous)) {
					rejected[i] = true
				}
			}
			previous = i
		}
		links := []LineAssociation{}
		for i, match := range matches {
			if match < 0 || rejected[i] {
				continue
			}
			links = append(links, LineAssociation{SourcePosition: i, OriginalPosition: indices[match], SourceTimeMS: *lines[i].StartMS, AssociatedTimeMS: times[match]})
			lines[i].StartMS = ms(times[match])
		}
		if len(links) == 0 {
			continue
		}
		ch.Diagnostics = append(ch.Diagnostics, Diagnostic{
			Code: "time_precision_alignment", Message: "在统一 offset 时间轴上以原文行首进行 ±10 毫秒唯一且无交叉的精度对齐；保留对齐前时间，不改变词时间",
			Inference: &Inference{RuleVersion: "time-precision-v1", Layer: 1, Count: len(links), Associations: links},
		})
		if err := validateDiagnostics(ch.Diagnostics); err != nil {
			return false, err
		}
		changed = true
	}
	return changed, nil
}
