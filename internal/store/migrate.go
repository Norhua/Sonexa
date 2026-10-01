package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
)

// Migration 是一个顺序编号的迁移单元。每个迁移在单个事务中执行。
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// ErrDatabaseNewer 表示数据库版本高于本程序支持的最高版本。
// 此时拒绝写入并提示用户升级应用，而不是尝试降级。
var ErrDatabaseNewer = errors.New("数据库版本高于当前程序支持的版本，请升级应用")

// migrations 是全部迁移，按 Version 升序应用。
//
// 本代际从空库创建；不执行旧模型的数据或封面转换。
var migrations = []Migration{
	{Version: 1, Name: "snapshot_model", SQL: migration0001Init},
	{Version: 2, Name: "cover_catalog", SQL: migration0002CoverCatalog},
}

const ModelGeneration = "upstream-snapshot-v1"

var ErrIncompatibleModel = errors.New("数据库模型不兼容，请先备份并清理旧版应用数据；程序不会自动删除数据")

func (s *Store) checkModelGeneration() error {
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='model_generation'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' AND name<>'schema_version'`).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			return ErrIncompatibleModel
		}
		return nil
	}
	var generation string
	if err := s.db.QueryRow(`SELECT generation FROM model_generation WHERE id=1`).Scan(&generation); err != nil {
		return fmt.Errorf("%w: %v", ErrIncompatibleModel, err)
	}
	if generation != ModelGeneration {
		return fmt.Errorf("%w（%s）", ErrIncompatibleModel, generation)
	}
	return nil
}

// LatestVersion 返回本程序支持的最高 schema 版本。
func LatestVersion() int {
	latest := 0
	for _, m := range migrations {
		if m.Version > latest {
			latest = m.Version
		}
	}
	return latest
}

// currentVersion 读取数据库已应用到的版本号；表不存在时为 0。
func currentVersion(db *sql.DB) (int, error) {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version    INTEGER NOT NULL,
		name       TEXT    NOT NULL,
		applied_at DATETIME NOT NULL
	)`); err != nil {
		return 0, fmt.Errorf("创建 schema_version 表失败: %w", err)
	}
	var version sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(version) FROM schema_version`).Scan(&version); err != nil {
		return 0, fmt.Errorf("读取 schema 版本失败: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}

// Migrate 按编号顺序应用全部未应用的迁移。
//
// 单个迁移在事务中执行：失败时整体回滚且版本号保持不变。
// 数据库版本高于程序支持的最高版本时返回 ErrDatabaseNewer。
func (s *Store) Migrate() error {
	if err := s.checkModelGeneration(); err != nil {
		return err
	}
	cur, err := currentVersion(s.db)
	if err != nil {
		return err
	}
	if cur > LatestVersion() {
		return fmt.Errorf("%w（数据库 v%d，程序支持 v%d）", ErrDatabaseNewer, cur, LatestVersion())
	}

	pending := make([]Migration, 0, len(migrations))
	for _, m := range migrations {
		if m.Version > cur {
			pending = append(pending, m)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Version < pending[j].Version })

	for _, m := range pending {
		if err := s.applyMigration(m); err != nil {
			return fmt.Errorf("迁移 %04d_%s 失败: %w", m.Version, m.Name, err)
		}
	}
	return nil
}

// applyMigration 在单个事务中执行一个迁移并记录版本号。
func (s *Store) applyMigration(m Migration) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后回滚是无操作

	if _, err := tx.Exec(m.SQL); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`INSERT INTO schema_version (version, name, applied_at) VALUES (?, ?, datetime('now'))`,
		m.Version, m.Name,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// SchemaVersion 返回数据库当前的 schema 版本。
func (s *Store) SchemaVersion() (int, error) { return currentVersion(s.db) }
