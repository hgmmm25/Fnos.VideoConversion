// sqlite_test.go — P0-2 SQLite 载体与迁移框架测试（提交 1）。

package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func openTestSQLite(t *testing.T) *storeSQLite {
	t.Helper()
	dir := t.TempDir()
	s, err := openSQLite(dir)
	if err != nil {
		t.Fatalf("openSQLite() 失败: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenSQLiteCreatesDB(t *testing.T) {
	dir := t.TempDir()
	s, err := openSQLite(dir)
	if err != nil {
		t.Fatalf("openSQLite() 失败: %v", err)
	}
	defer s.Close()

	if _, err := os.Stat(filepath.Join(dir, sqliteDBName)); err != nil {
		t.Fatalf("数据库文件未创建: %v", err)
	}
	v, err := s.version()
	if err != nil {
		t.Fatalf("version() 失败: %v", err)
	}
	if v != schemaVersion {
		t.Fatalf("期望 schemaVersion=%d，实际 %d", schemaVersion, v)
	}
}

func TestOpenSQLiteMigrateIdempotent(t *testing.T) {
	dir := t.TempDir()
	s1, err := openSQLite(dir)
	if err != nil {
		t.Fatalf("首次 openSQLite() 失败: %v", err)
	}
	s1.Close()

	// 重复打开（模拟重启）：迁移必须幂等、不报错、版本不变
	s2, err := openSQLite(dir)
	if err != nil {
		t.Fatalf("重复 openSQLite() 失败: %v", err)
	}
	defer s2.Close()
	v, err := s2.version()
	if err != nil {
		t.Fatalf("version() 失败: %v", err)
	}
	if v != schemaVersion {
		t.Fatalf("重复迁移后版本应保持 %d，实际 %d", schemaVersion, v)
	}
}

func TestOpenSQLiteCreatesAllTables(t *testing.T) {
	s := openTestSQLite(t)

	keyed := []string{"tasks", "history_tasks", "servers", "profiles", "locks", "settings", "video_cache", "projects", "node_caps", "asset_proxies"}
	seq := []string{"node_health_samples", "audit_log"}
	for _, name := range append(append([]string{}, keyed...), seq...) {
		var count int
		err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&count)
		if err != nil {
			t.Fatalf("查询表 %s 失败: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("表 %s 未创建", name)
		}
	}
}

func TestOpenSQLiteSchemaVersionTable(t *testing.T) {
	s := openTestSQLite(t)

	var ver int
	if err := s.db.QueryRow(`SELECT version FROM schema_version LIMIT 1`).Scan(&ver); err != nil {
		t.Fatalf("schema_version 读取失败: %v", err)
	}
	if ver != schemaVersion {
		t.Fatalf("schema_version=%d，期望 %d", ver, schemaVersion)
	}
}

// TestOpenSQLiteWALMode 验证 WAL 已启用（提交 1 冒烟）。
func TestOpenSQLiteWALMode(t *testing.T) {
	s := openTestSQLite(t)

	var journal string
	if err := s.db.QueryRow(`PRAGMA journal_mode`).Scan(&journal); err != nil {
		t.Fatalf("读取 journal_mode 失败: %v", err)
	}
	if journal != "wal" {
		t.Fatalf("期望 journal_mode=wal，实际 %s", journal)
	}
}

// TestOpenSQLiteWriteReadRoundTrip 单列 JSON 载体读写冒烟（为提交 2 persist 替换打底）。
func TestOpenSQLiteWriteReadRoundTrip(t *testing.T) {
	s := openTestSQLite(t)

	if _, err := s.db.Exec(`INSERT INTO settings (id, data, updated_at) VALUES ('default', ?, 1)`, `{"smbSharePath":"/mnt/share"}`); err != nil {
		t.Fatalf("写入 settings 失败: %v", err)
	}
	var data string
	if err := s.db.QueryRow(`SELECT data FROM settings WHERE id='default'`).Scan(&data); err != nil {
		t.Fatalf("读取 settings 失败: %v", err)
	}
	if data != `{"smbSharePath":"/mnt/share"}` {
		t.Fatalf("读取内容不符: %s", data)
	}
}

// TestOpenSQLiteBrokenMigrationChain 迁移链断裂应报错（防御性）。
func TestOpenSQLiteBrokenMigrationChain(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, sqliteDBName))
	if err != nil {
		t.Fatalf("sql.Open 失败: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE schema_version (version INTEGER NOT NULL); INSERT INTO schema_version (version) VALUES (99)`); err != nil {
		t.Fatalf("构造异常版本失败: %v", err)
	}
	db.Close()

	if _, err := openSQLite(dir); err == nil {
		t.Fatal("版本 99 > schemaVersion 应报错，实际未报错")
	}
}
