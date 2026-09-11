package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/norhua/sonexa/internal/importer"
	"github.com/norhua/sonexa/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ImportPreview 是导入向导的「映射 + 预览」页内容。
type ImportPreview struct {
	Filename  string             `json:"filename"`
	Detection importer.Detection `json:"detection"`
	// StatusLine 是顶部那行浅色状态，如「已识别: UTF-8 · 逗号分隔 · 含表头」。
	StatusLine string   `json:"statusLine"`
	Headers    []string `json:"headers"`
	// ColumnMap 是列索引到字段的映射，未命中为空串（忽略）。
	ColumnMap []string         `json:"columnMap"`
	Preview   importer.Preview `json:"preview"`
	// FieldOptions 是列映射下拉的可选字段。
	FieldOptions []FieldOption `json:"fieldOptions"`
	// Encodings 供手动面板的编码下拉使用。
	Encodings []string `json:"encodings"`
}

// FieldOption 是列映射下拉的一项。
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// PickImportFile 打开文件选择对话框。
func (a *App) PickImportFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("界面尚未就绪")
	}
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "选择要导入的表格文件",
		Filters: []wruntime.FileFilter{
			{DisplayName: "表格文件 (*.csv;*.tsv;*.xlsx)", Pattern: "*.csv;*.tsv;*.xlsx"},
			{DisplayName: "全部文件", Pattern: "*.*"},
		},
	})
}

// AnalyzeImportFile 读取文件并自动探测。
//
// 选文件后直接进入「映射 + 预览」，不先要求用户逐项配置。
func (a *App) AnalyzeImportFile(path, sheetName string) (*ImportPreview, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		// 导入入口的禁用与其他出网入口文案一致
		return nil, fmt.Errorf("%s", gate.Reason)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	res, err := importer.Parse(path, data, sheetName)
	if err != nil {
		return nil, err
	}

	out := &ImportPreview{
		Filename:   path,
		Detection:  res.Detection,
		StatusLine: res.Detection.StatusLine(),
		Headers:    res.Headers,
		Preview:    importer.BuildPreview(res, importer.PreviewRows),
		Encodings:  importer.SupportedEncodings(),
	}
	for _, f := range res.ColumnMap {
		out.ColumnMap = append(out.ColumnMap, string(f))
	}
	for _, f := range importer.AllFields() {
		out.FieldOptions = append(out.FieldOptions, FieldOption{
			Value: string(f), Label: importer.FieldDisplayName(f),
		})
	}
	return out, nil
}

// RemapImportColumns 按用户改写的列映射重新生成预览。
//
// 改写映射后预览随之更新。
func (a *App) RemapImportColumns(path, sheetName string, columnMap []string,
	encoding, delimiter string, hasHeader bool) (*ImportPreview, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	res, err := importer.Parse(path, data, sheetName)
	if err != nil {
		return nil, err
	}

	// 应用用户的改写
	if len(columnMap) == len(res.ColumnMap) {
		for i, v := range columnMap {
			res.ColumnMap[i] = importer.Field(v)
		}
	}

	out := &ImportPreview{
		Filename:   path,
		Detection:  res.Detection,
		StatusLine: res.Detection.StatusLine(),
		Headers:    res.Headers,
		Preview:    importer.BuildPreview(res, importer.PreviewRows),
		Encodings:  importer.SupportedEncodings(),
	}
	for _, f := range res.ColumnMap {
		out.ColumnMap = append(out.ColumnMap, string(f))
	}
	for _, f := range importer.AllFields() {
		out.FieldOptions = append(out.FieldOptions, FieldOption{
			Value: string(f), Label: importer.FieldDisplayName(f),
		})
	}
	return out, nil
}

// SubmitImport 提交导入：条目入队并自动开始匹配。
//
// 提交后窗口关闭，侧栏任务区出现进度条，匹配立即开始，无需用户再次触发。
func (a *App) SubmitImport(path, sheetName string, columnMap []string) (int64, error) {
	if err := a.ready(); err != nil {
		return 0, err
	}
	if gate := a.CheckOutbound(); !gate.Allowed {
		return 0, fmt.Errorf("%s", gate.Reason)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	res, err := importer.Parse(path, data, sheetName)
	if err != nil {
		return 0, err
	}
	if len(columnMap) == len(res.ColumnMap) {
		for i, v := range columnMap {
			res.ColumnMap[i] = importer.Field(v)
		}
	}

	rows := importer.AllRows(res)
	mapJSON, _ := json.Marshal(res.ColumnMap)

	batchID, err := a.store.CreateBatch(&store.ImportBatch{
		Filename:  path,
		Encoding:  &res.Detection.Encoding,
		Delimiter: &res.Detection.Delimiter,
		ColumnMap: strPtr(string(mapJSON)),
		TotalRows: len(rows),
	})
	if err != nil {
		return 0, err
	}

	items := make([]store.ImportItem, 0, len(rows))
	for _, r := range rows {
		// 缺少曲名的行被跳过，不入队
		if r.SkipReason != "" {
			continue
		}
		raw, _ := json.Marshal(r.Raw)
		items = append(items, store.ImportItem{
			RowIndex: r.Index, RawRow: string(raw),
			InTitle: &r.Title, InArtist: r.Artist, InAlbum: r.Album,
			InYear: r.Year, InTrackNo: r.TrackNo, InDurationMS: r.DurationMS,
		})
	}
	if err := a.store.EnqueueItems(batchID, items); err != nil {
		return 0, err
	}

	// 自动开始匹配
	if a.runner != nil {
		if err := a.runner.Start(context.Background(), batchID); err != nil {
			return batchID, err
		}
	}
	return batchID, nil
}

// TaskSummary 是侧栏任务区的常驻内容。
type TaskSummary struct {
	// ActiveBatch 非零时有正在运行的导入任务。
	ActiveBatch int64            `json:"activeBatch"`
	Filename    string           `json:"filename"`
	Processed   int              `json:"processed"`
	Total       int              `json:"total"`
	Stats       store.BatchStats `json:"stats"`
	Running     bool             `json:"running"`
	// ReviewPending 是复核待处置数（含待选与失败）。
	ReviewPending int `json:"reviewPending"`
}

// GetTaskSummary 返回任务区所需的统计。
func (a *App) GetTaskSummary() (*TaskSummary, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	out := &TaskSummary{}
	batches, err := a.store.ActiveBatches()
	if err != nil {
		return nil, err
	}
	if len(batches) > 0 {
		b := batches[len(batches)-1]
		stats, err := a.store.Stats(b.ID)
		if err != nil {
			return nil, err
		}
		out.ActiveBatch = b.ID
		out.Filename = b.Filename
		out.Stats = stats
		out.Processed = stats.Processed()
		out.Total = stats.Total
		out.Running = a.runner != nil && a.runner.IsRunning(b.ID)
	}

	pending, err := a.store.AllReviewItems()
	if err != nil {
		return nil, err
	}
	out.ReviewPending = len(pending)
	return out, nil
}

// PauseImport 暂停导入任务。恢复时从暂停处继续。
func (a *App) PauseImport(batchID int64) error {
	if a.runner == nil {
		return fmt.Errorf("队列未就绪")
	}
	return a.runner.Pause(batchID)
}

// ResumeImport 恢复导入任务。
func (a *App) ResumeImport(batchID int64) error {
	if a.runner == nil {
		return fmt.Errorf("队列未就绪")
	}
	return a.runner.Start(context.Background(), batchID)
}

func strPtr(s string) *string { return &s }
