package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// NewTrackInput 是创建曲目的输入。
type NewTrackInput struct {
	Title string
	// Credit 是曲目级署名原串。
	Credit *string
	// Album 是歌集名。非空时曲目会被挂到一条歌集上——
	// 有上游标识时用它 upsert，否则按 (歌集名, 歌集署名) 归拢一条本地歌集。
	Album *string
	// AlbumCredit 是歌集级署名。
	AlbumCredit *string

	// 溯源：来自上游结果时填入；「无需匹配」入库时留空。
	MatchSource *string
	MatchRef    *string

	// Metadata 是曲目自己的丰富信息（可选，整体可为空）。
	Metadata *MetadataInput
	// Release 是歌集侧的信息（可选）。给出时与 Album 一起决定曲目的归属。
	Release *ReleaseInput
}

// MetadataInput 是曲目级丰富信息的写入输入。全部字段可空。
//
// 发行级字段（类型、载体、国家、厂牌、目录号、碟数、音轨数、封面）
// 不在这里——它们归 ReleaseInput，只存在歌集上一份。
type MetadataInput struct {
	DiscNumber      *int
	TrackNumber     *int
	TrackNumberText *string
	DurationMS      *int
	Genre           *string
	Comment         *string

	RecordingRef *string
	WorkRef      *string
	ISRC         *string

	ProvenanceJSON *string
	LockedJSON     *string
}

// ReleaseInput 是歌集侧的写入输入。
type ReleaseInput struct {
	Provider         *string
	ProviderRef      *string
	ReleaseGroupRef  *string
	PrimaryType      *string
	SecondaryTypes   *string
	ReleaseStatus    *string
	MediumFormat     *string
	Country          *string
	ReleaseDate      *string
	FirstReleaseDate *string
	DiscTotal        *int
	TrackTotal       *int
	Label            *string
	CatalogNumber    *string
}

// CreateTrackWithProvenance 创建曲目、登记它的歌集并写入溯源。
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
		releaseID, err := resolveReleaseTx(tx, in)
		if err != nil {
			return err
		}

		now := time.Now().UTC()
		m := in.Metadata
		if m == nil {
			m = &MetadataInput{}
		}
		var matchedAt *time.Time
		if in.MatchSource != nil && in.MatchRef != nil {
			matchedAt = &now
		}

		res, err := tx.Exec(`
			INSERT INTO track (
				release_id, title, credit,
				disc_number, track_number, track_number_text, duration_ms, genre, comment,
				match_source, match_ref, matched_at, recording_ref, work_ref, isrc,
				enrich_state, provenance_json, locked_json, created_at, updated_at
			) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			nullableID(releaseID), in.Title, in.Credit,
			m.DiscNumber, m.TrackNumber, m.TrackNumberText, m.DurationMS, m.Genre, m.Comment,
			in.MatchSource, in.MatchRef, matchedAt, m.RecordingRef, m.WorkRef, m.ISRC,
			string(InitialEnrichState(in)), m.ProvenanceJSON, m.LockedJSON, now, now)
		if err != nil {
			return err
		}
		trackID, err = res.LastInsertId()
		if err != nil {
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

// resolveReleaseTx 决定新曲目挂在哪条歌集上。
//
// 有上游发行标识时 upsert 该歌集（并顺带登记发行组）；否则按歌集名归拢
// 一条本地歌集。歌集名也没有时返回 0——曲目可以没有歌集。
func resolveReleaseTx(tx *sql.Tx, in NewTrackInput) (int64, error) {
	rel := in.Release
	hasRef := rel != nil && rel.ProviderRef != nil && *rel.ProviderRef != ""
	if !hasRef {
		if in.Album == nil || strings.TrimSpace(*in.Album) == "" {
			return 0, nil
		}
		return findOrCreateLocalReleaseTx(tx, *in.Album, in.AlbumCredit)
	}

	var groupID *int64
	if rel.ReleaseGroupRef != nil && *rel.ReleaseGroupRef != "" {
		title := ""
		if in.Album != nil {
			title = *in.Album
		}
		id, err := upsertReleaseGroupTx(tx, &ReleaseGroup{
			Provider: rel.Provider, ProviderRef: rel.ReleaseGroupRef, Title: title,
			PrimaryType: normalizeTypePtr(rel.PrimaryType), SecondaryTypes: rel.SecondaryTypes,
			FirstReleaseDate: rel.FirstReleaseDate,
		})
		if err != nil {
			return 0, err
		}
		groupID = &id
	}

	title := ""
	if in.Album != nil {
		title = *in.Album
	}
	return upsertReleaseTx(tx, &Release{
		ReleaseGroupID:   groupID,
		Provider:         rel.Provider,
		ProviderRef:      rel.ProviderRef,
		Title:            title,
		Credit:           in.AlbumCredit,
		PrimaryType:      normalizeTypePtr(rel.PrimaryType),
		SecondaryTypes:   rel.SecondaryTypes,
		DiscTotal:        rel.DiscTotal,
		TrackTotal:       rel.TrackTotal,
		ReleaseStatus:    rel.ReleaseStatus,
		MediumFormat:     rel.MediumFormat,
		Country:          rel.Country,
		ReleaseDate:      rel.ReleaseDate,
		FirstReleaseDate: rel.FirstReleaseDate,
		Label:            rel.Label,
		CatalogNumber:    rel.CatalogNumber,
		TracklistState:   TracklistNone,
	})
}

func normalizeTypePtr(s *string) *string {
	if s == nil {
		return nil
	}
	t := NormalizeReleaseType(*s)
	if t == "" {
		return nil
	}
	return &t
}

func nullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
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

// TrackMetadata 是曲目详情的读出结构。全部可空字段用指针——缺失即缺失。
//
// 歌集侧的字段由 join 带出，便于详情抽屉一次取齐；它们不存在 track 表上。
type TrackMetadata struct {
	TrackID int64   `json:"trackId"`
	Title   string  `json:"title"`
	Credit  *string `json:"credit"`

	DiscNumber      *int    `json:"discNumber"`
	TrackNumber     *int    `json:"trackNumber"`
	TrackNumberText *string `json:"trackNumberText"`
	DurationMS      *int    `json:"durationMs"`
	Genre           *string `json:"genre"`
	Comment         *string `json:"comment"`

	MatchSource  *string `json:"matchSource"`
	MatchRef     *string `json:"matchRef"`
	RecordingRef *string `json:"recordingRef"`
	WorkRef      *string `json:"workRef"`
	ISRC         *string `json:"isrc"`

	ProvenanceJSON *string `json:"provenanceJson"`
	LockedJSON     *string `json:"lockedJson"`

	// 歌集侧（join 带出）
	ReleaseID        *int64  `json:"releaseId"`
	Album            *string `json:"album"`
	AlbumCredit      *string `json:"albumCredit"`
	PrimaryType      *string `json:"primaryType"`
	SecondaryTypes   *string `json:"secondaryTypes"`
	ReleaseStatus    *string `json:"releaseStatus"`
	MediumFormat     *string `json:"mediumFormat"`
	Country          *string `json:"country"`
	ReleaseDate      *string `json:"releaseDate"`
	FirstReleaseDate *string `json:"firstReleaseDate"`
	Year             *int    `json:"year"`
	DiscTotal        *int    `json:"discTotal"`
	TrackTotal       *int    `json:"trackTotal"`
	Label            *string `json:"label"`
	CatalogNumber    *string `json:"catalogNumber"`
	CoverPath        *string `json:"coverPath"`
	CoverSourceURL   *string `json:"coverSourceUrl"`
	ReleaseRef       *string `json:"releaseRef"`
	ReleaseGroupRef  *string `json:"releaseGroupRef"`
}

// GetMetadata 读取某曲目的详情；不存在时返回 nil。
func (s *Store) GetMetadata(trackID int64) (*TrackMetadata, error) {
	var m TrackMetadata
	var releaseDate, firstReleaseDate *string
	err := s.db.QueryRow(`
		SELECT t.id, t.title, t.credit,
		       t.disc_number, t.track_number, t.track_number_text, t.duration_ms,
		       t.genre, t.comment,
		       t.match_source, t.match_ref, t.recording_ref, t.work_ref, t.isrc,
		       t.provenance_json, t.locked_json,
		       t.release_id, r.title, r.credit, r.primary_type, r.secondary_types,
		       r.release_status, r.medium_format, r.country,
		       r.release_date, r.first_release_date,
		       r.disc_total, r.track_total, r.label, r.catalog_number,
		       r.cover_path, r.cover_source_url, r.provider_ref,
		       (SELECT rg.provider_ref FROM release_group rg WHERE rg.id = r.release_group_id)
		FROM track t LEFT JOIN release r ON r.id = t.release_id
		WHERE t.id = ?`, trackID).Scan(
		&m.TrackID, &m.Title, &m.Credit,
		&m.DiscNumber, &m.TrackNumber, &m.TrackNumberText, &m.DurationMS,
		&m.Genre, &m.Comment,
		&m.MatchSource, &m.MatchRef, &m.RecordingRef, &m.WorkRef, &m.ISRC,
		&m.ProvenanceJSON, &m.LockedJSON,
		&m.ReleaseID, &m.Album, &m.AlbumCredit, &m.PrimaryType, &m.SecondaryTypes,
		&m.ReleaseStatus, &m.MediumFormat, &m.Country,
		&releaseDate, &firstReleaseDate,
		&m.DiscTotal, &m.TrackTotal, &m.Label, &m.CatalogNumber,
		&m.CoverPath, &m.CoverSourceURL, &m.ReleaseRef, &m.ReleaseGroupRef)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.ReleaseDate, m.FirstReleaseDate = releaseDate, firstReleaseDate
	m.Year = (&Release{ReleaseDate: releaseDate, FirstReleaseDate: firstReleaseDate}).Year()
	return &m, nil
}

// trackEditColumns 是编辑界面可写入的曲目字段。
//
// 发行级字段不在这里——改它们走 UpdateReleaseFields，因为一处改动
// 对该歌集的全部曲目生效。
var trackEditColumns = map[string]string{
	"title":       "title",
	"credit":      "credit",
	"trackNumber": "track_number",
	"discNumber":  "disc_number",
	"durationMs":  "duration_ms",
	"genre":       "genre",
	"comment":     "comment",
}

// ApplyMetadataEdits 逐字段写入曲目信息，并更新逐字段来源与用户锁。
//
// 来源取值为 provider id 或 user（用户手动修改）。来源为 user 的字段
// 不被上游结果自动覆盖——该判定在调用方完成。
func (s *Store) ApplyMetadataEdits(trackID int64, edits map[string]string,
	provenance map[string]string, locked map[string]bool) error {
	if len(edits) == 0 && len(provenance) == 0 && len(locked) == 0 {
		return nil
	}

	return s.withTx(func(tx *sql.Tx) error {
		sets := []string{}
		args := []any{}
		for key, value := range edits {
			column, ok := trackEditColumns[key]
			if !ok {
				continue
			}
			if column == "title" && strings.TrimSpace(value) == "" {
				return ErrTitleRequired
			}
			sets = append(sets, column+" = ?")
			if isIntColumn(column) {
				args = append(args, parseIntOrNil(value))
			} else {
				args = append(args, nullIfBlank(value))
			}
		}
		if len(provenance) > 0 || len(locked) > 0 {
			provJSON, lockedJSON, err := marshalProvLock(provenance, locked)
			if err != nil {
				return err
			}
			sets = append(sets, "provenance_json = ?", "locked_json = ?")
			args = append(args, provJSON, lockedJSON)
		}
		if len(sets) == 0 {
			return nil
		}
		sets = append(sets, "updated_at = ?")
		args = append(args, time.Now().UTC(), trackID)

		if _, err := tx.Exec(
			"UPDATE track SET "+strings.Join(sets, ", ")+" WHERE id = ?", args...); err != nil {
			return err
		}
		return syncFTS(tx, trackID)
	})
}

func marshalProvLock(provenance map[string]string, locked map[string]bool) (string, string, error) {
	provJSON, err := json.Marshal(provenance)
	if err != nil {
		return "", "", err
	}
	lockedJSON, err := json.Marshal(locked)
	if err != nil {
		return "", "", err
	}
	return string(provJSON), string(lockedJSON), nil
}

func isIntColumn(column string) bool {
	switch column {
	case "track_number", "disc_number", "duration_ms", "track_total", "disc_total":
		return true
	}
	return false
}

func parseIntOrNil(s string) any {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return n
}

func nullIfBlank(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
