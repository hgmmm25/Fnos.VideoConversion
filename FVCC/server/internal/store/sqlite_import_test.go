// sqlite_import_test.go — P0-2 提交 2：旧 JSON 导入 SQLite 管线测试。

package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fvcc/internal/store/model"
)

// writeJSONFile 写入 JSON 测试数据文件。
func writeJSONFile(t *testing.T, dir, name string, v any) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// countTableRows 统计表行数。
func countTableRows(t *testing.T, sq *storeSQLite, table string) int {
	t.Helper()
	var n int
	if err := sq.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// backupDirs 列出 pre-sqlite 备份目录。
func backupDirs(t *testing.T, dataDir string) []string {
	t.Helper()
	dirs, err := filepath.Glob(filepath.Join(dataDir, "backup", "pre-sqlite-*"))
	if err != nil {
		t.Fatalf("glob backup: %v", err)
	}
	return dirs
}

// openDataDirSQLite 在 Load 后的 dataDir 上打开 SQLite 用于断言（测试自清理）。
func openDataDirSQLite(t *testing.T, dataDir string) *storeSQLite {
	t.Helper()
	sq, err := openSQLite(dataDir)
	if err != nil {
		t.Fatalf("openSQLite(%s) 失败: %v", dataDir, err)
	}
	t.Cleanup(func() { sq.Close() })
	return sq
}

func TestLoadImportLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, "tasks.json", model.TasksFile{Version: 1, Tasks: []model.Task{
		{ID: "t1", SourceFile: "/a.mp4"},
		{ID: "t2", SourceFile: "/b.mp4"},
	}})
	writeJSONFile(t, dir, "server.json", model.ServersFile{Version: 1, Servers: []model.Server{
		{ID: "s1", Name: "node1"},
	}})
	writeJSONFile(t, dir, "settings.json", model.SettingsFile{Version: 1, Settings: model.DefaultSettings()})
	writeJSONFile(t, dir, "audit_log.json", model.AuditLogFile{Version: 1, Entries: []model.AuditEntry{{}, {}}})
	writeJSONFile(t, dir, "projects.json", model.ProjectsFile{Version: 1, Projects: []model.Project{{ID: "p1"}}})

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	sq := openDataDirSQLite(t, dir)

	cases := map[string]int{
		"tasks": 2, "history_tasks": 0, "servers": 1, "profiles": 0,
		"locks": 0, "settings": 1, "video_cache": 0, "projects": 1,
		"node_caps": 0, "asset_proxies": 0, "node_health_samples": 0, "audit_log": 2,
	}
	for table, want := range cases {
		if got := countTableRows(t, sq, table); got != want {
			t.Errorf("表 %s 行数 = %d，期望 %d", table, got, want)
		}
	}

	// 导入标记已写入
	var mark string
	if err := sq.db.QueryRow(`SELECT value FROM import_meta WHERE key=?`, importMetaKey).Scan(&mark); err != nil {
		t.Fatalf("导入标记缺失: %v", err)
	}

	// 备份目录已生成且含 tasks.json
	backups := backupDirs(t, dir)
	if len(backups) != 1 {
		t.Fatalf("备份目录数量 = %d，期望 1", len(backups))
	}
	if _, err := os.Stat(filepath.Join(backups[0], "tasks.json")); err != nil {
		t.Fatalf("备份缺少 tasks.json: %v", err)
	}

	// 内存加载仍以 JSON 为准（行为零变化）
	if len(s.tasks) != 2 || len(s.servers) != 1 {
		t.Fatalf("内存数据不符: tasks=%d servers=%d", len(s.tasks), len(s.servers))
	}
}

func TestLoadImportLegacyJSON_Idempotent(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, "tasks.json", model.TasksFile{Version: 1, Tasks: []model.Task{{ID: "t1"}}})

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("首次 Load 失败: %v", err)
	}
	if err := s.Load(); err != nil { // 二次 Load 模拟重启
		t.Fatalf("二次 Load 失败: %v", err)
	}
	sq := openDataDirSQLite(t, dir)

	if got := countTableRows(t, sq, "tasks"); got != 1 {
		t.Fatalf("二次 Load 后 tasks 行数 = %d，期望 1（幂等不重复导入）", got)
	}
	if dirs := backupDirs(t, dir); len(dirs) != 1 {
		t.Fatalf("备份目录数量 = %d，期望 1（幂等不重复备份）", len(dirs))
	}
}

func TestLoadImportLegacyJSON_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("空目录 Load 失败: %v", err)
	}
	sq := openDataDirSQLite(t, dir)

	if got := countTableRows(t, sq, "tasks"); got != 0 {
		t.Fatalf("空目录 tasks 行数 = %d，期望 0", got)
	}
	if dirs := backupDirs(t, dir); len(dirs) != 0 {
		t.Fatalf("空目录不应生成备份: %v", dirs)
	}
	// 无导入标记：后续手动放入旧 JSON 仍可导入
	var n int
	if err := sq.db.QueryRow(`SELECT count(*) FROM import_meta`).Scan(&n); err != nil {
		t.Fatalf("import_meta 查询失败: %v", err)
	}
	if n != 0 {
		t.Fatalf("空目录不应写导入标记, import_meta 行数 = %d", n)
	}
}

// TestLoadImportLegacyJSON_CorruptJSON 损坏 JSON（宽松加载文件）不阻断启动，
// 空导入 + 备份原始文件（可人工恢复）。
func TestLoadImportLegacyJSON_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, "tasks.json", model.TasksFile{Version: 1, Tasks: []model.Task{{ID: "t1"}}})
	if err := os.WriteFile(filepath.Join(dir, "video_cache.json"), []byte("{corrupt"), 0o644); err != nil {
		t.Fatalf("写入损坏文件失败: %v", err)
	}

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("损坏 JSON 不应阻断启动: %v", err)
	}
	sq := openDataDirSQLite(t, dir)

	if got := countTableRows(t, sq, "tasks"); got != 1 {
		t.Fatalf("tasks 行数 = %d，期望 1", got)
	}
	if got := countTableRows(t, sq, "video_cache"); got != 0 {
		t.Fatalf("损坏文件导入行数 = %d，期望 0", got)
	}
	// 损坏文件也备份（可回滚恢复）
	if dirs := backupDirs(t, dir); len(dirs) != 1 {
		t.Fatalf("损坏文件也应触发备份: %v", dirs)
	}
}

// TestLoadImportLegacyJSON_SettingsNotExist 无 settings.json 时不得导入默认设置兜底值。
func TestLoadImportLegacyJSON_SettingsNotExist(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, "tasks.json", model.TasksFile{Version: 1, Tasks: []model.Task{{ID: "t1"}}})

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load 失败: %v", err)
	}
	sq := openDataDirSQLite(t, dir)

	if got := countTableRows(t, sq, "settings"); got != 0 {
		t.Fatalf("无 settings.json 不应导入默认设置, settings 行数 = %d", got)
	}
}
