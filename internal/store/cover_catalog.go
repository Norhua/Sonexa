package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// CoverCatalogKey 区分实际图片来源及其发行身份，不使用歌曲匹配来源代替图片来源。
type CoverCatalogKey struct {
	Source     string `json:"source"`
	ObjectType string `json:"objectType"`
	SourceRef  string `json:"sourceRef"`
}

type CoverCatalog struct {
	ID int64 `json:"id"`
	CoverCatalogKey
	State      string       `json:"state"`
	CheckedAt  *time.Time   `json:"-"`
	LastError  string       `json:"lastError"`
	RetryAfter *time.Time   `json:"-"`
	Version    int64        `json:"version"`
	Images     []CoverImage `json:"images"`
}

type CoverImage struct {
	ID            int64          `json:"id"`
	CatalogID     int64          `json:"catalogId"`
	SourceImageID string         `json:"sourceImageId"`
	Types         []string       `json:"types"`
	PrimaryType   string         `json:"primaryType"`
	Front         bool           `json:"front"`
	Back          bool           `json:"back"`
	Position      int            `json:"position"`
	OriginalURL   string         `json:"originalUrl"`
	Title         string         `json:"title"`
	Artist        string         `json:"artist"`
	Active        bool           `json:"active"`
	Variants      []CoverVariant `json:"variants"`
}

type CoverVariant struct {
	ID          int64  `json:"id"`
	ImageID     int64  `json:"imageId"`
	Spec        string `json:"spec"`
	URL         string `json:"url"`
	Constructed bool   `json:"constructed"`
	Width       *int   `json:"width"`
	Height      *int   `json:"height"`
	Format      string `json:"format"`
	State       string `json:"state"`
	LastError   string `json:"lastError"`
	Active      bool   `json:"active"`
}

func ensureCoverCatalogTx(tx *sql.Tx, key CoverCatalogKey) (int64, error) {
	if strings.TrimSpace(key.ObjectType) == "" || strings.TrimSpace(key.SourceRef) == "" {
		return 0, fmt.Errorf("封面目录缺少来源身份")
	}
	if _, err := tx.Exec(`INSERT INTO cover_catalog(source,object_type,source_ref) VALUES(?,?,?)
		ON CONFLICT(source,object_type,source_ref) DO NOTHING`, key.Source, key.ObjectType, key.SourceRef); err != nil {
		return 0, err
	}
	var id int64
	err := tx.QueryRow(`SELECT id FROM cover_catalog WHERE source=? AND object_type=? AND source_ref=?`, key.Source, key.ObjectType, key.SourceRef).Scan(&id)
	return id, err
}

// SaveCoverCatalog 保存完整、已解析成功的目录。保留旧图片身份与已下载文件，
// 对移出目录的图片只标 inactive，以免刷新删除用户已经采用的图片。
func (s *Store) SaveCoverCatalog(key CoverCatalogKey, images []CoverImage) (int64, error) {
	var id int64
	err := s.withTx(func(tx *sql.Tx) error {
		var err error
		id, err = ensureCoverCatalogTx(tx, key)
		if err != nil {
			return err
		}
		state := "found"
		if len(images) == 0 {
			state = "empty"
		}
		if _, err = tx.Exec(`UPDATE cover_catalog SET state=?,checked_at=?,last_error='',retry_after=NULL,version=version+1 WHERE id=?`, state, time.Now().UTC(), id); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE cover_variant SET active=0 WHERE image_id IN (SELECT id FROM cover_image WHERE catalog_id=?)`, id); err != nil {
			return err
		}
		if _, err = tx.Exec(`UPDATE cover_image SET active=0 WHERE catalog_id=?`, id); err != nil {
			return err
		}
		seen := map[string]bool{}
		for position, image := range images {
			if image.SourceImageID == "" || seen[image.SourceImageID] {
				return fmt.Errorf("封面图片身份缺失或重复: %q", image.SourceImageID)
			}
			seen[image.SourceImageID] = true
			if len(image.Variants) == 0 {
				return fmt.Errorf("图片 %s 没有规格", image.SourceImageID)
			}
			if image.PrimaryType == "" {
				image.PrimaryType = "Other"
			}
			if image.Types == nil {
				image.Types = []string{}
			}
			types, err := json.Marshal(image.Types)
			if err != nil {
				return err
			}
			_, err = tx.Exec(`INSERT INTO cover_image(catalog_id,source_image_id,types_json,primary_type,is_front,is_back,position,original_url,title,artist)
				VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(catalog_id,source_image_id) DO UPDATE SET
				types_json=excluded.types_json,primary_type=excluded.primary_type,is_front=excluded.is_front,
				is_back=excluded.is_back,position=excluded.position,original_url=excluded.original_url,
				title=excluded.title,artist=excluded.artist,active=1`, id, image.SourceImageID, string(types), image.PrimaryType,
				image.Front, image.Back, position, image.OriginalURL, image.Title, image.Artist)
			if err != nil {
				return err
			}
			var imageID int64
			if err = tx.QueryRow(`SELECT id FROM cover_image WHERE catalog_id=? AND source_image_id=?`, id, image.SourceImageID).Scan(&imageID); err != nil {
				return err
			}
			seenSpecs := map[string]bool{}
			for _, v := range image.Variants {
				if v.Spec == "" || seenSpecs[v.Spec] {
					return fmt.Errorf("图片 %s 的规格缺失或重复: %q", image.SourceImageID, v.Spec)
				}
				seenSpecs[v.Spec] = true
				// 目录只声明规格，不证明实际像素。保留未变 URL 的验证结果；
				// 主动刷新允许重试之前 unavailable/error 的规格。
				_, err = tx.Exec(`INSERT INTO cover_variant(image_id,spec,url,constructed) VALUES(?,?,?,?)
					ON CONFLICT(image_id,spec) DO UPDATE SET url=excluded.url,constructed=excluded.constructed,
					width=CASE WHEN cover_variant.url=excluded.url THEN cover_variant.width END,
					height=CASE WHEN cover_variant.url=excluded.url THEN cover_variant.height END,
					format=CASE WHEN cover_variant.url=excluded.url THEN cover_variant.format ELSE '' END,
					state=CASE WHEN cover_variant.url=excluded.url AND cover_variant.state='verified' THEN 'verified' ELSE 'unverified' END,
					last_error='',active=1`, imageID, v.Spec, v.URL, v.Constructed)
				if err != nil {
					return err
				}
			}
		}
		return nil
	})
	return id, err
}

// RecordCoverCatalogError 不把网络故障变成“无图”，也不清空先前成功目录。
func (s *Store) RecordCoverCatalogError(key CoverCatalogKey, message string, retryAfter time.Time) error {
	return s.withTx(func(tx *sql.Tx) error {
		id, err := ensureCoverCatalogTx(tx, key)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE cover_catalog SET state=CASE WHEN state IN ('found','empty') THEN state ELSE 'error' END,
			last_error=?,retry_after=? WHERE id=?`, message, retryAfter.UTC(), id)
		return err
	})
}

func (s *Store) GetCoverCatalog(key CoverCatalogKey) (*CoverCatalog, error) {
	var catalog CoverCatalog
	catalog.CoverCatalogKey = key
	err := s.db.QueryRow(`SELECT id,state,checked_at,last_error,retry_after,version FROM cover_catalog
		WHERE source=? AND object_type=? AND source_ref=?`, key.Source, key.ObjectType, key.SourceRef).Scan(
		&catalog.ID, &catalog.State, &catalog.CheckedAt, &catalog.LastError, &catalog.RetryAfter, &catalog.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT id,catalog_id,source_image_id,types_json,primary_type,is_front,is_back,position,original_url,title,artist,active
		FROM cover_image WHERE catalog_id=? AND active=1 ORDER BY position,id`, catalog.ID)
	if err != nil {
		return nil, err
	}
	catalog.Images = []CoverImage{}
	for rows.Next() {
		image, err := scanCoverImage(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		catalog.Images = append(catalog.Images, *image)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range catalog.Images {
		catalog.Images[i].Variants, err = s.coverVariants(catalog.Images[i].ID, true)
		if err != nil {
			return nil, err
		}
	}
	return &catalog, nil
}

func scanCoverImage(row interface{ Scan(...any) error }) (*CoverImage, error) {
	var image CoverImage
	var types string
	err := row.Scan(&image.ID, &image.CatalogID, &image.SourceImageID, &types, &image.PrimaryType, &image.Front, &image.Back,
		&image.Position, &image.OriginalURL, &image.Title, &image.Artist, &image.Active)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(types), &image.Types); err != nil {
		return nil, err
	}
	return &image, nil
}

func (s *Store) GetCoverImage(id int64) (*CoverImage, error) {
	image, err := scanCoverImage(s.db.QueryRow(`SELECT id,catalog_id,source_image_id,types_json,primary_type,is_front,is_back,position,original_url,title,artist,active FROM cover_image WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	image.Variants, err = s.coverVariants(id, false)
	return image, err
}

func (s *Store) coverVariants(imageID int64, activeOnly bool) ([]CoverVariant, error) {
	query := `SELECT id,image_id,spec,url,constructed,width,height,format,state,last_error,active FROM cover_variant WHERE image_id=?`
	if activeOnly {
		query += ` AND active=1`
	}
	rows, err := s.db.Query(query+` ORDER BY id`, imageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CoverVariant{}
	for rows.Next() {
		v, err := scanCoverVariant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func scanCoverVariant(row interface{ Scan(...any) error }) (*CoverVariant, error) {
	var v CoverVariant
	err := row.Scan(&v.ID, &v.ImageID, &v.Spec, &v.URL, &v.Constructed, &v.Width, &v.Height, &v.Format, &v.State, &v.LastError, &v.Active)
	return &v, err
}

func (s *Store) GetCoverVariant(id int64) (*CoverVariant, error) {
	v, err := scanCoverVariant(s.db.QueryRow(`SELECT id,image_id,spec,url,constructed,width,height,format,state,last_error,active FROM cover_variant WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (s *Store) RecordCoverVariantError(id int64, unavailable bool, message string) error {
	state := "error"
	if unavailable {
		state = "unavailable"
	}
	result, err := s.db.Exec(`UPDATE cover_variant SET state=?,last_error=? WHERE id=?`, state, message, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
