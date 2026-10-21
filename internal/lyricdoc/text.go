package lyricdoc

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrNoLyrics = errors.New("该候选暂无歌词")
var ErrFormat = errors.New("歌词格式无效")
var ErrTooLarge = errors.New("歌词内容过大")
var ErrContract = errors.New("歌词文档契约无效")

const MaxTextBytes = 2 << 20

const MaxTimeMS int64 = 24 * 3600 * 1000
const MaxProjectionBytes = MaxTextBytes

var lineTime = regexp.MustCompile(`^\[(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?\]`)
var wordTime = regexp.MustCompile(`<(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?>`)
var timeLike = regexp.MustCompile(`^[\[<]\d+:`)
var wordLike = regexp.MustCompile(`<\d+:[^>]*(?:>|$)`)
var metadata = regexp.MustCompile(`^\[[a-zA-Z]+:[^\]]*\]$`)

func normalizeText(text string) (string, error) {
	if len(text) > MaxTextBytes {
		return "", fmt.Errorf("%w：文本不能超过 2 MiB", ErrTooLarge)
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return "", fmt.Errorf("%w：必须是 UTF-8 文本，不能是 KRC/QRC 二进制", ErrFormat)
	}
	text = strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\ufeff")
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "krc1") || strings.HasPrefix(trimmed, "<?xml") || strings.HasPrefix(trimmed, "<Qrc") {
		return "", fmt.Errorf("%w：请先将 KRC/QRC 转换为文本或 LRC", ErrFormat)
	}
	return text, nil
}

// ParseText keeps imported text (apart from BOM/newlines). The reading
// projection does not rewrite the adoption text or infer word ends.
func ParseText(text string) (*Document, error) {
	return buildDocument(TextChannel(text, nil), nil, nil, nil, "", nil)
}

func parseTrack(text string) (*Track, string, error) {
	text, err := normalizeText(text)
	if err != nil {
		return nil, "", err
	}
	t := &Track{}
	content, used := false, 0
	var offset *int64
	for _, raw := range strings.Split(text, "\n") {
		if metadata.MatchString(strings.TrimSpace(raw)) {
			trimmed := strings.TrimSpace(raw)
			if strings.HasPrefix(strings.ToLower(trimmed), "[offset:") {
				n, parseErr := strconv.ParseInt(strings.TrimSpace(trimmed[8:len(trimmed)-1]), 10, 64)
				if parseErr != nil || n < -MaxTimeMS || n > MaxTimeMS || offset != nil && *offset != n {
					t.Diagnostics = append(t.Diagnostics, Diagnostic{Code: "invalid_offset", Message: "offset 标签无效或互相矛盾，无法确定实际时间"})
				} else {
					offset = ms(n)
					t.OffsetMS = n
				}
			}
			t.Metadata = append(t.Metadata, raw)
			continue
		}
		body := raw
		var starts []*int64
		for {
			m := lineTime.FindStringSubmatch(body)
			if m == nil {
				break
			}
			n, err := timestamp(m)
			if err != nil {
				return nil, "", err
			}
			starts = append(starts, ms(n))
			body = body[len(m[0]):]
		}
		if timeLike.MatchString(body) && strings.HasPrefix(body, "[") {
			return nil, "", fmt.Errorf("%w：行时间无效", ErrFormat)
		}
		line := Line{Text: body}
		if len(starts) > 0 {
			line.Parts, line.Text, err = parseWords(body)
			if err != nil {
				return nil, "", err
			}
		} else {
			starts = []*int64{nil}
		}
		if strings.TrimSpace(line.Text) != "" {
			content = true
		}
		cost := lineBytes(line)
		if len(starts) > (MaxProjectionBytes-used)/cost {
			return nil, "", fmt.Errorf("%w：时间戳展开超限", ErrTooLarge)
		}
		for _, start := range starts {
			copy := line
			copy.StartMS = start
			t.Lines = append(t.Lines, copy)
			used += cost
		}
	}
	if err := validateTrackSize(t); err != nil {
		return nil, "", err
	}
	if !content {
		return nil, "", ErrNoLyrics
	}
	return t, text, nil
}

// A tag between two text fragments is the next start, not proof of a real
// previous end. A trailing tag or the first of consecutive tags is an end.
func parseWords(body string) ([]Word, string, error) {
	for _, tag := range wordLike.FindAllString(body, -1) {
		if wordTime.FindString(tag) != tag {
			return nil, "", fmt.Errorf("%w：逐字标签无效", ErrFormat)
		}
	}
	matches := wordTime.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil, body, nil
	}
	parts := []Word{}
	var visible strings.Builder
	pos := 0
	var start *int64
	var previous int64 = -1
	for i, m := range matches {
		text := body[pos:m[0]]
		if text != "" {
			parts = append(parts, Word{StartMS: start, Text: text})
			visible.WriteString(text)
		}
		groups := wordTime.FindStringSubmatch(body[m[0]:m[1]])
		n, err := timestamp(groups)
		if err != nil {
			return nil, "", err
		}
		if n < previous {
			return nil, "", fmt.Errorf("%w：逐字时间倒序", ErrFormat)
		}
		previous = n
		isEnd := m[1] == len(body) || i+1 < len(matches) && matches[i+1][0] == m[1]
		if isEnd && text == "" && start != nil {
			// A pending start followed immediately by its end is a real empty
			// segment (native formats use these as timing placeholders). Keep
			// its bounds without inventing visible text or word coverage.
			parts = append(parts, Word{StartMS: start, EndMS: ms(n)})
			start = nil
		} else if isEnd && len(parts) > 0 && parts[len(parts)-1].StartMS != nil && parts[len(parts)-1].EndMS == nil {
			parts[len(parts)-1].EndMS = ms(n)
			start = nil
		} else {
			start = ms(n)
		}
		pos = m[1]
	}
	if pos < len(body) {
		parts = append(parts, Word{StartMS: start, Text: body[pos:]})
		visible.WriteString(body[pos:])
	} else if start != nil {
		parts = append(parts, Word{StartMS: start})
	}
	return parts, visible.String(), nil
}

func timestamp(m []string) (int64, error) {
	minutes, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || minutes > MaxTimeMS/60000 {
		return 0, fmt.Errorf("%w：时间超出 24 小时", ErrFormat)
	}
	seconds, _ := strconv.ParseInt(m[2], 10, 64)
	fraction := m[3] + strings.Repeat("0", 3-len(m[3]))
	millis, _ := strconv.ParseInt(fraction, 10, 64)
	n := minutes*60000 + seconds*1000 + millis
	if n > MaxTimeMS {
		return 0, fmt.Errorf("%w：时间超出 24 小时", ErrFormat)
	}
	return n, nil
}

func clock(n int64) string { return fmt.Sprintf("%02d:%02d.%03d", n/60000, n/1000%60, n%1000) }

func hasWords(t *Track) bool {
	for _, l := range t.Lines {
		for _, p := range l.Parts {
			if p.StartMS != nil && strings.TrimSpace(p.Text) != "" {
				return true
			}
		}
	}
	return false
}

func trackFormat(t *Track) string {
	if hasWords(t) {
		return "enhanced_lrc"
	}
	for _, l := range t.Lines {
		if l.StartMS != nil {
			return "lrc"
		}
	}
	return "text"
}

func projectTrack(t *Track, channel string) []PreviewRow {
	rows := make([]PreviewRow, 0, len(t.Lines))
	for _, l := range t.Lines {
		rows = append(rows, PreviewRow{TimeMS: l.StartMS, Text: l.Text, Channel: channel})
	}
	return rows
}

func serializeLine(l Line) (string, error) {
	var out strings.Builder
	valid := func(n *int64) bool { return n == nil || *n >= 0 && *n <= MaxTimeMS }
	if !valid(l.StartMS) || !valid(l.EndMS) {
		return "", fmt.Errorf("%w：行时间超出范围", ErrFormat)
	}
	if l.StartMS != nil {
		out.WriteString("[" + clock(*l.StartMS) + "]")
	}
	if len(l.Parts) == 0 {
		out.WriteString(l.Text)
	} else {
		var plain strings.Builder
		var previous int64 = -1
		for _, p := range l.Parts {
			if !valid(p.StartMS) || !valid(p.EndMS) || p.EndMS != nil && (p.StartMS == nil || *p.EndMS < *p.StartMS) || p.StartMS != nil && *p.StartMS < previous {
				return "", fmt.Errorf("%w：逐字时间无效", ErrFormat)
			}
			if p.StartMS != nil {
				out.WriteString("<" + clock(*p.StartMS) + ">")
				previous = *p.StartMS
			}
			out.WriteString(p.Text)
			plain.WriteString(p.Text)
			if p.EndMS != nil {
				out.WriteString("<" + clock(*p.EndMS) + ">")
				previous = *p.EndMS
			}
		}
		if plain.String() != l.Text {
			return "", fmt.Errorf("%w：词片段与行正文不一致", ErrFormat)
		}
	}
	if out.Len() > MaxTextBytes {
		return "", ErrTooLarge
	}
	return out.String(), nil
}

// SerializeTrack is for online conversion, not rewriting local imports.
func SerializeTrack(t *Track) (string, error) {
	if t == nil {
		return "", ErrNoLyrics
	}
	if err := validateTrackSize(t); err != nil {
		return "", err
	}
	var out strings.Builder
	written := false
	appendLine := func(s string) error {
		if len(s)+out.Len()+1 > MaxTextBytes {
			return ErrTooLarge
		}
		if written {
			out.WriteByte('\n')
		}
		written = true
		out.WriteString(s)
		return nil
	}
	for _, tag := range t.Metadata {
		if err := appendLine(tag); err != nil {
			return "", err
		}
	}
	if t.OffsetMS != 0 {
		hasOffset := false
		for _, tag := range t.Metadata {
			if isOffset(tag) {
				hasOffset = true
			}
		}
		if !hasOffset {
			if err := appendLine(fmt.Sprintf("[offset:%d]", t.OffsetMS)); err != nil {
				return "", err
			}
		}
	}
	for _, line := range t.Lines {
		s, err := serializeLine(line)
		if err != nil {
			return "", err
		}
		if err := appendLine(s); err != nil {
			return "", err
		}
	}
	return out.String(), nil
}

// TimedLine remains the simple row adapter used by line-only providers.
type TimedLine struct {
	MS   int
	Text string
}

func RenderTimed(lines []TimedLine) (string, error) {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].MS < lines[j].MS })
	t := &Track{}
	seen := map[string]bool{}
	for _, l := range lines {
		line := Line{StartMS: ms(int64(l.MS)), Text: l.Text}
		s, err := serializeLine(line)
		if err != nil {
			return "", err
		}
		if !seen[s] {
			seen[s] = true
			t.Lines = append(t.Lines, line)
		}
	}
	return SerializeTrack(t)
}

func mergeTimed(original string, extras ...string) (string, error) {
	tracks := []*Track{}
	for _, text := range append([]string{original}, extras...) {
		if strings.TrimSpace(text) == "" {
			continue
		}
		t, _, err := parseTrack(text)
		if err != nil {
			return "", err
		}
		tracks = append(tracks, t)
	}
	if len(tracks) == 1 {
		return original, nil
	}
	merged := &Track{}
	for i, t := range tracks {
		if i == 0 {
			merged.Metadata = t.Metadata
		}
		merged.Lines = append(merged.Lines, t.Lines...)
	}
	sort.SliceStable(merged.Lines, func(i, j int) bool {
		a, b := merged.Lines[i].StartMS, merged.Lines[j].StartMS
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return *a < *b
	})
	return SerializeTrack(merged)
}
