package importer

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, sub, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "golden", sub, name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取样例 %s 失败: %v", name, err)
	}
	return data
}

// TestEncodingDetectionGolden 覆盖 10.1：
// GB18030 / Shift_JIS / Big5 / UTF-16 的金样例各自被正确识别。
func TestEncodingDetectionGolden(t *testing.T) {
	cases := []struct {
		file     string
		want     string
		wantBOM  bool
		contains string
	}{
		{"utf8.csv", "UTF-8", false, "God knows..."},
		{"utf8_bom.csv", "UTF-8", true, "God knows..."},
		{"utf16le.csv", "UTF-16LE", true, "God knows..."},
		{"utf16be.csv", "UTF-16BE", true, "God knows..."},
		{"gb18030.csv", "GB18030", false, "骤雨"},
		{"shiftjis.csv", "Shift_JIS", false, "ハレ晴レユカイ"},
		{"big5.csv", "Big5", false, "驟雨"},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			got := DetectEncoding(fixture(t, "encoding", c.file))
			if got.Name != c.want {
				t.Errorf("编码 = %q, 期望 %q", got.Name, c.want)
			}
			if got.HadBOM != c.wantBOM {
				t.Errorf("HadBOM = %v, 期望 %v", got.HadBOM, c.wantBOM)
			}
			if !contains(got.Text, c.contains) {
				t.Errorf("解码结果不含 %q，实际开头: %.60q", c.contains, got.Text)
			}
		})
	}
}

// TestBOMSkipsScoring 覆盖「带字节序标记的文件直接按该编码解码」。
func TestBOMSkipsScoring(t *testing.T) {
	got := DetectEncoding(fixture(t, "encoding", "utf8_bom.csv"))
	if !got.HadBOM {
		t.Error("应由字节序标记直接判定")
	}
	if got.Confidence != 1.0 {
		t.Errorf("置信度 = %v, 期望 1.0", got.Confidence)
	}
	// BOM 字符不应残留在文本中
	if len(got.Text) > 0 && []rune(got.Text)[0] == '\ufeff' {
		t.Error("BOM 字符不应残留在解码结果中")
	}
}

// TestDelimiterDetectionGolden 覆盖 10.2。
func TestDelimiterDetectionGolden(t *testing.T) {
	cases := []struct {
		file string
		want rune
	}{
		{"utf8.csv", ','},
		{"tabs.tsv", '\t'},
		{"semicolon.csv", ';'},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			data := fixture(t, "encoding", c.file)
			text := DetectEncoding(data).Text
			got := DetectDelimiter(text, c.file)
			if got.Delimiter != c.want {
				t.Errorf("分隔符 = %q, 期望 %q", got.Delimiter, c.want)
			}
		})
	}
}

// TestExtensionMisleadsButContentWins 覆盖 10.2：
// 扩展名为 .csv 但实际以分号分隔时，统计结果使分号胜出。
func TestExtensionMisleadsButContentWins(t *testing.T) {
	data := fixture(t, "encoding", "semicolon.csv")
	text := DetectEncoding(data).Text

	got := DetectDelimiter(text, "semicolon.csv")
	if got.Delimiter != ';' {
		t.Errorf("分隔符 = %q, 期望分号——内容统计应优先于扩展名", got.Delimiter)
	}
	if got.FromExtension {
		t.Error("结果不应与扩展名的猜测一致")
	}
}

// TestHeaderDetectionGolden 覆盖 10.3：有表头与无表头各自判定正确。
func TestHeaderDetectionGolden(t *testing.T) {
	t.Run("典型表头", func(t *testing.T) {
		res, err := Parse("utf8.csv", fixture(t, "encoding", "utf8.csv"), "")
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if !res.Detection.HasHeader {
			t.Error("应判定首行为表头")
		}
		if res.Headers[0] != "曲名" {
			t.Errorf("表头首列 = %q", res.Headers[0])
		}
		// 表头不应混进数据行
		if len(res.Rows) != 3 {
			t.Errorf("数据行数 = %d, 期望 3", len(res.Rows))
		}
	})

	t.Run("无表头文件", func(t *testing.T) {
		res, err := Parse("no_header.csv", fixture(t, "encoding", "no_header.csv"), "")
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if res.Detection.HasHeader {
			t.Error("应判定为无表头")
		}
		// 列映射按列序号呈现
		if res.Headers[0] != "第 1 列" {
			t.Errorf("无表头时列名 = %q, 期望按列序号呈现", res.Headers[0])
		}
		if len(res.Rows) != 2 {
			t.Errorf("数据行数 = %d, 期望 2（首行也是数据）", len(res.Rows))
		}
	})
}

// TestColumnMappingEnglishHeaders 覆盖 10.4：
// 英文表头 Song Name, Artist, Album, Year, No. 全部自动命中。
func TestColumnMappingEnglishHeaders(t *testing.T) {
	res, err := Parse("english_headers.csv", fixture(t, "encoding", "english_headers.csv"), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	want := []Field{FieldTitle, FieldArtist, FieldAlbum, FieldYear, FieldTrackNo}
	if len(res.ColumnMap) != len(want) {
		t.Fatalf("映射列数 = %d, 期望 %d", len(res.ColumnMap), len(want))
	}
	for i, w := range want {
		if res.ColumnMap[i] != w {
			t.Errorf("第 %d 列 %q 映射为 %q, 期望 %q", i, res.Headers[i], res.ColumnMap[i], w)
		}
	}
}

// TestColumnMappingChineseHeaders 覆盖词典的中文写法。
func TestColumnMappingChineseHeaders(t *testing.T) {
	cases := []struct {
		header string
		want   Field
	}{
		{"曲名", FieldTitle}, {"歌名", FieldTitle}, {"标题", FieldTitle},
		{"艺术家", FieldArtist}, {"歌手", FieldArtist}, {"演唱", FieldArtist},
		{"专辑", FieldAlbum}, {"唱片", FieldAlbum}, {"歌集", FieldAlbum},
		{"年份", FieldYear}, {"发行年", FieldYear},
		{"音轨", FieldTrackNo}, {"曲序", FieldTrackNo}, {"序号", FieldTrackNo},
		{"碟号", FieldDiscNo},
		{"时长", FieldDuration}, {"长度", FieldDuration},
		{"Title", FieldTitle}, {"ARTIST", FieldArtist}, {"track no", FieldTrackNo},
	}
	for _, c := range cases {
		t.Run(c.header, func(t *testing.T) {
			if got := MapColumn(c.header); got != c.want {
				t.Errorf("MapColumn(%q) = %q, 期望 %q", c.header, got, c.want)
			}
		})
	}
}

// TestUnknownColumnIgnored 覆盖 10.4：未命中的列默认映射为忽略。
func TestUnknownColumnIgnored(t *testing.T) {
	if got := MapColumn("Note"); got != FieldIgnore {
		t.Errorf("未知列 Note 映射为 %q, 期望忽略", got)
	}
	if got := MapColumn("备注"); got != FieldIgnore {
		t.Errorf("未知列 备注 映射为 %q, 期望忽略", got)
	}
}

// TestXLSXMultiSheetRequiresSelection 覆盖 10.5：
// 多工作表时要求用户选择工作表。
func TestXLSXMultiSheetRequiresSelection(t *testing.T) {
	data := fixture(t, "delimiter", "multi_sheet.xlsx")

	res, err := Parse("multi_sheet.xlsx", data, "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(res.Detection.SheetNames) < 2 {
		t.Fatalf("工作表数 = %d, 期望多个", len(res.Detection.SheetNames))
	}
	if !res.Detection.NeedsAttention {
		t.Error("多工作表时应要求用户选择")
	}

	// 选定工作表后正常解析
	res2, err := Parse("multi_sheet.xlsx", data, "曲目")
	if err != nil {
		t.Fatalf("选定工作表后解析失败: %v", err)
	}
	if len(res2.Rows) != 2 {
		t.Errorf("数据行数 = %d, 期望 2", len(res2.Rows))
	}
	if res2.ColumnMap[0] != FieldTitle {
		t.Errorf("首列映射 = %q, 期望曲名", res2.ColumnMap[0])
	}
}

// TestXLSXSkipsEncodingAndDelimiter 覆盖 10.5：
// XLSX 跳过编码与分隔符探测。
func TestXLSXSkipsEncodingAndDelimiter(t *testing.T) {
	res, err := Parse("single_sheet.xlsx", fixture(t, "delimiter", "single_sheet.xlsx"), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if res.Detection.Kind != KindXLSX {
		t.Errorf("类型 = %q, 期望 xlsx", res.Detection.Kind)
	}
	if res.Detection.Delimiter != "" {
		t.Errorf("XLSX 不应有分隔符, 得到 %q", res.Detection.Delimiter)
	}
	// 状态行不提分隔符
	if contains(res.Detection.StatusLine(), "分隔") {
		t.Errorf("XLSX 的状态行不应提分隔符: %q", res.Detection.StatusLine())
	}
}

// TestPreviewAndSkipStats 覆盖 10.6：
// 214 行中 3 行缺曲名时，提交按钮显示 211 条。
func TestPreviewAndSkipStats(t *testing.T) {
	res, err := Parse("missing_titles.csv", fixture(t, "encoding", "missing_titles.csv"), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	p := BuildPreview(res, PreviewRows)
	if p.TotalRows != 214 {
		t.Errorf("总行数 = %d, 期望 214", p.TotalRows)
	}
	if p.SkippedRows != 3 {
		t.Errorf("跳过行数 = %d, 期望 3", p.SkippedRows)
	}
	if p.ImportableRows != 211 {
		t.Errorf("将导入 %d 条, 期望 211", p.ImportableRows)
	}
	if p.SkipSummary != "3 行缺少曲名，将跳过" {
		t.Errorf("跳过提示 = %q", p.SkipSummary)
	}
	// 预览至少 20 行
	if len(p.Rows) < 20 {
		t.Errorf("预览行数 = %d, 期望至少 20", len(p.Rows))
	}
}

// TestZeroConfigForCommonCSV 覆盖 10.6 与 F8：
// 常见 UTF-8 逗号分隔含表头的 CSV 全程零手动设置。
func TestZeroConfigForCommonCSV(t *testing.T) {
	res, err := Parse("utf8.csv", fixture(t, "encoding", "utf8.csv"), "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}

	if res.Detection.NeedsAttention {
		t.Errorf("常见 CSV 不应需要手动介入: %s", res.Detection.Reason)
	}
	if res.Detection.Encoding != "UTF-8" {
		t.Errorf("编码 = %q", res.Detection.Encoding)
	}
	if res.Detection.Delimiter != "逗号" {
		t.Errorf("分隔符 = %q", res.Detection.Delimiter)
	}
	if !res.Detection.HasHeader {
		t.Error("应识别出表头")
	}
	if !hasField(res.ColumnMap, FieldTitle) {
		t.Error("应自动映射出曲名列")
	}

	// 状态行文案
	want := "已识别: UTF-8 · 逗号分隔 · 含表头"
	if got := res.Detection.StatusLine(); got != want {
		t.Errorf("状态行 = %q, 期望 %q", got, want)
	}

	// 可直接提交
	p := BuildPreview(res, PreviewRows)
	if p.ImportableRows != 3 {
		t.Errorf("可导入 %d 条, 期望 3", p.ImportableRows)
	}
}

// TestLowConfidenceTriggersAttention 覆盖 17.3 的触发条件：
// 未能映射出曲名列时自动展开手动面板。
func TestLowConfidenceTriggersAttention(t *testing.T) {
	// 构造一份没有可识别曲名列的数据
	data := []byte("字段A,字段B\n值1,值2\n值3,值4\n")
	res, err := Parse("unknown.csv", data, "")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if !res.Detection.NeedsAttention {
		t.Error("未映射出曲名列时应自动展开手动面板")
	}
	if res.Detection.Reason == "" {
		t.Error("应说明需要关注的原因")
	}
}

// TestParseDuration 覆盖时长的多种写法。
func TestParseDuration(t *testing.T) {
	cases := []struct {
		in   string
		want *int
	}{
		{"3:41", intPtr(221000)},
		{"1:02:03", intPtr(3723000)},
		{"221", intPtr(221000)},
		{"221000", intPtr(221000)},
		{"", nil},
		{"abc", nil},
	}
	for _, c := range cases {
		got := ParseDuration(c.in)
		switch {
		case c.want == nil && got != nil:
			t.Errorf("ParseDuration(%q) = %v, 期望 nil", c.in, *got)
		case c.want != nil && got == nil:
			t.Errorf("ParseDuration(%q) = nil, 期望 %v", c.in, *c.want)
		case c.want != nil && got != nil && *got != *c.want:
			t.Errorf("ParseDuration(%q) = %v, 期望 %v", c.in, *got, *c.want)
		}
	}
}

// TestParseYearAndInt 覆盖年份与整数解析。
func TestParseYearAndInt(t *testing.T) {
	if y := ParseYear("2006-12-18"); y == nil || *y != 2006 {
		t.Errorf("ParseYear = %v, 期望 2006", y)
	}
	if y := ParseYear("abc"); y != nil {
		t.Errorf("无法解析时应返回 nil, 得到 %v", *y)
	}
	// 「2/12」这类写法取前一个数
	if n := ParseInt("2/12"); n == nil || *n != 2 {
		t.Errorf("ParseInt(2/12) = %v, 期望 2", n)
	}
	if n := ParseInt(""); n != nil {
		t.Errorf("空串应返回 nil, 得到 %v", *n)
	}
}

func intPtr(i int) *int { return &i }

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
