package qqmusic

import (
	"bytes"
	"compress/zlib"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyrics"
	des "github.com/norhua/sonexa/internal/provider/qqmusic/qrcdes"
)

// Online QRC is hex -> QQ's modified triple DES -> zlib. Local QMC-wrapped
// files are deliberately not accepted. Keep each stage bounded independently.
func decodeQRC(encoded string) (string, error) {
	if len(encoded) > 4<<20 {
		return "", fmt.Errorf("%w：QRC 数据过大", lyrics.ErrTooLarge)
	}
	data, err := hex.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data)%8 != 0 {
		return "", fmt.Errorf("QRC 十六进制或分组长度无效")
	}
	des.New([]byte("!@#)(NHL"), false).TransformBytes(data)
	des.New([]byte("123ZXC!@"), true).TransformBytes(data)
	des.New([]byte("!@#)(*$%"), false).TransformBytes(data)
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return "", fmt.Errorf("QRC 解压失败: %w", err)
	}
	defer r.Close()
	plain, err := io.ReadAll(io.LimitReader(r, lyrics.MaxTextBytes+1))
	if err != nil {
		return "", fmt.Errorf("QRC 解压失败: %w", err)
	}
	if len(plain) > lyrics.MaxTextBytes {
		return "", lyrics.ErrTooLarge
	}
	if !utf8.Valid(plain) || bytes.IndexByte(plain, 0) >= 0 {
		return "", fmt.Errorf("QRC 解码结果不是有效的 2 MiB 以内 UTF-8 文本")
	}
	return string(plain), nil
}

var qrcTrackName = regexp.MustCompile(`^Lyric_(\d+)$`)
var qrcTrackElement = regexp.MustCompile(`(?s)<Lyric_\d+\b[^<>]*/>`)

func extractQRCContent(raw string) (string, error) {
	parse := func(text string) (string, error) {
		decoder := xml.NewDecoder(strings.NewReader(text))
		path := []string{}
		tracks := map[int]string{}
		rootSeen := false
		for {
			token, err := decoder.Token()
			if err == io.EOF {
				break
			}
			if err != nil {
				return "", err
			}
			switch v := token.(type) {
			case xml.StartElement:
				if len(path) == 0 {
					if rootSeen || v.Name.Local != "QrcInfos" {
						return "", fmt.Errorf("invalid root")
					}
					rootSeen = true
				}
				path = append(path, v.Name.Local)
				if len(path) == 3 && path[1] == "LyricInfo" {
					match := qrcTrackName.FindStringSubmatch(v.Name.Local)
					if match == nil {
						return "", fmt.Errorf("unknown lyric track")
					}
					index, err := strconv.Atoi(match[1])
					if err != nil {
						return "", err
					}
					if _, exists := tracks[index]; exists {
						return "", fmt.Errorf("duplicate lyric track")
					}
					found := false
					for _, a := range v.Attr {
						if a.Name.Local == "LyricContent" {
							if found {
								return "", fmt.Errorf("duplicate content")
							}
							tracks[index] = a.Value
							found = true
						}
					}
					if !found {
						return "", fmt.Errorf("missing content")
					}
				}
			case xml.EndElement:
				path = path[:len(path)-1]
			case xml.CharData:
				if strings.TrimSpace(string(v)) != "" {
					return "", fmt.Errorf("unexpected container text")
				}
			}
		}
		if len(path) != 0 || len(tracks) == 0 {
			return "", fmt.Errorf("incomplete lyric container")
		}
		keys := []int{}
		for key := range tracks {
			keys = append(keys, key)
		}
		sort.Ints(keys)
		for _, key := range keys {
			if strings.TrimSpace(tracks[key]) != "" {
				return tracks[key], nil
			}
		}
		return "", lyrics.ErrNoLyrics
	}
	if content, err := parse(raw); err == nil {
		return content, nil
	}
	// Only a self-closing known track with LyricContent as its last attribute
	// has a unique repair boundary. Never search across '<' or '>' into a
	// neighbouring track; full XML validation still runs after quote repair.
	changed := false
	repaired := qrcTrackElement.ReplaceAllStringFunc(raw, func(element string) string {
		const marker = `LyricContent="`
		start := strings.Index(element, marker)
		if start < 0 || strings.Count(element, marker) != 1 {
			return element
		}
		start += len(marker)
		end := len(element) - 2
		for end > start && (element[end-1] == ' ' || element[end-1] == '\n' || element[end-1] == '\r' || element[end-1] == '\t') {
			end--
		}
		if end <= start || element[end-1] != '"' {
			return element
		}
		end--
		content := element[start:end]
		if !strings.Contains(content, `"`) {
			return element
		}
		// Attribute-looking suffixes are ambiguous, not literal quote repairs.
		if regexp.MustCompile(`"\s+[A-Za-z_:][\w:.-]*\s*=`).MatchString(content) {
			return element
		}
		changed = true
		return element[:start] + strings.ReplaceAll(content, `"`, "&quot;") + element[end:]
	})
	if changed {
		if content, err := parse(repaired); err == nil {
			return content, nil
		}
	}
	return "", fmt.Errorf("%w：QRC XML 边界无效或不唯一", lyrics.ErrFormat)
}

var qrcMetadata = regexp.MustCompile(`^\[[a-zA-Z]+:[^\]]*\]$`)
var qrcLineStart = regexp.MustCompile(`(?m)^\[\d+,\d+\]`)
var qrcLine = regexp.MustCompile(`^\[(\d+),(\d+)\](.*)$`)
var qrcWord = regexp.MustCompile(`\((\d+),(\d+)\)`)

func milliseconds(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 || n > 24*3600*1000 {
		return 0, fmt.Errorf("QRC 时间超出范围")
	}
	return n, nil
}
func clock(ms int) string { return fmt.Sprintf("%02d:%02d.%03d", ms/60000, ms/1000%60, ms%1000) }

// QRC word times are absolute milliseconds (not offsets from the line).
// Preserve real word ends too, including gaps, instead of deriving durations
// from the next word. Untimed credit lines stay ordinary LRC.
func qrcToLRC(raw string) (string, error) {
	if len(raw) > lyrics.MaxTextBytes {
		return "", fmt.Errorf("%w：QRC 文本过大", lyrics.ErrTooLarge)
	}
	if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("QRC 不是有效 UTF-8 文本")
	}
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "<") {
		var err error
		raw, err = extractQRCContent(raw)
		if err != nil {
			return "", err
		}
	}
	var out strings.Builder
	wordCount := 0
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if qrcMetadata.MatchString(line) {
			out.WriteString(line + "\n")
			continue
		}
		m := qrcLine.FindStringSubmatch(line)
		if m == nil {
			return "", fmt.Errorf("QRC 行时间解析失败")
		}
		start, err := milliseconds(m[1])
		if err != nil {
			return "", err
		}
		duration, err := milliseconds(m[2])
		if err != nil {
			return "", err
		}
		end := start + duration
		if end > 24*3600*1000 {
			return "", fmt.Errorf("QRC 行结束时间超出范围")
		}
		out.WriteString("[" + clock(start) + "]")
		words := qrcWord.FindAllStringSubmatchIndex(m[3], -1)
		pos, previousEnd := 0, start
		for _, w := range words {
			text := m[3][pos:w[0]]
			ts, err := milliseconds(m[3][w[2]:w[3]])
			if err != nil {
				return "", err
			}
			dur, err := milliseconds(m[3][w[4]:w[5]])
			if err != nil {
				return "", err
			}
			if ts < previousEnd || ts+dur > end {
				return "", fmt.Errorf("QRC 逐字时间无效或越界（行=%d-%d，前词结束=%d，当前=%d-%d，空词=%t）", start, end, previousEnd, ts, ts+dur, text == "")
			}
			if pos == 0 || ts != previousEnd {
				out.WriteString("<" + clock(ts) + ">")
			}
			out.WriteString(text)
			out.WriteString("<" + clock(ts+dur) + ">")
			if out.Len() > lyrics.MaxTextBytes {
				return "", lyrics.ErrTooLarge
			}
			pos, previousEnd = w[1], ts+dur
			if strings.TrimSpace(text) != "" {
				wordCount++
			}
		}
		if len(words) > 0 && strings.TrimSpace(m[3][pos:]) != "" {
			return "", fmt.Errorf("QRC 存在没有时间的尾部文字")
		}
		out.WriteString(m[3][pos:] + "\n")
		if out.Len() > lyrics.MaxTextBytes {
			return "", lyrics.ErrTooLarge
		}
	}
	if wordCount == 0 {
		return "", fmt.Errorf("QRC 未包含逐字时间")
	}
	return strings.TrimSpace(out.String()), nil
}
