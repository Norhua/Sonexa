package main

import (
	"fmt"
	"os"

	"github.com/norhua/sonexa/internal/export"
	"github.com/norhua/sonexa/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ExportRequest 是一次导出请求。
type ExportRequest struct {
	// Scope 为 all | playlist | filtered | selection。
	Scope string `json:"scope"`
	// Format 为 csv | json；Depth 为 basic | full。
	Format string `json:"format"`
	Depth  string `json:"depth"`
	// Filter 描述「当前筛选结果」或「当前歌单」的范围。
	Filter store.TrackFilter `json:"filter"`
	// TrackIDs 是「选中项」的范围。
	TrackIDs []int64 `json:"trackIds"`
}

// ExportResult 是导出的结果。
type ExportResult struct {
	Path  string `json:"path"`
	Count int    `json:"count"`
}

// ExportTracks 导出曲目。
//
// 只包含已入库的曲目——灰色行没有 track.id，本就不在待导出集合中。
// 导出产物不含任何封面图片。
func (a *App) ExportTracks(req ExportRequest) (*ExportResult, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	tracks, err := a.resolveExportScope(req)
	if err != nil {
		return nil, err
	}
	if len(tracks) == 0 {
		return nil, fmt.Errorf("所选范围内没有可导出的曲目")
	}

	opts := export.Options{
		Format:  export.Format(req.Format),
		Depth:   export.Depth(req.Depth),
		UTF8BOM: a.settings.Get().ImportExport.ExportUTF8BOM,
	}
	if opts.Format == "" {
		opts.Format = export.Format(a.settings.Get().ImportExport.ExportFormat)
	}
	if opts.Depth == "" {
		opts.Depth = export.DepthFull
	}

	ext := ".csv"
	if opts.Format == export.FormatJSON {
		ext = ".json"
	}
	path, err := wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "导出曲目",
		DefaultFilename: "sonexa-export" + ext,
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return nil, nil // 用户取消
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("创建导出文件失败: %w", err)
	}
	defer f.Close()

	if err := export.Write(f, tracks, opts); err != nil {
		return nil, err
	}
	return &ExportResult{Path: path, Count: len(tracks)}, nil
}

// resolveExportScope 按范围取出待导出曲目。
func (a *App) resolveExportScope(req ExportRequest) ([]export.Track, error) {
	var views []store.TrackView
	var err error

	switch export.Scope(req.Scope) {
	case export.ScopeSelection:
		for _, id := range req.TrackIDs {
			v, e := a.store.GetTrackView(id)
			if e != nil {
				return nil, e
			}
			if v != nil {
				views = append(views, *v)
			}
		}
	case export.ScopeAll:
		views, err = a.store.ListTracks(store.TrackFilter{})
	default:
		// 当前歌单 / 当前筛选结果都由 Filter 描述
		views, err = a.store.ListTracks(req.Filter)
	}
	if err != nil {
		return nil, err
	}

	out := make([]export.Track, 0, len(views))
	for _, v := range views {
		t := export.Track{
			Title: v.Title, Artist: v.Artist, Album: v.Album,
			Year: v.Year, TrackNumber: v.TrackNumber, DiscNumber: v.DiscNumber,
			DurationMS: v.DurationMS, Genre: v.Genre,
			MediumFormat: v.MediumFormat, Country: v.Country,
			PrimaryType: v.PrimaryType,
			MatchSource: v.MatchSource, MatchRef: v.MatchRef,
		}
		// 厂牌与目录号需读丰富信息
		if meta, e := a.store.GetMetadata(v.ID); e == nil && meta != nil {
			t.Label = meta.Label
			t.CatalogNumber = meta.CatalogNumber
			t.TrackCredit = meta.Credit
			t.AlbumCredit = meta.AlbumCredit
			t.ISRC = meta.ISRC
		}
		out = append(out, t)
	}
	return out, nil
}
