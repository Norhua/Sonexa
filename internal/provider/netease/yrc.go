package netease

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyricdoc"
)

var yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
var yrcWord = regexp.MustCompile(`\((\d+),(\d+),(-?\d+)\)`)

func yrcMillis(text string) (int64, error) {
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 || n > lyricdoc.MaxTimeMS {
		return 0, fmt.Errorf("%w：YRC 时间超出范围", lyricdoc.ErrFormat)
	}
	return n, nil
}
func yrcMS(n int64) *int64 { return &n }

func yrcChannel(text string) *lyricdoc.Channel {
	if strings.TrimSpace(text) == "" {
		return lyricdoc.AbsentChannel()
	}
	t, err := parseYRC(text)
	if err != nil {
		return lyricdoc.FailedChannel(err)
	}
	rendered, err := lyricdoc.SerializeTrack(t)
	if err != nil {
		return lyricdoc.FailedChannel(err)
	}
	ch := lyricChannel(rendered)
	if ch.Status == "available" {
		t.OffsetMS = ch.Track.OffsetMS
		t.Diagnostics = append(t.Diagnostics, ch.Track.Diagnostics...)
		ch.Track = t
	}
	return ch
}

func parseYRC(text string) (*lyricdoc.Track, error) {
	if len(text) > lyricdoc.MaxTextBytes {
		return nil, lyricdoc.ErrTooLarge
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, lyricdoc.ErrFormat
	}
	t := &lyricdoc.Track{}
	for _, raw := range strings.Split(strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff"), "\n") {
		if strings.TrimSpace(raw) == "" {
			t.Lines = append(t.Lines, lyricdoc.Line{Text: raw})
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(raw), "{") {
			// Known credits schema: c[].tx holds display text. Neither t nor
			// arbitrary JSON keys are assumed to be lyric timestamps.
			var credit struct {
				C []struct {
					Text *string `json:"tx"`
				} `json:"c"`
			}
			if err := json.Unmarshal([]byte(raw), &credit); err != nil || credit.C == nil {
				return nil, fmt.Errorf("%w：未知 YRC 署名结构", lyricdoc.ErrFormat)
			}
			var out strings.Builder
			for _, part := range credit.C {
				if part.Text == nil {
					return nil, lyricdoc.ErrFormat
				}
				out.WriteString(*part.Text)
			}
			t.Lines = append(t.Lines, lyricdoc.Line{Text: out.String()})
			continue
		}
		m := yrcLine.FindStringSubmatch(raw)
		if m == nil {
			// Recognized LRC metadata remains metadata; other free-form input
			// cannot masquerade as a valid native word-timing track.
			if regexp.MustCompile(`^\[[a-zA-Z]+:[^\]]*\]$`).MatchString(raw) {
				t.Metadata = append(t.Metadata, raw)
				continue
			}
			return nil, fmt.Errorf("%w：YRC 行时间无效", lyricdoc.ErrFormat)
		}
		start, err := yrcMillis(m[1])
		if err != nil {
			return nil, err
		}
		duration, err := yrcMillis(m[2])
		if err != nil {
			return nil, err
		}
		if duration > lyricdoc.MaxTimeMS-start {
			return nil, lyricdoc.ErrFormat
		}
		end := start + duration
		line := lyricdoc.Line{StartMS: yrcMS(start), EndMS: yrcMS(end)}
		matches := yrcWord.FindAllStringSubmatchIndex(m[3], -1)
		if len(matches) == 0 {
			line.Text = m[3]
		} else {
			if matches[0][0] != 0 {
				return nil, fmt.Errorf("%w：YRC 未计时前缀", lyricdoc.ErrFormat)
			}
			previous := start
			for i, match := range matches {
				wordStart, err := yrcMillis(m[3][match[2]:match[3]])
				if err != nil {
					return nil, err
				}
				wordDuration, err := yrcMillis(m[3][match[4]:match[5]])
				if err != nil {
					return nil, err
				}
				if wordStart < previous || wordStart > end || wordDuration > end-wordStart {
					return nil, fmt.Errorf("%w：YRC 词时间越界或倒序", lyricdoc.ErrFormat)
				}
				textEnd := len(m[3])
				if i+1 < len(matches) {
					textEnd = matches[i+1][0]
				}
				wordText := m[3][match[1]:textEnd]
				wordEnd := wordStart + wordDuration
				line.Parts = append(line.Parts, lyricdoc.Word{StartMS: yrcMS(wordStart), EndMS: yrcMS(wordEnd), Text: wordText})
				line.Text += wordText
				previous = wordEnd
			}
		}
		t.Lines = append(t.Lines, line)
	}
	return t, nil
}
