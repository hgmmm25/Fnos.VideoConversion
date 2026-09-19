package api

// B-01 验收测试（06 §2 持久化模型）：
//  1) 旧库（version 1，无 task_type）加载后列迁移成功且幂等，重启不改写文件；
//  2) 损坏 / 更高版本的 tasks.json 拒绝启动（禁止静默降级）；
//  3) projects CRUD + rev 乐观锁 + 名称唯一；
//  4) checksum 幂等查询（活动 / 成功）；
//  5) node_caps 主键覆盖、健康采样裁剪、审计日志过滤。

import (
	"fvcc/internal/store"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeTasksFile(t *testing.T, dir string, f TasksFile) {
	t.Helper()
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		t.Fatalf("marshal tasks file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), data, 0o644); err != nil {
		t.Fatalf("write tasks file: %v", err)
	}
}

func loadStore(t *testing.T, dir string) *Store {
	t.Helper()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	return s
}

func TestLoadMigratesLegacyTasksFile(t *testing.T) {
	dir := t.TempDir()
	now := time.Now().Truncate(time.Second)
	writeTasksFile(t, dir, TasksFile{Version: 1, Tasks: []Task{{
		ID: "task_legacy_1", OrderID: 1, FileName: "a.mp4", SourceFile: `D:\videos\a.mp4`,
		ServerID: "_local_", ProfileID: "pf_1", Status: StatusQueue,
		CreatedAt: now, UpdatedAt: now,
	}}})

	s := loadStore(t, dir)
	got, ok := s.GetTask("task_legacy_1")
	if !ok {
		t.Fatalf("迁移后任务丢失")
	}
	if got.TaskType != TaskTypeTranscode {
		t.Fatalf("task_type 未迁移，期望 %q，实际 %q", TaskTypeTranscode, got.TaskType)
	}

	path := filepath.Join(dir, "tasks.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取回写后的 tasks.json 失败: %v", err)
	}
	var f TasksFile
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("回写文件不可解析: %v", err)
	}
	if f.Version != store.StoreSchemaVersion {
		t.Fatalf("文件版本未升级到 %d，实际 %d", store.StoreSchemaVersion, f.Version)
	}
	if f.Tasks[0].TaskType != TaskTypeTranscode {
		t.Fatalf("回写文件中的 task_type 不正确: %q", f.Tasks[0].TaskType)
	}

	// 重启幂等：二次 Load 不应再改写文件
	s2 := loadStore(t, dir)
	if _, ok := s2.GetTask("task_legacy_1"); !ok {
		t.Fatalf("二次加载后任务丢失")
	}
	raw2, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("二次读取失败: %v", err)
	}
	if string(raw2) != string(raw) {
		t.Fatalf("二次 Load 改写了 tasks.json，迁移非幂等")
	}
}

func TestLoadRejectsCorruptTasksFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte("{ this is not json"), 0o644); err != nil {
		t.Fatalf("准备损坏文件失败: %v", err)
	}
	s := NewStore(dir)
	if err := s.Load(); err == nil {
		t.Fatalf("损坏的 tasks.json 必须拒绝启动（禁止静默降级）")
	}
}

func TestLoadRejectsFutureTasksVersion(t *testing.T) {
	dir := t.TempDir()
	writeTasksFile(t, dir, TasksFile{Version: store.StoreSchemaVersion + 1, Tasks: []Task{}})
	s := NewStore(dir)
	if err := s.Load(); err == nil {
		t.Fatalf("高于支持版本的任务文件必须拒绝启动")
	}
}

func TestProjectCRUDAndOptimisticLock(t *testing.T) {
	dir := t.TempDir()
	s := loadStore(t, dir)

	p, err := s.CreateProject(Project{
		Name:     "我的项目",
		Timeline: Timeline{Width: 1920, Height: 1080, FPS: 30, SampleRate: 48000, Audio: true},
		Clips: []EDLClip{
			{ClipID: "c_00000001", AssetID: "a_0000000a", File: "demo/a_01.mp4", InMs: 0, OutMs: 1000, Speed: 1, SourceDurationMs: 60000},
			{ClipID: "c_00000002", AssetID: "a_0000000b", File: "demo/a_02.mp4", InMs: 2000, OutMs: 3500, Speed: 1, SourceDurationMs: 60000},
		},
	})
	if err != nil {
		t.Fatalf("CreateProject 失败: %v", err)
	}
	if p.Rev != 1 {
		t.Fatalf("新建项目 rev 应为 1，实际 %d", p.Rev)
	}
	if len(p.ID) != 10 || p.ID[:2] != "p_" {
		t.Fatalf("项目 ID 形状不符（期望 p_<8hex>）: %q", p.ID)
	}
	if p.ClipCount != 2 || p.TotalMs != 2500 {
		t.Fatalf("派生字段错误：clipCount=%d totalMs=%d", p.ClipCount, p.TotalMs)
	}
	if p.Clips[0].ClipID != "c_00000001" || p.Clips[1].ClipID != "c_00000002" {
		t.Fatalf("片段顺序/ID 未按输入保留: %+v", p.Clips)
	}
	if p.Timeline.FPS != 30 || p.Timeline.SampleRate != 48000 {
		t.Fatalf("时间线默认值未生效: %+v", p.Timeline)
	}

	if _, err := s.CreateProject(Project{Name: "我的项目"}); !errors.Is(err, ErrProjectNameUsed) {
		t.Fatalf("重名项目应返回 ErrProjectNameUsed，实际 %v", err)
	}

	upd, err := s.UpdateProject(p.ID, 1, Project{
		Name:  "我的项目",
		Clips: []EDLClip{{ClipID: "c_00000001", AssetID: "a_0000000a", File: "demo/a_01.mp4", InMs: 0, OutMs: 2000, Speed: 1, SourceDurationMs: 60000}},
	})
	if err != nil {
		t.Fatalf("UpdateProject 失败: %v", err)
	}
	if upd.Rev != 2 || upd.TotalMs != 2000 || upd.ClipCount != 1 {
		t.Fatalf("更新结果错误：rev=%d totalMs=%d clipCount=%d", upd.Rev, upd.TotalMs, upd.ClipCount)
	}

	if _, err := s.UpdateProject(p.ID, 1, Project{Name: "我的项目"}); !errors.Is(err, ErrRevConflict) {
		t.Fatalf("旧 rev 更新应返回 ErrRevConflict，实际 %v", err)
	}
	if _, err := s.UpdateProject("p_missing", 1, Project{Name: "x"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("不存在的项目应返回 ErrProjectNotFound，实际 %v", err)
	}

	// 落盘校验
	s2 := loadStore(t, dir)
	p2, ok := s2.GetProject(p.ID)
	if !ok {
		t.Fatalf("重启后项目丢失")
	}
	if p2.Rev != 2 || p2.ClipCount != 1 || p2.TotalMs != 2000 {
		t.Fatalf("重启后项目数据不一致：rev=%d clipCount=%d totalMs=%d", p2.Rev, p2.ClipCount, p2.TotalMs)
	}
	if len(s2.GetProjects()) != 1 {
		t.Fatalf("项目列表条数错误: %d", len(s2.GetProjects()))
	}

	if !s2.DeleteProject(p.ID) {
		t.Fatalf("删除项目失败")
	}
	if s2.DeleteProject(p.ID) {
		t.Fatalf("重复删除应返回 false")
	}
}

func TestChecksumIdempotencyQueries(t *testing.T) {
	dir := t.TempDir()
	s := loadStore(t, dir)

	s.UpsertTask(Task{
		ID: "task_r1", OrderID: 1, TaskType: TaskTypeRenderEDL, ProjectID: "p_1",
		Checksum: "ck_abc", Status: StatusQueue, Progress: 0,
	})

	if _, ok := s.FindActiveTaskByChecksum("ck_abc"); !ok {
		t.Fatalf("进行中任务未被 checksum 命中")
	}
	if _, ok := s.FindSuccessTaskByChecksum("ck_abc"); ok {
		t.Fatalf("未完成任务不应被成功查询命中")
	}
	if _, ok := s.FindActiveTaskByChecksum("ck_other"); ok {
		t.Fatalf("不存在的 checksum 不应命中")
	}
	if _, ok := s.FindActiveTaskByChecksum(""); ok {
		t.Fatalf("空 checksum 不应命中")
	}

	s.UpdateTaskStatus("task_r1", StatusCompleted, 100, "")

	if _, ok := s.FindActiveTaskByChecksum("ck_abc"); ok {
		t.Fatalf("已完成任务不应被进行中查询命中")
	}
	hit, ok := s.FindSuccessTaskByChecksum("ck_abc")
	if !ok || hit.ID != "task_r1" {
		t.Fatalf("已完成任务未被成功查询命中: %+v", hit)
	}
}

func TestCooldownBackoffAndDueList(t *testing.T) {
	if got := store.CooldownBackoffSec(0); got != 30 {
		t.Fatalf("attempt=0 退避应为 30s，实际 %d", got)
	}
	if got := store.CooldownBackoffSec(1); got != 60 {
		t.Fatalf("attempt=1 退避应为 60s，实际 %d", got)
	}
	if got := store.CooldownBackoffSec(4); got != 480 {
		t.Fatalf("attempt=4 退避应为 480s，实际 %d", got)
	}
	if got := store.CooldownBackoffSec(10); got != 600 {
		t.Fatalf("退避上限应为 600s，实际 %d", got)
	}

	dir := t.TempDir()
	s := loadStore(t, dir)
	past := time.Now().Add(-time.Minute)
	future := time.Now().Add(time.Minute)
	s.UpsertTask(Task{ID: "t_due", OrderID: 1, Status: StatusCooldown, CoolDownUntil: &past, CooldownReason: "E_NODE_OFFLINE"})
	s.UpsertTask(Task{ID: "t_wait", OrderID: 2, Status: StatusCooldown, CoolDownUntil: &future})
	s.UpsertTask(Task{ID: "t_normal", OrderID: 3, Status: StatusQueue})

	due := s.ListCooldownDue(time.Now())
	if len(due) != 1 || due[0].ID != "t_due" {
		t.Fatalf("冷却到期扫描结果错误: %+v", due)
	}
}

func TestNodeCapsAndHealthSamples(t *testing.T) {
	dir := t.TempDir()
	s := loadStore(t, dir)

	s.UpsertNodeCaps(NodeCaps{
		ServerID: "srv1", AgentVersion: "0.1.0", OS: "windows", CPUCores: 16,
		GPU:           []GPUInfo{{Vendor: "nvidia", Name: "RTX 4070", Encoders: []string{"h264_nvenc"}}},
		Encoders:      []string{"libx264"},
		MaxConcurrent: 2,
	})
	s.UpsertNodeCaps(NodeCaps{ServerID: "srv1", AgentVersion: "0.2.0", MaxConcurrent: 3})

	c, ok := s.GetNodeCaps("srv1")
	if !ok {
		t.Fatalf("节点能力未写入")
	}
	if c.AgentVersion != "0.2.0" || c.MaxConcurrent != 3 {
		t.Fatalf("节点能力未按主键覆盖: %+v", c)
	}
	if len(s.GetAllNodeCaps()) != 1 {
		t.Fatalf("同一 server_id 应只有一条能力快照，实际 %d", len(s.GetAllNodeCaps()))
	}

	base := time.Now().Add(-10 * time.Minute).Truncate(time.Second)
	for i := 0; i < 5; i++ {
		s.AppendNodeHealthSample(NodeHealthSample{
			ServerID: "srv1", SampledAt: base.Add(time.Duration(i) * time.Minute),
			Online: true, Running: i, CPUPct: float64(i), MemPct: 50,
		})
	}
	list := s.ListNodeHealthSamples("srv1", time.Time{}, 0)
	if len(list) != 5 {
		t.Fatalf("采样条数错误: %d", len(list))
	}
	if list[0].Running != 4 {
		t.Fatalf("采样未按时间倒序，首条 Running=%d", list[0].Running)
	}
	if got := s.ListNodeHealthSamples("srv1", time.Time{}, 2); len(got) != 2 {
		t.Fatalf("limit 未生效: %d", len(got))
	}
	if removed := s.PruneNodeHealthSamples(2); removed != 3 {
		t.Fatalf("裁剪条数错误，期望 3，实际 %d", removed)
	}

	s2 := loadStore(t, dir)
	if got := len(s2.ListNodeHealthSamples("srv1", time.Time{}, 0)); got != 2 {
		t.Fatalf("重启后采样数错误: %d", got)
	}
	if _, ok := s2.GetNodeCaps("srv1"); !ok {
		t.Fatalf("重启后节点能力丢失")
	}
}

func TestAuditLogAppendAndFilter(t *testing.T) {
	dir := t.TempDir()
	s := loadStore(t, dir)

	s.AppendAudit(AuditEntry{Actor: "admin", Action: "credential.create", Target: "cred_1", Result: "ok"})
	s.AppendAudit(AuditEntry{Actor: "admin", Action: "project.delete", Target: "p_1"})

	if got := len(s.ListAudit(0, "")); got != 2 {
		t.Fatalf("审计条数错误: %d", got)
	}
	cred := s.ListAudit(0, "credential")
	if len(cred) != 1 || cred[0].Target != "cred_1" {
		t.Fatalf("按动作过滤失败: %+v", cred)
	}
	if got := s.ListAudit(0, ""); got[1].Result == "" {
		t.Fatalf("Result 默认值未补全")
	}

	s2 := loadStore(t, dir)
	if got := len(s2.ListAudit(0, "")); got != 2 {
		t.Fatalf("重启后审计条数错误: %d", got)
	}
}
