package qqmusic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
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

func newQQChannels() *lyrics.Payload {
	return &lyrics.Payload{Original: lyrics.AbsentChannel(), Translation: lyrics.AbsentChannel(), Romanization: lyrics.AbsentChannel(), WordTiming: "unavailable"}
}
func failedQQChannels(err error) *lyrics.Payload {
	err = qqFormatError(err)
	return &lyrics.Payload{Original: lyrics.FailedChannel(err), Translation: lyrics.FailedChannel(err), Romanization: lyrics.FailedChannel(err), WordTiming: "error"}
}
func qqBlockData(raw map[string]json.RawMessage, name string) (*qqLyricData, error) {
	body, err := decodeBlock(raw, name)
	if err != nil {
		return nil, err
	}
	var data *qqLyricData
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("QQ 歌词数据解析失败: %w", err)
	}
	if data == nil {
		return nil, fmt.Errorf("QQ 歌词响应缺少 data")
	}
	return data, nil
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
func qqFormatError(err error) error {
	var business *provider.BusinessError
	if err == nil || errors.As(err, &business) || errors.Is(err, lyrics.ErrTooLarge) {
		return err
	}
	return fmt.Errorf("%w：%w", lyrics.ErrFormat, err)
}

func qqDecodedChannel(raw string, err error) *lyrics.Channel {
	if err != nil {
		return lyrics.FailedChannel(qqFormatError(err))
	}
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "<QrcInfos") || strings.HasPrefix(trimmed, "<?xml") || qrcLineStart.MatchString(raw) {
		raw, err = qrcToLRC(raw)
	}
	return lyrics.TextChannel(raw, qqFormatError(err))
}
func qqRomanization(text string) *lyrics.Channel {
	if text == "" {
		return lyrics.AbsentChannel()
	}
	return qqDecodedChannel(decodeQRC(text))
}
func parseQQPlay(body []byte) *lyrics.Payload {
	out := newQQChannels()
	response, err := decodeEnvelope(body)
	if err != nil {
		return failedQQChannels(err)
	}
	for i, name := range []string{"req", "plain"} {
		data, err := qqBlockData(response, name)
		if err != nil {
			err = qqFormatError(err)
			out.Warnings = append(out.Warnings, err.Error())
			if i == 0 {
				out.WordTiming = "error"
			}
			out.Original = lyrics.PreferredChannel(out.Original, lyrics.FailedChannel(err))
			out.Translation = lyrics.PreferredChannel(out.Translation, lyrics.FailedChannel(err))
			out.Romanization = lyrics.PreferredChannel(out.Romanization, lyrics.FailedChannel(err))
			continue
		}
		original := qqDecodedChannel(qqText(data.Lyric, data.QRC == 1 || data.Crypt == 1))
		if original.Status == "available" {
			actual, parseErr := lyrics.ParseText(original.Text)
			if parseErr == nil && actual.WordTiming == "available" {
				out.WordTiming = "available"
				out.WordOriginal = lyrics.PreferredChannel(out.WordOriginal, original)
			}
		} else if original.Status == "error" && i == 0 {
			out.WordTiming = "error"
			out.Warnings = append(out.Warnings, "逐字歌词解析失败："+original.Message)
		}
		out.Original = lyrics.PreferredChannel(out.Original, original)
		out.Translation = lyrics.PreferredChannel(out.Translation, qqDecodedChannel(qqText(data.Trans, data.Crypt == 1)))
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
		// Transport errors have no permission to start another protocol stage.
		if !lyrics.CanFallback(err) {
			return nil, err
		}
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
	if !lyrics.CanFallback(err) {
		return nil, err
	}
	for _, ch := range []*lyrics.Channel{channels.Original, channels.Translation, channels.Romanization} {
		if ch.Err != nil && !lyrics.CanFallback(ch.Err) {
			return nil, ch.Err
		}
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
