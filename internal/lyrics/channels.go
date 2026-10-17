package lyrics

import (
	"errors"
	"fmt"
)

func AbsentChannel() *Channel          { return &Channel{Status: "unavailable"} }
func FailedChannel(err error) *Channel { return &Channel{Status: "error", Message: err.Error()} }
func TextChannel(text string, err error) *Channel {
	if err != nil {
		return FailedChannel(err)
	}
	doc, err := parseNetworkText(text)
	if errors.Is(err, ErrNoLyrics) {
		return AbsentChannel()
	}
	if err != nil {
		return FailedChannel(err)
	}
	return &Channel{Text: doc.Text, Status: "available"}
}
func PreferredChannel(a, b *Channel) *Channel {
	if a.Status == "available" {
		return a
	}
	if b.Status == "available" || a.Status == "unavailable" {
		return b
	}
	return a
}

func channelDocument(original, translation, romanization *Channel, timing string, warnings []string) (*Document, error) {
	if original.Status == "error" {
		return nil, fmt.Errorf("原文歌词解析失败: %s", original.Message)
	}
	if original.Status != "available" {
		return nil, ErrNoLyrics
	}
	merged, err := mergeTimed(original.Text, translation.Text, romanization.Text)
	var doc *Document
	if err == nil {
		doc, err = parseNetworkText(merged)
	}
	if err != nil {
		return nil, err
	}
	doc.Original, doc.Translation, doc.Romanization = original, translation, romanization
	doc.WordTiming, doc.Warnings = timing, warnings
	for _, extra := range []struct {
		name    string
		channel *Channel
	}{{"翻译", translation}, {"罗马音", romanization}} {
		if extra.channel.Status == "error" {
			doc.Warnings = append(doc.Warnings, extra.name+"解析失败："+extra.channel.Message)
		}
		if extra.channel.Status == "available" && (!lineTime.MatchString(extra.channel.Text) || !lineTime.MatchString(original.Text)) {
			doc.Warnings = append(doc.Warnings, extra.name+"没有可对齐的时间轴，保留为非同步文本")
		}
	}
	return doc, nil
}
