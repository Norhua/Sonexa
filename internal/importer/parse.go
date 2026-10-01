package importer

import (
	"encoding/csv"
	"fmt"
	"io"
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

// Overrides 是用户在手动面板中改写的探测结果。
//
// 零值表示「沿用自动探测」。低置信时用户需要能真正改掉结果——
// 控件若不能影响解析，那它只是装饰。
type Overrides struct {
	Encoding  string
	Delimiter rune
	HasHeader *bool
}

// Parse 解析文件并自动探测编码、分隔符、表头与列映射。
//
// 选文件后直接进入「映射 + 预览」，不先要求用户逐项配置。
func Parse(filename string, data []byte, sheetName string) (*ParseResult, error) {
	return ParseWithOverrides(filename, data, sheetName, Overrides{})
}

// ParseWithOverrides 与 Parse 相同，但允许用户覆盖探测结果。
func ParseWithOverrides(filename string, data []byte, sheetName string, ov Overrides) (*ParseResult, error) {
	kind := DetectFileKind(filename)
	if kind == KindXLSX {
		return parseXLSX(filename, data, sheetName)
	}
	return parseDelimitedWithOverrides(filename, data, ov)
}

// parseDelimited 解析 CSV / TSV。
func parseDelimited(filename string, data []byte) (*ParseResult, error) {
	return parseDelimitedWithOverrides(filename, data, Overrides{})
}

// parseDelimitedWithOverrides 解析 CSV / TSV，并应用用户的改写。
func parseDelimitedWithOverrides(filename string, data []byte, ov Overrides) (*ParseResult, error) {
	enc := DetectEncoding(data)
	// 用户显式指定了编码时按指定解码，而不是沿用探测结果
	if ov.Encoding != "" && ov.Encoding != enc.Name {
		text, err := DecodeAs(ov.Encoding, data)
		if err != nil {
			return nil, fmt.Errorf("按编码 %s 解码失败: %w", ov.Encoding, err)
		}
		enc = EncodingResult{Name: ov.Encoding, Confidence: 1.0, Text: text}
	}
	text := StripBOM(enc.Text)

	delim := DetectDelimiter(text, filename)
	// 用户显式指定了分隔符时以用户的为准
	if ov.Delimiter != 0 {
		delim = DelimiterResult{Delimiter: ov.Delimiter, Confidence: 1.0}
	}

	records, err := readDelimited(text, delim.Delimiter)
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("文件为空或无法解析出任何行")
	}

	first := 0
	for first < len(records) && isBlankRow(records[first]) {
		first++
	}
	if first == len(records) {
		return nil, fmt.Errorf("文件没有有效数据行")
	}
	header := DetectHeader(records[first:])
	// 用户显式指定了首行是否为表头时以用户的为准
	if ov.HasHeader != nil {
		header = HeaderResult{HasHeader: *ov.HasHeader, Confidence: 1.0}
	}

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
		res.Headers = records[first]
		res.Rows = append(append([][]string{}, records[:first]...), records[first+1:]...)
	} else {
		// 无表头时列映射按列序号呈现
		res.Headers = numberedHeaders(len(records[first]))
		res.Rows = records
	}
	res.ColumnMap = MapColumns(res.Headers)

	// 用户已经手动确认过编码时不再提示「需要关注」
	conf := enc.Confidence
	if ov.Encoding != "" {
		conf = 1.0
	}
	applyAttention(res, conf)
	return res, nil
}

// readDelimited 按指定分隔符读取，容忍每行字段数不一致。
func readDelimited(text string, delim rune) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	// 表格导出常有字段数不齐的行，不能因此整个失败
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	records := [][]string{}
	nextLine := 1
	var offset int64
	for {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("解析表格失败: %w", err)
		}
		startLine, _ := r.FieldPos(0)
		for line := nextLine; line < startLine; line++ {
			records = append(records, []string{})
		}
		records = append(records, record)
		nextOffset := r.InputOffset()
		nextLine += strings.Count(text[offset:nextOffset], "\n")
		offset = nextOffset
	}
	// encoding/csv 忽略空物理行；保留这些行用于原始行号和跳过统计。
	for nextLine <= strings.Count(text, "\n") {
		records = append(records, []string{})
		nextLine++
	}

	return records, nil
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

	records := rows
	if len(records) == 0 {
		return nil, fmt.Errorf("工作表 %q 为空", sheetName)
	}

	first := 0
	for first < len(records) && isBlankRow(records[first]) {
		first++
	}
	if first == len(records) {
		return nil, fmt.Errorf("文件没有有效数据行")
	}
	header := DetectHeader(records[first:])
	res.Detection.HasHeader = header.HasHeader
	if header.HasHeader {
		res.Headers = records[first]
		res.Rows = append(append([][]string{}, records[:first]...), records[first+1:]...)
	} else {
		res.Headers = numberedHeaders(len(records[first]))
		res.Rows = records
	}
	res.ColumnMap = MapColumns(res.Headers)

	applyAttention(res, 1.0)
	return res, nil
}

// applyAttention 判定是否需要自动展开手动面板。
//
// 触发条件：编码探测置信度低于 80%，或未能映射出有效数据列。
func applyAttention(res *ParseResult, encodingConfidence float64) {
	if encodingConfidence < 0.8 {
		res.Detection.NeedsAttention = true
		res.Detection.Reason = "编码探测置信度较低，请确认编码是否正确"
		return
	}
	hasMapped := false
	for _, field := range res.ColumnMap {
		if field != FieldIgnore {
			hasMapped = true
		}
	}
	if !hasMapped {
		res.Detection.NeedsAttention = true
		res.Detection.Reason = "未能自动识别有效数据列，请确认列映射"
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
	// SkipSummary 是跳过原因的可读汇总，仅统计真正空行或无有效映射内容。
	SkipSummary       string `json:"skipSummary"`
	MissingArtistRows int    `json:"missingArtistRows"`
	MissingAlbumRows  int    `json:"missingAlbumRows"`
	MissingTitleRows  int    `json:"missingTitleRows"`
	MissingSummary    string `json:"missingSummary"`
}

// BuildPreview 按列映射生成预览与跳过统计。
//
// 预览至少 20 行；统计将被跳过的行数与原因（不包括缺少基本字段）。
func BuildPreview(res *ParseResult, previewRows int) Preview {
	if previewRows <= 0 {
		previewRows = PreviewRows
	}

	p := Preview{TotalRows: len(res.Rows)}

	for i, raw := range res.Rows {
		row := mapRow(i, raw, res.ColumnMap)
		if row.SkipReason != "" {
			p.SkippedRows++
		} else {
			p.ImportableRows++
			if row.Artist == nil {
				p.MissingArtistRows++
			}
			if row.Album == nil {
				p.MissingAlbumRows++
			}
			if row.Title == "" {
				p.MissingTitleRows++
			}
		}
		if len(p.Rows) < previewRows {
			p.Rows = append(p.Rows, row)
		}
	}

	if p.SkippedRows > 0 {
		p.SkipSummary = fmt.Sprintf("%d 行没有有效映射内容，将跳过", p.SkippedRows)
	}
	if p.MissingTitleRows+p.MissingArtistRows+p.MissingAlbumRows > 0 {
		p.MissingSummary = fmt.Sprintf("缺少曲名 %d 行、艺术家 %d 行、专辑 %d 行，仍将导入并可逐条复核", p.MissingTitleRows, p.MissingArtistRows, p.MissingAlbumRows)
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

	if isBlankRow(raw) {
		row.SkipReason = "空行"
	} else if row.Title == "" && row.Artist == nil && row.Album == nil && row.Year == nil && row.TrackNo == nil && row.DiscNo == nil && row.DurationMS == nil {
		row.SkipReason = "没有有效映射内容"
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
