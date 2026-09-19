package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

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

	// 用户的改写必须真正影响解析，否则手动面板只是装饰
	ov := importer.Overrides{Encoding: encoding, HasHeader: &hasHeader}
	if r := []rune(delimiter); len(r) > 0 {
		ov.Delimiter = r[0]
	}
	res, err := importer.ParseWithOverrides(path, data, sheetName, ov)
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
func (a *App) SubmitImport(path, sheetName string, columnMap []string,
	encoding, delimiter string, hasHeader bool) (int64, error) {
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
	// 提交时用与预览时相同的改写，使「所见即所导」
	ov := importer.Overrides{Encoding: encoding, HasHeader: &hasHeader}
	if r := []rune(delimiter); len(r) > 0 {
		ov.Delimiter = r[0]
	}
	res, err := importer.ParseWithOverrides(path, data, sheetName, ov)
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

// ImportBatchView 是导入队列页面上的一个批次。
type ImportBatchView struct {
	ID       int64  `json:"id"`
	Filename string `json:"filename"`
	// Status 为 running | paused | done。
	Status string `json:"status"`
	// FileRows 是文件里的总行数，含因缺少曲名而被跳过、从未入队的行。
	// 它只用于「文件里有多少行」这一信息，不做进度分母。
	FileRows int `json:"fileRows"`
	// TotalRows 是真正入队的条目数，即进度的分母。
	//
	// 两者必须分开：缺曲名的行在提交时就被跳过、不写进 import_item，
	// 用文件行数当分母的话，只要有一行被跳过，进度就永远停在 90% 几。
	TotalRows int              `json:"totalRows"`
	Processed int              `json:"processed"`
	Stats     store.BatchStats `json:"stats"`
	// Running 是驱动器的真实状态；与 Status 可能短暂不一致（如刚重启尚未恢复）。
	Running bool `json:"running"`
	// CreatedAt / FinishedAt 是 RFC3339 时间串，理由见 AlbumDetail.FetchedAt。
	CreatedAt  string  `json:"createdAt"`
	FinishedAt *string `json:"finishedAt"`
	// FailedTitles 逐行列出失败条目的原始曲名，供用户核对。
	FailedTitles []string `json:"failedTitles"`
}

// recentBatchLimit 是导入队列页面回看的批次数。
const recentBatchLimit = 20

// GetImportQueue 返回导入队列页面所需的批次列表，最新的在前。
//
// 任务区从「侧栏里展开的一小块」改成独立页面后，这里要给出的不只是
// 当前这一批的进度，还有刚跑完那几批的统计——用户回头核对失败条目时
// 任务往往已经结束了。
func (a *App) GetImportQueue() ([]ImportBatchView, error) {
	if err := a.ready(); err != nil {
		return nil, err
	}

	batches, err := a.store.RecentBatches(recentBatchLimit)
	if err != nil {
		return nil, err
	}

	out := make([]ImportBatchView, 0, len(batches))
	for _, b := range batches {
		stats, err := a.store.Stats(b.ID)
		if err != nil {
			return nil, err
		}
		v := ImportBatchView{
			ID: b.ID, Filename: b.Filename, Status: string(b.Status),
			FileRows: b.TotalRows, TotalRows: stats.Total,
			Processed: stats.Processed(), Stats: stats,
			Running:      a.runner != nil && a.runner.IsRunning(b.ID),
			CreatedAt:    b.CreatedAt.UTC().Format(time.RFC3339),
			FinishedAt:   formatTime(b.FinishedAt),
			FailedTitles: []string{},
		}
		if failed, err := a.store.FailedItems(b.ID); err == nil {
			for _, it := range failed {
				v.FailedTitles = append(v.FailedTitles, describeItem(it))
			}
		}
		out = append(out, v)
	}
	return out, nil
}

// describeItem 返回一条目的可读描述，供失败列表使用。
func describeItem(it store.ImportItem) string {
	return fmt.Sprintf("%s · %s · %s", derefOr(it.InTitle, "—"),
		derefOr(it.InArtist, "—"), derefOr(it.InAlbum, "—"))
}
