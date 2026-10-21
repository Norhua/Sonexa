package kugou

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/provider/lyricfetch"
)

// Only the declared type plus Latin transliteration text confirms type=0.
// Han phonetic guides remain additional/unknown, not fabricated romanization.
func latinTransliteration(text string) bool {
	found := false
	for _, r := range text {
		if !unicode.IsLetter(r) {
			continue
		}
		if !unicode.In(r, unicode.Latin) {
			return false
		}
		found = true
	}
	return found
}

func addKRCLanguage(p *lyricdoc.Input, track *lyricdoc.Track, encoded string) {
	p.Translation, p.Romanization = lyricdoc.AbsentChannel(), lyricdoc.AbsentChannel()
	if encoded == "" {
		return
	}
	fail := func(err error) {
		p.Translation, p.Romanization = lyricdoc.FailedChannel(err), lyricdoc.FailedChannel(err)
	}
	if len(encoded) > lyricdoc.MaxTextBytes {
		fail(lyricdoc.ErrTooLarge)
		return
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if len(data) > lyricdoc.MaxTextBytes {
		fail(lyricdoc.ErrTooLarge)
		return
	}
	if err != nil || !utf8.Valid(data) {
		fail(fmt.Errorf("%w：KRC language 编码无效", lyricdoc.ErrFormat))
		return
	}
	var envelope struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Content == nil {
		fail(fmt.Errorf("%w：KRC language 结构无效", lyricdoc.ErrFormat))
		return
	}
	native := []lyricdoc.Line{}
	for _, l := range track.Lines {
		if l.StartMS != nil {
			native = append(native, l)
		}
	}
	for _, raw := range envelope.Content {
		var descriptor struct {
			Type    *int            `json:"type"`
			Content json.RawMessage `json:"lyricContent"`
		}
		if err := json.Unmarshal(raw, &descriptor); err != nil || descriptor.Type == nil {
			fail(fmt.Errorf("%w：KRC language 缺少类型", lyricdoc.ErrFormat))
			continue
		}
		var rows [][]string
		if err := json.Unmarshal(descriptor.Content, &rows); err != nil || rows == nil {
			bad := lyricdoc.FailedChannel(fmt.Errorf("%w：KRC language 行结构无效", lyricdoc.ErrFormat))
			if *descriptor.Type == 1 {
				p.Translation = bad
			} else if *descriptor.Type == 0 {
				p.Romanization = bad
			} else {
				p.Additional = append(p.Additional, bad)
			}
			continue
		}
		t := &lyricdoc.Track{OffsetMS: track.OffsetMS}
		var plain strings.Builder
		for i, row := range rows {
			text := strings.Join(row, "")
			plain.WriteString(text)
			l := lyricdoc.Line{Text: text}
			if len(rows) == len(native) {
				l.StartMS = native[i].StartMS
				l.ParentID, l.Voice = native[i].ID, native[i].Voice
				// type=0 syllables correspond to native words only when every
				// count agrees; otherwise preserve the text without guessed timing.
				if *descriptor.Type == 0 && len(row) == len(native[i].Parts) {
					for j, text := range row {
						w := native[i].Parts[j]
						w.Text, w.Ruby = text, nil
						l.Parts = append(l.Parts, w)
					}
				}
			}
			t.Lines = append(t.Lines, l)
		}
		ch := lyricdoc.TrackChannel(t, nil)
		ch.Origin = "embedded"
		if len(rows) != len(native) {
			ch.SyncStatus = "unsynced"
			ch.Diagnostics = append(ch.Diagnostics, lyricdoc.Diagnostic{Code: "language_count", Message: "language 行数与原始计时行不符，保留非同步文字"})
		}
		switch *descriptor.Type {
		case 1:
			p.Translation = lyricdoc.PreferredChannel(p.Translation, ch)
		case 0:
			if ch.Status == "available" && !latinTransliteration(plain.String()) {
				ch.Status = "unknown"
				ch.Diagnostics = append(ch.Diagnostics, lyricdoc.Diagnostic{Code: "unclassified_language", Message: "type=0 内容不是可确认的拉丁音译，保留为附加文字"})
				p.Additional = append(p.Additional, ch)
				if p.Romanization.Status != "available" {
					p.Romanization = lyricdoc.UnknownChannel()
				}
			} else {
				p.Romanization = lyricdoc.PreferredChannel(p.Romanization, ch)
			}
		default:
			if ch.Status == "available" {
				ch.Status = "unknown"
			}
			p.Additional = append(p.Additional, ch)
		}
	}
}

func krcPayload(encoded string) (*lyricdoc.Input, error) {
	raw, err := decodeKRC(encoded)
	if err != nil {
		return nil, err
	}
	t, language, err := parseKRC(raw)
	if err != nil {
		if !lyricfetch.CanFallback(err) {
			return nil, err
		}
		p := &lyricdoc.Input{Original: lyricdoc.FailedChannel(err), WordTiming: "error"}
		// The corrupt original cannot justify any language timestamps. Keep
		// already decoded language text independently for a same-song fallback.
		addKRCLanguage(p, &lyricdoc.Track{}, language)
		return p, err
	}
	text, err := lyricdoc.SerializeTrack(t)
	if err != nil {
		return nil, err
	}
	p := &lyricdoc.Input{Original: lyricdoc.TextChannel(text, nil), WordTiming: "unavailable"}
	if p.Original.Track != nil {
		t.OffsetMS = p.Original.Track.OffsetMS
		t.Diagnostics = append(t.Diagnostics, p.Original.Track.Diagnostics...)
		p.Original.Track = t
	}
	addKRCLanguage(p, t, language)
	if p.Original.Status == "available" {
		d, err := lyricdoc.ParseText(text)
		if err != nil {
			return nil, err
		}
		p.WordTiming = d.WordTiming
	}
	return p, nil
}
