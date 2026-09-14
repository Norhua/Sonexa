package store

import (
	"database/sql"
	"fmt"
	"time"
)

// EnrichState 是一条曲目的「补全信息匹配」状态。
//
// 它存在库里而不是每次推导：三种状态里有一种（absent）推导不出来——
// 「上游确实没有可补的东西」与「还没问过」在数据上长得一模一样。
type EnrichState string

const (
	// EnrichPending 未匹配：有上游身份，详情还没取。
	EnrichPending EnrichState = "pending"
	// EnrichFetched 已匹配：详情已落库。
	EnrichFetched EnrichState = "fetched"
	// EnrichAbsent 已匹配（无可补）：没有发行标识，无从取起。
	//
	// 记 absent 而不是留在 pending，否则后台任务会一直回到这条曲目上。
	EnrichAbsent EnrichState = "absent"
	// EnrichSkipped 无需匹配：用户明确要求收录且不追求外部身份。
	EnrichSkipped EnrichState = "skipped"
)

// MaxEnrichAttempts 是一条曲目连续取详情失败几次后退出工作集。
//
// 退出不是放弃：用户点「开始」时计数清零，那一条会被重新试一遍。
// 不设上限的话，一条永久取不到的曲目会在每一轮里都排在队首。
const MaxEnrichAttempts = 5

// EnrichTarget 是工作集里的一条：补它需要的最少信息。
type EnrichTarget struct {
	TrackID  int64
	Provider string
	// ReleaseRef 是发行标识。MusicBrainz 用 release MBID，
	// iTunes 用 `country:collectionID`——两者都存在 release_mbid 列上。
	ReleaseRef string
	Attempts   int
}

// EnrichCounts 是三种用户可见状态的计数。
//
// Matched 合并了 fetched 与 absent：对用户来说「上游没有更多可补的」
// 与「已经补完了」是同一件事——都不必再等。
type EnrichCounts struct {
	Matched int `json:"matched"`
	Pending int `json:"pending"`
	Skipped int `json:"skipped"`
}

// InitialEnrichState 判定一条新建曲目的补全状态。
//
// 只有一处判定，创建路径（导入自动入库、复核接受、搜索页加入曲库、
// 无需匹配）全部经由它——分散判定必然漂移，而漂移的后果是某条路径入库的曲目
// 永远不被后台任务看到。
func InitialEnrichState(in NewTrackInput) EnrichState {
	if in.MatchSource == nil || *in.MatchSource == "" {
		// 没有上游身份：这是「无需匹配」入库的那一种
		return EnrichSkipped
	}
	if in.Metadata != nil && in.Metadata.ReleaseMBID != nil && *in.Metadata.ReleaseMBID != "" {
		return EnrichPending
	}
	// 有身份但没有发行标识：详情无从取起
	return EnrichAbsent
}

// TracksNeedingEnrichment 返回待补全的工作集，最早入库的在前。
//
// limit ≤ 0 表示不限量：工作集要**同步**枚举完整，调用方才能当场知道
// 「有没有要做的事」，而不是静默地跑一个空循环。这是一次本地查询，值这个代价。
func (s *Store) TracksNeedingEnrichment(limit int) ([]EnrichTarget, error) {
	query := `
		SELECT m.track_id, COALESCE(m.match_source, ''), COALESCE(m.release_mbid, ''),
		       m.enrich_attempts
		FROM track_metadata m
		JOIN track t ON t.id = m.track_id
		WHERE m.enrich_state = ?
		  AND COALESCE(m.match_source, '') <> ''
		  AND COALESCE(m.release_mbid, '') <> ''
		  AND m.enrich_attempts < ?
		ORDER BY m.track_id`
	args := []any{string(EnrichPending), MaxEnrichAttempts}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("查询待补全曲目失败: %w", err)
	}
	defer rows.Close()

	var out []EnrichTarget
	for rows.Next() {
		var t EnrichTarget
		if err := rows.Scan(&t.TrackID, &t.Provider, &t.ReleaseRef, &t.Attempts); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// EnrichStats 返回三种状态的计数。
//
// 未记录状态的行（理论上不该有）按 pending 计：宁可让后台任务多看一眼，
// 也不要把一条没补的曲目算成已补。
func (s *Store) EnrichStats() (EnrichCounts, error) {
	var c EnrichCounts
	err := s.db.QueryRow(`
		SELECT
			SUM(CASE WHEN enrich_state IN ('fetched','absent') THEN 1 ELSE 0 END),
			SUM(CASE WHEN enrich_state = 'skipped' THEN 1 ELSE 0 END),
			SUM(CASE WHEN enrich_state = 'pending' OR enrich_state IS NULL THEN 1 ELSE 0 END)
		FROM track_metadata`).Scan(&nullInt{&c.Matched}, &nullInt{&c.Skipped}, &nullInt{&c.Pending})
	if err != nil {
		return c, fmt.Errorf("统计补全状态失败: %w", err)
	}
	return c, nil
}

// EnrichStateOf 读取某曲目的补全状态。没有丰富信息行时按「无需匹配」处理。
func (s *Store) EnrichStateOf(trackID int64) (EnrichState, error) {
	var state sql.NullString
	err := s.db.QueryRow(
		`SELECT enrich_state FROM track_metadata WHERE track_id = ?`, trackID).Scan(&state)
	if err == sql.ErrNoRows {
		return EnrichSkipped, nil
	}
	if err != nil {
		return "", err
	}
	if !state.Valid || state.String == "" {
		// 状态缺失按待补处理：宁可多看一眼，也不要把没补的算成已补
		return EnrichPending, nil
	}
	return EnrichState(state.String), nil
}

// SetEnrichState 写入某曲目的补全状态并清掉错误与重试计数。
func (s *Store) SetEnrichState(trackID int64, state EnrichState) error {
	return s.withTx(func(tx *sql.Tx) error {
		if err := ensureMetadataRow(tx, trackID); err != nil {
			return err
		}
		_, err := tx.Exec(`
			UPDATE track_metadata
			SET enrich_state = ?, enrich_attempts = 0, enrich_error = NULL, updated_at = ?
			WHERE track_id = ?`, string(state), time.Now().UTC(), trackID)
		return err
	})
}

// RecordEnrichFailure 记录一次补全失败：保持 pending、累加重试计数。
//
// 失败不回滚入库，也不改变曲目的身份——曲目已经在库里，缺的只是细节。
// 原因写在 enrich_error 而不是 comment 上：comment 是用户自己的字段，
// 往里塞 `[丰富失败] …` 会把他写的备注冲掉。
func (s *Store) RecordEnrichFailure(trackID int64, reason string) error {
	return s.withTx(func(tx *sql.Tx) error {
		if err := ensureMetadataRow(tx, trackID); err != nil {
			return err
		}
		_, err := tx.Exec(`
			UPDATE track_metadata
			SET enrich_state    = ?,
			    enrich_attempts = enrich_attempts + 1,
			    enrich_error    = ?,
			    updated_at      = ?
			WHERE track_id = ?`, string(EnrichPending), reason, time.Now().UTC(), trackID)
		return err
	})
}

// ResetEnrichAttempts 把待补全条目的重试计数清零。
//
// 用户点「开始」时调用：那是他在说「再试一次」，而达到上限退出工作集的
// 那几条必须有一条回来的路，否则它们会永久停在「未匹配」上而界面无从解释。
func (s *Store) ResetEnrichAttempts() error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(`
			UPDATE track_metadata
			SET enrich_attempts = 0
			WHERE enrich_state = ? AND enrich_attempts > 0`, string(EnrichPending))
		return err
	})
}

// ensureMetadataRow 保证丰富信息行存在。
//
// 「无需匹配」入库的曲目也有这一行（创建时就写了），但编辑与补全两条路径
// 都不该依赖那个前提——缺行时静默补一行比报错更符合调用方的预期。
func ensureMetadataRow(tx *sql.Tx, trackID int64) error {
	_, err := tx.Exec(
		`INSERT INTO track_metadata (track_id, updated_at) VALUES (?, ?)
		 ON CONFLICT(track_id) DO NOTHING`, trackID, time.Now().UTC())
	return err
}

// nullInt 把 SQL 的 NULL（空表时的 SUM）读成 0。
type nullInt struct{ dst *int }

func (n *nullInt) Scan(value any) error {
	if value == nil {
		*n.dst = 0
		return nil
	}
	switch v := value.(type) {
	case int64:
		*n.dst = int(v)
	case int:
		*n.dst = v
	case float64:
		*n.dst = int(v)
	default:
		return fmt.Errorf("无法把 %T 读为整数", value)
	}
	return nil
}
