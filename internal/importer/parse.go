package importer

import (
	"encoding/csv"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// PreviewRows 是预览至少展示的行数。
const PreviewRows = 20

// FileKind 是导入文件的类型。
type FileKind string

const (
	KindCSV  FileKind = "csv"
	KindTSV  FileKind = "tsv"
	KindXLSX FileKind = "xlsx"
)

// DetectFileKind 按扩展名判定文件类型。
func DetectFileKind(filename string) FileKind {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx", ".xlsm":
		return KindXLSX
	case ".tsv", ".tab":
		return KindTSV
	default:
		return KindCSV
	}
}

// Detection 是一次自动探测的完整结果，对应界面顶部那行浅色状态。
type Detection struct {
	Kind     FileKind `json:"kind"`
	Encoding string   `json:"encoding"`
	// Delimiter 以可读形式给出（逗号 / 制表符 / 分号 / 竖线）。
	Delimiter     string  `json:"delimiter"`
	DelimiterRune string  `json:"delimiterRune"`
	HasHeader     bool    `json:"hasHeader"`
	Confidence    float64 `json:"confidence"`
	// NeedsAttention 为真时界面自动展开并高亮手动面板。
	NeedsAttention bool `json:"needsAttention"`
	// Reason 说明为何需要关注。
	Reason string `json:"reason"`
	// SheetNames 在 XLSX 有多个工作表时非空，此时要求用户选择。
	SheetNames []string `json:"sheetNames"`
}

// StatusLine 返回界面顶部的状态文案，如
// 「已识别: UTF-8 · 逗号分隔 · 含表头」。
func (d Detection) StatusLine() string {
	parts := []string{d.Encoding}
	if d.Kind != KindXLSX {
		parts = append(parts, d.Delimiter+"分隔")
	}
	if d.HasHeader {
		parts = append(parts, "含表头")
	} else {
		parts = append(parts, "无表头")
	}
	return "已识别: " + strings.Join(parts, " · ")
}

// ParseResult 是解析后的表格内容。
type ParseResult struct {
	Detection Detection  `json:"detection"`
	Headers   []string   `json:"headers"`
	Rows      [][]string `json:"rows"`
	// ColumnMap 是列索引到字段的映射。
	ColumnMap []Field `json:"columnMap"`
}

// Parse 解析文件并自动探测编码、分隔符、表头与列映射。
//
// 选文件后直接进入「映射 + 预览」，不先要求用户逐项配置。
func Parse(filename string, data []byte, sheetName string) (*ParseResult, error) {
	kind := DetectFileKind(filename)
	if kind == KindXLSX {
		return parseXLSX(filename, data, sheetName)
	}
	return parseDelimited(filename, data)
}

// parseDelimited 解析 CSV / TSV。
func parseDelimited(filename string, data []byte) (*ParseResult, error) {
	enc := DetectEncoding(data)
	text := StripBOM(enc.Text)

	delim := DetectDelimiter(text, filename)

	records, err := readDelimited(text, delim.Delimiter)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("文件为空或无法解析出任何行")
	}

	header := DetectHeader(records)

	res := &ParseResult{
		Detection: Detection{
			Kind:          DetectFileKind(filename),
			Encoding:      enc.Name,
			Delimiter:     delimiterDisplayName(delim.Delimiter),
			DelimiterRune: string(delim.Delimiter),
			HasHeader:     header.HasHeader,
			Confidence:    minFloat(enc.Confidence, delim.Confidence),
		},
	}

	if header.HasHeader {
		res.Headers = records[0]
		res.Rows = records[1:]
	} else {
		// 无表头时列映射按列序号呈现
		res.Headers = numberedHeaders(len(records[0]))
		res.Rows = records
	}
	res.ColumnMap = MapColumns(res.Headers)

	applyAttention(res, enc.Confidence)
	return res, nil
}

// readDelimited 按指定分隔符读取，容忍每行字段数不一致。
func readDelimited(text string, delim rune) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	// 表格导出常有字段数不齐的行，不能因此整个失败
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析表格失败: %w", err)
	}

	// 去掉全空行
	var out [][]string
	for _, rec := range records {
		if isBlankRow(rec) {
			continue
		}
		out = append(out, rec)
	}
	return out, nil
}

// parseXLSX 解析 XLSX。
//
// 无编码与分隔符问题，跳过那两步探测；多工作表时要求用户选择。
func parseXLSX(filename string, data []byte, sheetName string) (*ParseResult, error) {
	f, err := excelize.OpenReader(strings.NewReader(string(data)))
	if err != nil {
		return nil, fmt.Errorf("打开 XLSX 失败: %w", err)
	}
	defer f.Close()

	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return nil, fmt.Errorf("工作簿中没有工作表")
	}

	res := &ParseResult{
		Detection: Detection{
			Kind:       KindXLSX,
			Encoding:   "XLSX",
			Confidence: 1.0,
			SheetNames: sheets,
		},
	}

	// 多工作表且用户尚未选择时，要求选择
	if len(sheets) > 1 && sheetName == "" {
		res.Detection.NeedsAttention = true
		res.Detection.Reason = "该工作簿含多个工作表，请选择要导入的工作表"
		return res, nil
	}
	if sheetName == "" {
		sheetName = sheets[0]
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, fmt.Errorf("读取工作表 %q 失败: %w", sheetName, err)
	}

	var records [][]string
	for _, r := range rows {
		if isBlankRow(r) {
			continue
		}
		records = append(records, r)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("工作表 %q 为空", sheetName)
	}

	header := DetectHeader(records)
	res.Detection.HasHeader = header.HasHeader
	if header.HasHeader {
		res.Headers = records[0]
		res.Rows = records[1:]
	} else {
		res.Headers = numberedHeaders(len(records[0]))
		res.Rows = records
	}
	res.ColumnMap = MapColumns(res.Headers)

	applyAttention(res, 1.0)
	return res, nil
}

// applyAttention 判定是否需要自动展开手动面板。
//
// 触发条件：编码探测置信度低于 80%，或未能映射出曲名列。
func applyAttention(res *ParseResult, encodingConfidence float64) {
	if encodingConfidence < 0.8 {
		res.Detection.NeedsAttention = true
		res.Detection.Reason = "编码探测置信度较低，请确认编码是否正确"
		return
	}
	if !hasField(res.ColumnMap, FieldTitle) {
		res.Detection.NeedsAttention = true
		res.Detection.Reason = "未能自动识别出「曲名」列，请手动指定"
	}
}

func hasField(mapping []Field, f Field) bool {
	for _, m := range mapping {
		if m == f {
			return true
		}
	}
	return false
}

// Row 是映射后的一行输入。
type Row struct {
	Index      int      `json:"index"`
	Raw        []string `json:"raw"`
	Title      string   `json:"title"`
	Artist     *string  `json:"artist"`
	Album      *string  `json:"album"`
	Year       *int     `json:"year"`
	TrackNo    *int     `json:"trackNo"`
	DiscNo     *int     `json:"discNo"`
	DurationMS *int     `json:"durationMs"`
	// SkipReason 非空时该行将被跳过。
	SkipReason string `json:"skipReason"`
}

// Preview 是预览与跳过统计的结果。
type Preview struct {
	Rows []Row `json:"rows"`
	// TotalRows 是文件中的数据行总数。
	TotalRows int `json:"totalRows"`
	// ImportableRows 是实际将导入的条数。
	ImportableRows int `json:"importableRows"`
	// SkippedRows 是将被跳过的行数。
	SkippedRows int `json:"skippedRows"`
	// SkipSummary 是跳过原因的可读汇总，如「3 行缺少曲名，将跳过」。
	SkipSummary string `json:"skipSummary"`
}

// BuildPreview 按列映射生成预览与跳过统计。
//
// 预览至少 20 行；统计将被跳过的行数与原因（如缺少曲名）。
func BuildPreview(res *ParseResult, previewRows int) Preview {
	if previewRows <= 0 {
		previewRows = PreviewRows
	}

	p := Preview{TotalRows: len(res.Rows)}
	missingTitle := 0

	for i, raw := range res.Rows {
		row := mapRow(i, raw, res.ColumnMap)
		if row.SkipReason != "" {
			p.SkippedRows++
			if row.SkipReason == "缺少曲名" {
				missingTitle++
			}
		} else {
			p.ImportableRows++
		}
		if len(p.Rows) < previewRows {
			p.Rows = append(p.Rows, row)
		}
	}

	if missingTitle > 0 {
		p.SkipSummary = fmt.Sprintf("%d 行缺少曲名，将跳过", missingTitle)
	} else if p.SkippedRows > 0 {
		p.SkipSummary = fmt.Sprintf("%d 行将被跳过", p.SkippedRows)
	}
	return p
}

// mapRow 按列映射把原始行转为输入行。
func mapRow(index int, raw []string, mapping []Field) Row {
	row := Row{Index: index, Raw: raw}

	for col, field := range mapping {
		if col >= len(raw) {
			break
		}
		value := strings.TrimSpace(raw[col])
		if value == "" {
			continue
		}
		switch field {
		case FieldTitle:
			row.Title = value
		case FieldArtist:
			row.Artist = &value
		case FieldAlbum:
			row.Album = &value
		case FieldYear:
			row.Year = ParseYear(value)
		case FieldTrackNo:
			row.TrackNo = ParseInt(value)
		case FieldDiscNo:
			row.DiscNo = ParseInt(value)
		case FieldDuration:
			row.DurationMS = ParseDuration(value)
		}
	}

	// 曲名是必填项：缺少曲名的行被跳过
	if row.Title == "" {
		row.SkipReason = "缺少曲名"
	}
	return row
}

// AllRows 返回全部映射后的行（不截断），供提交导入时使用。
func AllRows(res *ParseResult) []Row {
	out := make([]Row, 0, len(res.Rows))
	for i, raw := range res.Rows {
		out = append(out, mapRow(i, raw, res.ColumnMap))
	}
	return out
}

func isBlankRow(rec []string) bool {
	for _, cell := range rec {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}

func numberedHeaders(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("第 %d 列", i+1)
	}
	return out
}

func delimiterDisplayName(d rune) string {
	switch d {
	case ',':
		return "逗号"
	case '\t':
		return "制表符"
	case ';':
		return "分号"
	case '|':
		return "竖线"
	default:
		return string(d)
	}
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
