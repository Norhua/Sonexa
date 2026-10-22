package lyricdoc

import (
	"fmt"
	"sort"
	"strings"
)

type channelTrack struct {
	key     string
	channel *Channel
}
type taggedLine struct {
	line    Line
	channel string
}

func lineHasWords(l Line) bool {
	for _, p := range l.Parts {
		if p.StartMS != nil && strings.TrimSpace(p.Text) != "" {
			return true
		}
	}
	return false
}

// Duplicate detection includes word timing; a line-only copy loses to a timed
// copy, but two distinct word timelines at the same line start both survive.
func deduplicate(lines []taggedLine) ([]taggedLine, error) {
	groups := map[string][]int{}
	out := []taggedLine{}
	removed := map[int]bool{}
	for _, entry := range lines {
		l := entry.line
		if l.StartMS == nil {
			out = append(out, entry)
			continue
		}
		end := ""
		if l.EndMS != nil {
			end = fmt.Sprint(*l.EndMS)
		}
		key := fmt.Sprintf("%s\x00%d\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%s\x00%t", entry.channel, *l.StartMS, l.Text, l.ID, l.ParentID, l.Language, l.Voice, fmt.Sprint(l.Ruby), end, l.Independent)
		rendered, err := serializeLine(l)
		if err != nil {
			return nil, err
		}
		skip := false
		for _, index := range groups[key] {
			if removed[index] {
				continue
			}
			prior := out[index].line
			priorText, _ := serializeLine(prior)
			if rendered == priorText || !lineHasWords(l) && lineHasWords(prior) {
				skip = true
				break
			}
			if lineHasWords(l) && !lineHasWords(prior) {
				removed[index] = true
			}
		}
		if !skip {
			groups[key] = append(groups[key], len(out))
			out = append(out, entry)
		}
	}
	filtered := out[:0]
	for i, l := range out {
		if !removed[i] {
			filtered = append(filtered, l)
		}
	}
	return filtered, nil
}

func assembleTracks(channels []channelTrack, options buildOptions) (*Track, []PreviewRow, error) {
	originalChannels := channels
	var offset int64
	var err error
	channels, offset, err = commonTimeline(channels)
	if err != nil {
		return nil, nil, err
	}
	resolveParent := parentResolver(channels[0].channel.Track)
	for _, c := range channels[1:] {
		if c.channel.Track == nil {
			continue
		}
		for i := range c.channel.Track.Lines {
			l := &c.channel.Track.Lines[i]
			if l.ParentID == "" {
				continue
			}
			target := resolveParent(*l)
			if target < 0 {
				return nil, nil, fmt.Errorf("%w：附加行父标识不存在", ErrFormat)
			}
			parent := channels[0].channel.Track.Lines[target]
			if (l.StartMS == nil) != (parent.StartMS == nil) || l.StartMS != nil && *l.StartMS != *parent.StartMS || l.Voice != "" && l.Voice != parent.Voice {
				return nil, nil, fmt.Errorf("%w：附加行与显式父行事实矛盾", ErrFormat)
			}
			l.Voice = parent.Voice
		}
	}
	_, err = alignPrecision(channels)
	if err != nil {
		return nil, nil, err
	}
	if options.infer {
		if err := alignNearby(channels); err != nil {
			return nil, nil, err
		}
		labelLanguages(channels, options.translationFallback)
	}
	// Every delivered channel uses the same axis as XLRC, not only channels
	// whose millisecond precision happened to need correction.
	for _, c := range channels {
		if c.channel.Track != nil {
			c.channel.Text, err = SerializeTrack(c.channel.Track)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	merged := &Track{Metadata: append([]string(nil), channels[0].channel.Track.Metadata...), OffsetMS: offset}
	entries := []taggedLine{}
	used := 0
	for _, c := range channels {
		if c.channel.Track == nil {
			continue
		}
		if c.channel.Status != "available" && c.key != "original" {
			c.key = "additional"
		}
		for _, l := range c.channel.Track.Lines {
			used += lineBytes(l)
			if used > MaxProjectionBytes {
				return nil, nil, ErrTooLarge
			}
			entries = append(entries, taggedLine{l, c.key})
		}
	}
	// Deduplication is channel-local; equal translations in different
	// languages or voices must never erase one another.
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].line.StartMS, entries[j].line.StartMS
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return *a < *b
	})
	originalRows := &Track{}
	rowIndices := []int{}
	originalID := map[string]bool{}
	for i, e := range entries {
		if e.channel == "original" {
			if e.line.ID != "" {
				if originalID[e.line.ID] {
					return nil, nil, ErrFormat
				}
				originalID[e.line.ID] = true
			}
			originalRows.Lines = append(originalRows.Lines, e.line)
			rowIndices = append(rowIndices, i)
		}
		merged.Lines = append(merged.Lines, e.line)
	}
	rows := make([]PreviewRow, len(entries))
	attached := map[int]bool{}
	for i, e := range entries {
		rows[i] = previewLine(e.line, e.channel)
	}
	resolveRow := parentResolver(originalRows)
	findParent := func(l Line) int {
		if i := resolveRow(l); i >= 0 {
			return rowIndices[i]
		}
		return -1
	}
	for i, e := range entries {
		if e.channel == "original" {
			continue
		}
		index := findParent(e.line)
		if e.channel == "additional" && e.line.ParentID == "" {
			continue
		}
		if index < 0 {
			if e.line.ParentID != "" {
				return nil, nil, fmt.Errorf("%w：附加行父标识不存在", ErrFormat)
			}
			continue
		}
		target := &rows[index]
		if e.channel == "translation" {
			target.Translation = append(target.Translation, e.line.Text)
		}
		if e.channel == "romanization" {
			target.Romanization = append(target.Romanization, e.line.Text)
		}
		target.Annotations = append(target.Annotations, Annotation{Channel: e.channel, Language: lineLanguage(e.line, e.channel), Text: e.line.Text, Ruby: e.line.Ruby})
		attached[i] = true
	}
	result := []PreviewRow{}
	for i, row := range rows {
		if !attached[i] {
			result = append(result, row)
		}
	}
	// Extras left unassociated by the selected producer policy retain their
	// own times; local input never opts into the online nearby heuristic.
	for _, c := range channels[1:] {
		if c.channel.Track == nil {
			continue
		}
		matched, total := 0, 0
		for _, l := range c.channel.Track.Lines {
			if strings.TrimSpace(l.Text) == "" {
				continue
			}
			total++
			if c.channel.Status == "available" && findParent(l) >= 0 && (c.key != "additional" || l.ParentID != "") {
				matched++
			}
		}
		if matched < total {
			c.channel.Diagnostics = append(c.channel.Diagnostics, Diagnostic{Code: "unaligned", Message: "附加文字无法唯一关联原文，保留独立行及原时间"})
			if matched > 0 {
				c.channel.SyncStatus = "partial"
			}
		}
	}
	for i := range channels {
		originalChannels[i].channel.Track = channels[i].channel.Track
		originalChannels[i].channel.Text = channels[i].channel.Text
		originalChannels[i].channel.Diagnostics = channels[i].channel.Diagnostics
		originalChannels[i].channel.SyncStatus = channels[i].channel.SyncStatus
	}
	if projectionBytes(result) > MaxProjectionBytes {
		return nil, nil, ErrTooLarge
	}
	return merged, result, nil
}
