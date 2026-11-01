package store

import (
	"database/sql"
	"errors"
)

// CandidateCoverTarget 在同一只读语句中按候选自己的来源发行取得封面目标与修订。
// 不由相关 track 推导发行；未命中与查询失败分别返回。
func (s *Store) CandidateCoverTarget(source, ref string) (int64, int64, error) {
	if source == "" || ref == "" {
		return 0, 0, nil
	}
	var id, revision int64
	err := s.db.QueryRow(`SELECT r.id,COALESCE(c.revision,0) FROM release r
	 LEFT JOIN release_cover_selection c ON c.release_id=r.id
	 WHERE r.provider=? AND r.provider_ref=?`, source, ref).Scan(&id, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return id, revision, err
}

func (s *Store) CoverCatalogKeyForImage(imageID int64) (CoverCatalogKey, error) {
	var key CoverCatalogKey
	err := s.db.QueryRow(`SELECT c.source,c.object_type,c.source_ref FROM cover_catalog c
		JOIN cover_image i ON i.catalog_id=c.id WHERE i.id=?`, imageID).Scan(&key.Source, &key.ObjectType, &key.SourceRef)
	return key, err
}

func (s *Store) DeleteUnprotectedCoverFile(id int64) error {
	_, err := s.db.Exec(`DELETE FROM cover_file WHERE id=? AND protected=0`, id)
	return err
}
