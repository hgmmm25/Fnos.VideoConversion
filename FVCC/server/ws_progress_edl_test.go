package main

// B-07 验收测试之二（06 §4.2 进度 loop / §6 事件聚合）：调度侧接通链路
//  1) Store.ApplyRenderProgress：进度单调不回退、空值字段保持、越界钳制、终态不收口覆盖、落盘；
//  2) Scheduler.HandleRemoteProgress：渲染任务按 remote_task_id/本端 ID 反查 → 落库 stage/seg/
//     outTimeMs/speed → 经 BroadcastTaskUpdateFull 聚合广播（含「第 x/y 段」文案）；
//     转码任务保持既有 (0,100] 且不回退语义；未匹配上报忽略。

import (
	"fvcc/internal/store"
	"testing"
	"time"
)

func newWSProgressHub() (*Hub, *wsEventRecorder) {
	hub := NewHub()
	rec := newWSRecorder()
	hub.SetEmitHook(rec.hook)
	return hub, rec
}

// ---------- 1) Store.ApplyRenderProgress ----------

func TestB07ApplyRenderProgressStore(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertTask(Task{
		ID: "t_edl", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "remote_edl_1", Progress: 10, Stage: StagePrepare,
	})
	s.UpsertTask(Task{
		ID: "t_done", OrderID: 2, Status: StatusCompleted, TaskType: TaskTypeRenderEDL,
		ServerID: "srv1", Progress: 100, Stage: StageFinalize,
	})

	// 首次上报：全量字段落库
	got, ok := s.ApplyRenderProgress("t_edl", store.RenderProgress{
		Progress: 40, Stage: StageSegment, SegIndex: 2, SegTotal: 4,
		OutTimeMs: 2000, TotalMs: 8000, Speed: "1.5x",
	})
	if !ok {
		t.Fatal("进行中任务应命中")
	}
	if got.Progress != 40 || got.Stage != StageSegment || got.SegIndex != 2 || got.SegTotal != 4 ||
		got.OutTimeMs != 2000 || got.TotalMs != 8000 || got.Speed != "1.5x" {
		t.Fatalf("全量进度落库错误: %+v", got)
	}

	// 单调不回退：迟到的低进度被忽略，其余同帧字段保持
	got, ok = s.ApplyRenderProgress("t_edl", store.RenderProgress{Progress: 15})
	if !ok || got.Progress != 40 {
		t.Fatalf("进度不应回退，实际 %v (ok=%v)", got.Progress, ok)
	}

	// 空值保持：stage/seg/speed 为空或 <=0 时不清空既有值
	got, _ = s.ApplyRenderProgress("t_edl", store.RenderProgress{Progress: 40, Stage: "", SegIndex: 0, OutTimeMs: 0, Speed: ""})
	if got.Stage != StageSegment || got.SegIndex != 2 || got.OutTimeMs != 2000 || got.Speed != "1.5x" {
		t.Fatalf("空值字段不应清空既有进度: %+v", got)
	}

	// 越界钳制：>100 → 100，<0 → 0（此处任务已为 40，低位钳制不产生回退）
	got, _ = s.ApplyRenderProgress("t_edl", store.RenderProgress{Progress: 150})
	if got.Progress != 100 {
		t.Fatalf("进度上界应钳制为 100，实际 %v", got.Progress)
	}

	// 终态任务不被进度上报覆盖
	before, _ := s.GetTask("t_done")
	got, ok = s.ApplyRenderProgress("t_done", store.RenderProgress{Progress: 50, Stage: StageSegment})
	if ok {
		t.Fatalf("终态任务不应命中进度落库: %+v", got)
	}
	after, _ := s.GetTask("t_done")
	if after.Progress != before.Progress || after.Stage != before.Stage {
		t.Fatalf("终态任务被进度上报改写: %+v → %+v", before, after)
	}

	// 不存在任务
	if _, ok := s.ApplyRenderProgress("t_missing", store.RenderProgress{Progress: 10}); ok {
		t.Fatal("不存在的任务不应命中")
	}

	// 落盘校验：从同一目录重新加载（06 §2.2）。
	// 注意 Store.Load 的启动崩溃恢复语义：非终态的中断任务会被重置为 QUEUE
	// （Status/Progress/RemoteTaskID/UploadChunkIdx/DownloadOffset 清零）；
	// 而 B-07 新增的渲染进度列属展示性进度，重载后原样保留。
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("二次 Load() 失败: %v", err)
	}
	reloaded, ok := s2.GetTask("t_edl")
	if !ok {
		t.Fatal("重载后任务 t_edl 丢失")
	}
	if reloaded.Stage != StageSegment || reloaded.SegIndex != 2 || reloaded.SegTotal != 4 ||
		reloaded.OutTimeMs != 2000 || reloaded.TotalMs != 8000 || reloaded.Speed != "1.5x" {
		t.Fatalf("渲染进度列未正确落盘: %+v", reloaded)
	}
	if reloaded.Status != StatusQueue || reloaded.Progress != 0 || reloaded.RemoteTaskID != "" {
		t.Fatalf("中断任务应被崩溃恢复重置为 QUEUE: %+v", reloaded)
	}
	done, _ := s2.GetTask("t_done")
	if done.Status != StatusCompleted || done.Progress != 100 {
		t.Fatalf("终态任务不应被崩溃恢复改写: %+v", done)
	}
}

// ---------- 2) Scheduler.HandleRemoteProgress（渲染任务）----------

func TestB07HandleRemoteProgressRender(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	hub, rec := newWSProgressHub()
	sch.hub = hub

	s.UpsertTask(Task{
		ID: "t_edl", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "remote_edl_1", Progress: 0, TotalMs: 30000,
	})

	// 首帧：按 remote_task_id 反查 → 落库 → 立即广播（含「第 x/y 段」文案）
	sch.HandleRemoteProgress("srv1", RemoteProgress{
		TaskID: "remote_edl_1", Progress: 10, Stage: StageSegment,
		SegIndex: 1, SegTotal: 5, OutTimeMs: 1000, TotalMs: 30000, Speed: "1.2x",
	})
	snap := mustTask(t, s, "t_edl")
	if snap.Progress != 10 || snap.Stage != StageSegment || snap.SegIndex != 1 || snap.SegTotal != 5 ||
		snap.OutTimeMs != 1000 || snap.Speed != "1.2x" {
		t.Fatalf("进度落库错误: %+v", snap)
	}

	evs := rec.events(t, "task_update")
	if len(evs) != 1 {
		t.Fatalf("首帧应立即广播 1 条，实际 %d 条", len(evs))
	}
	if got := jsonStr(t, evs[0], "status"); got != "RUNNING" {
		t.Fatalf("status 应为 RUNNING，实际 %s", got)
	}
	if got := jsonStr(t, evs[0], "message"); got != "第 1/5 段" {
		t.Fatalf("message 应为「第 1/5 段」，实际 %s", got)
	}
	if got := jsonStr(t, evs[0], "stage"); got != StageSegment {
		t.Fatalf("stage 应为 %s，实际 %s", StageSegment, got)
	}
	if got := jsonNum(t, evs[0], "outTimeMs"); got != 1000 {
		t.Fatalf("outTimeMs 应为 1000，实际 %v", got)
	}
	if got := jsonStr(t, evs[0], "speed"); got != "1.2x" {
		t.Fatalf("speed 应为 1.2x，实际 %s", got)
	}

	// 500ms 窗口内：不追加发送；到期补发最新值
	sch.HandleRemoteProgress("srv1", RemoteProgress{
		TaskID: "remote_edl_1", Progress: 25, Stage: StageSegment,
		SegIndex: 2, SegTotal: 5, OutTimeMs: 2500, Speed: "1.4x",
	})
	if got := rec.count("task_update"); got != 1 {
		t.Fatalf("聚合窗口内不应追加广播，实际 %d 条", got)
	}
	time.Sleep(taskUpdateAggInterval + 300*time.Millisecond)
	evs = rec.events(t, "task_update")
	if len(evs) != 2 {
		t.Fatalf("窗口到期应补发最新值，实际 %d 条", len(evs))
	}
	last := evs[len(evs)-1]
	if got := jsonNum(t, last, "progress"); got != 25 {
		t.Fatalf("补发进度应为 25，实际 %v", got)
	}
	if got := jsonStr(t, last, "message"); got != "第 2/5 段" {
		t.Fatalf("补发文案应为「第 2/5 段」，实际 %s", got)
	}
	if snap := mustTask(t, s, "t_edl"); snap.SegIndex != 2 || snap.OutTimeMs != 2500 || snap.Speed != "1.4x" {
		t.Fatalf("窗口内进度未落库: %+v", snap)
	}

	// 迟到低进度：落库不回退，且广播内容无变化时不重复广播
	sch.HandleRemoteProgress("srv1", RemoteProgress{
		TaskID: "remote_edl_1", Progress: 25, Stage: StageSegment,
		SegIndex: 2, SegTotal: 5, OutTimeMs: 2500, Speed: "1.4x",
	})
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("内容未变不应重复广播，实际 %d 条", got)
	}
	// 迟到低进度（不带分段信息）：progress 单调不回退，空值字段保持原值
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "remote_edl_1", Progress: 5})
	if snap := mustTask(t, s, "t_edl"); snap.Progress != 25 || snap.SegIndex != 2 || snap.Stage != StageSegment {
		t.Fatalf("进度不应回退且空值字段应保持: %+v", snap)
	}
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("迟到低进度不应产生新广播，实际 %d 条", got)
	}

	// 空 TaskID / 未匹配上报：忽略且不广播
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "", Progress: 60})
	sch.HandleRemoteProgress("srv2", RemoteProgress{TaskID: "remote_unknown", Progress: 60})
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("未匹配上报不应广播，实际 %d 条", got)
	}

	// 收口任务不参与匹配：committed 终态后迟到上报被忽略
	s.UpdateTaskStatus("t_edl", StatusCompleted, 100, "")
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "remote_edl_1", Progress: 80, Stage: StageFinalize})
	if snap := mustTask(t, s, "t_edl"); snap.Progress != 100 {
		t.Fatalf("终态任务不应被进度上报改写: %+v", snap)
	}
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("终态任务的迟到上报不应广播，实际 %d 条", got)
	}
}

// ---------- 3) Scheduler.HandleRemoteProgress（转码任务，既有语义）----------

func TestB07HandleRemoteProgressTranscode(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	hub, rec := newWSProgressHub()
	sch.hub = hub

	s.UpsertTask(Task{
		ID: "t_tr", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeTranscode,
		ServerID: "srv1", Progress: 0,
	})

	// 合法进度：落库 + 广播
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "t_tr", Progress: 30, Stage: StageSegment})
	if got := mustTask(t, s, "t_tr"); got.Progress != 30 {
		t.Fatalf("转码进度应为 30，实际 %v", got.Progress)
	}
	evs := rec.events(t, "task_update")
	if len(evs) != 1 {
		t.Fatalf("合法进度应广播 1 条，实际 %d 条", len(evs))
	}
	if got := jsonNum(t, evs[0], "progress"); got != 30 {
		t.Fatalf("广播进度应为 30，实际 %v", got)
	}

	// 回退 / 越界 / 非正进度：忽略
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "t_tr", Progress: 20})
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "t_tr", Progress: 130})
	sch.HandleRemoteProgress("srv1", RemoteProgress{TaskID: "t_tr", Progress: 0})
	if got := mustTask(t, s, "t_tr"); got.Progress != 30 {
		t.Fatalf("非法进度不应改写，实际 %v", got.Progress)
	}
	if got := rec.count("task_update"); got != 1 {
		t.Fatalf("非法进度不应广播，实际 %d 条", got)
	}

	// 转码任务不被渲染字段污染
	sch.HandleRemoteProgress("srv1", RemoteProgress{
		TaskID: "t_tr", Progress: 50, Stage: StageSegment, SegIndex: 3, SegTotal: 6,
	})
	if got := mustTask(t, s, "t_tr"); got.Stage != "" || got.SegIndex != 0 || got.SegTotal != 0 {
		t.Fatalf("转码任务不应写入渲染阶段字段: %+v", got)
	}
}
