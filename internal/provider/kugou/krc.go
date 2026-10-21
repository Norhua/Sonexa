package kugou

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyricdoc"
)

// Protocol constant, not an authentication credential. Native KRC payloads
// use the same cyclic XOR after the four-byte magic and before zlib.
var krcXOR = [...]byte{0x40, 0x47, 0x61, 0x77, 0x5e, 0x32, 0x74, 0x47, 0x51, 0x36, 0x31, 0x2d, 0xce, 0xd2, 0x6e, 0x69}

func decodeKRC(encoded string) (string, error) {
	if len(encoded) > 4<<20 {
		return "", lyricdoc.ErrTooLarge
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) < 5 || string(data[:4]) != "krc1" {
		return "", fmt.Errorf("%w：KRC 编码或文件头无效", lyricdoc.ErrFormat)
	}
	data = data[4:]
	for i := range data {
		data[i] ^= krcXOR[i%len(krcXOR)]
	}
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("%w：KRC 压缩数据无效", lyricdoc.ErrFormat)
	}
	defer r.Close()
	text, err := io.ReadAll(io.LimitReader(r, lyricdoc.MaxTextBytes+1))
	if len(text) > lyricdoc.MaxTextBytes {
		return "", lyricdoc.ErrTooLarge
	}
	if err != nil || !utf8.Valid(text) || bytes.IndexByte(text, 0) >= 0 {
		return "", fmt.Errorf("%w：KRC 解压或文本编码失败", lyricdoc.ErrFormat)
	}
	return string(text), nil
}

var krcLine = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
var krcWord = regexp.MustCompile(`<(-?\d+),(\d+),(-?\d+)>`)
var krcMetadata = regexp.MustCompile(`^\[[a-zA-Z]+:[^\]]*\]$`)

func krcMS(n int64) *int64 { return &n }
func krcNumber(raw string, signed bool) (int64, error) {
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n > lyricdoc.MaxTimeMS || n < -lyricdoc.MaxTimeMS || !signed && n < 0 {
		return 0, fmt.Errorf("%w：KRC 时间超出范围", lyricdoc.ErrFormat)
	}
	return n, nil
}

// The returned native lines still include empty timing rows: language indices
// must be resolved before display filtering, sorting or deduplication.
func parseKRC(raw string) (*lyricdoc.Track, string, error) {
	if len(raw) > lyricdoc.MaxTextBytes {
		return nil, "", lyricdoc.ErrTooLarge
	}
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return nil, "", lyricdoc.ErrFormat
	}
	t := &lyricdoc.Track{}
	language := ""
	for _, line := range strings.Split(strings.TrimPrefix(strings.ReplaceAll(raw, "\r\n", "\n"), "\ufeff"), "\n") {
		if strings.HasPrefix(line, "[language:") && strings.HasSuffix(line, "]") {
			if language != "" {
				return nil, "", fmt.Errorf("%w：重复 KRC language", lyricdoc.ErrFormat)
			}
			language = line[10 : len(line)-1]
			continue
		}
		if krcMetadata.MatchString(line) {
			t.Metadata = append(t.Metadata, line)
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := krcLine.FindStringSubmatch(line)
		if m == nil {
			return nil, language, fmt.Errorf("%w：KRC 行时间无效", lyricdoc.ErrFormat)
		}
		start, err := krcNumber(m[1], false)
		if err != nil {
			return nil, language, err
		}
		duration, err := krcNumber(m[2], false)
		if err != nil {
			return nil, language, err
		}
		if duration > lyricdoc.MaxTimeMS-start {
			return nil, language, lyricdoc.ErrFormat
		}
		l := lyricdoc.Line{StartMS: krcMS(start), EndMS: krcMS(start + duration)}
		matches := krcWord.FindAllStringSubmatchIndex(m[3], -1)
		if len(matches) == 0 {
			l.Text = m[3]
		} else {
			if matches[0][0] != 0 {
				return nil, language, lyricdoc.ErrFormat
			}
			previous := int64(-1)
			var text strings.Builder
			for i, match := range matches {
				rel, err := krcNumber(m[3][match[2]:match[3]], true)
				if err != nil {
					return nil, language, err
				}
				dur, err := krcNumber(m[3][match[4]:match[5]], false)
				if err != nil {
					return nil, language, err
				}
				absolute := start + rel // operands bounded to +/-24h, no integer overflow
				if absolute < 0 || absolute > lyricdoc.MaxTimeMS || dur > lyricdoc.MaxTimeMS-absolute || absolute < previous {
					return nil, language, fmt.Errorf("%w：KRC 绝对词时间越界或倒序", lyricdoc.ErrFormat)
				}
				end := len(m[3])
				if i+1 < len(matches) {
					end = matches[i+1][0]
				}
				word := m[3][match[1]:end]
				text.WriteString(word)
				l.Parts = append(l.Parts, lyricdoc.Word{StartMS: krcMS(absolute), EndMS: krcMS(absolute + dur), Text: word})
				previous = absolute + dur
			}
			l.Text = text.String()
		}
		l.ID = fmt.Sprintf("krc-%d", len(t.Lines))
		l, err = lyricdoc.DecodeInline(l)
		if err != nil {
			return nil, language, err
		}
		t.Lines = append(t.Lines, l)
	}
	return t, language, nil
}
