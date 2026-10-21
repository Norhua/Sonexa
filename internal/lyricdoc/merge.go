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
		key := fmt.Sprintf("%d\x00%s", *l.StartMS, l.Text)
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

func assembleTracks(channels []channelTrack) (*Track, []PreviewRow, error) {
	originalChannels := channels
	var offset int64
	var err error
	channels, offset, err = commonTimeline(channels)
	if err != nil {
		return nil, nil, err
	}
	anyAligned, err := alignPrecision(channels)
	if err != nil {
		return nil, nil, err
	}
	// When precision alignment changes a line, retain the common axis in all
	// delivered channel projections too. This keeps offsets/text replayable,
	// including an auxiliary line that originally preceded time zero.
	if anyAligned {
		for _, c := range channels {
			if c.channel.Track != nil {
				c.channel.Text, err = SerializeTrack(c.channel.Track)
				if err != nil {
					return nil, nil, err
				}
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
		for _, l := range c.channel.Track.Lines {
			used += lineBytes(l)
			if used > MaxProjectionBytes {
				return nil, nil, ErrTooLarge
			}
			entries = append(entries, taggedLine{l, c.key})
		}
	}
	entries, err = deduplicate(entries)
	if err != nil {
		return nil, nil, err
	}
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
	originalAt := map[int64][]int{}
	for i, e := range entries {
		if e.channel == "original" && e.line.StartMS != nil && strings.TrimSpace(e.line.Text) != "" {
			originalAt[*e.line.StartMS] = append(originalAt[*e.line.StartMS], i)
		}
		merged.Lines = append(merged.Lines, e.line)
	}
	rows := make([]PreviewRow, len(entries))
	attached := map[int]bool{}
	for i, e := range entries {
		rows[i] = PreviewRow{TimeMS: e.line.StartMS, Text: e.line.Text, Channel: e.channel}
	}
	for i, e := range entries {
		if e.channel != "translation" && e.channel != "romanization" || e.line.StartMS == nil {
			continue
		}
		matches := originalAt[*e.line.StartMS]
		if len(matches) != 1 {
			continue
		}
		target := &rows[matches[0]]
		if e.channel == "translation" {
			target.Translation = append(target.Translation, e.line.Text)
		} else {
			target.Romanization = append(target.Romanization, e.line.Text)
		}
		attached[i] = true
	}
	result := []PreviewRow{}
	for i, row := range rows {
		if !attached[i] {
			result = append(result, row)
		}
	}
	// Unassociated extras retain their times after bounded precision alignment.
	// They never inherit an ambiguous or more distant line time.
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
			if l.StartMS != nil && len(originalAt[*l.StartMS]) == 1 {
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
		if anyAligned {
			originalChannels[i].channel.Track = channels[i].channel.Track
			originalChannels[i].channel.Text = channels[i].channel.Text
		}
		originalChannels[i].channel.Diagnostics = channels[i].channel.Diagnostics
		originalChannels[i].channel.SyncStatus = channels[i].channel.SyncStatus
	}
	if projectionBytes(result) > MaxProjectionBytes {
		return nil, nil, ErrTooLarge
	}
	return merged, result, nil
}
