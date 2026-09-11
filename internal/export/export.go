// Package export 实现 CSV 与 JSON 导出。
package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
)

// Format 是导出格式。
type Format string

const (
	FormatCSV  Format = "csv"
	FormatJSON Format = "json"
)

// Depth 是字段深度。
type Depth string

const (
	// DepthBasic 仅基本信息：曲名、艺术家、专辑名三列。
	DepthBasic Depth = "basic"
	// DepthFull 基本信息 + 丰富信息。
	DepthFull Depth = "full"
)

// Scope 是导出范围。
type Scope string

const (
	ScopeAll       Scope = "all"       // 全库
	ScopePlaylist  Scope = "playlist"  // 当前歌单
	ScopeFiltered  Scope = "filtered"  // 当前筛选结果
	ScopeSelection Scope = "selection" // 选中项
)

// utf8BOM 是带 BOM 的 UTF-8 前缀。
//
// 默认写入 BOM：常见表格软件据此识别 UTF-8，否则中日文会显示为乱码。
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// Track 是一条待导出的曲目。
//
// 全部可空字段用指针：缺失字段导出为空而不是零值——
// 导出 0 会让「没有年份」与「年份是 0」无法区分。
type Track struct {
	Title  string  `json:"title"`
	Artist *string `json:"artist"`
	Album  *string `json:"album"`

	// 丰富信息（DepthFull 时导出）
	Year          *int    `json:"year,omitempty"`
	TrackNumber   *int    `json:"trackNumber,omitempty"`
	DiscNumber    *int    `json:"discNumber,omitempty"`
	DurationMS    *int    `json:"durationMs,omitempty"`
	Genre         *string `json:"genre,omitempty"`
	Label         *string `json:"label,omitempty"`
	CatalogNumber *string `json:"catalogNumber,omitempty"`
	MediumFormat  *string `json:"mediumFormat,omitempty"`
	Country       *string `json:"country,omitempty"`
	PrimaryType   *string `json:"primaryType,omitempty"`
	TrackCredit   *string `json:"trackCredit,omitempty"`
	AlbumCredit   *string `json:"albumCredit,omitempty"`
	ISRC          *string `json:"isrc,omitempty"`
	MatchSource   *string `json:"matchSource,omitempty"`
	MatchRef      *string `json:"matchRef,omitempty"`
}

// Options 是一次导出的选项。
type Options struct {
	Format  Format
	Depth   Depth
	UTF8BOM bool
}

// DefaultOptions 返回默认导出选项。
func DefaultOptions() Options {
	return Options{Format: FormatCSV, Depth: DepthFull, UTF8BOM: true}
}

// basicHeaders 是「仅基本信息」的列。
var basicHeaders = []string{"曲名", "艺术家", "专辑"}

// fullHeaders 是「基本 + 丰富」的列。
//
// 列名与导入的表头词典对齐，使导出的 CSV 能被本应用读回且自动命中列映射。
var fullHeaders = []string{
	"曲名", "艺术家", "专辑",
	"年份", "音轨", "碟号", "时长",
	"风格", "厂牌", "目录号", "载体", "国家", "类型",
	"曲目署名", "专辑署名", "ISRC", "匹配来源", "匹配标识",
}

// Write 把曲目写出为指定格式。
//
// 导出不包含任何封面图片——上游图片的版权各异，
// 本地缓存自用可行但不得随导出分发。
func Write(w io.Writer, tracks []Track, opts Options) error {
	switch opts.Format {
	case FormatJSON:
		return writeJSON(w, tracks, opts)
	default:
		return writeCSV(w, tracks, opts)
	}
}

func writeCSV(w io.Writer, tracks []Track, opts Options) error {
	if opts.UTF8BOM {
		if _, err := w.Write(utf8BOM); err != nil {
			return err
		}
	}

	cw := csv.NewWriter(w)
	headers := basicHeaders
	if opts.Depth == DepthFull {
		headers = fullHeaders
	}
	if err := cw.Write(headers); err != nil {
		return err
	}

	for _, t := range tracks {
		if err := cw.Write(rowFor(t, opts.Depth)); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// rowFor 生成一行。缺失字段写空串而非零值。
func rowFor(t Track, depth Depth) []string {
	row := []string{t.Title, str(t.Artist), str(t.Album)}
	if depth != DepthFull {
		return row
	}
	return append(row,
		num(t.Year),
		num(t.TrackNumber),
		num(t.DiscNumber),
		num(t.DurationMS),
		str(t.Genre),
		str(t.Label),
		str(t.CatalogNumber),
		str(t.MediumFormat),
		str(t.Country),
		str(t.PrimaryType),
		str(t.TrackCredit),
		str(t.AlbumCredit),
		str(t.ISRC),
		str(t.MatchSource),
		str(t.MatchRef),
	)
}

func writeJSON(w io.Writer, tracks []Track, opts Options) error {
	payload := tracks
	if opts.Depth == DepthBasic {
		payload = make([]Track, len(tracks))
		for i, t := range tracks {
			// 仅保留基本信息，其余字段因 omitempty 不出现在输出中
			payload[i] = Track{Title: t.Title, Artist: t.Artist, Album: t.Album}
		}
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// str 返回可空字符串的导出值；缺失为空串。
func str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// num 返回可空整数的导出值；缺失为空串而不是 0。
func num(n *int) string {
	if n == nil {
		return ""
	}
	return strconv.Itoa(*n)
}

// FormatDuration 把毫秒格式化为 m:ss，供界面显示使用。
func FormatDuration(ms *int) string {
	if ms == nil {
		return "—"
	}
	total := *ms / 1000
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}
