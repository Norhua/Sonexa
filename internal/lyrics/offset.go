package lyrics

import (
	"fmt"
	"strings"
)

func isOffset(tag string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(tag)), "[offset:")
}
func invalidOffset(t *Track) bool {
	for _, d := range t.Diagnostics {
		if d.Code == "invalid_offset" {
			return true
		}
	}
	return false
}

// The returned copies share no timestamp pointers with provider-owned tracks.
// Only multitrack assembly folds offsets, and all tracks get the same output
// offset. Negative effective times stay negative without negative LRC tags.
func commonTimeline(channels []channelTrack) ([]channelTrack, int64, error) {
	count := 0
	var first *int64
	same := true
	minActual, maxActual := int64(0), int64(0)
	for _, c := range channels {
		t := c.channel.Track
		if t == nil {
			continue
		}
		count++
		if first == nil {
			first = ms(t.OffsetMS)
		} else if *first != t.OffsetMS {
			same = false
		}
		visit := func(n *int64) {
			if n == nil {
				return
			}
			actual := *n - t.OffsetMS
			minActual = min(minActual, actual)
			maxActual = max(maxActual, actual)
		}
		for _, l := range t.Lines {
			visit(l.StartMS)
			visit(l.EndMS)
			for _, w := range l.Parts {
				visit(w.StartMS)
				visit(w.EndMS)
			}
		}
	}
	if count == 1 {
		return channels, channels[0].channel.Track.OffsetMS, nil
	}
	for _, c := range channels {
		if c.channel.Track != nil && invalidOffset(c.channel.Track) {
			return nil, 0, fmt.Errorf("%w：不能合并含矛盾或无效 offset 的通道", ErrFormat)
		}
	}
	global := -minActual
	if same {
		global = *first
	}
	if maxActual+global > MaxTimeMS {
		return nil, 0, fmt.Errorf("%w：合并时间轴超出 24 小时", ErrFormat)
	}
	out := make([]channelTrack, len(channels))
	for i, c := range channels {
		out[i] = c
		if c.channel.Track == nil {
			continue
		}
		ch := *c.channel
		t := *ch.Track
		t.Lines = make([]Line, len(ch.Track.Lines))
		t.Metadata = nil
		for _, tag := range ch.Track.Metadata {
			if !isOffset(tag) {
				t.Metadata = append(t.Metadata, tag)
			}
		}
		shift := func(n *int64) *int64 {
			if n == nil {
				return nil
			}
			return ms(*n - ch.Track.OffsetMS + global)
		}
		for j, l := range ch.Track.Lines {
			l.StartMS, l.EndMS = shift(l.StartMS), shift(l.EndMS)
			l.Parts = append([]Word(nil), l.Parts...)
			for k := range l.Parts {
				l.Parts[k].StartMS, l.Parts[k].EndMS = shift(l.Parts[k].StartMS), shift(l.Parts[k].EndMS)
			}
			t.Lines[j] = l
		}
		t.OffsetMS = global
		ch.Track = &t
		out[i].channel = &ch
	}
	return out, global, nil
}
