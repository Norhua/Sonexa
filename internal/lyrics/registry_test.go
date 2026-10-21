package lyrics

import (
	"context"
	"reflect"
	"testing"
)

func TestRegistrySingleCapabilityAndStableList(t *testing.T) {
	entries := []Entry{{&lyricsOnly{id: "163"}, "网易云音乐"}, {&lyricsOnly{id: "qq"}, "QQ 音乐"}, {&lyricsOnly{id: "kugou"}, "酷狗音乐"}}
	r, err := NewRegistry(entries...)
	if err != nil {
		t.Fatal(err)
	}
	want := []Source{{"163", "网易云音乐"}, {"qq", "QQ 音乐"}, {"kugou", "酷狗音乐"}}
	entries[0].Name = "changed"
	if !reflect.DeepEqual(r.List(), want) {
		t.Fatal(r.List())
	}
	list := r.List()
	list[0].Name = "changed"
	if !reflect.DeepEqual(r.List(), want) {
		t.Fatal("列表泄漏内部状态")
	}
	p, err := r.Get("qq")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.SearchLyrics(context.Background(), Query{Title: "song"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("cover-only"); err == nil {
		t.Fatal("未知能力未拒绝")
	}
	if p.(*lyricsOnly).calls != 1 {
		t.Fatal("未知来源触发了请求")
	}
}

func TestRegistryRejectsDuplicateAndMissingSources(t *testing.T) {
	for _, entries := range [][]Entry{{{}}, {{Provider: &lyricsOnly{}}}, {{Provider: &lyricsOnly{id: "same"}}, {Provider: &lyricsOnly{id: "same"}}}} {
		if r, err := NewRegistry(entries...); err == nil || r != nil {
			t.Fatal("非法集合被发布")
		}
	}
	var r *Registry
	if list := r.List(); list == nil || len(list) != 0 {
		t.Fatal(list)
	}
	if _, err := r.Get("qq"); err == nil {
		t.Fatal("未初始化不能回退静态来源")
	}
	r, err := NewRegistry(Entry{Provider: &lyricsOnly{id: "only"}})
	if err != nil || r.List()[0].Name != "歌词来源" {
		t.Fatal(r, err)
	}
}
