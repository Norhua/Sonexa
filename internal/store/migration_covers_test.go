package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

func openV1CoverStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v1.db")
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=foreign_keys(ON)", path))
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{db: db}
	t.Cleanup(func() { s.Close() })
	if _, err := currentVersion(db); err != nil {
		t.Fatal(err)
	}
	if err := s.applyMigration(Migration{Version: 1, Name: "init", SQL: migration0001Init}); err != nil {
		t.Fatal(err)
	}
	return s, path
}

func TestCoverMigrationFailureRollsBackAllTables(t *testing.T) {
	s, _ := openV1CoverStore(t)
	if err := s.applyMigration(Migration{Version: 2, Name: "broken_cover", SQL: migration0002CoverCatalog + " INVALID SQL;"}); err == nil {
		t.Fatal("故障迁移应失败")
	}
	version, err := s.SchemaVersion()
	if err != nil || version != 1 {
		t.Fatalf("版本提前前进: %d %v", version, err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name LIKE 'cover_%' OR name = 'release_cover_selection'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("表未整体回滚: %d %v", n, err)
	}
	if err := s.Migrate(); err != nil {
		t.Fatalf("失败后不能重试: %v", err)
	}
}

func TestCoverSchemaIdentityConstraints(t *testing.T) {
	s := openTestStore(t)
	exec := func(query string) {
		t.Helper()
		if _, err := s.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO cover_catalog(id,source,object_type,source_ref) VALUES(1,'caa','release','r'),(2,'caa','release-group','r')`)
	exec(`INSERT INTO cover_image(id,catalog_id,source_image_id) VALUES(1,1,'front'),(2,1,'back'),(3,2,'front')`)
	exec(`INSERT INTO cover_variant(id,image_id,spec) VALUES(1,1,'250'),(2,1,'1200'),(3,2,'250')`)
	exec(`INSERT INTO release_group(id,created_at,updated_at) VALUES(1,datetime('now'),datetime('now'))`)
	exec(`INSERT INTO release(id,release_group_id,created_at,updated_at) VALUES(1,1,datetime('now'),datetime('now'))`)
	exec(`INSERT INTO release_cover_selection VALUES(1,1,1,1,datetime('now'))`)
	for _, query := range []string{
		`INSERT INTO cover_catalog(source,object_type,source_ref) VALUES('caa','release','r')`,
		`INSERT INTO cover_image(catalog_id,source_image_id) VALUES(1,'front')`,
		`INSERT INTO cover_variant(image_id,spec) VALUES(1,'250')`,
		`UPDATE cover_catalog SET state='failed' WHERE id=1`,
		`UPDATE release_cover_selection SET variant_id=3 WHERE release_id=1`,
		`INSERT INTO release_cover_selection VALUES(1,1,2,2,datetime('now'))`,
	} {
		if _, err := s.db.Exec(query); err == nil {
			t.Errorf("应拒绝违反身份约束: %s", query)
		}
	}
}
