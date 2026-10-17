package caa

import (
	"os"
	"testing"
)

func TestCoverCatalogPreservesImagesAndVariants(t *testing.T) {
	body := []byte(`{"release":"https://musicbrainz.org/release/actual","images":[
		{"id":"1","front":true,"types":["Front","Booklet"],"image":"http://archive.org/full.png","thumbnails":{"250":"http://archive.org/s.jpg","small":"http://archive.org/s.jpg","500":"http://archive.org/m.jpg","large":"http://archive.org/m.jpg","1200":"http://archive.org/l.jpg"}},
		{"id":2,"back":true,"types":["Back"],"image":"http://archive.org/back.png"},
		{"id":"3","types":["Medium"],"thumbnails":{"small":"http://archive.org/disc.jpg"}}
	]}`)
	got, err := ParseCoverCatalog(body)
	if err != nil || got.Release != "https://musicbrainz.org/release/actual" || len(got.Images) != 3 {
		t.Fatalf("目录解析失败: %+v %v", got, err)
	}
	if len(got.Images[0].Variants) != 4 || len(got.Images[0].Types) != 2 || got.Images[1].ID != "2" || got.Images[1].Type != "Back" || got.Images[2].Type != "Medium" {
		t.Fatal("图片身份、类型或档位丢失")
	}
	if got.Images[2].Variants[0].Spec != "250" || got.Images[0].Variants[0].URL != "https://archive.org/s.jpg" {
		t.Fatal("旧别名/HTTPS 归一失败")
	}
}

func TestCoverCatalogEmptyInvalidAndRealMedium(t *testing.T) {
	for _, body := range []string{`{}`, `{"images":null}`, `broken`, `{"images":[{"id":"1"}]}`, `{"images":[{"image":"x"}]}`} {
		if _, err := ParseCoverCatalog([]byte(body)); err == nil {
			t.Fatalf("无效目录必须报错: %s", body)
		}
	}
	got, err := ParseCoverCatalog([]byte(`{"images":[]}`))
	if err != nil || len(got.Images) != 0 {
		t.Fatal("有效空目录被视作错误")
	}
	body, err := os.ReadFile("../../../testdata/golden/responses/musicbrainz/caa_lines.json")
	if err != nil {
		t.Fatal(err)
	}
	got, err = ParseCoverCatalog(body)
	if err != nil || len(got.Images) != 1 || got.Images[0].Type != "Medium" || len(got.Images[0].Variants) != 4 {
		t.Fatalf("真实 Medium 样例解析失败: %+v %v", got, err)
	}
}
