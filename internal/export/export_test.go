package export

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"
)

func sptr(s string) *string { return &s }
func iptr(i int) *int       { return &i }

func sampleTracks() []Track {
	return []Track{
		{
			Title: "God knows...", Artist: sptr("平野綾"), Album: sptr("涼宮ハルヒの詰合"),
			Year: iptr(2006), TrackNumber: iptr(2), DurationMS: iptr(332000),
			Genre: sptr("anime"), Label: sptr("Lantis"), MediumFormat: sptr("CD"),
			Country: sptr("JP"), MatchSource: sptr("musicbrainz"), MatchRef: sptr("trk-1"),
		},
		{
			// 「无需匹配」入库的曲目：丰富信息整体为空
			Title: "无名曲", Artist: sptr("未知艺术家"),
		},
	}
}

// TestCSVDefaultsToUTF8BOM 覆盖 20.3：
// 默认带 BOM，使常见表格软件打开含中日文的 CSV 不乱码。
func TestCSVDefaultsToUTF8BOM(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleTracks(), DefaultOptions()); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	data := buf.Bytes()
	if len(data) < 3 || data[0] != 0xEF || data[1] != 0xBB || data[2] != 0xBF {
		t.Error("默认应写入 UTF-8 BOM")
	}
	// BOM 之后是正常的 UTF-8 内容
	text := string(data[3:])
	if !strings.Contains(text, "God knows...") || !strings.Contains(text, "平野綾") {
		t.Errorf("内容异常: %.100q", text)
	}
}

// TestBasicDepthOnlyThreeColumns 覆盖 20.2：仅基本信息为三列。
func TestBasicDepthOnlyThreeColumns(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Depth = DepthBasic
	if err := Write(&buf, sampleTracks(), opts); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	records := parseCSV(t, buf.Bytes())
	if len(records[0]) != 3 {
		t.Errorf("列数 = %d, 期望 3", len(records[0]))
	}
	want := []string{"曲名", "艺术家", "专辑"}
	for i, w := range want {
		if records[0][i] != w {
			t.Errorf("第 %d 列 = %q, 期望 %q", i, records[0][i], w)
		}
	}
}

// TestFullDepthIncludesRichFields 覆盖 20.2：
// 基本 + 丰富时额外包含年份、音轨号等字段。
func TestFullDepthIncludesRichFields(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleTracks(), DefaultOptions()); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	records := parseCSV(t, buf.Bytes())
	header := strings.Join(records[0], ",")
	for _, field := range []string{"年份", "音轨", "碟号", "时长", "风格", "厂牌", "目录号", "载体", "国家"} {
		if !strings.Contains(header, field) {
			t.Errorf("表头缺少 %q: %s", field, header)
		}
	}
}

// TestMissingFieldsAreEmptyNotZero 覆盖 20.2 与「缺失值不被伪装」：
// 缺失字段为空而不是零值。
func TestMissingFieldsAreEmptyNotZero(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, sampleTracks(), DefaultOptions()); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	records := parseCSV(t, buf.Bytes())
	// 第二条是丰富信息整体为空的曲目
	row := records[2]
	yearIdx := indexOf(records[0], "年份")
	if row[yearIdx] != "" {
		t.Errorf("缺失的年份导出为 %q, 期望空串而不是 0", row[yearIdx])
	}
	trackNoIdx := indexOf(records[0], "音轨")
	if row[trackNoIdx] != "" {
		t.Errorf("缺失的音轨号导出为 %q, 期望空串", row[trackNoIdx])
	}
	// 有值的字段照常导出
	if records[1][yearIdx] != "2006" {
		t.Errorf("有值的年份 = %q, 期望 2006", records[1][yearIdx])
	}
}

// TestJSONExport 覆盖 20.1 的 JSON 格式。
func TestJSONExport(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.Format = FormatJSON
	if err := Write(&buf, sampleTracks(), opts); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	var got []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("JSON 无法解析: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("条目数 = %d, 期望 2", len(got))
	}
	if got[0]["title"] != "God knows..." {
		t.Errorf("首条曲名 = %v", got[0]["title"])
	}
	// 缺失字段不出现（omitempty），而不是导出为 0
	if _, has := got[1]["year"]; has {
		t.Error("缺失的年份不应出现在 JSON 中")
	}
}

// TestJSONBasicDepthDropsRichFields 确保仅基本信息时不带丰富字段。
func TestJSONBasicDepthDropsRichFields(t *testing.T) {
	var buf bytes.Buffer
	opts := Options{Format: FormatJSON, Depth: DepthBasic}
	if err := Write(&buf, sampleTracks(), opts); err != nil {
		t.Fatalf("导出失败: %v", err)
	}

	var got []map[string]any
	json.Unmarshal(buf.Bytes(), &got)
	if _, has := got[0]["year"]; has {
		t.Error("仅基本信息时不应含年份")
	}
	if _, has := got[0]["genre"]; has {
		t.Error("仅基本信息时不应含风格")
	}
	if got[0]["title"] != "God knows..." {
		t.Error("基本信息应保留")
	}
}

// TestNoCoverImagesInOutput 覆盖 20.4：
// 导出产物不含图片文件也不含内嵌图片数据。
func TestNoCoverImagesInOutput(t *testing.T) {
	for _, format := range []Format{FormatCSV, FormatJSON} {
		var buf bytes.Buffer
		opts := DefaultOptions()
		opts.Format = format
		if err := Write(&buf, sampleTracks(), opts); err != nil {
			t.Fatalf("导出失败: %v", err)
		}

		text := buf.String()
		for _, marker := range []string{"cover", "封面", "artwork", "data:image", "base64", ".jpg", ".png"} {
			if strings.Contains(strings.ToLower(text), marker) {
				t.Errorf("%s 导出含图片相关内容 %q", format, marker)
			}
		}
	}
}

// TestBOMCanBeDisabled 确保 BOM 可关闭。
func TestBOMCanBeDisabled(t *testing.T) {
	var buf bytes.Buffer
	opts := DefaultOptions()
	opts.UTF8BOM = false
	Write(&buf, sampleTracks(), opts)

	data := buf.Bytes()
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		t.Error("关闭后不应写入 BOM")
	}
}

// TestExportedCSVHeadersMatchImportDictionary 覆盖 20.6：
// 导出的 CSV 被本应用导入时列映射应自动命中。
//
// 此处断言表头用的就是导入词典认得的写法；
// 完整往返在集成验收中验证。
func TestExportedCSVHeadersMatchImportDictionary(t *testing.T) {
	var buf bytes.Buffer
	Write(&buf, sampleTracks(), DefaultOptions())
	records := parseCSV(t, buf.Bytes())

	// 这些列名必须是导入词典中的常见写法
	mustMap := map[string]bool{
		"曲名": true, "艺术家": true, "专辑": true,
		"年份": true, "音轨": true, "碟号": true, "时长": true,
	}
	var matched int
	for _, h := range records[0] {
		if mustMap[h] {
			matched++
		}
	}
	if matched < 7 {
		t.Errorf("只有 %d 个列名在导入词典中，表头为 %v", matched, records[0])
	}
}

func parseCSV(t *testing.T, data []byte) [][]string {
	t.Helper()
	// 跳过 BOM
	if len(data) >= 3 && data[0] == 0xEF {
		data = data[3:]
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		t.Fatalf("解析导出的 CSV 失败: %v", err)
	}
	return records
}

func indexOf(row []string, want string) int {
	for i, v := range row {
		if v == want {
			return i
		}
	}
	return -1
}
