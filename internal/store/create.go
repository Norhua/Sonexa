package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// NewTrackInput 是创建曲目的输入。
type NewTrackInput struct {
	Title  string
	Artist *string
	Album  *string

	// 溯源：来自上游结果时填入；「无需匹配」入库时留空。
	MatchSource *string
	MatchRef    *string

	// 丰富信息（可选，整体可为空）
	Metadata *MetadataInput
}

// MetadataInput 是丰富信息的写入输入。全部字段可空。
type MetadataInput struct {
	Title            *string
	Album            *string
	TrackCredit      *string
	AlbumCredit      *string
	CreditSpansJSON  *string
	PrimaryType      *string
	SecondaryTypes   *string
	ReleaseStatus    *string
	MediumFormat     *string
	Country          *string
	Year             *int
	ReleaseDate      *string
	FirstReleaseDate *string
	TrackNumber      *int
	TrackNumberText  *string
	TrackTotal       *int
	DiscNumber       *int
	DiscTotal        *int
	DurationMS       *int
	Genre            *string
	Label            *string
	CatalogNumber    *string
	Comment          *string
	RecordingMBID    *string
	ReleaseMBID      *string
	ReleaseGroupMBID *string
	WorkMBID         *string
	ISRC             *string
	ITunesTrackID    *string
	ITunesCollection *string
	ProvenanceJSON   *string
	LockedJSON       *string
}

// CreateTrackWithProvenance 创建曲目并写入溯源与丰富信息。
//
// 若溯源撞上唯一约束（并发/竞态：两个路径几乎同时加入同一条上游结果），
// 返回 *ErrAlreadyExists 并带上已有曲目的 id——
// 调用方据此静默转为「跳转到已有曲目」，不向用户报错。
func (s *Store) CreateTrackWithProvenance(in NewTrackInput) (int64, error) {
	if strings.TrimSpace(in.Title) == "" {
		return 0, ErrTitleRequired
	}

	// 预查：已存在则直接返回，不必进事务
	if in.MatchSource != nil && in.MatchRef != nil {
		if existing, err := s.FindByMatchRef(*in.MatchSource, *in.MatchRef); err != nil {
			return 0, err
		} else if existing != 0 {
			return existing, &ErrAlreadyExists{TrackID: existing}
		}
	}

	var trackID int64
	err := s.withTx(func(tx *sql.Tx) error {
		now := time.Now().UTC()
		res, err := tx.Exec(`
			INSERT INTO track (title, artist, album, created_at, updated_at)
			VALUES (?,?,?,?,?)`, in.Title, in.Artist, in.Album, now, now)
		if err != nil {
			return err
		}
		trackID, err = res.LastInsertId()
		if err != nil {
			return err
		}

		if err := insertMetadata(tx, trackID, in, now); err != nil {
			return err
		}
		return syncFTS(tx, trackID)
	})

	if err != nil {
		// 事务内撞唯一键：另一路径抢先入库了
		if isUniqueViolation(err) && in.MatchSource != nil && in.MatchRef != nil {
			if existing, findErr := s.FindByMatchRef(*in.MatchSource, *in.MatchRef); findErr == nil && existing != 0 {
				return existing, &ErrAlreadyExists{TrackID: existing}
			}
		}
		return 0, err
	}
	return trackID, nil
}

// insertMetadata 写入丰富信息与溯源。
func insertMetadata(tx *sql.Tx, trackID int64, in NewTrackInput, now time.Time) error {
	m := in.Metadata
	if m == nil {
		m = &MetadataInput{}
	}
	var matchedAt *time.Time
	if in.MatchSource != nil && in.MatchRef != nil {
		matchedAt = &now
	}

	_, err := tx.Exec(`
		INSERT INTO track_metadata (
			track_id, title, album, track_credit, album_credit, credit_spans_json,
			primary_type, secondary_types, release_status, medium_format, country,
			year, release_date, first_release_date,
			track_number, track_number_text, track_total, disc_number, disc_total, duration_ms,
			genre, label, catalog_number, comment,
			recording_mbid, release_mbid, release_group_mbid, work_mbid, isrc,
			itunes_track_id, itunes_collection_id,
			match_source, match_ref, matched_at,
			provenance_json, locked_json, updated_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		trackID, m.Title, m.Album, m.TrackCredit, m.AlbumCredit, m.CreditSpansJSON,
		m.PrimaryType, m.SecondaryTypes, m.ReleaseStatus, m.MediumFormat, m.Country,
		m.Year, m.ReleaseDate, m.FirstReleaseDate,
		m.TrackNumber, m.TrackNumberText, m.TrackTotal, m.DiscNumber, m.DiscTotal, m.DurationMS,
		m.Genre, m.Label, m.CatalogNumber, m.Comment,
		m.RecordingMBID, m.ReleaseMBID, m.ReleaseGroupMBID, m.WorkMBID, m.ISRC,
		m.ITunesTrackID, m.ITunesCollection,
		in.MatchSource, in.MatchRef, matchedAt,
		m.ProvenanceJSON, m.LockedJSON, now)
	return err
}

// isUniqueViolation 报告错误是否为唯一约束冲突。
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

// GetMetadata 读取某曲目的丰富信息；不存在时返回 nil。
func (s *Store) GetMetadata(trackID int64) (*TrackMetadata, error) {
	var m TrackMetadata
	err := s.db.QueryRow(`
		SELECT track_id, title, album, track_credit, album_credit, credit_spans_json,
		       primary_type, secondary_types, release_status, medium_format, country,
		       year, release_date, first_release_date,
		       track_number, track_number_text, track_total, disc_number, disc_total, duration_ms,
		       genre, label, catalog_number, comment,
		       cover_path, cover_source, cover_source_url, cover_width, cover_height,
		       recording_mbid, release_mbid, release_group_mbid, work_mbid, isrc,
		       itunes_track_id, itunes_collection_id,
		       match_source, match_ref, matched_at,
		       provenance_json, locked_json
		FROM track_metadata WHERE track_id = ?`, trackID).Scan(
		&m.TrackID, &m.Title, &m.Album, &m.TrackCredit, &m.AlbumCredit, &m.CreditSpansJSON,
		&m.PrimaryType, &m.SecondaryTypes, &m.ReleaseStatus, &m.MediumFormat, &m.Country,
		&m.Year, &m.ReleaseDate, &m.FirstReleaseDate,
		&m.TrackNumber, &m.TrackNumberText, &m.TrackTotal, &m.DiscNumber, &m.DiscTotal, &m.DurationMS,
		&m.Genre, &m.Label, &m.CatalogNumber, &m.Comment,
		&m.CoverPath, &m.CoverSource, &m.CoverSourceURL, &m.CoverWidth, &m.CoverHeight,
		&m.RecordingMBID, &m.ReleaseMBID, &m.ReleaseGroupMBID, &m.WorkMBID, &m.ISRC,
		&m.ITunesTrackID, &m.ITunesCollection,
		&m.MatchSource, &m.MatchRef, &m.MatchedAt,
		&m.ProvenanceJSON, &m.LockedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// TrackMetadata 是丰富信息的读出结构。全部字段可空——缺失即缺失。
type TrackMetadata struct {
	TrackID          int64
	Title            *string
	Album            *string
	TrackCredit      *string
	AlbumCredit      *string
	CreditSpansJSON  *string
	PrimaryType      *string
	SecondaryTypes   *string
	ReleaseStatus    *string
	MediumFormat     *string
	Country          *string
	Year             *int
	ReleaseDate      *string
	FirstReleaseDate *string
	TrackNumber      *int
	TrackNumberText  *string
	TrackTotal       *int
	DiscNumber       *int
	DiscTotal        *int
	DurationMS       *int
	Genre            *string
	Label            *string
	CatalogNumber    *string
	Comment          *string
	CoverPath        *string
	CoverSource      *string
	CoverSourceURL   *string
	CoverWidth       *int
	CoverHeight      *int
	RecordingMBID    *string
	ReleaseMBID      *string
	ReleaseGroupMBID *string
	WorkMBID         *string
	ISRC             *string
	ITunesTrackID    *string
	ITunesCollection *string
	MatchSource      *string
	MatchRef         *string
	MatchedAt        *time.Time
	ProvenanceJSON   *string
	LockedJSON       *string
}
