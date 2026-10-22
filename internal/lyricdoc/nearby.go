package lyricdoc

import (
	"sort"
	"strings"
)

const nearbyToleranceMS int64 = 2000

// alignNearby is opt-in for online producers, after explicit/exact/precision
// associations. It is a heuristic, not evidence that two texts mean the same
// thing: choose a unique nearest original within 2s, reserve exact parents,
// reject competing claims and time crossings (including empty timing markers).
// Complexity is O((originals + extras) log originals), not a pairwise matrix.
func alignNearby(channels []channelTrack) error {
	original := channels[0].channel.Track
	indices := []int{}
	times := []int64{}
	for i, l := range original.Lines {
		if l.StartMS != nil && strings.TrimSpace(l.Text) != "" {
			indices = append(indices, i)
			times = append(times, *l.StartMS)
		}
	}
	resolve := parentResolver(original)
	distance := func(a, b int64) int64 {
		if a < b {
			return b - a
		}
		return a - b
	}
	for _, entry := range channels {
		ch := entry.channel
		if (entry.key != "translation" && entry.key != "romanization") || !AvailableContent(ch) {
			continue
		}
		lines := ch.Track.Lines
		matches := make([]int, len(lines))
		claims := make([]int, len(original.Lines))
		for i, l := range lines {
			matches[i] = -1
			if target := resolve(l); target >= 0 {
				claims[target]++
				continue
			}
			if l.Independent || l.ParentID != "" || l.StartMS == nil || strings.TrimSpace(l.Text) == "" {
				continue
			}
			t := *l.StartMS
			next := sort.Search(len(times), func(j int) bool { return times[j] >= t })
			best := next
			if next == len(times) || next > 0 && distance(t, times[next-1]) < distance(t, times[next]) {
				best = next - 1
			}
			if best < 0 || distance(t, times[best]) > nearbyToleranceMS {
				continue
			}
			// Equal-distance alternatives, including simultaneous voices, are
			// ambiguous. Never choose one just because it occurred first.
			delta := distance(t, times[best])
			if best > 0 && distance(t, times[best-1]) == delta || best+1 < len(times) && distance(t, times[best+1]) == delta {
				continue
			}
			target := indices[best]
			parent := original.Lines[target]
			if l.Voice != "" && l.Voice != parent.Voice || l.EndMS != nil && times[best] > *l.EndMS {
				continue
			}
			matches[i] = target
			claims[target]++
		}
		for i, target := range matches {
			if target >= 0 && claims[target] != 1 {
				matches[i] = -1
			}
		}
		planned := func(i int) int64 {
			if target := matches[i]; target >= 0 {
				return *original.Lines[target].StartMS
			}
			return *lines[i].StartMS
		}
		rejected := make([]bool, len(lines))
		previous := -1
		for i, l := range lines {
			if l.StartMS == nil {
				continue
			}
			if previous >= 0 {
				if matches[previous] >= 0 && planned(previous) > min(*l.StartMS, planned(i)) {
					rejected[previous] = true
				}
				if matches[i] >= 0 && planned(i) < max(*lines[previous].StartMS, planned(previous)) {
					rejected[i] = true
				}
			}
			previous = i
		}
		links := []LineAssociation{}
		for i, target := range matches {
			if target < 0 || rejected[i] {
				continue
			}
			t := *original.Lines[target].StartMS
			links = append(links, LineAssociation{SourcePosition: i, OriginalPosition: target, SourceTimeMS: *lines[i].StartMS, AssociatedTimeMS: t})
			lines[i].StartMS = ms(t)
		}
		if len(links) != 0 {
			ch.Diagnostics = append(ch.Diagnostics, Diagnostic{
				Code: "time_nearby_alignment", Message: "在共同时间轴上以 ±2 秒内唯一最近且无竞争、无交叉的原文行首启发式关联；保留调整前时间和真实词时间，可能误判",
				Inference: &Inference{RuleVersion: "time-nearby-v1", Layer: 1, Count: len(links), Associations: links},
			})
			if err := validateDiagnostics(ch.Diagnostics); err != nil {
				return err
			}
		}
	}
	return nil
}
