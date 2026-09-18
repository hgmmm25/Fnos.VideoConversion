package task

// A-06 断点续跑（resume）与 workDir 复用/孤儿清理单元测试。
//
// 覆盖范围（对应 06 §3.3 / §3.4、05 §4.5）：
//  1. 任务类型判定与续跑段数钳制（isRenderTaskType / effectiveResumeSegDone）；
//  2. 阶段信息合并的单调语义（mergeRenderRunInfo）；
//  3. 重启恢复决策（recoverRuntimeTask）：RUNNING 渲染任务 → Waiting + ResumePending，
//     传统转码任务保持"整任务重跑"；
//  4. workDir 复用锚点落库（markRenderPlan / setRenderStage）；
//  5. 片段前缀完整性校验与断点残留清理（ffmpeg.ResumableSegmentPrefix）；
//  6. 孤儿 workDir 清理计划（TTL 7 天 + 保留最近 3 个）与真实扫描。
//
// 说明：本机无 CGO/gcc，`github.com/mattn/go-sqlite3` 为 stub，故用例不打开 SQLite
// （不调用 Init/loadTasksFromDB/saveTaskToDB），仅以内存 TaskManager 覆盖恢复决策与续跑锚点逻辑。

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/ffmpeg"
)

// ------------------------------------------------------------
// 测试脚手架
// ------------------------------------------------------------

// newBareTaskManager 构造无持久化的 TaskManager（db 为 nil，saveTaskToDB 自动跳过）。
func newBareTaskManager() *TaskManager {
	return &TaskManager{
		tasks:          map[string]*Task{},
		db:             nil,
		stopChan:       make(chan struct{}),
		lastNotifyTime: map[string]time.Time{},
		lastNotifyProg: map[string]float64{},
	}
}

// withTestManager 临时替换包级 manager（测试串行执行，结束后复位）。
func withTestManager(t *testing.T, mgr *TaskManager) {
	t.Helper()
	old := manager
	manager = mgr
	t.Cleanup(func() { manager = old })
}

func writeFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("写入文件失败 %s: %v", path, err)
	}
}

func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("期望路径不存在: %s (err=%v)", path, err)
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("期望路径存在: %s (err=%v)", path, err)
	}
}

// ------------------------------------------------------------
// 1. 任务类型判定 + 续跑段数钳制
// ------------------------------------------------------------

func TestA06IsRenderTaskType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"TRANSCODE", false},
		{"transcode", false},
		{"RENDER_EDL", true},
		{"render_edl", true},
		{"  render_edl  ", true},
		{"GEN_PROXY", true},
		{"gen_proxy", true},
		{"UNKNOWN", false},
	}
	for _, c := range cases {
		if got := isRenderTaskType(c.in); got != c.want {
			t.Fatalf("isRenderTaskType(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}
}

func TestA06EffectiveResumeSegDone(t *testing.T) {
	base := func() *Task {
		return &Task{
			TaskID:        "t",
			TaskType:      TaskTypeRenderEDL,
			PayloadJSON:   `{"projectId":"p1"}`,
			Status:        StatusWaiting,
			ResumePending: true,
			SegDone:       3,
			SegTotal:      8,
		}
	}

	cases := []struct {
		name string
		mut  func(*Task)
		want int
	}{
		{"正常续跑取 SegDone", func(*Task) {}, 3},
		{"无续跑标记则整任务重跑", func(x *Task) { x.ResumePending = false }, 0},
		{"workDir 已清理则不续跑", func(x *Task) { x.FileCleaned = true }, 0},
		{"载荷不可重放则不续跑", func(x *Task) { x.PayloadJSON = "  " }, 0},
		{"SegDone 为 0 则不续跑", func(x *Task) { x.SegDone = 0 }, 0},
		{"SegDone 超过 SegTotal 时钳制", func(x *Task) { x.SegDone = 99 }, 8},
		{"SegTotal 未知时按 SegDone 原值", func(x *Task) { x.SegTotal = 0 }, 3},
	}
	for _, c := range cases {
		task := base()
		c.mut(task)
		if got := effectiveResumeSegDone(task); got != c.want {
			t.Fatalf("%s: effectiveResumeSegDone = %d, 期望 %d", c.name, got, c.want)
		}
	}

	if got := effectiveResumeSegDone(nil); got != 0 {
		t.Fatalf("nil 任务应返回 0，实际 %d", got)
	}
}

// ------------------------------------------------------------
// 2. 阶段信息合并（单调推进）
// ------------------------------------------------------------

func TestA06MergeRenderRunInfo(t *testing.T) {
	task := &Task{Stage: "prepare", StageIndex: 0, SegDone: 0, Progress: 0}

	// 第一次上报：prepare 完成
	changed := mergeRenderRunInfo(task, ffmpeg.StageRunInfo{Stage: "prepare", OverallPct: 5})
	if !changed || task.Stage != "prepare" || task.Progress != 5 {
		t.Fatalf("prepare 上报异常: stage=%s progress=%v changed=%v", task.Stage, task.Progress, changed)
	}

	// 进度回退不得覆盖
	if changed := mergeRenderRunInfo(task, ffmpeg.StageRunInfo{Stage: "segment", Index: 1, Total: 4, SegDone: 1, OverallPct: 3}); changed {
		t.Fatalf("进度回退应返回 changed=false")
	}
	if task.Progress != 5 {
		t.Fatalf("进度回退被覆盖: %v", task.Progress)
	}
	if task.Stage != "segment" || task.StageIndex != 1 || task.StageTotal != 4 || task.SegDone != 1 {
		t.Fatalf("阶段/段信息未合并: stage=%s idx=%d total=%d segDone=%d", task.Stage, task.StageIndex, task.StageTotal, task.SegDone)
	}

	// 段序号回退不得覆盖（06 §4.2 只允许正向推进）；SegDone=0 保持原值
	mergeRenderRunInfo(task, ffmpeg.StageRunInfo{Stage: "segment", Index: 1, Total: 4, SegDone: 0, OverallPct: 40})
	if task.StageIndex != 1 {
		t.Fatalf("段序号不应回退: %d", task.StageIndex)
	}
	if task.SegDone != 1 {
		t.Fatalf("SegDone=0 应保持原值: %d", task.SegDone)
	}
	if task.Progress != 40 {
		t.Fatalf("进度应推进到 40: %v", task.Progress)
	}

	// 正常推进
	mergeRenderRunInfo(task, ffmpeg.StageRunInfo{Stage: "segment", Index: 3, Total: 4, SegDone: 3, OverallPct: 80})
	if task.StageIndex != 3 || task.SegDone != 3 || task.Progress != 80 {
		t.Fatalf("推进异常: idx=%d segDone=%d progress=%v", task.StageIndex, task.SegDone, task.Progress)
	}

	if changed := mergeRenderRunInfo(nil, ffmpeg.StageRunInfo{OverallPct: 100}); changed {
		t.Fatalf("nil 任务应返回 false")
	}
}

// ------------------------------------------------------------
// 3. 重启恢复决策 + workDir 复用锚点
// ------------------------------------------------------------

func TestA06RecoverRuntimeTask(t *testing.T) {
	// 渲染类：RUNNING → Waiting + ResumePending，段信息保留
	render := &Task{
		TaskID: "render-resume", Status: StatusTranscoding, TaskType: TaskTypeRenderEDL,
		PayloadJSON: `{"projectId":"p1"}`, Stage: "segment", StageIndex: 2,
		SegDone: 2, SegTotal: 5, Progress: 42,
	}
	requeued, resumable := recoverRuntimeTask(render)
	if !requeued || !resumable {
		t.Fatalf("渲染任务应重新入队且可续跑，实际 requeued=%v resumable=%v", requeued, resumable)
	}
	if render.Status != StatusWaiting || !render.ResumePending {
		t.Fatalf("渲染任务应 Waiting+ResumePending，实际 status=%s resumePending=%v", render.Status, render.ResumePending)
	}
	if render.SegDone != 2 || render.SegTotal != 5 || render.Stage != "segment" {
		t.Fatalf("续跑锚点不应被恢复流程清空: segDone=%d segTotal=%d stage=%s", render.SegDone, render.SegTotal, render.Stage)
	}
	if got := effectiveResumeSegDone(render); got != 2 {
		t.Fatalf("重启后待校验段数应为 2，实际 %d", got)
	}

	// 代理任务同样可续跑（同属渲染类）
	proxy := &Task{TaskID: "proxy-resume", Status: StatusTranscoding, TaskType: TaskTypeGenProxy}
	if _, resumable := recoverRuntimeTask(proxy); !resumable {
		t.Fatalf("GEN_PROXY 应被视为可续跑任务")
	}

	// 传统转码：状态回归 Waiting，但不得带续跑标记（行为与改造前一致）
	legacy := &Task{TaskID: "transcode-legacy", Status: StatusTranscoding, TaskType: TaskTypeTranscode, Progress: 30}
	requeued, resumable = recoverRuntimeTask(legacy)
	if !requeued || resumable {
		t.Fatalf("转码任务应重新入队且不可续跑，实际 requeued=%v resumable=%v", requeued, resumable)
	}
	if legacy.Status != StatusWaiting || legacy.ResumePending {
		t.Fatalf("转码任务应 Waiting 且无续跑标记，实际 status=%s resumePending=%v", legacy.Status, legacy.ResumePending)
	}

	// 非 RUNNING 任务不改动
	for _, st := range []TaskStatus{StatusWaiting, StatusSuccess, StatusFailed, StatusPaused, StatusCancelled, StatusClientDisconnect} {
		x := &Task{TaskID: "x", Status: st, TaskType: TaskTypeRenderEDL}
		if requeued, resumable := recoverRuntimeTask(x); requeued || resumable {
			t.Fatalf("状态 %s 不应被恢复流程改动", st)
		}
		if x.Status != st || x.ResumePending {
			t.Fatalf("状态 %s 被意外改写: %s", st, x.Status)
		}
	}
	if requeued, resumable := recoverRuntimeTask(nil); requeued || resumable {
		t.Fatalf("nil 任务应返回 false/false")
	}
}

func TestA06ResumeAnchorLifecycle(t *testing.T) {
	mgr := newBareTaskManager()
	withTestManager(t, mgr)
	mgr.tasks["render-resume"] = &Task{
		TaskID: "render-resume", Status: StatusWaiting, TaskType: TaskTypeRenderEDL,
		PayloadJSON: `{"projectId":"p1"}`, ResumePending: true, SegDone: 2, SegTotal: 5,
	}

	// 计划构造成功即消费续跑标记（避免同进程内重试误判续跑）
	markRenderPlan("render-resume", 5)
	rt := mgr.tasks["render-resume"]
	if rt.ResumePending {
		t.Fatalf("markRenderPlan 后应清除续跑标记")
	}
	if rt.SegTotal != 5 {
		t.Fatalf("markRenderPlan 应记录 SegTotal=5，实际 %d", rt.SegTotal)
	}
	if got := effectiveResumeSegDone(rt); got != 0 {
		t.Fatalf("标记已消费后不应再续跑，实际 %d", got)
	}

	// 执行期上报：SegDone 作为下次重启的续跑锚点（仅推进时落库）
	setRenderStage("render-resume", ffmpeg.StageRunInfo{Stage: "segment", Index: 3, Total: 5, SegDone: 3, OverallPct: 66})
	if rt.SegDone != 3 || rt.StageIndex != 3 || rt.Progress != 66 {
		t.Fatalf("setRenderStage 合并异常: segDone=%d idx=%d progress=%v", rt.SegDone, rt.StageIndex, rt.Progress)
	}

	// 模拟再次崩溃重启：Running → 带 SegDone=3 的续跑任务，从第 4 段继续
	rt.Status = StatusTranscoding
	if requeued, resumable := recoverRuntimeTask(rt); !requeued || !resumable {
		t.Fatalf("二次重启应重新入队并续跑")
	}
	if got := effectiveResumeSegDone(rt); got != 3 {
		t.Fatalf("二次恢复待校验段数应为 3，实际 %d", got)
	}
}

// ------------------------------------------------------------
// 4. 片段前缀完整性校验与断点残留清理（workDir 复用）
// ------------------------------------------------------------

func TestA06ResumableSegmentPrefix(t *testing.T) {
	workDir := t.TempDir()
	segPath := func(i int) string { return filepath.Join(workDir, fmt.Sprintf("seg_%03d.mp4", i)) }

	stages := make([]ffmpeg.RenderStage, 0, 6)
	for i := 1; i <= 5; i++ {
		stages = append(stages, ffmpeg.RenderStage{
			Name: ffmpeg.StageSegment, Output: segPath(i), WeightPct: 10,
		})
	}
	stages = append(stages, ffmpeg.RenderStage{
		Name: ffmpeg.StageFinalize, Output: filepath.Join(workDir, "out.mp4"), WeightPct: 5,
	})
	plan := &ffmpeg.RenderPlan{WorkDir: workDir, Stages: stages}

	// 正常情况：前 3 段完整，第 4 段缺失、第 5 段为半成品残留
	writeFile(t, segPath(1), 10)
	writeFile(t, segPath(2), 10)
	writeFile(t, segPath(3), 10)
	writeFile(t, segPath(5), 10)

	if got := ffmpeg.ResumableSegmentPrefix(plan, 3); got != 3 {
		t.Fatalf("前 3 段完整时应返回 3，实际 %d", got)
	}
	mustNotExist(t, segPath(5)) // 断点之后的残留必须清理，避免拼进半成品
	mustExist(t, segPath(1))

	// 第 4 段存在但为空：视为不完整，从第 4 段重跑并清理其后的残留
	writeFile(t, segPath(4), 0)
	writeFile(t, segPath(5), 10)
	if got := ffmpeg.ResumableSegmentPrefix(plan, 4); got != 3 {
		t.Fatalf("第 4 段为空时应返回 3，实际 %d", got)
	}
	mustNotExist(t, segPath(4))
	mustNotExist(t, segPath(5))

	// want <= 0：不做校验也不清理（等价整任务重跑）
	writeFile(t, segPath(5), 10)
	if got := ffmpeg.ResumableSegmentPrefix(plan, 0); got != 0 {
		t.Fatalf("want=0 应返回 0，实际 %d", got)
	}
	mustExist(t, segPath(5))

	// want 超过实际段数：钳制到 5
	writeFile(t, segPath(4), 10)
	if got := ffmpeg.ResumableSegmentPrefix(plan, 9); got != 5 {
		t.Fatalf("want 超界应钳制为 5，实际 %d", got)
	}

	// nil / 无 segment 阶段：安全返回 0
	if got := ffmpeg.ResumableSegmentPrefix(nil, 3); got != 0 {
		t.Fatalf("nil plan 应返回 0，实际 %d", got)
	}
	if got := ffmpeg.ResumableSegmentPrefix(&ffmpeg.RenderPlan{WorkDir: workDir}, 3); got != 0 {
		t.Fatalf("无 segment 阶段应返回 0，实际 %d", got)
	}
}

// ------------------------------------------------------------
// 5. 孤儿 workDir 清理
// ------------------------------------------------------------

func TestA06PlanOrphanWorkDirCleanup(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	orphans := []orphanWorkDir{
		{Name: "fresh-1", ModTime: now.Add(-1 * time.Hour)},
		{Name: "fresh-2", ModTime: now.Add(-2 * time.Hour)},
		{Name: "fresh-3", ModTime: now.Add(-3 * time.Hour)},
		{Name: "old-8d", ModTime: now.Add(-8 * 24 * time.Hour)},
		{Name: "old-10d", ModTime: now.Add(-10 * 24 * time.Hour)},
	}
	got := planOrphanWorkDirCleanup(orphans, now)
	want := map[string]bool{"old-8d": true, "old-10d": true}
	if len(got) != len(want) {
		t.Fatalf("应删除 2 个过期孤儿，实际 %v", got)
	}
	for _, name := range got {
		if !want[name] {
			t.Fatalf("不应删除 %s（最近 3 个需保留）", name)
		}
	}

	// 未过期的不删
	if got := planOrphanWorkDirCleanup([]orphanWorkDir{{Name: "x", ModTime: now.Add(-6 * 24 * time.Hour)}}, now); len(got) != 0 {
		t.Fatalf("未过期孤儿不应删除: %v", got)
	}
	// 全部都很旧，但最新 3 个仍无条件保留
	allOld := []orphanWorkDir{
		{Name: "a", ModTime: now.Add(-30 * 24 * time.Hour)},
		{Name: "b", ModTime: now.Add(-20 * 24 * time.Hour)},
		{Name: "c", ModTime: now.Add(-10 * 24 * time.Hour)},
	}
	if got := planOrphanWorkDirCleanup(allOld, now); len(got) != 0 {
		t.Fatalf("最近 3 个应无条件保留: %v", got)
	}
	if got := planOrphanWorkDirCleanup(nil, now); got != nil {
		t.Fatalf("空输入应返回 nil，实际 %v", got)
	}
}

func TestA06CleanupOrphanWorkDirs(t *testing.T) {
	oldCfg := config.Get()
	cfg := *oldCfg
	tempRoot := t.TempDir()
	cfg.TempDir = tempRoot
	config.Set(&cfg)
	t.Cleanup(func() { config.Set(oldCfg) })

	mgr := newBareTaskManager()
	// 活跃任务（未清理）目录必须保留；已清理任务目录不享受保护
	mgr.tasks["active-task"] = &Task{TaskID: "active-task", Status: StatusWaiting}
	mgr.tasks["cleaned-task"] = &Task{TaskID: "cleaned-task", Status: StatusSuccess, FileCleaned: true}
	withTestManager(t, mgr)

	adir := filepath.Join(tempRoot, "active-task")
	cdir := filepath.Join(tempRoot, "cleaned-task")
	paths := map[string]string{}
	for _, n := range []string{"active-task", "cleaned-task", "fresh-1", "fresh-2", "fresh-3", "old-8d", "old-10d"} {
		paths[n] = filepath.Join(tempRoot, n)
		if err := os.MkdirAll(paths[n], 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
	}
	old := time.Now().Add(-10 * 24 * time.Hour)
	for _, n := range []string{"active-task", "cleaned-task", "old-8d", "old-10d"} {
		if err := os.Chtimes(paths[n], old, old); err != nil {
			t.Fatalf("设置 mtime 失败: %v", err)
		}
	}

	cleanupOrphanWorkDirs()

	mustExist(t, adir) // 活跃任务目录无条件保留
	mustExist(t, paths["fresh-1"])
	mustExist(t, paths["fresh-2"])
	mustExist(t, paths["fresh-3"])
	mustNotExist(t, cdir)             // FileCleaned 任务目录按孤儿处理（已过期）
	mustNotExist(t, paths["old-8d"])  // 过期且不在最近 3 个
	mustNotExist(t, paths["old-10d"]) //
}
