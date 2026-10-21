package lyricdoc

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Conservative accounting includes slice/pointer overhead as well as text;
// duplicate timestamps must not turn a small encoded input into a huge DTO.
func lineBytes(l Line) int {
	n := 96 + len(l.Text) + 64*len(l.Parts)
	for _, p := range l.Parts {
		n += len(p.Text)
	}
	return n
}

func validateTrackSize(t *Track) error {
	if t == nil {
		return ErrNoLyrics
	}
	n := 0
	for _, tag := range t.Metadata {
		if !utf8.ValidString(tag) || strings.ContainsRune(tag, 0) || !metadata.MatchString(strings.TrimSpace(tag)) {
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
		if !utf8.ValidString(l.Text) || strings.ContainsRune(l.Text, 0) {
			return ErrFormat
		}
	}
	if t.OffsetMS < -MaxTimeMS || t.OffsetMS > MaxTimeMS {
		return fmt.Errorf("%w：offset 超出范围", ErrFormat)
	}
	return nil
}

func projectionBytes(rows []PreviewRow) int {
	n := 0
	for _, r := range rows {
		n += 128 + len(r.Text) + len(r.Channel)
		for _, s := range r.Translation {
			n += 16 + len(s)
		}
		for _, s := range r.Romanization {
			n += 16 + len(s)
		}
	}
	return n
}
