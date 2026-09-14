// Package store 封装 SQLite 访问：迁移、FTS 同步、provider_cache 与全部写操作。
//
// 写操作集中在本包，便于统一加重试并避免 SQLite 单写者下的锁竞争。
package store

import (
	"database/sql"
	"fmt"

	"github.com/norhua/sonexa/internal/matching"
	_ "modernc.org/sqlite"
)

// Store 持有数据库连接。
type Store struct {
	db *sql.DB
	// ladder 是发行优选阶梯的配置，由 SetLadderConfig 注入用户设置。
	// 专辑视图选代表 release 与匹配阶段共用它。
	ladder *matching.LadderConfig
}

// Open 打开（或创建）位于 path 的数据库并应用全部未应用的迁移。
func Open(path string) (*Store, error) {
	// WAL 让后台匹配队列的写与前台读并发进行；busy_timeout 兜住短暂的锁争用。
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	s := &Store{db: db}
	if err := s.Migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// DB 暴露底层连接，供本包内的查询使用。
func (s *Store) DB() *sql.DB { return s.db }

// Close 关闭数据库连接。
func (s *Store) Close() error { return s.db.Close() }
