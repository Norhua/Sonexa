package lyrics

import (
	"errors"
	"fmt"
	"strings"
)

func AbsentChannel() *Channel {
	return &Channel{Status: "unavailable", Origin: "independent", SyncStatus: "unknown"}
}
func UnknownChannel() *Channel {
	return &Channel{Status: "unknown", Origin: "unknown", SyncStatus: "unknown"}
}
func FailedChannel(err error) *Channel {
	return &Channel{Status: "error", Origin: "independent", SyncStatus: "unknown", Message: err.Error(), Err: err}
}
func TextChannel(text string, err error) *Channel {
	if err != nil {
		return FailedChannel(err)
	}
	return normalizeChannel(&Channel{Text: text, Status: "available", Origin: "independent"})
}
func normalizeChannel(ch *Channel) *Channel {
	if ch == nil {
		return UnknownChannel()
	}
	copy := *ch
	copy.Diagnostics = append([]Diagnostic(nil), ch.Diagnostics...)
	if copy.Origin == "" {
		copy.Origin = "independent"
	}
	if copy.SyncStatus == "" {
		copy.SyncStatus = "unknown"
	}
	if ch.Status != "available" && !(ch.Status == "unknown" && (ch.Text != "" || ch.Track != nil)) {
		return &copy
	}
	text := ch.Text
	var err error
	if ch.Track != nil && text == "" {
		text, err = SerializeTrack(ch.Track)
	}
	if err == nil {
		copy.Track, copy.Text, err = parseTrack(text)
	}
	if err == nil && ch.Track != nil && ch.Text == "" {
		copy.Track.Diagnostics = append(copy.Track.Diagnostics, ch.Track.Diagnostics...)
	}
	if errors.Is(err, ErrNoLyrics) {
		copy.Status, copy.Text, copy.Track = "unavailable", "", nil
		return &copy
	}
	if err != nil {
		copy.Status, copy.Message, copy.Err, copy.Text, copy.Track = "error", err.Error(), err, "", nil
		return &copy
	}
	copy.Diagnostics = append(copy.Diagnostics, copy.Track.Diagnostics...)
	timed, nonempty := 0, 0
	for _, l := range copy.Track.Lines {
		if strings.TrimSpace(l.Text) == "" {
			continue
		}
		nonempty++
		if l.StartMS != nil {
			timed++
		}
	}
	copy.SyncStatus = "synced"
	if timed == 0 {
		copy.SyncStatus = "unsynced"
	} else if timed < nonempty {
		copy.SyncStatus = "partial"
	}
	if copy.Status == "available" && copy.Origin == "unknown" {
		copy.Status = "unknown"
	}
	if ch.SyncStatus == "partial" && copy.SyncStatus == "synced" {
		copy.SyncStatus = "partial"
	}
	return &copy
}
func PreferredChannel(a, b *Channel) *Channel {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.Status == "available" {
		return a
	}
	if b.Status == "available" || a.Status == "unavailable" {
		return b
	}
	return a
}

func channelDocument(original, translation, romanization *Channel, timing string, warnings []string) (*Document, error) {
	return buildDocument(original, translation, romanization, nil, timing, warnings)
}

func buildDocument(original, translation, romanization *Channel, additional []*Channel, timing string, warnings []string) (*Document, error) {
	original, translation, romanization = normalizeChannel(original), normalizeChannel(translation), normalizeChannel(romanization)
	if original.Status == "error" {
		if original.Err != nil {
			return nil, fmt.Errorf("原文歌词解析失败: %w", original.Err)
		}
		return nil, fmt.Errorf("%w：原文歌词解析失败: %s", ErrFormat, original.Message)
	}
	if original.Status != "available" {
		return nil, ErrNoLyrics
	}
	for _, extra := range []*Channel{translation, romanization} {
		if AvailableContent(extra) && duplicateOriginal(original.Track, extra.Track) {
			extra.Status, extra.Text, extra.Track = "unavailable", "", nil
			extra.Diagnostics = append(extra.Diagnostics, Diagnostic{"duplicate_original", "仅为原文副本，不计作附加内容"})
		}
	}
	doc := &Document{Original: original, Translation: translation, Romanization: romanization, Warnings: append([]string(nil), warnings...)}
	if hasWords(original.Track) {
		timing = "available"
	} else if timing == "" || timing == "available" {
		timing = "unavailable"
	}
	doc.WordTiming = timing
	channels := []channelTrack{{"original", original}, {"translation", translation}, {"romanization", romanization}}
	for _, extra := range []struct {
		name, key string
		ch        *Channel
	}{{"翻译", "translation", translation}, {"罗马音", "romanization", romanization}} {
		ch := extra.ch
		if ch.Status == "error" {
			doc.Warnings = append(doc.Warnings, extra.name+"解析失败："+ch.Message)
		}
		if ch.Track != nil {
			if ch.SyncStatus != "synced" || original.SyncStatus != "synced" {
				doc.Warnings = append(doc.Warnings, extra.name+"没有可对齐的时间轴，保留为非同步文本")
			}
		}
	}
	for _, ch := range additional {
		ch = normalizeChannel(ch)
		doc.Additional = append(doc.Additional, ch)
		channels = append(channels, channelTrack{"additional", ch})
	}
	merged, rows, err := assembleTracks(channels)
	if err != nil {
		return nil, err
	}
	doc.Rows = rows
	doc.OffsetMS = merged.OffsetMS
	for _, c := range channels {
		if c.channel.Track != nil && invalidOffset(c.channel.Track) {
			doc.Warnings = append(doc.Warnings, "offset 无法确认，原始标签保留；实际时间不可用于评分")
		}
	}
	hasExtra := false
	for _, c := range channels[1:] {
		if c.channel.Track != nil {
			hasExtra = true
		}
	}
	if hasExtra {
		doc.Text, err = SerializeTrack(merged)
	} else {
		doc.Text = original.Text
	}
	if err != nil {
		return nil, err
	}
	doc.Format = trackFormat(merged)
	return doc, nil
}
