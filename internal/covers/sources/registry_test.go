package sources

import (
	"context"
	"reflect"
	"testing"
)

func TestRegistrySingleCapabilityAndStableList(t *testing.T) {
	entries := []Entry{{&coversOnly{id: "caa"}, "CAA", false}, {&coversOnly{id: "itunes"}, "iTunes", true}, {&coversOnly{id: "qq"}, "QQ", false}, {&coversOnly{id: "163"}, "网易云", false}}
	r, err := NewRegistry(entries...)
	if err != nil {
		t.Fatal(err)
	}
	want := []Info{{"caa", "CAA", false}, {"itunes", "iTunes", true}, {"qq", "QQ", false}, {"163", "网易云", false}}
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
	if _, err := p.SearchCovers(context.Background(), Query{Title: "song"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("lyrics-only"); err == nil {
		t.Fatal("未知能力未拒绝")
	}
	if p.(*coversOnly).calls != 1 {
		t.Fatal("未知来源触发了请求")
	}
}

func TestRegistryRejectsDuplicateAndMissingSources(t *testing.T) {
	for _, entries := range [][]Entry{{{}}, {{Provider: &coversOnly{}}}, {{Provider: &coversOnly{id: "same"}}, {Provider: &coversOnly{id: "same"}}}} {
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
	r, err := NewRegistry(Entry{Provider: &coversOnly{id: "only"}})
	if err != nil || r.List()[0].Name != "封面来源" {
		t.Fatal(r, err)
	}
}
