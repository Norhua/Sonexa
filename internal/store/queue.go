package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ItemStatus 是待匹配条目的状态，取自固定集合。
type ItemStatus string

const (
	StatusQueued    ItemStatus = "queued"    // 排队中
	StatusMatching  ItemStatus = "matching"  // 匹配中
	StatusImported  ItemStatus = "imported"  // 已入库
	StatusReview    ItemStatus = "review"    // 待复核
	StatusFailed    ItemStatus = "failed"    // 失败
	StatusSkipped   ItemStatus = "skipped"   // 已跳过
	StatusMerged    ItemStatus = "merged"    // 已合并
	StatusDiscarded ItemStatus = "discarded" // 已丢弃
)

// ReviewReason 是条目进入复核队列的成因。
//
// 三种成因在导入统计里落到两个互不重叠的数上：疑似重复与多义计入「进入复核」，
// 无结果计入「未匹配」。仅凭 status='review' 分不出来，因此单独记一列。
type ReviewReason string

const (
	// ReviewDuplicate 本地疑似重复（分流规则 1）。
	ReviewDuplicate ReviewReason = "duplicate"
	// ReviewAmbiguous 多条高置信无法唯一裁决（分流规则 3 同组多份发行、规则 4 跨发行组）。
	ReviewAmbiguous ReviewReason = "ambiguous"
	// ReviewNoMatch 无结果或全部低置信（分流规则 5）。
	ReviewNoMatch ReviewReason = "no_match"
	// ReviewReasonUnchanged 表示沿用条目已有的成因，不改写它。
	//
	// 复核队列里的「改搜」与「都不匹配」只是更新候选快照，
	// 条目为什么落到复核里这件事没有变。
	ReviewReasonUnchanged ReviewReason = ""
)

// AllStatuses 返回全部状态，供统计与界面使用。
func AllStatuses() []ItemStatus {
	return []ItemStatus{
		StatusQueued, StatusMatching, StatusImported, StatusReview,
		StatusFailed, StatusSkipped, StatusMerged, StatusDiscarded,
	}
}

// IsTerminal 报告该状态是否为终态（不再自动流转）。
func (s ItemStatus) IsTerminal() bool {
	switch s {
	case StatusImported, StatusSkipped, StatusMerged, StatusDiscarded:
		return true
	}
	return false
}

// BatchStatus 是导入批次的状态。
type BatchStatus string

const (
	BatchRunning BatchStatus = "running"
	BatchPaused  BatchStatus = "paused"
	BatchDone    BatchStatus = "done"
)

// ImportBatch 是一次导入。
type ImportBatch struct {
	ID         int64       `json:"id"`
	Filename   string      `json:"filename"`
	Encoding   *string     `json:"encoding"`
	Delimiter  *string     `json:"delimiter"`
	ColumnMap  *string     `json:"columnMap"`
	TotalRows  int         `json:"totalRows"`
	Status     BatchStatus `json:"status"`
	CreatedAt  time.Time   `json:"createdAt"`
	FinishedAt *time.Time  `json:"finishedAt"`
}

// ImportItem 是队列中的一个条目。
type ImportItem struct {
	ID       int64 `json:"id"`
	BatchID  int64 `json:"batchId"`
	RowIndex int   `json:"rowIndex"`
	// RawRow 是原始整行 JSON，永不修改——
	// 用户在复核队列改搜只影响查询，不动原值。
	RawRow        string     `json:"rawRow"`
	InTitle       *string    `json:"inTitle"`
	InArtist      *string    `json:"inArtist"`
	InAlbum       *string    `json:"inAlbum"`
	InYear        *int       `json:"inYear"`
	InTrackNo     *int       `json:"inTrackNo"`
	InDiscNo      *int       `json:"inDiscNo"`
	InDurationMS  *int       `json:"inDurationMs"`
	Status        ItemStatus `json:"status"`
	AttemptCount  int        `json:"attemptCount"`
	LastError     *string    `json:"lastError"`
	LastErrorAt   *time.Time `json:"lastErrorAt"`
	NextRetryAt   *time.Time `json:"nextRetryAt"`
	Candidates    *string    `json:"candidates"`
	TriedVariants *string    `json:"triedVariants"`
	// ReviewReason 是进入复核的成因；非复核状态时为空。
	ReviewReason   *string `json:"reviewReason"`
	ReviewEvidence *string `json:"reviewEvidence"`
	TrackID        *int64  `json:"trackId"`
	MergedInto     *int64  `json:"mergedInto"`
}

// CreateBatch 建立一个导入批次。
func (s *Store) CreateBatch(b *ImportBatch) (int64, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC()
		res, err := tx.Exec(`
			INSERT INTO import_batch (filename, encoding, delimiter, column_map, total_rows, status, created_at)
			VALUES (?,?,?,?,?,?,?)`,
			b.Filename, b.Encoding, b.Delimiter, b.ColumnMap, b.TotalRows, string(BatchRunning), now)
		if err != nil {
			return fmt.Errorf("建立导入批次失败: %w", err)
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// EnqueueItems 把一批条目写入队列。
//
// 队列持久化到 SQLite：1000 行 × 1 req/s ≈ 17 分钟，用户必然会中途关窗口，
// 内存队列意味着重启即丢。
func (s *Store) EnqueueItems(batchID int64, items []ImportItem) error {
	return s.withTx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(`
			INSERT INTO import_item (
				batch_id, row_index, raw_row, in_title, in_artist, in_album,
				in_year, in_track_no, in_disc_no, in_duration_ms, status, attempt_count, updated_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,0,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		now := time.Now().UTC()
		for _, it := range items {
			if _, err := stmt.Exec(batchID, it.RowIndex, it.RawRow,
				it.InTitle, it.InArtist, it.InAlbum,
				it.InYear, it.InTrackNo, it.InDiscNo, it.InDurationMS,
				string(StatusQueued), now); err != nil {
				return fmt.Errorf("写入第 %d 行失败: %w", it.RowIndex, err)
			}
		}
		return nil
	})
}

// ClaimNextItem 取出下一个待处理条目并置为匹配中。
//
// 在事务内「取出并标记」，避免多个工作者拿到同一条。
// 返回 nil 表示当前没有可处理的条目。
func (s *Store) ClaimNextItem(batchID int64) (*ImportItem, error) {
	var item *ImportItem
	err := s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC()
		row := tx.QueryRow(`
			SELECT id, batch_id, row_index, raw_row, in_title, in_artist, in_album,
			       in_year, in_track_no, in_disc_no, in_duration_ms, status, attempt_count,
			       last_error, last_error_at, next_retry_at, candidates, tried_variants,
			       review_reason, review_evidence, track_id, merged_into
			FROM import_item
			WHERE batch_id = ? AND status = ?
			  AND (next_retry_at IS NULL OR next_retry_at <= ?)
			ORDER BY next_retry_at IS NOT NULL, row_index
			LIMIT 1`, batchID, string(StatusQueued), now)

		it, err := scanItem(row)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE import_item SET status = ?, updated_at = ? WHERE id = ?`,
			string(StatusMatching), now, it.ID); err != nil {
			return err
		}
		it.Status = StatusMatching
		item = it
		return nil
	})
	return item, err
}

// scanItem 从一行结果中读出条目。
func scanItem(row interface{ Scan(...any) error }) (*ImportItem, error) {
	var it ImportItem
	var status string
	err := row.Scan(&it.ID, &it.BatchID, &it.RowIndex, &it.RawRow,
		&it.InTitle, &it.InArtist, &it.InAlbum,
		&it.InYear, &it.InTrackNo, &it.InDiscNo, &it.InDurationMS, &status, &it.AttemptCount,
		&it.LastError, &it.LastErrorAt, &it.NextRetryAt, &it.Candidates, &it.TriedVariants,
		&it.ReviewReason, &it.ReviewEvidence, &it.TrackID, &it.MergedInto)
	if err != nil {
		return nil, err
	}
	it.Status = ItemStatus(status)
	return &it, nil
}

// MarkImported 把条目置为已入库并记录曲目 id。
func (s *Store) MarkImported(itemID, trackID int64) error {
	return s.setStatus(itemID, StatusImported, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE import_item SET track_id = ? WHERE id = ?`, trackID, itemID)
		return err
	})
}

// MarkReview 把条目置为待复核，记录成因并保存候选快照与已尝试变体。
//
// reason 为 ReviewReasonUnchanged 时不改写已有成因。
func (s *Store) MarkReview(itemID int64, reason ReviewReason, candidatesJSON, variantsJSON string, evidence ...string) error {
	return s.setStatus(itemID, StatusReview, func(tx *sql.Tx) error {
		if len(evidence) > 0 {
			if _, err := tx.Exec("UPDATE import_item SET review_evidence=? WHERE id=?", evidence[0], itemID); err != nil {
				return err
			}
		}
		if reason == ReviewReasonUnchanged {
			_, err := tx.Exec(
				`UPDATE import_item SET candidates = ?, tried_variants = ? WHERE id = ?`,
				nullIfEmpty(candidatesJSON), nullIfEmpty(variantsJSON), itemID)
			return err
		}
		_, err := tx.Exec(
			`UPDATE import_item SET review_reason = ?, candidates = ?, tried_variants = ? WHERE id = ?`,
			string(reason), nullIfEmpty(candidatesJSON), nullIfEmpty(variantsJSON), itemID)
		return err
	})
}

// MarkMerged 把条目置为已合并并指向库中已有曲目。
//
// 合并不新建曲目：标记 merged_into 后条目从队列中消失，库中曲目数不变。
func (s *Store) MarkMerged(itemID, targetTrackID int64) error {
	return s.setStatus(itemID, StatusMerged, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE import_item SET merged_into = ? WHERE id = ?`, targetTrackID, itemID)
		return err
	})
}

// MarkDiscarded 丢弃条目且不入库。
func (s *Store) MarkDiscarded(itemID int64) error {
	return s.setStatus(itemID, StatusDiscarded, nil)
}

// MarkSkipped 把条目置为已跳过。
func (s *Store) MarkSkipped(itemID int64) error {
	return s.setStatus(itemID, StatusSkipped, nil)
}

// setStatus 在事务内更新状态并执行附加操作。
func (s *Store) setStatus(itemID int64, status ItemStatus, extra func(*sql.Tx) error) error {
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(
			`UPDATE import_item SET status = ?, updated_at = ? WHERE id = ?`,
			string(status), time.Now().UTC(), itemID); err != nil {
			return err
		}
		if extra != nil {
			return extra(tx)
		}
		return nil
	})
}

// RecordFailure 记录一次失败并决定是退避重排还是标记失败。
//
// 累计失败达到 maxAttempts 后标记为失败状态并停止自动重排；
// 未达上限时按 backoff 重新排队。
func (s *Store) RecordFailure(itemID int64, errMsg string, backoff time.Duration, maxAttempts int) (ItemStatus, error) {
	var final ItemStatus
	err := s.withTx(func(tx *sql.Tx) error {
		var attempts int
		if err := tx.QueryRow(
			`SELECT attempt_count FROM import_item WHERE id = ?`, itemID).Scan(&attempts); err != nil {
			return err
		}
		attempts++
		now := time.Now().UTC()

		if attempts >= maxAttempts {
			final = StatusFailed
			_, err := tx.Exec(`
				UPDATE import_item SET status = ?, attempt_count = ?,
					last_error = ?, last_error_at = ?, next_retry_at = NULL, updated_at = ?
				WHERE id = ?`,
				string(StatusFailed), attempts, errMsg, now, now, itemID)
			return err
		}

		final = StatusQueued
		retryAt := now.Add(backoff)
		_, err := tx.Exec(`
			UPDATE import_item SET status = ?, attempt_count = ?,
				last_error = ?, last_error_at = ?, next_retry_at = ?, updated_at = ?
			WHERE id = ?`,
			string(StatusQueued), attempts, errMsg, now, retryAt, now, itemID)
		return err
	})
	return final, err
}

// RetryItem 让失败条目重新进入匹配（复核队列的「立即重试」）。
func (s *Store) RetryItem(itemID int64) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE import_item SET status = ?, next_retry_at = NULL, updated_at = ?
			WHERE id = ?`, string(StatusQueued), time.Now().UTC(), itemID)
		return err
	})
}

// ResetStaleMatching 把中断时停留在「匹配中」的条目退回排队。
//
// 应用重启后调用：这些条目上次未跑完，需要重新处理，
// 而已完成的条目不受影响、不重复处理。
func (s *Store) ResetStaleMatching() error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE import_item SET status = ?, updated_at = ? WHERE status = ?`,
			string(StatusQueued), time.Now().UTC(), string(StatusMatching))
		return err
	})
}

// BatchStats 是一次导入的分状态计数。
type BatchStats struct {
	Total    int `json:"total"`
	Queued   int `json:"queued"`
	Matching int `json:"matching"`
	Imported int `json:"imported"`
	// Review 是待复核总数，三种成因都在里面。
	Review int `json:"review"`
	// Unmatched 是 Review 中成因为「无结果 / 全部低置信」的那部分。
	//
	// 它是 Review 的子集而不是另一个状态：这些条目同样停在复核队列里等人处置。
	// 统计行要的「进入复核」是 Review - Unmatched。
	Unmatched int `json:"unmatched"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Merged    int `json:"merged"`
	Discarded int `json:"discarded"`
}

// Processed 返回已处理（不在队列中等待）的条目数。
func (b BatchStats) Processed() int {
	return b.Imported + b.Review + b.Failed + b.Skipped + b.Merged + b.Discarded
}

// Stats 统计某批次各状态的条目数。
func (s *Store) Stats(batchID int64) (BatchStats, error) {
	rows, err := s.db.Query(
		`SELECT status, COUNT(*) FROM import_item WHERE batch_id = ? GROUP BY status`, batchID)
	if err != nil {
		return BatchStats{}, err
	}
	defer rows.Close()

	var st BatchStats
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return BatchStats{}, err
		}
		st.Total += n
		switch ItemStatus(status) {
		case StatusQueued:
			st.Queued = n
		case StatusMatching:
			st.Matching = n
		case StatusImported:
			st.Imported = n
		case StatusReview:
			st.Review = n
		case StatusFailed:
			st.Failed = n
		case StatusSkipped:
			st.Skipped = n
		case StatusMerged:
			st.Merged = n
		case StatusDiscarded:
			st.Discarded = n
		}
	}
	if err := rows.Err(); err != nil {
		return BatchStats{}, err
	}

	// 「未匹配」是待复核里的一个子集，取不到状态分组里，单独数一次
	if err := s.db.QueryRow(
		`SELECT COUNT(*) FROM import_item WHERE batch_id = ? AND status = ? AND review_reason = ?`,
		batchID, string(StatusReview), string(ReviewNoMatch)).Scan(&st.Unmatched); err != nil {
		return BatchStats{}, err
	}
	return st, nil
}

// ReviewOnly 返回「进入复核」的条数：待复核总数减去无结果的那部分。
//
// 统计行要的四个数互不重叠，这一个是其中之一。
func (b BatchStats) ReviewOnly() int {
	n := b.Review - b.Unmatched
	if n < 0 {
		return 0
	}
	return n
}

// FailedItems 返回失败条目，供完成统计逐行列出其原始输入。
func (s *Store) FailedItems(batchID int64) ([]ImportItem, error) {
	return s.itemsByStatus(batchID, StatusFailed)
}

// ReviewItems 返回待复核条目。
func (s *Store) ReviewItems(batchID int64) ([]ImportItem, error) {
	return s.itemsByStatus(batchID, StatusReview)
}

// AllReviewItems 返回全部批次的待复核与失败条目，供复核队列逐条浏览。
func (s *Store) AllReviewItems() ([]ImportItem, error) {
	rows, err := s.db.Query(`
		SELECT id, batch_id, row_index, raw_row, in_title, in_artist, in_album,
		       in_year, in_track_no, in_disc_no, in_duration_ms, status, attempt_count,
		       last_error, last_error_at, next_retry_at, candidates, tried_variants,
		       review_reason, review_evidence, track_id, merged_into
		FROM import_item
		WHERE status IN (?, ?)
		ORDER BY batch_id, row_index`, string(StatusReview), string(StatusFailed))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectItems(rows)
}

func (s *Store) itemsByStatus(batchID int64, status ItemStatus) ([]ImportItem, error) {
	rows, err := s.db.Query(`
		SELECT id, batch_id, row_index, raw_row, in_title, in_artist, in_album,
		       in_year, in_track_no, in_disc_no, in_duration_ms, status, attempt_count,
		       last_error, last_error_at, next_retry_at, candidates, tried_variants,
		       review_reason, review_evidence, track_id, merged_into
		FROM import_item WHERE batch_id = ? AND status = ? ORDER BY row_index`,
		batchID, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectItems(rows)
}

func collectItems(rows *sql.Rows) ([]ImportItem, error) {
	var out []ImportItem
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// GetItem 按 id 读取条目。
func (s *Store) GetItem(itemID int64) (*ImportItem, error) {
	row := s.db.QueryRow(`
		SELECT id, batch_id, row_index, raw_row, in_title, in_artist, in_album,
		       in_year, in_track_no, in_disc_no, in_duration_ms, status, attempt_count,
		       last_error, last_error_at, next_retry_at, candidates, tried_variants,
		       review_reason, review_evidence, track_id, merged_into
		FROM import_item WHERE id = ?`, itemID)
	it, err := scanItem(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return it, err
}

// SetBatchStatus 更新批次状态（暂停 / 恢复 / 完成）。
func (s *Store) SetBatchStatus(batchID int64, status BatchStatus) error {
	return s.withTx(func(tx *sql.Tx) error {
		if status == BatchDone {
			_, err := tx.Exec(
				`UPDATE import_batch SET status = ?, finished_at = ? WHERE id = ?`,
				string(status), time.Now().UTC(), batchID)
			return err
		}
		_, err := tx.Exec(`UPDATE import_batch SET status = ? WHERE id = ?`, string(status), batchID)
		return err
	})
}

// GetBatch 按 id 读取批次。
func (s *Store) GetBatch(batchID int64) (*ImportBatch, error) {
	var b ImportBatch
	var status string
	err := s.db.QueryRow(`
		SELECT id, filename, encoding, delimiter, column_map, total_rows, status, created_at, finished_at
		FROM import_batch WHERE id = ?`, batchID).Scan(
		&b.ID, &b.Filename, &b.Encoding, &b.Delimiter, &b.ColumnMap,
		&b.TotalRows, &status, &b.CreatedAt, &b.FinishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.Status = BatchStatus(status)
	return &b, nil
}

// ActiveBatches 返回尚未完成的批次，供重启后继续。
func (s *Store) ActiveBatches() ([]ImportBatch, error) {
	rows, err := s.db.Query(`
		SELECT id, filename, encoding, delimiter, column_map, total_rows, status, created_at, finished_at
		FROM import_batch WHERE status != ? ORDER BY created_at`, string(BatchDone))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ImportBatch
	for rows.Next() {
		var b ImportBatch
		var status string
		if err := rows.Scan(&b.ID, &b.Filename, &b.Encoding, &b.Delimiter, &b.ColumnMap,
			&b.TotalRows, &status, &b.CreatedAt, &b.FinishedAt); err != nil {
			return nil, err
		}
		b.Status = BatchStatus(status)
		out = append(out, b)
	}
	return out, rows.Err()
}

// MarshalJSONField 把任意值序列化为可空的 JSON 字符串字段。
func MarshalJSONField(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// RecentBatches 返回最近的若干批次，最新的在前。
//
// 与 ActiveBatches 的区别是它也包含已完成的批次——导入队列页面要让用户
// 看到刚刚跑完的那一批的统计，而不是任务一结束页面就空掉。
func (s *Store) RecentBatches(limit int) ([]ImportBatch, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.Query(`
		SELECT id, filename, encoding, delimiter, column_map, total_rows, status, created_at, finished_at
		FROM import_batch ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ImportBatch
	for rows.Next() {
		var b ImportBatch
		var status string
		if err := rows.Scan(&b.ID, &b.Filename, &b.Encoding, &b.Delimiter, &b.ColumnMap,
			&b.TotalRows, &status, &b.CreatedAt, &b.FinishedAt); err != nil {
			return nil, err
		}
		b.Status = BatchStatus(status)
		out = append(out, b)
	}
	return out, rows.Err()
}
