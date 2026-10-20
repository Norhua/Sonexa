package kugou

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyrics"
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

func addKRCLanguage(p *lyrics.Payload, track *lyrics.Track, encoded string) {
	p.Translation, p.Romanization = lyrics.AbsentChannel(), lyrics.AbsentChannel()
	if encoded == "" {
		return
	}
	fail := func(err error) { p.Translation, p.Romanization = lyrics.FailedChannel(err), lyrics.FailedChannel(err) }
	if len(encoded) > lyrics.MaxTextBytes {
		fail(lyrics.ErrTooLarge)
		return
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if len(data) > lyrics.MaxTextBytes {
		fail(lyrics.ErrTooLarge)
		return
	}
	if err != nil || !utf8.Valid(data) {
		fail(fmt.Errorf("%w：KRC language 编码无效", lyrics.ErrFormat))
		return
	}
	var envelope struct {
		Content []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Content == nil {
		fail(fmt.Errorf("%w：KRC language 结构无效", lyrics.ErrFormat))
		return
	}
	native := []lyrics.Line{}
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
			fail(fmt.Errorf("%w：KRC language 缺少类型", lyrics.ErrFormat))
			continue
		}
		var rows [][]string
		if err := json.Unmarshal(descriptor.Content, &rows); err != nil || rows == nil {
			bad := lyrics.FailedChannel(fmt.Errorf("%w：KRC language 行结构无效", lyrics.ErrFormat))
			if *descriptor.Type == 1 {
				p.Translation = bad
			} else if *descriptor.Type == 0 {
				p.Romanization = bad
			} else {
				p.Additional = append(p.Additional, bad)
			}
			continue
		}
		t := &lyrics.Track{OffsetMS: track.OffsetMS}
		var plain strings.Builder
		for i, row := range rows {
			text := strings.Join(row, "")
			plain.WriteString(text)
			l := lyrics.Line{Text: text}
			if len(rows) == len(native) {
				l.StartMS = native[i].StartMS
			}
			t.Lines = append(t.Lines, l)
		}
		text, err := lyrics.SerializeTrack(t)
		ch := lyrics.TextChannel(text, err)
		ch.Origin = "embedded"
		if len(rows) != len(native) {
			ch.SyncStatus = "unsynced"
			ch.Diagnostics = append(ch.Diagnostics, lyrics.Diagnostic{Code: "language_count", Message: "language 行数与原始计时行不符，保留非同步文字"})
		}
		switch *descriptor.Type {
		case 1:
			p.Translation = lyrics.PreferredChannel(p.Translation, ch)
		case 0:
			if ch.Status == "available" && !latinTransliteration(plain.String()) {
				ch.Status = "unknown"
				ch.Diagnostics = append(ch.Diagnostics, lyrics.Diagnostic{Code: "unclassified_language", Message: "type=0 内容不是可确认的拉丁音译，保留为附加文字"})
				p.Additional = append(p.Additional, ch)
				if p.Romanization.Status != "available" {
					p.Romanization = lyrics.UnknownChannel()
				}
			} else {
				p.Romanization = lyrics.PreferredChannel(p.Romanization, ch)
			}
		default:
			if ch.Status == "available" {
				ch.Status = "unknown"
			}
			p.Additional = append(p.Additional, ch)
		}
	}
}

func krcPayload(encoded string) (*lyrics.Payload, error) {
	raw, err := decodeKRC(encoded)
	if err != nil {
		return nil, err
	}
	t, language, err := parseKRC(raw)
	if err != nil {
		if !lyrics.CanFallback(err) {
			return nil, err
		}
		p := &lyrics.Payload{Original: lyrics.FailedChannel(err), WordTiming: "error"}
		// The corrupt original cannot justify any language timestamps. Keep
		// already decoded language text independently for a same-song fallback.
		addKRCLanguage(p, &lyrics.Track{}, language)
		return p, err
	}
	text, err := lyrics.SerializeTrack(t)
	if err != nil {
		return nil, err
	}
	p := &lyrics.Payload{Original: lyrics.TextChannel(text, nil), WordTiming: "unavailable"}
	if p.Original.Track != nil {
		t.OffsetMS = p.Original.Track.OffsetMS
	}
	addKRCLanguage(p, t, language)
	if p.Original.Status == "available" {
		d, err := lyrics.ParseText(text)
		if err != nil {
			return nil, err
		}
		p.WordTiming = d.WordTiming
	}
	return p, nil
}
