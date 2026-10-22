package qqmusic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/norhua/sonexa/internal/lyricdoc"
	"github.com/norhua/sonexa/internal/provider"
)

var lyricJSONP = regexp.MustCompile(`(?s)^\s*[A-Za-z_$][\w$]*(?:\.[A-Za-z_$][\w$]*)*\((.*)\);?\s*$`)

// legacyLyrics 保留上游原文及分通道解码结果。元数据保存原文，手动歌词使用
// 已解码通道；翻译解码错误不应抹掉可用原文或导致元数据其他字段丢失。
type legacyLyrics struct {
	RawOriginal                     string
	Original, Translation           string
	OriginalError, TranslationError error
}

func decodeLegacyText(text string) (string, error) {
	text = html.UnescapeString(text)
	if text != "" && !strings.ContainsAny(text, "[\n ") {
		if b, err := base64.StdEncoding.DecodeString(text); err == nil {
			if !utf8.Valid(b) {
				return "", fmt.Errorf("歌词 Base64 解码后不是 UTF-8")
			}
			text = html.UnescapeString(string(b))
		}
	}
	return text, nil
}

func parseLegacyLyrics(body []byte) (*legacyLyrics, error) {
	if !json.Valid(body) {
		if m := lyricJSONP.FindSubmatch(body); len(m) == 2 {
			body = m[1]
		}
	}
	var r lyricResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("解析 QQ 音乐歌词响应失败: %w", err)
	}
	if r.Code == nil {
		return nil, fmt.Errorf("QQ 音乐歌词响应缺少状态码")
	}
	// Observed legacy no-lyric response. Require all three codes and no
	// content; a conflicting/missing status remains an upstream failure.
	if *r.Code == -1901 && r.Retcode != nil && *r.Retcode == -1901 && r.Subcode != nil && *r.Subcode == -1901 && strings.TrimSpace(r.Lyric) == "" && strings.TrimSpace(r.Trans) == "" {
		return nil, lyricdoc.ErrNoLyrics
	}
	for _, code := range []*int{r.Code, r.Retcode, r.Subcode} {
		if code != nil && *code != 0 {
			return nil, &provider.BusinessError{Source: "QQ 音乐", Block: "legacy lyrics", Code: *code}
		}
	}
	out := &legacyLyrics{RawOriginal: r.Lyric}
	out.Original, out.OriginalError = decodeLegacyText(r.Lyric)
	out.Translation, out.TranslationError = decodeLegacyText(r.Trans)
	return out, nil
}

func (p *Provider) legacyLyrics(ctx context.Context, songMID string, mode provider.CacheMode) (*legacyLyrics, error) {
	params := map[string]string{
		"songmid": songMID, "g_tk": "5381", "loginUin": "0", "hostUin": "0", "format": "json", "inCharset": "utf8", "outCharset": "utf-8", "notice": "0", "platform": "yqq", "needNewCode": "0", "nobase64": "1",
	}
	body, err := p.client.Get(ctx, provider.GetRequest{
		Provider: provider.QQQuota, Endpoint: lyricPath, BaseURL: LyricBase,
		Params: params, ParamsHash: p.hash(params), CacheMode: mode,
		Headers: map[string]string{"Referer": "https://y.qq.com/"}, MaxResponseBytes: 4 << 20,
		Redirects: &provider.RedirectPolicy{Hosts: []string{"u.y.qq.com", "c.y.qq.com"}, MaxRedirects: 4},
	})
	if err != nil {
		return nil, err
	}
	return parseLegacyLyrics(body)
}

// ParseLyric 为元数据投影保留上游原文，不合并翻译或规范化行标签。
func ParseLyric(body []byte) (*string, error) {
	parsed, err := parseLegacyLyrics(body)
	if err != nil {
		return nil, err
	}
	return strPtr(strings.TrimSpace(parsed.RawOriginal)), nil
}

func (p *Provider) fetchLyric(ctx context.Context, songMID string, skipCache bool) (*string, error) {
	parsed, err := p.legacyLyrics(ctx, songMID, provider.CacheModeForRefresh(skipCache))
	if err != nil {
		return nil, err
	}
	return strPtr(strings.TrimSpace(parsed.RawOriginal)), nil
}
