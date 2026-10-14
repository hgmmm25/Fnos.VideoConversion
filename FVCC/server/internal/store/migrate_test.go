// migrate_test.go — P3-5 阶段 B：版本化数据迁移链测试。
// 验收（STORE_PERSISTENCE_ROADMAP §5）：旧版本数据升级后字段正确、重复执行幂等、
// 迁移前自动备份、锚点双载体（SQLite meta + JSON 镜像）一致、版本过高拒绝启动。

package store

import (
	"strings"
	"testing"
	"time"

	"fvcc/internal/store/model"
)

// countBackupsKind 统计指定 kind 的备份数（走 ListBackups，忽略 pre-sqlite 目录备份）。
func countBackupsKind(t *testing.T, s *Store, kind string) int {
	t.Helper()
	list, err := s.ListBackups()
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	n := 0
	for _, b := range list {
		if b.Kind == kind {
			n++
		}
	}
	return n
}

// countAuditAction 统计指定 action 的审计条目数（包内直读，避免走过滤 API）。
func countAuditAction(t *testing.T, s *Store, action string) int {
	t.Helper()
	s.mu.RLock()
	defer s.mu.RUnlock()
	n := 0
	for _, e := range s.auditLog {
		if e.Action == action {
			n++
		}
	}
	return n
}

func TestMigrationChainFromLegacyData(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)
	// 旧版数据：tasks 缺 TaskType 列（version 1 时代），projects 缺 schema_ver。
	writeJSONFile(t, dir, "tasks.json", model.TasksFile{Version: 1, Tasks: []model.Task{
		{ID: "t_legacy", OrderID: 1, FileName: "a.mp4", SourceFile: "/videos/a.mp4", CreatedAt: now, UpdatedAt: now},
	}})
	writeJSONFile(t, dir, "projects.json", model.ProjectsFile{Version: 1, Projects: []model.Project{
		{ID: "p_legacy", Name: "旧项目", CreatedAt: now, UpdatedAt: now},
	}})

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// 迁移结果：字段补齐正确。
	task, ok := s.GetTask("t_legacy")
	if !ok {
		t.Fatalf("旧任务丢失")
	}
	if task.TaskType != model.TaskTypeTranscode {
		t.Fatalf("task_type 未迁移，期望 %q 实际 %q", model.TaskTypeTranscode, task.TaskType)
	}
	proj, ok := s.GetProject("p_legacy")
	if !ok {
		t.Fatalf("旧项目丢失")
	}
	if proj.SchemaVer != 1 {
		t.Fatalf("schema_ver 未迁移，期望 1 实际 %d", proj.SchemaVer)
	}

	// 锚点：双载体一致抬到链尾。
	target := targetDataVersion()
	if v, ok := readJSONDataVersion(s.path(dataVersionFile)); !ok || v != target {
		t.Fatalf("JSON 锚点=%d/%v，期望 %d", v, ok, target)
	}
	v, ok, err := readSQLiteDataVersion(dir)
	if err != nil || !ok || v != target {
		t.Fatalf("SQLite 锚点=%d/%v err=%v，期望 %d", v, ok, err, target)
	}

	// 迁移前自动备份 + 审计落一条 schema.migrate。
	if n := countBackupsKind(t, s, BackupKindPreMigration); n != 1 {
		t.Fatalf("premigration 备份数=%d，期望 1", n)
	}
	if n := countAuditAction(t, s, "schema.migrate"); n != 1 {
		t.Fatalf("schema.migrate 审计数=%d，期望 1", n)
	}

	// 重启幂等：二次 Load 不再迁移（无新备份/新审计），数据仍在。
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("二次 Load: %v", err)
	}
	if n := countBackupsKind(t, s2, BackupKindPreMigration); n != 1 {
		t.Fatalf("二次 Load 产生新 premigration 备份（n=%d），迁移链非幂等", n)
	}
	if n := countAuditAction(t, s2, "schema.migrate"); n != 1 {
		t.Fatalf("二次 Load 追加 schema.migrate 审计（n=%d），迁移链非幂等", n)
	}
	if task2, ok := s2.GetTask("t_legacy"); !ok || task2.TaskType != model.TaskTypeTranscode {
		t.Fatalf("二次加载任务状态异常: ok=%v taskType=%q", ok, task2.TaskType)
	}
}

func TestMigrationFreshStoreStampsWithoutBackup(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// 全新安装：直接抬锚，不迁移、不备份。
	target := targetDataVersion()
	if v, ok := readJSONDataVersion(s.path(dataVersionFile)); !ok || v != target {
		t.Fatalf("全新安装未初始化锚点：%d/%v", v, ok)
	}
	if n := countBackupsKind(t, s, BackupKindPreMigration); n != 0 {
		t.Fatalf("全新安装不应产生 premigration 备份，实际 %d", n)
	}
	if n := countAuditAction(t, s, "schema.migrate"); n != 0 {
		t.Fatalf("全新安装不应有 schema.migrate 审计，实际 %d", n)
	}
}

func TestMigrationPartialChainRunsOnlyPendingSteps(t *testing.T) {
	dir := t.TempDir()
	// 锚点已停在 2（1→2 已执行）：本应只补项目 schema_ver，任务列补齐被跳过。
	writeJSONFile(t, dir, "tasks.json", model.TasksFile{Version: 1, Tasks: []model.Task{
		{ID: "t1", OrderID: 1, FileName: "a.mp4", SourceFile: "/a.mp4"},
	}})
	writeJSONFile(t, dir, "projects.json", model.ProjectsFile{Version: 1, Projects: []model.Project{
		{ID: "p1", Name: "proj"},
	}})
	writeJSONFile(t, dir, dataVersionFile, 2)

	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := s.GetProject("p1"); !ok {
		t.Fatalf("项目丢失")
	}
	proj, _ := s.GetProject("p1")
	if proj.SchemaVer != 1 {
		t.Fatalf("2→3 步未执行：schema_ver=%d", proj.SchemaVer)
	}
	task, ok := s.GetTask("t1")
	if !ok {
		t.Fatalf("任务丢失")
	}
	if task.TaskType != "" {
		t.Fatalf("1→2 步已被锚点跳过，TaskType 不应被补齐，实际 %q", task.TaskType)
	}
	if v, _ := readJSONDataVersion(s.path(dataVersionFile)); v != targetDataVersion() {
		t.Fatalf("链尾未收口：anchor=%d", v)
	}
	if n := countBackupsKind(t, s, BackupKindPreMigration); n != 1 {
		t.Fatalf("premigration 备份数=%d，期望 1", n)
	}
}

func TestMigrationFutureDataVersionRefusesStart(t *testing.T) {
	dir := t.TempDir()
	writeJSONFile(t, dir, dataVersionFile, targetDataVersion()+10)

	s := NewStore(dir)
	err := s.Load()
	if err == nil {
		t.Fatalf("高于程序支持的 data_version 应拒绝启动")
	}
	if !strings.Contains(err.Error(), "请升级") {
		t.Fatalf("错误信息应提示升级，实际: %v", err)
	}
}
