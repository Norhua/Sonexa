package importer

import (
	"bytes"
	"testing"

	"github.com/norhua/sonexa/internal/export"
)

// TestExportImportRoundTrip 覆盖 20.6：
// 导出的 CSV 被本应用导入时编码/分隔符/表头自动识别且列映射自动命中。
func TestExportImportRoundTrip(t *testing.T) {
	// 导出一批含中日文的曲目
	tracks := []export.Track{
		{Title: "God knows...", Artist: strp("平野綾"), Album: strp("涼宮ハルヒの詰合"),
			Year: intp(2006), TrackNumber: intp(2), DurationMS: intp(332000)},
		{Title: "ハレ晴レユカイ", Artist: strp("平野綾"), Album: strp("涼宮ハルヒの詰合"),
			Year: intp(2006), TrackNumber: intp(5)},
	}
	var buf bytes.Buffer
	if err := export.Write(&buf, tracks, export.DefaultOptions()); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	// 再用导入功能读回
	res, err := Parse("我的歌单.csv", buf.Bytes(), "")
	if err != nil {
		t.Fatalf("导入失败: %v", err)
	}

	// 编码自动识别为 UTF-8（BOM）
	if res.Detection.Encoding != "UTF-8" {
		t.Errorf("编码 = %q, 期望 UTF-8", res.Detection.Encoding)
	}
	// 分隔符识别为逗号
	if res.Detection.Delimiter != "逗号" {
		t.Errorf("分隔符 = %q, 期望逗号", res.Detection.Delimiter)
	}
	// 表头被识别
	if !res.Detection.HasHeader {
		t.Error("应识别出表头")
	}
	// 无需手动介入
	if res.Detection.NeedsAttention {
		t.Errorf("往返导入不应需要手动设置: %s", res.Detection.Reason)
	}

	// 列映射自动命中曲名、艺术家、专辑
	for _, want := range []Field{FieldTitle, FieldArtist, FieldAlbum} {
		if !hasField(res.ColumnMap, want) {
			t.Errorf("列映射未命中 %q, 映射为 %v (表头 %v)", want, res.ColumnMap, res.Headers)
		}
	}

	// 数据行完整读回
	rows := AllRows(res)
	if len(rows) != 2 {
		t.Fatalf("读回 %d 行, 期望 2", len(rows))
	}
	if rows[0].Title != "God knows..." {
		t.Errorf("首行曲名 = %q", rows[0].Title)
	}
	if rows[0].Artist == nil || *rows[0].Artist != "平野綾" {
		t.Errorf("首行艺术家 = %v（中日文应无乱码）", rows[0].Artist)
	}
	if rows[0].Year == nil || *rows[0].Year != 2006 {
		t.Errorf("首行年份 = %v", rows[0].Year)
	}
}

func strp(s string) *string { return &s }
func intp(i int) *int       { return &i }
