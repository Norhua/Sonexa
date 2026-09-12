package main

import (
	"strings"
	"testing"
)

// TestUpstreamQueryStripsNoiseAndKeepsAlternates 覆盖手动检索与复核「改搜」的
// 文本处理，它必须与导入匹配那条路径一致。
//
// 用户在复核队列里「改搜」时填的往往正是从表格里复制过来的原串
// （`原田瞳（原田ひとみ）`）。带着译名/读音注释直接做短语检索必然落空，
// 而括号里那种写法恰恰是上游用的那个——它必须作为备选写法进查询。
func TestUpstreamQueryStripsNoiseAndKeepsAlternates(t *testing.T) {
	q := upstreamQuery("綿雪の夢（繁雪之梦）", "原田瞳（原田ひとみ）", "心の羽根（ドラゴンボール改ver.）（心...", "JP")

	if q.Title != "綿雪の夢" {
		t.Errorf("曲名 = %q, 期望剥成主干 綿雪の夢", q.Title)
	}
	if q.Artist != "原田瞳" {
		t.Errorf("艺术家 = %q, 期望剥成主干 原田瞳", q.Artist)
	}
	if q.Album != "心の羽根" {
		t.Errorf("歌集 = %q, 期望去掉截断与注释后的 心の羽根", q.Album)
	}
	if len(q.ArtistAlternates) == 0 || q.ArtistAlternates[0] != "原田ひとみ" {
		t.Errorf("备选写法 = %v, 期望含 原田ひとみ", q.ArtistAlternates)
	}
	if q.Country != "JP" {
		t.Errorf("地区 = %q, 应原样传下去", q.Country)
	}
}

// TestUpstreamQueryKeepsVersionMarker 确保版本标记不被当成噪音剥掉：
// 它是这条录音身份的一部分，剥掉就换了一首歌。
func TestUpstreamQueryKeepsVersionMarker(t *testing.T) {
	q := upstreamQuery("That Girl (DJLIU Remix)", "Liu", "That Girl", "")
	if !strings.Contains(q.Title, "Remix") {
		t.Errorf("曲名 = %q, 版本标记不得剥掉", q.Title)
	}
}
