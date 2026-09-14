package store

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/norhua/sonexa/internal/provider"
)

// WriteDetailColumns 把详情中的署名原串与上游标识写入曲目及其歌集。
//
// 这些字段不走编辑界面那套「填充/保持」：它们是上游事实的直接映射，
// 由 ApplyDetailToTrack 在拉取详情时写入。
//
// 曲目侧与歌集侧分开写：署名、录音标识、作品标识、ISRC、音轨序号属于
// 这一首；碟数、音轨数、发行日期、类型、厂牌、目录号属于这张歌集。
func (s *Store) WriteDetailColumns(trackID int64, d *provider.Detail) error {
	if d == nil {
		return nil
	}
	now := time.Now().UTC()
	return s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`
			UPDATE track SET
				credit            = COALESCE(?, credit),
				title             = COALESCE(NULLIF(?,''), title),
				recording_ref     = COALESCE(?, recording_ref),
				work_ref          = COALESCE(?, work_ref),
				isrc              = COALESCE(?, isrc),
				track_number_text = COALESCE(?, track_number_text),
				updated_at        = ?
			WHERE id = ?`,
			d.TrackCredit, deref(d.Title),
			d.RecordingRef, d.WorkRef, d.ISRC, d.TrackNumberText,
			now, trackID); err != nil {
			return err
		}

		var releaseID *int64
		if err := tx.QueryRow(`SELECT release_id FROM track WHERE id = ?`, trackID).Scan(&releaseID); err != nil {
			return err
		}
		if releaseID == nil {
			return syncFTS(tx, trackID)
		}

		// 发行组：详情通常带着它，补上之后同一专辑的不同版本才能合为一条
		var groupID *int64
		if d.ReleaseGroupRef != nil && *d.ReleaseGroupRef != "" {
			source := string(d.Provider)
			id, err := upsertReleaseGroupTx(tx, &ReleaseGroup{
				Provider: &source, ProviderRef: d.ReleaseGroupRef,
				Title:            deref(d.AlbumTitle),
				PrimaryType:      normalizeTypePtr(d.PrimaryType),
				SecondaryTypes:   secondaryTypesJSON(d.SecondaryTypes),
				FirstReleaseDate: d.FirstReleaseDate,
			})
			if err != nil {
				return err
			}
			groupID = &id
		}

		if _, err := tx.Exec(`
			UPDATE release SET
				release_group_id   = COALESCE(?, release_group_id),
				title              = COALESCE(NULLIF(?,''), title),
				credit             = COALESCE(?, credit),
				primary_type       = COALESCE(?, primary_type),
				secondary_types    = COALESCE(?, secondary_types),
				release_status     = COALESCE(?, release_status),
				track_total        = COALESCE(?, track_total),
				disc_total         = COALESCE(?, disc_total),
				release_date       = COALESCE(?, release_date),
				first_release_date = COALESCE(?, first_release_date),
				label              = COALESCE(?, label),
				catalog_number     = COALESCE(?, catalog_number),
				updated_at         = ?
			WHERE id = ?`,
			groupID, deref(d.AlbumTitle), d.AlbumCredit,
			normalizeTypePtr(d.PrimaryType), secondaryTypesJSON(d.SecondaryTypes),
			d.ReleaseStatus, d.TrackTotal, d.DiscTotal,
			d.ReleaseDate, d.FirstReleaseDate, d.Label, d.CatalogNumber,
			now, *releaseID); err != nil {
			return err
		}
		return syncFTS(tx, trackID)
	})
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// secondaryTypesJSON 把次要类型数组序列化为 JSON。
func secondaryTypesJSON(types []string) *string {
	if len(types) == 0 {
		return nil
	}
	data, err := json.Marshal(types)
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
}

// MarkTrackForReview 标记某曲目需要人工确认。
//
// 「拆分不确定时不拆」的落点：保留原串、不建立猜测性的艺术家记录，
// 而是把该曲目标记出来交由用户处置。
//
// 写在 enrich_error 而不是 comment 上：comment 是用户自己的备注字段，
// 往里塞 `[需确认] …` 会把他写的内容冲掉。
func (s *Store) MarkTrackForReview(trackID int64, reason string) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`UPDATE track SET enrich_error = ?, updated_at = ? WHERE id = ?`,
			"[需确认] "+reason, time.Now().UTC(), trackID)
		return err
	})
}

// TracksNeedingReview 返回被标记为需人工确认的曲目。
func (s *Store) TracksNeedingReview() ([]int64, error) {
	rows, err := s.db.Query(
		`SELECT id FROM track WHERE enrich_error LIKE '[需确认]%'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
