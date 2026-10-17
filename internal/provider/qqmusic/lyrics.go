package qqmusic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyrics"
	"github.com/norhua/sonexa/internal/provider"
)

func (p *Provider) SearchLyrics(ctx context.Context, q lyrics.Query) ([]lyrics.Candidate, error) {
	songs, err := p.searchSongs(ctx, lyrics.SearchTerm(q), lyrics.MaxResults, provider.NoStore)
	if err != nil {
		return nil, err
	}
	out := []lyrics.Candidate{}
	for _, s := range songs {
		if !ValidMID(s.MID) {
			continue
		}
		out = append(out, lyrics.Candidate{Ref: s.MID, Title: songTitle(s), Artist: creditOf(s.Singer), Album: albumName(s.Album), DurationMS: s.Interval * 1000})
	}
	return out, nil
}

// crypt=0 applies to ordinary LRC/translation Base64, not encrypted QRC/roma.
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

func newQQChannels() *lyrics.Payload {
	return &lyrics.Payload{Original: lyrics.AbsentChannel(), Translation: lyrics.AbsentChannel(), Romanization: lyrics.AbsentChannel(), WordTiming: "unavailable"}
}
func failedQQChannels(err error) *lyrics.Payload {
	return &lyrics.Payload{Original: lyrics.FailedChannel(err), Translation: lyrics.FailedChannel(err), Romanization: lyrics.FailedChannel(err), WordTiming: "error"}
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
func qqRomanization(text string) *lyrics.Channel {
	if text == "" {
		return lyrics.AbsentChannel()
	}
	raw, err := decodeQRC(text)
	if err != nil {
		return lyrics.FailedChannel(err)
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "<") || qrcLineStart.MatchString(raw) {
		raw, err = qrcToLRC(raw)
	}
	return lyrics.TextChannel(raw, err)
}
func parseQQPlay(body []byte) *lyrics.Payload {
	out := newQQChannels()
	var response struct {
		Code       *int
		Req, Plain *qqLyricBlock
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
			out.Warnings = append(out.Warnings, err.Error())
			if i == 0 {
				out.WordTiming = "error"
			}
			out.Original = lyrics.PreferredChannel(out.Original, lyrics.FailedChannel(err))
			out.Translation = lyrics.PreferredChannel(out.Translation, lyrics.FailedChannel(err))
			out.Romanization = lyrics.PreferredChannel(out.Romanization, lyrics.FailedChannel(err))
			continue
		}
		original := lyrics.AbsentChannel()
		if data.QRC == 1 && data.Lyric != "" {
			raw, err := decodeQRC(data.Lyric)
			if err == nil {
				raw, err = qrcToLRC(raw)
			}
			original = lyrics.TextChannel(raw, err)
			if original.Status == "available" {
				out.WordTiming = "available"
			} else {
				out.WordTiming = "error"
				out.Warnings = append(out.Warnings, "逐字歌词解析失败："+original.Message)
			}
		} else {
			original = lyrics.TextChannel(qqText(data.Lyric, data.Crypt == 1))
		}
		out.Original = lyrics.PreferredChannel(out.Original, original)
		out.Translation = lyrics.PreferredChannel(out.Translation, lyrics.TextChannel(qqText(data.Trans, data.Crypt == 1)))
		out.Romanization = lyrics.PreferredChannel(out.Romanization, qqRomanization(data.Roma))
	}
	return out
}

func legacyPayload(parsed *legacyLyrics) *lyrics.Payload {
	return &lyrics.Payload{Original: lyrics.TextChannel(parsed.Original, parsed.OriginalError), Translation: lyrics.TextChannel(parsed.Translation, parsed.TranslationError), Romanization: lyrics.AbsentChannel(), WordTiming: "unsupported"}
}

func (p *Provider) FetchLyrics(ctx context.Context, c lyrics.Candidate) (*lyrics.Payload, error) {
	blocks := map[string]any{}
	for name, qrc := range map[string]int{"req": 1, "plain": 0} {
		blocks[name] = map[string]any{"module": "music.musichallSong.PlayLyricInfo", "method": "GetPlayLyricInfo", "param": map[string]any{
			"songMid": c.Ref, "crypt": 0, "qrc": qrc, "qrc_t": 0, "trans": 1, "trans_t": 0, "roma": 1, "roma_t": 0, "lrc_t": 0, "type": 0,
		}}
	}
	body, err := p.musicu(ctx, provider.NoStore, blocks)
	channels := newQQChannels()
	if err == nil {
		channels = parseQQPlay(body)
	} else {
		channels = failedQQChannels(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, parseErr := lyrics.DocumentFromPayload(channels); parseErr == nil {
		return channels, nil
	} else {
		err = parseErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	parsed, fallbackErr := p.legacyLyrics(ctx, c.Ref, provider.NoStore)
	var legacy *lyrics.Payload
	if fallbackErr == nil {
		legacy = legacyPayload(parsed)
		_, fallbackErr = lyrics.DocumentFromPayload(legacy)
	}
	if fallbackErr != nil {
		return nil, fmt.Errorf("QQ 新歌词接口：%w；普通歌词回退：%v", err, fallbackErr)
	}
	channels.Original = legacy.Original
	channels.Translation = lyrics.PreferredChannel(channels.Translation, legacy.Translation)
	channels.Romanization = lyrics.PreferredChannel(channels.Romanization, legacy.Romanization)
	channels.WordTiming = "error"
	channels.Warnings = append(channels.Warnings, "QQ 新歌词接口未取得可用原文，已回退普通歌词："+err.Error())
	return channels, nil
}

var _ lyrics.Provider = (*Provider)(nil)
