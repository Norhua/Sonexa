package lyrics

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
var lineTime = regexp.MustCompile(`\[(\d{1,3}):([0-5]?\d)(?:[.:](\d{1,3}))?\]`)
var wordTime = regexp.MustCompile(`<\d{1,3}:[0-5]?\d(?:[.:]\d{1,3})?>`)
var metadata = regexp.MustCompile(`^\[[a-zA-Z]+:[^\]]*\]$`)

// ParseText preserves raw LRC/Enhanced LRC; it never invents timing for text.
func ParseText(text string) (*Document, error) {
	if len(text) > MaxTextBytes {
		return nil, fmt.Errorf("歌词不能超过 2 MiB")
	}
	if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
		return nil, fmt.Errorf("歌词必须是 UTF-8 文本，不能是 KRC/QRC 二进制")
	}
	text = strings.TrimSpace(strings.TrimPrefix(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\ufeff"))
	if strings.HasPrefix(text, "krc1") || strings.HasPrefix(text, "<?xml") || strings.HasPrefix(text, "<Qrc") {
		return nil, fmt.Errorf("请先将 KRC/QRC 转换为文本或 LRC")
	}
	content := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if metadata.MatchString(line) {
			continue
		}
		if strings.TrimSpace(wordTime.ReplaceAllString(lineTime.ReplaceAllString(line, ""), "")) != "" {
			content = true
			break
		}
	}
	if !content {
		return nil, ErrNoLyrics
	}
	format := "text"
	if lineTime.MatchString(text) {
		format = "lrc"
		if wordTime.MatchString(text) {
			format = "enhanced_lrc"
		}
	}
	return &Document{Text: text, Format: format}, nil
}

func parseNetworkText(text string) (*Document, error) {
	punctuation := strings.NewReplacer("，", "", ",", "", "。", "", " ", "", "！", "", "!", "")
	for _, line := range strings.Split(text, "\n") {
		plain := punctuation.Replace(strings.TrimSpace(wordTime.ReplaceAllString(lineTime.ReplaceAllString(line, ""), "")))
		switch plain {
		case "纯音乐请欣赏", "纯音乐请您欣赏", "此歌曲为没有填词的纯音乐请您欣赏", "暂无歌词", "此歌曲暂无歌词":
			return nil, ErrNoLyrics
		}
	}
	return ParseText(text)
}

// TimedLine is decoded content, not a platform-specific timestamp format.
type TimedLine struct {
	MS   int
	Text string
}

func timestamp(m []string) int {
	minutes, _ := strconv.Atoi(m[1])
	seconds, _ := strconv.Atoi(m[2])
	fraction := m[3]
	for len(fraction) < 3 {
		fraction += "0"
	}
	ms, _ := strconv.Atoi(fraction)
	return (minutes*60+seconds)*1000 + ms
}
func RenderTimed(lines []TimedLine) (string, error) {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].MS < lines[j].MS })
	var out strings.Builder
	seen := map[string]bool{}
	for _, l := range lines {
		line := fmt.Sprintf("[%02d:%02d.%03d]%s", l.MS/60000, l.MS/1000%60, l.MS%1000, l.Text)
		if !seen[line] {
			if out.Len()+len(line)+1 > MaxTextBytes {
				return "", fmt.Errorf("合并后的歌词超过 2 MiB")
			}
			seen[line] = true
			if out.Len() > 0 {
				out.WriteByte('\n')
			}
			out.WriteString(line)
		}
	}
	return out.String(), nil
}
func mergeTimed(original string, extras ...string) (string, error) {
	hasExtra := false
	for _, extra := range extras {
		if strings.TrimSpace(extra) != "" {
			hasExtra = true
		}
	}
	if !hasExtra {
		return original, nil
	}
	if !lineTime.MatchString(original) {
		return strings.Join(append([]string{original}, extras...), "\n"), nil
	}
	lines := []TimedLine{}
	tags, untimed := []string{}, []string{}
	for index, raw := range append([]string{original}, extras...) {
		for _, l := range strings.Split(raw, "\n") {
			matches := lineTime.FindAllStringSubmatch(l, -1)
			if len(matches) == 0 {
				if index == 0 {
					tags = append(tags, l)
				} else if strings.TrimSpace(l) != "" && !metadata.MatchString(strings.TrimSpace(l)) {
					untimed = append(untimed, l)
				}
				continue
			}
			text := strings.TrimSpace(lineTime.ReplaceAllString(l, ""))
			for _, m := range matches {
				lines = append(lines, TimedLine{timestamp(m), text})
			}
		}
	}
	rendered, err := RenderTimed(lines)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.Join(tags, "\n") + "\n" + rendered + "\n" + strings.Join(untimed, "\n")), nil
}
