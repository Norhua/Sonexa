package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// TracklistState 是发行清单的状态。
type TracklistState string

const (
	TracklistNone     TracklistState = "none"     // 无清单（如导入自动入库只写了 release 行）
	TracklistPartial  TracklistState = "partial"  // 部分
	TracklistComplete TracklistState = "complete" // 完整
	TracklistFailed   TracklistState = "failed"   // 获取失败
)

// Release 是发行记录，镜像上游事实，不承载任何用户数据。
type Release struct {
	ID               int64
	Provider         string
	ProviderRef      string
	ReleaseGroupRef  *string
	Title            string
	AlbumCredit      *string
	PrimaryType      *string
	SecondaryTypes   *string
	ReleaseStatus    *string
	MediumFormat     *string
	Country          *string
	ReleaseDate      *string
	FirstReleaseDate *string
	TrackTotal       *int
	DiscTotal        *int
	CoverPath        *string
	CoverSourceURL   *string
	TracklistState   TracklistState
	FetchedAt        *time.Time
}

// ReleaseTrack 是清单中的一个位置。
type ReleaseTrack struct {
	DiscNumber      int
	TrackNumber     int
	TrackRef        *string // 与 track_metadata.match_ref 同一取值方案
	RecordingRef    *string // 用于「已有其他版本」判定
	Title           string
	TrackCredit     *string
	DurationMS      *int
	TrackNumberText *string
}

// UpsertRelease 按 (provider, provider_ref) 写入或更新发行，返回其 id。
//
// 重复写入走更新而非插入——同一张发行会被多次遇到（搜索、详情、清单补拉），
// 每次都插一条会撞唯一约束。
func (s *Store) UpsertRelease(r *Release) (int64, error) {
	now := time.Now().UTC()
	_, err := s.db.Exec(`
		INSERT INTO release (
			provider, provider_ref, release_group_ref, title, album_credit,
			primary_type, secondary_types, release_status, medium_format, country,
			release_date, first_release_date, track_total, disc_total,
			cover_path, cover_source_url, tracklist_state, fetched_at, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(provider, provider_ref) DO UPDATE SET
			release_group_ref  = COALESCE(excluded.release_group_ref, release.release_group_ref),
			title              = excluded.title,
			album_credit       = COALESCE(excluded.album_credit, release.album_credit),
			primary_type       = COALESCE(excluded.primary_type, release.primary_type),
			secondary_types    = COALESCE(excluded.secondary_types, release.secondary_types),
			release_status     = COALESCE(excluded.release_status, release.release_status),
			medium_format      = COALESCE(excluded.medium_format, release.medium_format),
			country            = COALESCE(excluded.country, release.country),
			release_date       = COALESCE(excluded.release_date, release.release_date),
			first_release_date = COALESCE(excluded.first_release_date, release.first_release_date),
			track_total        = COALESCE(excluded.track_total, release.track_total),
			disc_total         = COALESCE(excluded.disc_total, release.disc_total),
			cover_path         = COALESCE(excluded.cover_path, release.cover_path),
			cover_source_url   = COALESCE(excluded.cover_source_url, release.cover_source_url),
			-- 清单状态只升不降：已有完整清单时，一次只写 release 行的调用不该把它退回 none
			tracklist_state    = CASE
				WHEN excluded.tracklist_state = 'complete' THEN 'complete'
				WHEN release.tracklist_state = 'complete' THEN 'complete'
				ELSE excluded.tracklist_state END,
			fetched_at         = COALESCE(excluded.fetched_at, release.fetched_at),
			updated_at         = excluded.updated_at`,
		r.Provider, r.ProviderRef, r.ReleaseGroupRef, r.Title, r.AlbumCredit,
		r.PrimaryType, r.SecondaryTypes, r.ReleaseStatus, r.MediumFormat, r.Country,
		r.ReleaseDate, r.FirstReleaseDate, r.TrackTotal, r.DiscTotal,
		r.CoverPath, r.CoverSourceURL, string(r.TracklistState), r.FetchedAt, now)
	if err != nil {
		return 0, fmt.Errorf("写入发行失败: %w", err)
	}

	var id int64
	if err := s.db.QueryRow(
		`SELECT id FROM release WHERE provider = ? AND provider_ref = ?`,
		r.Provider, r.ProviderRef).Scan(&id); err != nil {
		return 0, fmt.Errorf("读取发行 id 失败: %w", err)
	}
	return id, nil
}

// ReplaceTracklist 以整份清单替换某发行的既有清单，并更新清单状态与获取时间。
// 整体在一个事务内完成，避免出现半份清单。
func (s *Store) ReplaceTracklist(releaseID int64, tracks []ReleaseTrack, state TracklistState) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`DELETE FROM release_track WHERE release_id = ?`, releaseID); err != nil {
		return fmt.Errorf("清空旧清单失败: %w", err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO release_track (
			release_id, disc_number, track_number, track_ref, recording_ref,
			title, track_credit, duration_ms, track_number_text
		) VALUES (?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, t := range tracks {
		if _, err := stmt.Exec(releaseID, t.DiscNumber, t.TrackNumber, t.TrackRef,
			t.RecordingRef, t.Title, t.TrackCredit, t.DurationMS, t.TrackNumberText); err != nil {
			return fmt.Errorf("写入清单位置 %d-%d 失败: %w", t.DiscNumber, t.TrackNumber, err)
		}
	}
	if _, err := tx.Exec(
		`UPDATE release SET tracklist_state = ?, fetched_at = ?, updated_at = ? WHERE id = ?`,
		string(state), time.Now().UTC(), time.Now().UTC(), releaseID); err != nil {
		return err
	}
	return tx.Commit()
}

// GetReleaseByRef 按 (provider, provider_ref) 读取发行。
func (s *Store) GetReleaseByRef(provider, ref string) (*Release, error) {
	var r Release
	var state string
	err := s.db.QueryRow(`
		SELECT id, provider, provider_ref, release_group_ref, title, album_credit,
		       primary_type, secondary_types, release_status, medium_format, country,
		       release_date, first_release_date, track_total, disc_total,
		       cover_path, cover_source_url, tracklist_state, fetched_at
		FROM release WHERE provider = ? AND provider_ref = ?`, provider, ref).Scan(
		&r.ID, &r.Provider, &r.ProviderRef, &r.ReleaseGroupRef, &r.Title, &r.AlbumCredit,
		&r.PrimaryType, &r.SecondaryTypes, &r.ReleaseStatus, &r.MediumFormat, &r.Country,
		&r.ReleaseDate, &r.FirstReleaseDate, &r.TrackTotal, &r.DiscTotal,
		&r.CoverPath, &r.CoverSourceURL, &state, &r.FetchedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.TracklistState = TracklistState(state)
	return &r, nil
}

// GetTracklist 读取某发行的清单，按碟号与音轨号升序。
func (s *Store) GetTracklist(releaseID int64) ([]ReleaseTrack, error) {
	rows, err := s.db.Query(`
		SELECT disc_number, track_number, track_ref, recording_ref,
		       title, track_credit, duration_ms, track_number_text
		FROM release_track WHERE release_id = ?
		ORDER BY disc_number, track_number`, releaseID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ReleaseTrack
	for rows.Next() {
		var t ReleaseTrack
		if err := rows.Scan(&t.DiscNumber, &t.TrackNumber, &t.TrackRef, &t.RecordingRef,
			&t.Title, &t.TrackCredit, &t.DurationMS, &t.TrackNumberText); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
