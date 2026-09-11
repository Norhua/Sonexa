package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
)

// openTestStore 在临时目录中开一个真实的数据库文件（而非 :memory:），
// 因为 WAL 与并发行为需要文件型数据库才能如实测到。
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateAppliesToLatest(t *testing.T) {
	s := openTestStore(t)

	got, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if want := LatestVersion(); got != want {
		t.Errorf("schema 版本 = %d, 期望 %d", got, want)
	}
}

// TestMigrateIsIdempotent 覆盖「升级后启动」：重复打开不重复应用迁移。
func TestMigrateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")

	s1, err := Open(path)
	if err != nil {
		t.Fatalf("首次打开失败: %v", err)
	}
	s1.Close()

	s2, err := Open(path)
	if err != nil {
		t.Fatalf("再次打开失败: %v", err)
	}
	defer s2.Close()

	var n int
	if err := s2.DB().QueryRow(`SELECT COUNT(*) FROM schema_version`).Scan(&n); err != nil {
		t.Fatalf("统计迁移记录失败: %v", err)
	}
	if n != LatestVersion() {
		t.Errorf("迁移记录数 = %d, 期望 %d（迁移被重复应用）", n, LatestVersion())
	}
}

// TestFailedMigrationRollsBack 覆盖「迁移执行失败」：
// 该迁移的全部改动被回滚，且版本号保持不变。
func TestFailedMigrationRollsBack(t *testing.T) {
	s := openTestStore(t)
	before, _ := s.SchemaVersion()

	bad := Migration{
		Version: 9999,
		Name:    "bad",
		SQL: `CREATE TABLE should_not_survive (id INTEGER PRIMARY KEY);
		      THIS IS NOT VALID SQL;`,
	}
	if err := s.applyMigration(bad); err == nil {
		t.Fatal("期望迁移失败，实际成功")
	}

	after, err := s.SchemaVersion()
	if err != nil {
		t.Fatalf("读取版本失败: %v", err)
	}
	if after != before {
		t.Errorf("版本号 = %d, 期望保持 %d", after, before)
	}

	// 迁移内先执行的 CREATE TABLE 也必须一并回滚
	var name string
	err = s.DB().QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='should_not_survive'`,
	).Scan(&name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("失败迁移建的表未被回滚（err=%v）", err)
	}
}

// TestDatabaseNewerThanProgram 覆盖「数据库版本高于程序」：
// 拒绝写入并提示升级，而不是尝试降级。
func TestDatabaseNewerThanProgram(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("打开失败: %v", err)
	}
	// 模拟用户曾用更新版本的程序打开过这个库
	if _, err := s.DB().Exec(
		`INSERT INTO schema_version (version, name, applied_at) VALUES (?, 'future', datetime('now'))`,
		LatestVersion()+1,
	); err != nil {
		t.Fatalf("写入未来版本失败: %v", err)
	}
	s.Close()

	_, err = Open(path)
	if !errors.Is(err, ErrDatabaseNewer) {
		t.Fatalf("期望 ErrDatabaseNewer, 实际 %v", err)
	}
}
