package store

import "time"

type LegacyCover struct {
	ReleaseID int64
	Path      string
	SourceURL string
	FetchedAt *time.Time
}

func (s *Store) PendingLegacyCovers() ([]LegacyCover, error) {
	rows, err := s.db.Query(`SELECT m.release_id,m.legacy_path,COALESCE(r.cover_source_url,''),r.cover_fetched_at
		FROM cover_legacy_migration m JOIN release r ON r.id=m.release_id WHERE m.state<>'complete' ORDER BY m.release_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LegacyCover{}
	for row := rows.Next(); row; row = rows.Next() {
		var item LegacyCover
		if err := rows.Scan(&item.ReleaseID, &item.Path, &item.SourceURL, &item.FetchedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) SetLegacyCoverMigration(releaseID int64, complete bool, message string) error {
	state := "error"
	if complete {
		state = "complete"
	}
	_, err := s.db.Exec(`UPDATE cover_legacy_migration SET state=?,last_error=? WHERE release_id=?`, state, message, releaseID)
	return err
}

// CatalogsForRelease 只按已绑定发行身份复用目录，既不按名称匹配，也不改变收藏身份。
func (s *Store) CatalogsForRelease(releaseID int64) ([]CoverCatalogKey, error) {
	release, err := s.GetRelease(releaseID)
	if err != nil || release == nil {
		return nil, err
	}
	if release.Provider == nil || release.ProviderRef == nil || *release.ProviderRef == "" {
		return nil, nil
	}
	switch *release.Provider {
	case "musicbrainz":
		keys := []CoverCatalogKey{{Source: "caa", ObjectType: "release", SourceRef: *release.ProviderRef}}
		if release.ReleaseGroupRef != nil && *release.ReleaseGroupRef != "" {
			keys = append(keys, CoverCatalogKey{Source: "caa", ObjectType: "release-group", SourceRef: *release.ReleaseGroupRef})
		}
		return keys, nil
	case "itunes":
		return []CoverCatalogKey{{Source: "itunes", ObjectType: "album", SourceRef: *release.ProviderRef}}, nil
	default:
		return nil, nil
	}
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
