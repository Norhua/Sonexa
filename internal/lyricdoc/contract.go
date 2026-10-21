package lyricdoc

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

func cloneTrack(t *Track) *Track {
	out := *t
	out.Metadata = append([]string(nil), t.Metadata...)
	out.Diagnostics = append([]Diagnostic(nil), t.Diagnostics...)
	out.Lines = append([]Line(nil), t.Lines...)
	copyTime := func(n *int64) *int64 {
		if n == nil {
			return nil
		}
		return ms(*n)
	}
	for i := range out.Lines {
		l := &out.Lines[i]
		l.StartMS, l.EndMS = copyTime(l.StartMS), copyTime(l.EndMS)
		l.Parts = append([]Word(nil), l.Parts...)
		for j := range l.Parts {
			w := &l.Parts[j]
			w.StartMS, w.EndMS = copyTime(w.StartMS), copyTime(w.EndMS)
		}
	}
	return &out
}

func canonicalTrack(t *Track) error {
	if t == nil {
		return nil
	}
	entries := make([]taggedLine, len(t.Lines))
	for i, l := range t.Lines {
		entries[i] = taggedLine{line: l}
	}
	entries, err := deduplicate(entries)
	if err != nil {
		return err
	}
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i].line.StartMS, entries[j].line.StartMS
		return a != nil && (b == nil || *a < *b)
	})
	t.Lines = make([]Line, len(entries))
	for i, e := range entries {
		t.Lines[i] = e.line
	}
	return nil
}

// Compare representable semantics; native line ends are deliberately not
// invented by parsing LRC, nor discarded from a supplied structured track.
func matchingTrackText(a, b *Track) error {
	a, b = cloneTrack(a), cloneTrack(b)
	if err := canonicalTrack(a); err != nil {
		return err
	}
	if err := canonicalTrack(b); err != nil {
		return err
	}
	x, err := SerializeTrack(a)
	if err != nil {
		return err
	}
	y, err := SerializeTrack(b)
	if err != nil {
		return err
	}
	if x != y {
		return fmt.Errorf("%w：通道文本与结构不一致", ErrFormat)
	}
	return nil
}

// RetainedBytes conservatively accounts for text, tracks, rows and diagnostics
// even when strings happen to share storage. It is also suitable for session
// admission accounting. It never serializes or repairs a document.
func RetainedBytes(d *Document) int {
	if d == nil {
		return 0
	}
	n := 256 + len(d.Text) + projectionBytes(d.Rows)
	addDiagnostics := func(ds []Diagnostic) {
		for _, v := range ds {
			n += 64 + len(v.Code) + len(v.Message)
		}
	}
	addDiagnostics(d.Diagnostics)
	for _, w := range d.Warnings {
		n += 16 + len(w)
	}
	channels := append([]*Channel{d.Original, d.Translation, d.Romanization}, d.Additional...)
	for _, ch := range channels {
		if ch == nil {
			continue
		}
		n += 160 + len(ch.Text) + len(ch.Message) + len(ch.Status) + len(ch.Origin) + len(ch.SyncStatus)
		addDiagnostics(ch.Diagnostics)
		if ch.Track == nil {
			continue
		}
		n += 96
		for _, tag := range ch.Track.Metadata {
			n += 16 + len(tag)
		}
		for _, l := range ch.Track.Lines {
			n += lineBytes(l)
		}
		addDiagnostics(ch.Track.Diagnostics)
	}
	return n
}

func checkDocument(d *Document) error {
	if d == nil || !AvailableContent(d.Original) || d.Text == "" {
		return fmt.Errorf("%w：缺少有效原文", ErrContract)
	}
	if len(d.Text) > MaxTextBytes || projectionBytes(d.Rows) > MaxProjectionBytes || RetainedBytes(d) > 32<<20 {
		return ErrTooLarge
	}
	if !utf8.ValidString(d.Text) || strings.ContainsRune(d.Text, 0) || len(d.Rows) == 0 {
		return ErrContract
	}
	if d.Format != "text" && d.Format != "lrc" && d.Format != "enhanced_lrc" {
		return ErrContract
	}
	if d.WordTiming != "available" && d.WordTiming != "unavailable" && d.WordTiming != "unsupported" && d.WordTiming != "error" {
		return ErrContract
	}
	if (d.WordTiming == "available") != hasWords(d.Original.Track) {
		return ErrContract
	}
	for _, ch := range append([]*Channel{d.Original, d.Translation, d.Romanization}, d.Additional...) {
		if ch == nil {
			return fmt.Errorf("%w：缺少通道状态", ErrContract)
		}
		if ch.Origin != "independent" && ch.Origin != "embedded" && ch.Origin != "unknown" {
			return ErrContract
		}
		switch ch.Status {
		case "available":
			if !AvailableContent(ch) {
				return ErrContract
			}
		case "unknown":
		case "unavailable", "error":
			if ch.Track != nil || ch.Text != "" {
				return ErrContract
			}
		default:
			return ErrContract
		}
		if ch.Track == nil {
			if ch.SyncStatus != "unknown" || ch.Text != "" {
				return ErrContract
			}
			continue
		}
		if ch.Text == "" || ch.SyncStatus != "synced" && ch.SyncStatus != "unsynced" && ch.SyncStatus != "partial" {
			return ErrContract
		}
		if err := validateTrackSize(ch.Track); err != nil {
			return err
		}
	}
	return nil
}

func fingerprint(d *Document) ([32]byte, error) {
	h := sha256.New()
	// Stream the bounded DTO into the digest; no second copy of the retained
	// document or derived lyrics text is built by the consumer.
	err := json.NewEncoder(h).Encode(d)
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result, err
}

func seal(d *Document) error {
	if err := checkDocument(d); err != nil {
		return err
	}
	var err error
	d.integrity, err = fingerprint(d)
	return err
}

// Validate only accepts or rejects. The construction seal covers every public
// projection, including track/row times, channel facts and diagnostics. A
// half-built result or any later mutation cannot be "fixed" at this boundary.
func Validate(d *Document) error {
	if err := checkDocument(d); err != nil {
		return err
	}
	got, err := fingerprint(d)
	if err != nil {
		return fmt.Errorf("%w：%v", ErrContract, err)
	}
	if d.integrity == ([32]byte{}) || got != d.integrity {
		return fmt.Errorf("%w：文档未完成构建或交付内容已改变", ErrContract)
	}
	return nil
}
