package netease

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/norhua/sonexa/internal/lyrics"
)

func lyricResponse(t *testing.T, original, translation, romanization string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"code": 200, "lrc": map[string]string{"lyric": original},
		"tlyric": map[string]string{"lyric": translation}, "romalrc": map[string]string{"lyric": romanization},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestLyricsNormalizeNativePlaceholders(t *testing.T) {
	for _, text := range []string{
		"[00:01.00]作曲 : 人\n[00:05.00]纯音乐，请欣赏",
		"[00:01]纯音乐，请您欣赏！", "[00:01]此歌曲为没有填词的纯音乐，请您欣赏",
		"暂无歌词", "[00:01]此歌曲暂无歌词", "[00:01]<00:01>纯音乐，请欣赏",
	} {
		if payload, err := parseLyrics(lyricResponse(t, text, "translation", "roma")); !errors.Is(err, lyrics.ErrNoLyrics) || payload != nil {
			t.Fatalf("未归一化占位 %q: %+v %v", text, payload, err)
		}
	}
	payload, err := parseLyrics(lyricResponse(t, "[00:01]原文", "[00:01]暂无歌词", "纯音乐，请欣赏"))
	if err != nil || payload.Original.Status != "available" || payload.Translation.Status != "unavailable" || payload.Romanization.Status != "unavailable" {
		t.Fatalf("附加通道占位影响原文: %+v %v", payload, err)
	}
	doc, err := lyrics.DocumentFromPayload(payload)
	if err != nil || doc.Text != "[00:01]原文" {
		t.Fatal(doc, err)
	}
}

func TestLyricsKeepTextAndValidationErrors(t *testing.T) {
	payload, err := parseLyrics(lyricResponse(t, "[00:01]原文", "[00:01]翻译", "[00:01]roma"))
	if err != nil || payload.Translation.Text != "[00:01]翻译" || payload.Romanization.Status != "available" {
		t.Fatal(payload, err)
	}
	for _, text := range []string{"[00:01]纯音乐，请欣赏\n" + strings.Repeat("x", lyrics.MaxTextBytes), "暂无歌词\x00"} {
		if ch := lyricChannel(text); ch.Status != "error" {
			t.Fatalf("占位不能掩盖格式或大小错误: %+v", ch)
		}
	}
}
