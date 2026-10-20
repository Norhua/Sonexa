package netease

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyrics"
)

var yrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
var yrcWord = regexp.MustCompile(`\((\d+),(\d+),(-?\d+)\)`)

func yrcMillis(text string) (int64, error) {
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil || n < 0 || n > lyrics.MaxTimeMS {
		return 0, fmt.Errorf("%w：YRC 时间超出范围", lyrics.ErrFormat)
	}
	return n, nil
}
func yrcMS(n int64) *int64 { return &n }

func yrcChannel(text string) *lyrics.Channel {
	if strings.TrimSpace(text) == "" {
		return lyrics.AbsentChannel()
	}
	t, err := parseYRC(text)
	if err != nil {
		return lyrics.FailedChannel(err)
	}
	rendered, err := lyrics.SerializeTrack(t)
	if err != nil {
		return lyrics.FailedChannel(err)
	}
	return lyricChannel(rendered)
}

func parseYRC(text string) (*lyrics.Track, error) {
	if len(text) > lyrics.MaxTextBytes {
		return nil, lyrics.ErrTooLarge
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, lyrics.ErrFormat
	}
	t := &lyrics.Track{}
	for _, raw := range strings.Split(strings.TrimPrefix(strings.ReplaceAll(text, "\r\n", "\n"), "\ufeff"), "\n") {
		if strings.TrimSpace(raw) == "" {
			t.Lines = append(t.Lines, lyrics.Line{Text: raw})
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
				return nil, fmt.Errorf("%w：未知 YRC 署名结构", lyrics.ErrFormat)
			}
			var out strings.Builder
			for _, part := range credit.C {
				if part.Text == nil {
					return nil, lyrics.ErrFormat
				}
				out.WriteString(*part.Text)
			}
			t.Lines = append(t.Lines, lyrics.Line{Text: out.String()})
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
			return nil, fmt.Errorf("%w：YRC 行时间无效", lyrics.ErrFormat)
		}
		start, err := yrcMillis(m[1])
		if err != nil {
			return nil, err
		}
		duration, err := yrcMillis(m[2])
		if err != nil {
			return nil, err
		}
		if duration > lyrics.MaxTimeMS-start {
			return nil, lyrics.ErrFormat
		}
		end := start + duration
		line := lyrics.Line{StartMS: yrcMS(start), EndMS: yrcMS(end)}
		matches := yrcWord.FindAllStringSubmatchIndex(m[3], -1)
		if len(matches) == 0 {
			line.Text = m[3]
		} else {
			if matches[0][0] != 0 {
				return nil, fmt.Errorf("%w：YRC 未计时前缀", lyrics.ErrFormat)
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
					return nil, fmt.Errorf("%w：YRC 词时间越界或倒序", lyrics.ErrFormat)
				}
				textEnd := len(m[3])
				if i+1 < len(matches) {
					textEnd = matches[i+1][0]
				}
				wordText := m[3][match[1]:textEnd]
				wordEnd := wordStart + wordDuration
				line.Parts = append(line.Parts, lyrics.Word{StartMS: yrcMS(wordStart), EndMS: yrcMS(wordEnd), Text: wordText})
				line.Text += wordText
				previous = wordEnd
			}
		}
		t.Lines = append(t.Lines, line)
	}
	return t, nil
}
