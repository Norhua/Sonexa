package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// TracklistState 是发行清单的状态。
type TracklistState string

const (
	TracklistNone     TracklistState = "none"     // 无清单
	TracklistPartial  TracklistState = "partial"  // 部分
	TracklistComplete TracklistState = "complete" // 完整
	TracklistFailed   TracklistState = "failed"   // 获取失败
)

// 歌集类型的取值域。对齐 MusicBrainz 的 release-group 主类型。
const (
	ReleaseTypeAlbum     = "album"
	ReleaseTypeEP        = "ep"
	ReleaseTypeSingle    = "single"
	ReleaseTypeBroadcast = "broadcast"
	ReleaseTypeOther     = "other"
)

// NormalizeReleaseType 把上游给的类型归一到取值域；无法归一时返回 other。
//
// 空串返回空串——「上游没说」与「上游说了 Other」是两件事，
// 前者应当呈现为缺失，后者是一个确定的值。
func NormalizeReleaseType(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return ""
	}
	switch t {
	case ReleaseTypeAlbum, ReleaseTypeEP, ReleaseTypeSingle, ReleaseTypeBroadcast, ReleaseTypeOther:
		return t
	}
	return ReleaseTypeOther
}

// ReleaseGroup 是发行组：同一专辑的不同版本合为一条。
type ReleaseGroup struct {
	ID               int64   `json:"id"`
	Provider         *string `json:"provider"`
	ProviderRef      *string `json:"providerRef"`
	Title            string  `json:"title"`
	PrimaryType      *string `json:"primaryType"`
	SecondaryTypes   *string `json:"secondaryTypes"`
	FirstReleaseDate *string `json:"firstReleaseDate"`
	// ReleaseCount 是该组下共有几个 release，取自上游 browse，可手动改。
	ReleaseCount *int `json:"releaseCount"`
}

// Release 是歌集。
type Release struct {
	ID             int64   `json:"id"`
	ReleaseGroupID *int64  `json:"releaseGroupId"`
	Provider       *string `json:"provider"`
	ProviderRef    *string `json:"providerRef"`

	Title       string  `json:"title"`
	Credit      *string `json:"credit"`
	PrimaryType *string `json:"primaryType"`
	// SecondaryTypes 是 JSON 数组串。
	SecondaryTypes *string `json:"secondaryTypes"`
	DiscTotal      *int    `json:"discTotal"`
	TrackTotal     *int    `json:"trackTotal"`

	CoverPath      *string `json:"coverPath"`
	CoverSourceURL *string `json:"coverSourceUrl"`
	CoverWidth     *int    `json:"coverWidth"`
	CoverHeight    *int    `json:"coverHeight"`

	ReleaseStatus    *string `json:"releaseStatus"`
	MediumFormat     *string `json:"mediumFormat"`
	Country          *string `json:"country"`
	ReleaseDate      *string `json:"releaseDate"`
	FirstReleaseDate *string `json:"firstReleaseDate"`
	Label            *string `json:"label"`
	CatalogNumber    *string `json:"catalogNumber"`

	TracklistState TracklistState `json:"tracklistState"`
	// FetchedAt 不导出给前端：绑定层导出的结构体不得出现 time.Time，
	// 需要时间就在绑定层转成 RFC3339 字符串。
	FetchedAt *time.Time `json:"-"`

	// 逐字段来源与用户锁：歌集级字段的来源与锁落在这里，
	// 与曲目级的那一份同构（整体读写，新增字段无需改动存储形状）。
	ProvenanceJSON *string `json:"provenanceJson"`
	LockedJSON     *string `json:"lockedJson"`

	// ReleaseGroupRef 是便利字段，由 join 带出，不对应本表的列。
	ReleaseGroupRef *string `json:"releaseGroupRef"`
}

// Year 返回歌集年份：取发行日期的前四位。
//
// 仅返回具体发行日期的年份；曲目的独立当前年份由同步规则决定。
func (r *Release) Year() *int {
	for _, d := range []*string{r.ReleaseDate} {
		if d == nil || len(*d) < 4 {
			continue
		}
		var y int
		if _, err := fmt.Sscanf((*d)[:4], "%d", &y); err == nil && y > 0 {
			return &y
		}
	}
	return nil
}

// ReleaseTrack 是清单中的一个位置。
type ReleaseTrack struct {
	DiscNumber      int     `json:"discNumber"`
	TrackNumber     int     `json:"trackNumber"`
	TrackRef        *string `json:"trackRef"`
	RecordingRef    *string `json:"recordingRef"`
	Title           string  `json:"title"`
	TrackCredit     *string `json:"trackCredit"`
	DurationMS      *int    `json:"durationMs"`
	TrackNumberText *string `json:"trackNumberText"`
}

const releaseColumns = `
	r.id, r.release_group_id, r.provider, r.provider_ref,
	r.title, r.credit, r.primary_type, r.secondary_types, r.disc_total, r.track_total,
	r.cover_path, r.cover_source_url, r.cover_width, r.cover_height,
	r.release_status, r.medium_format, r.country, r.release_date, r.first_release_date,
	r.label, r.catalog_number, r.tracklist_state, r.fetched_at,
	r.provenance_json, r.locked_json,
	(SELECT rg.provider_ref FROM release_group_read rg WHERE rg.id = r.release_group_id)`

func scanRelease(sc interface{ Scan(...any) error }) (*Release, error) {
	var r Release
	var state string
	err := sc.Scan(&r.ID, &r.ReleaseGroupID, &r.Provider, &r.ProviderRef,
		&r.Title, &r.Credit, &r.PrimaryType, &r.SecondaryTypes, &r.DiscTotal, &r.TrackTotal,
		&r.CoverPath, &r.CoverSourceURL, &r.CoverWidth, &r.CoverHeight,
		&r.ReleaseStatus, &r.MediumFormat, &r.Country, &r.ReleaseDate, &r.FirstReleaseDate,
		&r.Label, &r.CatalogNumber, &state, &r.FetchedAt,
		&r.ProvenanceJSON, &r.LockedJSON, &r.ReleaseGroupRef)
	if err != nil {
		return nil, err
	}
	r.TracklistState = TracklistState(state)
	return &r, nil
}

// UpsertReleaseGroup 按 (provider, provider_ref) 写入或更新发行组。
func (s *Store) UpsertReleaseGroup(g *ReleaseGroup) (int64, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		id, err = upsertReleaseGroupTx(tx, g)
		return err
	})
	return id, err
}

func upsertReleaseGroupTx(tx *sql.Tx, g *ReleaseGroup) (int64, error) {
	return writeLegacyGroupTx(tx, g)
}

func countFetchedAt(count *int, now time.Time) *time.Time {
	if count == nil {
		return nil
	}
	return &now
}

// SetReleaseGroupCount 记录某发行组下的 release 数。
func (s *Store) SetReleaseGroupCount(groupID int64, count int) error {
	return s.withTx(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE release_group_snapshot SET release_count=?,revision=revision+1 WHERE group_id=?", count, groupID)
		return err
	})
}

// GetReleaseGroup 按 id 读取发行组。
func (s *Store) GetReleaseGroup(id int64) (*ReleaseGroup, error) {
	var g ReleaseGroup
	err := s.db.QueryRow(`
		SELECT id, provider, provider_ref, title, primary_type, secondary_types,
		       first_release_date, release_count
		FROM release_group_read WHERE id = ?`, id).Scan(
		&g.ID, &g.Provider, &g.ProviderRef, &g.Title, &g.PrimaryType,
		&g.SecondaryTypes, &g.FirstReleaseDate, &g.ReleaseCount)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// UpsertRelease 按 (provider, provider_ref) 写入或更新歌集，返回其 id。
//
// 没有上游标识时不做 upsert——本地歌集靠 FindOrCreateLocalRelease 按
// (标题, 署名) 归拢，那是另一套判定。
func (s *Store) UpsertRelease(r *Release) (int64, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		id, err = upsertReleaseTx(tx, r)
		return err
	})
	return id, err
}

func upsertReleaseTx(tx *sql.Tx, r *Release) (int64, error) { return writeLegacyReleaseTx(tx, r) }

func stateOrNone(s TracklistState) string {
	if s == "" {
		return string(TracklistNone)
	}
	return string(s)
}

func createLocalReleaseTx(tx *sql.Tx, r *Release) (int64, error) { return writeLegacyReleaseTx(tx, r) }

// FindOrCreateLocalRelease 按 (标题, 署名) 找到或创建一条本地歌集。
//
// 没有上游标识的曲目也要有归属：此前它们靠「标准化专辑名 + 署名」在查询时
// 现场派生分组，于是专辑视图要为这一种单独走一条代码路径，而歌集的字段
// （碟数、封面、类型）对它们根本无处可放。给一条真正的 release 行之后，
// 两者合流——专辑视图只有一种取数方式。
func (s *Store) FindOrCreateLocalRelease(title string, credit *string) (int64, error) {
	if strings.TrimSpace(title) == "" {
		return 0, nil
	}
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		id, err = findOrCreateLocalReleaseTx(tx, title, credit)
		return err
	})
	return id, err
}

func findOrCreateLocalReleaseTx(tx *sql.Tx, title string, credit *string) (int64, error) {
	if strings.TrimSpace(title) == "" {
		return 0, nil
	}
	var id int64
	err := tx.QueryRow(`
		SELECT id FROM release_read
		WHERE provider_ref IS NULL
		  AND LOWER(title) = LOWER(?)
		  AND LOWER(COALESCE(credit,'')) = LOWER(COALESCE(?,''))
		LIMIT 1`, title, credit).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	return createLocalReleaseTx(tx, &Release{Title: title, Credit: credit})
}

// ReplaceTracklist 以整份清单替换某歌集的既有清单，并更新状态与获取时间。
func (s *Store) ReplaceTracklist(releaseID int64, tracks []ReleaseTrack, state TracklistState) error {
	return s.withTx(func(tx *sql.Tx) error {
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
		now := time.Now().UTC()
		_, err = tx.Exec(
			`UPDATE release SET tracklist_state = ?, fetched_at = ?, updated_at = ? WHERE id = ?`,
			string(state), now, now, releaseID)
		return err
	})
}

// GetRelease 按 id 读取歌集。
func (s *Store) GetRelease(id int64) (*Release, error) {
	r, err := scanRelease(s.db.QueryRow(
		"SELECT "+releaseColumns+" FROM release_read r WHERE r.id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// GetReleaseByRef 按 (provider, provider_ref) 读取歌集。
func (s *Store) GetReleaseByRef(provider, ref string) (*Release, error) {
	r, err := scanRelease(s.db.QueryRow(
		"SELECT "+releaseColumns+" FROM release_read r WHERE r.provider = ? AND r.provider_ref = ?",
		provider, ref))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// GetTracklist 读取某歌集的清单，按碟号与音轨号升序。
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

// releaseEditColumns 是编辑界面可写入的歌集字段。
var releaseEditColumns = map[string]string{
	"title":         "title",
	"credit":        "credit",
	"primaryType":   "primary_type",
	"discTotal":     "disc_total",
	"trackTotal":    "track_total",
	"coverPath":     "cover_path",
	"label":         "label",
	"catalogNumber": "catalog_number",
	"mediumFormat":  "medium_format",
	"country":       "country",
	"releaseDate":   "release_date",
}

// UpdateReleaseFields 逐字段写入歌集，并更新逐字段来源与用户锁。
func (s *Store) UpdateReleaseFields(releaseID int64, edits map[string]string, provenance map[string]string, locked map[string]bool) error {
	return s.editLegacyFields("release", releaseID, edits, provenance, locked)
}

// DeleteOrphanLocalRelease 删除不再被任何曲目引用的本地歌集。
//
// 只删本地的：上游来源的歌集同时是清单缓存，曲目删光后仍应保留，
// 这一条 library/release-catalog 已有规定。
func (s *Store) DeleteOrphanLocalRelease(releaseID int64) error {
	return s.withTx(func(tx *sql.Tx) error { return deleteOrphanLocalReleaseTx(tx, releaseID) })
}

func deleteOrphanLocalReleaseTx(tx *sql.Tx, releaseID int64) error {
	var groupID int64
	if err := tx.QueryRow("SELECT release_group_id FROM release WHERE id=?", releaseID).Scan(&groupID); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if _, err := tx.Exec("DELETE FROM release WHERE id=? AND provider_ref IS NULL AND NOT EXISTS (SELECT 1 FROM track WHERE release_id=?)", releaseID, releaseID); err != nil {
		return err
	}
	_, err := tx.Exec("DELETE FROM release_group WHERE id=? AND NOT EXISTS (SELECT 1 FROM release WHERE release_group_id=?) AND NOT EXISTS (SELECT 1 FROM release_group_ref WHERE group_id=?)", groupID, groupID, groupID)
	return err
}
