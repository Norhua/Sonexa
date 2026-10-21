package lyricdoc

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Conservative accounting includes slice/pointer overhead as well as text;
// duplicate timestamps must not turn a small encoded input into a huge DTO.
func lineBytes(l Line) int {
	n := 160 + len(l.Text) + len(l.ID) + len(l.ParentID) + len(l.Language) + len(l.Voice) + rubyBytes(l.Ruby) + 64*len(l.Parts)
	for _, p := range l.Parts {
		n += len(p.Text) + rubyBytes(p.Ruby)
	}
	return n
}

func validateTrackSize(t *Track) error {
	if t == nil {
		return ErrNoLyrics
	}
	n := 0
	for _, tag := range t.Metadata {
		if !utf8.ValidString(tag) || strings.ContainsRune(tag, 0) || !xlrcHeader.MatchString(strings.TrimSpace(tag)) {
			return fmt.Errorf("%w：元信息无效", ErrFormat)
		}
		n += len(tag) + 32
		if n > MaxProjectionBytes {
			return ErrTooLarge
		}
	}
	for _, l := range t.Lines {
		n += lineBytes(l)
		if n > MaxProjectionBytes {
			return ErrTooLarge
		}
		if !utf8.ValidString(l.Text+l.Voice+l.Language+l.ID+l.ParentID) || strings.ContainsAny(l.Text, "\x00\r\n") || strings.ContainsAny(l.Voice+l.ID+l.ParentID, "\x00\r\n[]") || l.Language != "" && !languageTag.MatchString(l.Language) {
			return ErrFormat
		}
		if l.Independent && l.ParentID != "" {
			return ErrFormat
		}
		if _, err := serializeLine(l); err != nil {
			return err
		}
	}
	if t.OffsetMS < -MaxTimeMS || t.OffsetMS > MaxTimeMS {
		return fmt.Errorf("%w：offset 超出范围", ErrFormat)
	}
	return nil
}

func rubyBytes(rs []Ruby) int {
	n := 32 * len(rs)
	for _, r := range rs {
		n += len(r.Reading)
	}
	return n
}

func projectionBytes(rows []PreviewRow) int {
	n := 0
	for _, r := range rows {
		n += 192 + len(r.Text) + len(r.Channel) + len(r.Voice) + len(r.Language) + rubyBytes(r.Ruby)
		for _, a := range r.Annotations {
			n += 64 + len(a.Channel) + len(a.Language) + len(a.Text) + rubyBytes(a.Ruby)
		}
		for _, s := range r.Translation {
			n += 16 + len(s)
		}
		for _, s := range r.Romanization {
			n += 16 + len(s)
		}
	}
	return n
}
