package lyrics

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"
)

func absentChannel() *Channel          { return &Channel{Status: "unavailable"} }
func failedChannel(err error) *Channel { return &Channel{Status: "error", Message: err.Error()} }
func textChannel(text string, err error) *Channel {
	if err != nil {
		return failedChannel(err)
	}
	doc, err := parseNetworkText(text)
	if errors.Is(err, ErrNoLyrics) {
		return absentChannel()
	}
	if err != nil {
		return failedChannel(err)
	}
	return &Channel{Text: doc.Text, Status: "available"}
}

func channelDocument(original, translation, romanization *Channel, timing string, warnings []string) (*Document, error) {
	if original.Status == "error" {
		return nil, fmt.Errorf("原文歌词解析失败: %s", original.Message)
	}
	if original.Status != "available" {
		return nil, ErrNoLyrics
	}
	merged, err := mergeTimed(original.Text, translation.Text, romanization.Text)
	var doc *Document
	if err == nil {
		doc, err = parseNetworkText(merged)
	}
	if err != nil {
		return nil, err
	}
	doc.Original, doc.Translation, doc.Romanization = original, translation, romanization
	doc.WordTiming, doc.Warnings = timing, warnings
	for _, extra := range []struct {
		name    string
		channel *Channel
	}{{"翻译", translation}, {"罗马音", romanization}} {
		if extra.channel.Status == "error" {
			doc.Warnings = append(doc.Warnings, extra.name+"解析失败："+extra.channel.Message)
		}
		if extra.channel.Status == "available" && (!lineTime.MatchString(extra.channel.Text) || !lineTime.MatchString(original.Text)) {
			doc.Warnings = append(doc.Warnings, extra.name+"没有可对齐的时间轴，保留为非同步文本")
		}
	}
	return doc, nil
}

// crypt=0 controls ordinary LRC/translation Base64, not QRC/romanization:
// QQ still returns those as encrypted hex. Do not infer availability from a
// successful HTTP response or decode failures from an empty field.
func decodeQQBase64(text string) (string, error) {
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	b, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return "", fmt.Errorf("Base64 解码失败: %w", err)
	}
	if !utf8.Valid(b) {
		return "", fmt.Errorf("歌词不是 UTF-8 编码")
	}
	return html.UnescapeString(string(b)), nil
}

type qqLyricData struct {
	QRC   int    `json:"qrc"`
	Crypt int    `json:"crypt"`
	Lyric string `json:"lyric"`
	Trans string `json:"trans"`
	Roma  string `json:"roma"`
}
type qqLyricBlock struct {
	Code *int         `json:"code"`
	Data *qqLyricData `json:"data"`
}
type qqChannels struct {
	original, translation, romanization *Channel
	timing                              string
	warnings                            []string
}

func newQQChannels() qqChannels {
	return qqChannels{absentChannel(), absentChannel(), absentChannel(), "unavailable", nil}
}
func failedQQChannels(err error) qqChannels {
	return qqChannels{failedChannel(err), failedChannel(err), failedChannel(err), "error", nil}
}
func (c qqChannels) document() (*Document, error) {
	return channelDocument(c.original, c.translation, c.romanization, c.timing, c.warnings)
}

func qqBlockData(block *qqLyricBlock) (*qqLyricData, error) {
	if block == nil || block.Code == nil {
		return nil, fmt.Errorf("QQ 歌词响应缺少状态码")
	}
	if *block.Code != 0 {
		return nil, fmt.Errorf("QQ 歌词接口错误（code=%d）", *block.Code)
	}
	if block.Data == nil {
		return nil, fmt.Errorf("QQ 歌词响应缺少 data")
	}
	return block.Data, nil
}
func qqText(text string, encrypted bool) (string, error) {
	if text == "" {
		return "", nil
	}
	if encrypted {
		return decodeQRC(text)
	}
	return decodeQQBase64(text)
}
func qqRomanization(text string) *Channel {
	if text == "" {
		return absentChannel()
	}
	raw, err := decodeQRC(text)
	if err != nil {
		return failedChannel(err)
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "<") || qrcLineStart.MatchString(raw) {
		raw, err = qrcToLRC(raw)
	}
	return textChannel(raw, err)
}
func preferredChannel(a, b *Channel) *Channel {
	if a.Status == "available" {
		return a
	}
	if b.Status == "available" || a.Status == "unavailable" {
		return b
	}
	return a
}

func parseQQPlay(body []byte) qqChannels {
	out := newQQChannels()
	var response struct {
		Code  *int
		Req   *qqLyricBlock
		Plain *qqLyricBlock
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return failedQQChannels(fmt.Errorf("QQ 歌词 JSON 解析失败: %w", err))
	}
	if response.Code == nil || *response.Code != 0 {
		return failedQQChannels(fmt.Errorf("QQ 歌词接口不可用或需要认证"))
	}
	for i, block := range []*qqLyricBlock{response.Req, response.Plain} {
		data, err := qqBlockData(block)
		if err != nil {
			out.warnings = append(out.warnings, err.Error())
			if i == 0 {
				out.timing = "error"
			}
			out.original = preferredChannel(out.original, failedChannel(err))
			out.translation = preferredChannel(out.translation, failedChannel(err))
			out.romanization = preferredChannel(out.romanization, failedChannel(err))
			continue
		}
		original := absentChannel()
		if data.QRC == 1 && data.Lyric != "" {
			raw, err := decodeQRC(data.Lyric)
			if err == nil {
				raw, err = qrcToLRC(raw)
			}
			original = textChannel(raw, err)
			if original.Status == "available" {
				out.timing = "available"
			} else {
				out.timing = "error"
				out.warnings = append(out.warnings, "逐字歌词解析失败："+original.Message)
			}
		} else {
			original = textChannel(qqText(data.Lyric, data.Crypt == 1))
		}
		out.original = preferredChannel(out.original, original)
		out.translation = preferredChannel(out.translation, textChannel(qqText(data.Trans, data.Crypt == 1)))
		out.romanization = preferredChannel(out.romanization, qqRomanization(data.Roma))
	}
	return out
}

func (s Service) fetchQQ(ctx context.Context, c Candidate) (*Document, error) {
	// Batch normal and word-timed lyrics in one quota-controlled HTTP request.
	// The normal block is a lossless fallback if QRC is absent or malformed.
	payload := map[string]any{"comm": map[string]any{"ct": 19, "cv": 1859, "uin": 0}}
	for name, qrc := range map[string]int{"req": 1, "plain": 0} {
		payload[name] = map[string]any{"module": "music.musichallSong.PlayLyricInfo", "method": "GetPlayLyricInfo", "param": map[string]any{
			"songMid": c.Ref, "crypt": 0, "qrc": qrc, "qrc_t": 0, "trans": 1, "trans_t": 0, "roma": 1, "roma_t": 0, "lrc_t": 0, "type": 0,
		}}
	}
	body, err := s.request(ctx, "qq", "https://u.y.qq.com", "/cgi-bin/musicu.fcg", nil, payload)
	channels := newQQChannels()
	if err == nil {
		channels = parseQQPlay(body)
	} else {
		channels = failedQQChannels(err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if doc, parseErr := channels.document(); parseErr == nil {
		return doc, nil
	} else {
		err = parseErr
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// A legacy fallback is visibly partial; it must not erase a successfully
	// decoded translation/romanization from the new endpoint.
	body, fallbackErr := s.request(ctx, "qq", "https://c.y.qq.com", "/lyric/fcgi-bin/fcg_query_lyric_new.fcg", map[string]string{
		"songmid": c.Ref, "g_tk": "5381", "loginUin": "0", "hostUin": "0", "format": "json", "inCharset": "utf8", "outCharset": "utf-8", "notice": "0", "platform": "yqq", "needNewCode": "0", "nobase64": "1",
	}, nil)
	var legacy *Document
	if fallbackErr == nil {
		legacy, fallbackErr = parseDocument("qq", body)
	}
	if fallbackErr != nil {
		return nil, fmt.Errorf("QQ 新歌词接口：%w；普通歌词回退：%v", err, fallbackErr)
	}
	channels.original = legacy.Original
	channels.translation = preferredChannel(channels.translation, legacy.Translation)
	channels.romanization = preferredChannel(channels.romanization, legacy.Romanization)
	channels.timing = "error"
	channels.warnings = append(channels.warnings, "QQ 新歌词接口未取得可用原文，已回退普通歌词："+err.Error())
	return channels.document()
}
