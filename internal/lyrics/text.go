package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
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
var jsonp = regexp.MustCompile(`(?s)^\s*[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*\((.*)\);?\s*$`)

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

// Some services return timed credits plus an instrumental/no-lyrics marker
// even when their nolyric flag is false. Those are not usable lyric documents.
func parseNetworkText(text string) (*Document, error) {
	punctuation := strings.NewReplacer("，", "", ",", "", "。", "", " ", "", "！", "", "!", "")
	for _, line := range strings.Split(text, "\n") {
		plain := punctuation.Replace(strings.TrimSpace(lineTime.ReplaceAllString(line, "")))
		switch plain {
		case "纯音乐请欣赏", "纯音乐请您欣赏", "此歌曲为没有填词的纯音乐请您欣赏", "暂无歌词", "此歌曲暂无歌词":
			return nil, ErrNoLyrics
		}
	}
	return ParseText(text)
}

func unwrapJSON(body []byte) []byte {
	if json.Valid(body) {
		return body
	}
	if m := jsonp.FindSubmatch(body); len(m) == 2 {
		return m[1]
	}
	return body
}
func decodeLyric(text string) (string, error) {
	text = html.UnescapeString(text)
	// nobase64=1 can be ignored by the endpoint. Only decode valid UTF-8 Base64.
	if !strings.ContainsAny(text, "[\n ") && text != "" {
		if b, err := base64.StdEncoding.DecodeString(text); err == nil && utf8.Valid(b) {
			text = html.UnescapeString(string(b))
		}
	}
	return text, nil
}
func parseDocument(source string, body []byte) (*Document, error) {
	switch source {
	case "163":
		var r struct {
			Code        int
			Nolyric     bool
			Uncollected bool
			Lrc         struct{ Lyric string }
			Tlyric      struct{ Lyric string }
			Romalrc     struct{ Lyric string }
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		if r.Code != 200 {
			return nil, fmt.Errorf("网易云歌词响应异常（code=%d）", r.Code)
		}
		if r.Nolyric || r.Uncollected || strings.TrimSpace(r.Lrc.Lyric) == "" {
			return nil, ErrNoLyrics
		}
		return parseNetworkText(mergeTimed(r.Lrc.Lyric, r.Tlyric.Lyric, r.Romalrc.Lyric))
	case "qq":
		var r struct {
			Code    *int
			Retcode *int
			Lyric   string
			Trans   string
		}
		if err := json.Unmarshal(unwrapJSON(body), &r); err != nil {
			return nil, fmt.Errorf("QQ 歌词解析失败: %w", err)
		}
		if r.Code == nil || *r.Code != 0 || (r.Retcode != nil && *r.Retcode != 0) {
			return nil, fmt.Errorf("QQ 歌词接口不可用或需要认证")
		}
		text, _ := decodeLyric(r.Lyric)
		trans, _ := decodeLyric(r.Trans)
		if strings.TrimSpace(text) == "" {
			return nil, ErrNoLyrics
		}
		return parseNetworkText(mergeTimed(text, trans))
	case "kuwo":
		var r struct {
			Status int
			Msg    string
			Data   *struct {
				Lrclist []struct {
					Time      json.RawMessage
					LineLyric string
				}
			}
		}
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, err
		}
		if r.Status != 200 {
			return nil, fmt.Errorf("酷我歌词接口错误（status=%d）：%s", r.Status, r.Msg)
		}
		if r.Data == nil || r.Data.Lrclist == nil {
			return nil, ErrNoLyrics
		}
		timed := []timedLine{}
		plain := []string{}
		for _, l := range r.Data.Lrclist {
			text := html.UnescapeString(l.LineLyric)
			if strings.TrimSpace(text) == "" {
				continue
			}
			t, err := strconv.ParseFloat(strings.Trim(string(l.Time), `"`), 64)
			if err != nil || math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t > 24*3600 {
				plain = append(plain, text)
				continue
			}
			timed = append(timed, timedLine{int(math.Round(t * 1000)), text})
		}
		if len(plain) > 0 { // Mixed invalid timing: keep all content as plain text, not fake timestamps.
			plain = nil
			for _, l := range r.Data.Lrclist {
				plain = append(plain, html.UnescapeString(l.LineLyric))
			}
			return parseNetworkText(strings.Join(plain, "\n"))
		}
		return parseNetworkText(renderTimed(timed))
	}
	return nil, fmt.Errorf("未知歌词来源")
}

type timedLine struct {
	ms   int
	text string
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
func renderTimed(lines []timedLine) string {
	sort.SliceStable(lines, func(i, j int) bool { return lines[i].ms < lines[j].ms })
	out := []string{}
	seen := map[string]bool{}
	for _, l := range lines {
		line := fmt.Sprintf("[%02d:%02d.%03d]%s", l.ms/60000, l.ms/1000%60, l.ms%1000, l.text)
		if !seen[line] {
			seen[line] = true
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
func mergeTimed(original string, extras ...string) string {
	// Keep enhanced timing and metadata untouched if there is nothing to merge.
	hasExtra := false
	for _, extra := range extras {
		if strings.TrimSpace(extra) != "" {
			hasExtra = true
		}
	}
	if !hasExtra || !lineTime.MatchString(original) {
		return original
	}
	lines := []timedLine{}
	tags := []string{}
	for index, raw := range append([]string{original}, extras...) {
		for _, l := range strings.Split(raw, "\n") {
			matches := lineTime.FindAllStringSubmatch(l, -1)
			if len(matches) == 0 {
				if index == 0 {
					tags = append(tags, l)
				}
				continue
			}
			text := strings.TrimSpace(lineTime.ReplaceAllString(l, ""))
			for _, m := range matches {
				lines = append(lines, timedLine{timestamp(m), text})
			}
		}
	}
	return strings.Join(tags, "\n") + "\n" + renderTimed(lines)
}
func (s Service) fetchKugou(ctx context.Context, c Candidate) (*Document, error) {
	body, err := s.request(ctx, "kugou", "https://lyrics.kugou.com", "/search", map[string]string{"ver": "1", "man": "yes", "client": "pc", "keyword": c.Title + " " + c.Artist, "hash": c.Ref, "duration": strconv.Itoa(c.DurationMS)}, nil)
	if err != nil {
		return nil, err
	}
	var r struct {
		Status     int
		Candidates []struct {
			ID        string
			Accesskey string
			Duration  int
		}
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, err
	}
	if r.Status != 200 {
		return nil, fmt.Errorf("酷狗歌词候选接口错误（status=%d）", r.Status)
	}
	if len(r.Candidates) == 0 {
		return nil, ErrNoLyrics
	}
	// The song hash is sent to the lyric endpoint; among returned versions prefer
	// the closest duration instead of silently taking a shortened remix.
	if c.DurationMS > 0 {
		sort.SliceStable(r.Candidates, func(i, j int) bool {
			return math.Abs(float64(r.Candidates[i].Duration-c.DurationMS)) < math.Abs(float64(r.Candidates[j].Duration-c.DurationMS))
		})
	}
	candidate := r.Candidates[0]
	if candidate.ID == "" || candidate.Accesskey == "" {
		return nil, fmt.Errorf("酷狗歌词候选缺少下载标识")
	}
	body, err = s.request(ctx, "kugou", "https://lyrics.kugou.com", "/download", map[string]string{"ver": "1", "client": "pc", "id": candidate.ID, "accesskey": candidate.Accesskey, "fmt": "lrc", "charset": "utf8"}, nil)
	if err != nil {
		return nil, err
	}
	var d struct {
		Status    int
		ErrorCode int `json:"error_code"`
		Fmt       string
		Content   string
	}
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, err
	}
	if d.Status != 200 || d.ErrorCode != 0 || d.Fmt != "lrc" {
		return nil, fmt.Errorf("酷狗没有返回可用的 LRC 歌词")
	}
	text, err := base64.StdEncoding.DecodeString(d.Content)
	if err != nil {
		return nil, fmt.Errorf("酷狗歌词 Base64 解码失败: %w", err)
	}
	return parseNetworkText(string(text))
}
