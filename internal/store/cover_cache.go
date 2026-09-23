package store

import "time"

// MarkAutoCoverPreview 仅由自动选图成功路径调用；手动候选预览不能改变自动封面。
func (s *Store) MarkAutoCoverPreview(variantID int64) error {
	_, err := s.db.Exec(`UPDATE cover_file SET auto_reusable=1 WHERE variant_id=? AND release_id IS NULL`, variantID)
	return err
}

func (s *Store) AutoPreviewFiles(catalogID int64) ([]CoverFile, error) {
	return s.queryCoverFiles(`SELECT `+coverFileColumns+` FROM cover_file WHERE release_id IS NULL AND auto_reusable=1
		AND variant_id IN (SELECT v.id FROM cover_variant v JOIN cover_image i ON i.id=v.image_id WHERE i.catalog_id=?) ORDER BY id`, catalogID)
}

func (s *Store) AllCoverFiles() ([]CoverFile, error) {
	return s.queryCoverFiles(`SELECT ` + coverFileColumns + ` FROM cover_file ORDER BY last_used_at,id`)
}

func (s *Store) TouchCoverFile(id int64) error {
	_, err := s.db.Exec(`UPDATE cover_file SET last_used_at=? WHERE id=?`, time.Now().UTC(), id)
	return err
}

func (s *Store) SetCoverCatalogRelease(id int64, releaseURL string) error {
	_, err := s.db.Exec(`UPDATE cover_catalog SET actual_release_url=? WHERE id=?`, releaseURL, id)
	return err
}

func (s *Store) IsLegacyCoverPath(path string) (bool, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM cover_legacy_migration WHERE legacy_path=?`, path).Scan(&count)
	return count > 0, err
}
