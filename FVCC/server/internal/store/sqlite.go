// sqlite.go — P0-2 SQLite 持久化载体（提交 1：schema + 迁移框架）。
//
// 设计决策（2026-09-21，依据《P0-2_SQLite迁移成本评估.md》）：
//   FVCC Store 为「内存索引 + 脏标记 + 定时批量落盘」架构，86 个业务方法读写内存，
//   SQLite 仅作为持久化载体替换 JSON 文件。故实体表采用单列 JSON 存储：
//       CREATE TABLE <name> (id TEXT PRIMARY KEY, data TEXT /* 整行 model JSON */, updated_at INTEGER)
//   与 loadJSON/saveJSON 序列化语义完全一致，业务方法零改动；查询仍在内存索引执行，
//   不依赖 SQL 列级查询（YAGNI，未来 schema 变更走迁移链加列）。
//   追加型时序/审计表（node_health_samples / audit_log）无自然主键，用
//       seq INTEGER PRIMARY KEY AUTOINCREMENT
//   保持插入顺序即追加顺序，与现有 Append 语义一致。
//
// 构建链约束：FVCC 交叉编译 linux/amd64 且 CGO_ENABLED=0（build-env.ps1 / build.ps1），
//   故选用纯 Go 驱动 modernc.org/sqlite（C 源码转译，无 CGO）；mattn/go-sqlite3（CGO）
//   在 CGO=0 下 stub 报错，不可用。

package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动（无 CGO，支持 linux/amd64 交叉编译）
)

// sqliteDBName SQLite 数据库文件名（位于 dataDir 下）。
const sqliteDBName = "fvcc.db"

// schemaVersion 当前存储 schema 版本。每次 schema 变更递增，并追加迁移函数。
const schemaVersion = 1

// migration 单步迁移：从 from 版本迁移到 to 版本（要求 to == from+1，线性链）。
type migration struct {
	from int
	to   int
	apply func(tx *sql.Tx) error
}

// migrations 版本迁移链（按 from 升序）。每步必须幂等：同版本重复执行结果一致，
// 迁移失败整体回滚（事务），版本号不回退。
var migrations = []migration{
	// v0 -> v1：初始建表（12 张实体表，单列 JSON 存储）。
	{
		from: 0, to: 1,
		apply: func(tx *sql.Tx) error {
			// keyed 表：id TEXT PRIMARY KEY（单列 JSON）
			keyed := []string{
				"tasks",            // model.Task.ID
				"history_tasks",    // model.Task.ID（保留最近 1000 条）
				"servers",          // model.Server.ID
				"profiles",         // model.Profile.ID
				"locks",            // model.Lock.ServerID（每 server 一条，含传输锁/转码锁）
				"settings",         // 单对象，固定 id='default'
				"video_cache",      // model.VideoInfoCache.Path
				"projects",         // model.Project.ID
				"node_caps",        // model.NodeCaps.ServerID
				"asset_proxies",    // model.AssetProxy.AssetKey
			}
			for _, name := range keyed {
				if _, err := tx.Exec(fmt.Sprintf(
					`CREATE TABLE IF NOT EXISTS %s (
						id         TEXT PRIMARY KEY,
						data       TEXT NOT NULL,
						updated_at INTEGER NOT NULL
					)`, name)); err != nil {
					return fmt.Errorf("create table %s: %w", name, err)
				}
			}
			// seq 表：追加型数据，无自然主键（插入顺序 = 追加顺序）
			seq := []string{
				"node_health_samples", // model.NodeHealthSample
				"audit_log",           // model.AuditEntry
			}
			for _, name := range seq {
				if _, err := tx.Exec(fmt.Sprintf(
					`CREATE TABLE IF NOT EXISTS %s (
						seq        INTEGER PRIMARY KEY AUTOINCREMENT,
						data       TEXT NOT NULL,
						updated_at INTEGER NOT NULL
					)`, name)); err != nil {
					return fmt.Errorf("create table %s: %w", name, err)
				}
			}
			return nil
		},
	},
}

// storeSQLite 封装 SQLite 句柄（持久化载体，不承载业务查询）。
type storeSQLite struct {
	db   *sql.DB
	path string
}

// openSQLite 打开（或创建）dataDir 下的 fvcc.db 并执行迁移到最新版本。
// 单连接串行化：SQLite 单写者，与现有内存 RWMutex 串行写语义一致。
func openSQLite(dataDir string) (*storeSQLite, error) {
	path := filepath.Join(dataDir, sqliteDBName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",   // WAL：读不阻塞写，崩溃恢复更强
		"PRAGMA synchronous=NORMAL", // WAL 下 NORMAL 足够（fsync 频率适中）
		"PRAGMA busy_timeout=5000",  // 写锁忙等待，避免瞬时报 database is locked
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("pragma %s: %w", pragma, err)
		}
	}
	s := &storeSQLite{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close 关闭数据库连接。
func (s *storeSQLite) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// migrate 将 schema 从当前版本线性推进到 schemaVersion。
// 幂等：已应用版本直接跳过；迁移在单事务内执行，任一步失败整体回滚。
func (s *storeSQLite) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin migrate tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // 提交成功后 Rollback 为 no-op

	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_version: %w", err)
	}
	var ver int
	err = tx.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&ver)
	if errors.Is(err, sql.ErrNoRows) {
		ver = 0
		if _, err := tx.Exec(`INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return fmt.Errorf("init schema_version: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("read schema_version: %w", err)
	}
	if ver == schemaVersion {
		return tx.Commit() // 已最新，无迁移（仍提交以创建 schema_version）
	}
	if ver > schemaVersion {
		return fmt.Errorf("schema version %d > code version %d: 请先升级程序", ver, schemaVersion)
	}
	for _, m := range migrations {
		if m.to <= ver {
			continue // 已应用
		}
		if m.from != ver {
			return fmt.Errorf("migration chain broken: current=%d next.from=%d", ver, m.from)
		}
		if err := m.apply(tx); err != nil {
			return fmt.Errorf("migrate %d->%d: %w", m.from, m.to, err)
		}
		if _, err := tx.Exec(`UPDATE schema_version SET version = ?`, m.to); err != nil {
			return fmt.Errorf("bump schema_version to %d: %w", m.to, err)
		}
		ver = m.to
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migrate: %w", err)
	}
	return nil
}

// version 返回当前 schema 版本（测试/诊断用）。
func (s *storeSQLite) version() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&v)
	return v, err
}
