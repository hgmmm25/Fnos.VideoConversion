package store

// B-07 验收测试之二（06 §4.2 进度 loop），P2-1 B轮由 server/ws_progress_edl_test.go
// 第一部分迁入：Store.ApplyRenderProgress 进度单调不回退、空值字段保持、越界钳制、
// 终态不收口覆盖、落盘。

import (
	"testing"

	"fvcc/internal/store/model"
)

func TestB07ApplyRenderProgressStore(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertTask(model.Task{
		ID: "t_edl", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "remote_edl_1", Progress: 10, Stage: model.StagePrepare,
	})
	s.UpsertTask(model.Task{
		ID: "t_done", OrderID: 2, Status: model.StatusCompleted, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", Progress: 100, Stage: model.StageFinalize,
	})

	// 首次上报：全量字段落库
	got, ok := s.ApplyRenderProgress("t_edl", RenderProgress{
		Progress: 40, Stage: model.StageSegment, SegIndex: 2, SegTotal: 4,
		OutTimeMs: 2000, TotalMs: 8000, Speed: "1.5x",
	})
	if !ok {
		t.Fatal("进行中任务应命中")
	}
	if got.Progress != 40 || got.Stage != model.StageSegment || got.SegIndex != 2 || got.SegTotal != 4 ||
		got.OutTimeMs != 2000 || got.TotalMs != 8000 || got.Speed != "1.5x" {
		t.Fatalf("全量进度落库错误: %+v", got)
	}

	// 单调不回退：迟到的低进度被忽略，其余同帧字段保持
	got, ok = s.ApplyRenderProgress("t_edl", RenderProgress{Progress: 15})
	if !ok || got.Progress != 40 {
		t.Fatalf("进度不应回退，实际 %v (ok=%v)", got.Progress, ok)
	}

	// 空值保持：stage/seg/speed 为空或 <=0 时不清空既有值
	got, _ = s.ApplyRenderProgress("t_edl", RenderProgress{Progress: 40, Stage: "", SegIndex: 0, OutTimeMs: 0, Speed: ""})
	if got.Stage != model.StageSegment || got.SegIndex != 2 || got.OutTimeMs != 2000 || got.Speed != "1.5x" {
		t.Fatalf("空值字段不应清空既有进度: %+v", got)
	}

	// 越界钳制：>100 → 100，<0 → 0（此处任务已为 40，低位钳制不产生回退）
	got, _ = s.ApplyRenderProgress("t_edl", RenderProgress{Progress: 150})
	if got.Progress != 100 {
		t.Fatalf("进度上界应钳制为 100，实际 %v", got.Progress)
	}

	// 终态任务不被进度上报覆盖
	before, _ := s.GetTask("t_done")
	got, ok = s.ApplyRenderProgress("t_done", RenderProgress{Progress: 50, Stage: model.StageSegment})
	if ok {
		t.Fatalf("终态任务不应命中进度落库: %+v", got)
	}
	after, _ := s.GetTask("t_done")
	if after.Progress != before.Progress || after.Stage != before.Stage {
		t.Fatalf("终态任务被进度上报改写: %+v → %+v", before, after)
	}

	// 不存在任务
	if _, ok := s.ApplyRenderProgress("t_missing", RenderProgress{Progress: 10}); ok {
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
	if reloaded.Stage != model.StageSegment || reloaded.SegIndex != 2 || reloaded.SegTotal != 4 ||
		reloaded.OutTimeMs != 2000 || reloaded.TotalMs != 8000 || reloaded.Speed != "1.5x" {
		t.Fatalf("渲染进度列未正确落盘: %+v", reloaded)
	}
	if reloaded.Status != model.StatusQueue || reloaded.Progress != 0 || reloaded.RemoteTaskID != "" {
		t.Fatalf("中断任务应被崩溃恢复重置为 QUEUE: %+v", reloaded)
	}
	done, _ := s2.GetTask("t_done")
	if done.Status != model.StatusCompleted || done.Progress != 100 {
		t.Fatalf("终态任务不应被崩溃恢复改写: %+v", done)
	}
}
