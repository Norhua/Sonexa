package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

func TestSnapshotModelSchema(t *testing.T) {
	s := openTestStore(t)
	for _, table := range []string{"release_group_current", "release_group_ref", "release_group_snapshot", "release_current", "release_snapshot", "track_current", "track_snapshot", "track_identifier", "lyrics_file", "group_change_proposal", "release_membership_decision", "operation_result"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil || n != 1 {
			t.Fatalf("missing %s: %v", table, err)
		}
	}
	for _, table := range []string{"track", "release", "release_group"} {
		var n int
		if err := s.db.QueryRow("SELECT count(*) FROM pragma_table_info(?) WHERE name IN ('title','credit','locked_json','provenance_json')", table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("mixed current fields on %s: %v", table, err)
		}
	}
	for _, query := range []string{
		`INSERT INTO release_group(id,created_at,updated_at) VALUES(1,datetime('now'),datetime('now'))`,
		`INSERT INTO release_group_current(group_id) VALUES(1)`,
		`INSERT INTO release_group_ref VALUES(1,'musicbrainz','group-a'),(1,'qq','group-b')`,
		`INSERT INTO release(id,release_group_id,provider,provider_ref,created_at,updated_at) VALUES(1,1,'musicbrainz','a',datetime('now'),datetime('now')),(2,1,'musicbrainz','b',datetime('now'),datetime('now'))`,
		`INSERT INTO release_current(release_id) VALUES(1),(2)`,
		`INSERT INTO track(id,created_at,updated_at) VALUES(1,datetime('now'),datetime('now'))`,
		`INSERT INTO track_current(track_id) VALUES(1)`,
	} {
		if _, err := s.db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	var release *int64
	var title *string
	if err := s.db.QueryRow("SELECT release_id,title FROM track t JOIN track_current c ON c.track_id=t.id WHERE t.id=1").Scan(&release, &title); err != nil || release != nil || title != nil {
		t.Fatalf("missing values not null: %v", err)
	}
	for _, query := range []string{
		`INSERT INTO release_group_ref VALUES(1,'musicbrainz','group-a')`,
		`INSERT INTO release(release_group_id,provider,provider_ref,created_at,updated_at) VALUES(1,'musicbrainz','a',datetime('now'),datetime('now'))`,
		`INSERT INTO release(provider,provider_ref,created_at,updated_at) VALUES('qq','a',datetime('now'),datetime('now'))`,
		`INSERT INTO track(match_source,created_at,updated_at) VALUES('qq',datetime('now'),datetime('now'))`,
	} {
		if _, err := s.db.Exec(query); err == nil {
			t.Fatalf("accepted inconsistent identity: %s", query)
		}
	}
}

func TestOldModelRejectedWithoutDeletingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE track(id INTEGER PRIMARY KEY,title TEXT); INSERT INTO track VALUES(1,'旧收藏'); CREATE TABLE schema_version(version INTEGER,name TEXT,applied_at TEXT); INSERT INTO schema_version VALUES(1,'init','2026-01-01')"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); !errors.Is(err, ErrIncompatibleModel) {
		t.Fatalf("expected incompatible model, got %v", err)
	}
	var title string
	if err := db.QueryRow("SELECT title FROM track WHERE id=1").Scan(&title); err != nil || title != "旧收藏" {
		t.Fatal("legacy data changed", err)
	}
	db.Close()
}
