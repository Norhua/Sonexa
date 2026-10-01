package store

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
