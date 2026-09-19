package scheduler

// B-07 验收测试之二（06 §4.2 进度 loop / §6 事件聚合），P2-1 B轮由
// server/ws_progress_edl_test.go 第二、三部分迁入（随 scheduler 域收口）：
//   Scheduler.HandleRemoteProgress：渲染任务按 remote_task_id/本端 ID 反查 → 落库
//   stage/seg/outTimeMs/speed → 经 BroadcastTaskUpdateFull 聚合广播（含「第 x/y 段」文案）；
//   转码任务保持既有 (0,100] 且不回退语义；未匹配上报忽略。

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"fvcc/internal/remote"
	"fvcc/internal/store/model"
	"fvcc/internal/ws"
)

// ===== 广播出口录制器（替代真实 WS 连接，捕获事件载荷）=====

type wsEventRecorder struct {
	mu  sync.Mutex
	raw [][]byte
}

func newWSRecorder() *wsEventRecorder { return &wsEventRecorder{} }

func (r *wsEventRecorder) hook(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raw = append(r.raw, append([]byte(nil), b...))
}

func (r *wsEventRecorder) count(typ string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, b := range r.raw {
		var m map[string]interface{}
		if json.Unmarshal(b, &m) == nil && m["type"] == typ {
			n++
		}
	}
	return n
}

func (r *wsEventRecorder) events(t *testing.T, typ string) []map[string]interface{} {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]map[string]interface{}, 0)
	for _, b := range r.raw {
		var m map[string]interface{}
		if json.Unmarshal(b, &m) == nil && m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

func jsonNum(t *testing.T, m map[string]interface{}, key string) float64 {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("事件缺少字段 %s: %+v", key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("字段 %s 不是数值: %T(%v)", key, v, v)
	}
	return f
}

func jsonStr(t *testing.T, m map[string]interface{}, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("事件缺少字段 %s: %+v", key, m)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("字段 %s 不是字符串: %T(%v)", key, v, v)
	}
	return s
}

func newWSProgressHub() (*ws.Hub, *wsEventRecorder) {
	hub := ws.NewHub()
	rec := newWSRecorder()
	hub.SetEmitHook(rec.hook)
	return hub, rec
}

// ---------- 2) Scheduler.HandleRemoteProgress（渲染任务）----------

func TestB07HandleRemoteProgressRender(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	hub, rec := newWSProgressHub()
	sch.hub = hub

	s.UpsertTask(model.Task{
		ID: "t_edl", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "remote_edl_1", Progress: 0, TotalMs: 30000,
	})

	// 首帧：按 remote_task_id 反查 → 落库 → 立即广播（含「第 x/y 段」文案）
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{
		TaskID: "remote_edl_1", Progress: 10, Stage: model.StageSegment,
		SegIndex: 1, SegTotal: 5, OutTimeMs: 1000, TotalMs: 30000, Speed: "1.2x",
	})
	snap := mustTask(t, s, "t_edl")
	if snap.Progress != 10 || snap.Stage != model.StageSegment || snap.SegIndex != 1 || snap.SegTotal != 5 ||
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
	if got := jsonStr(t, evs[0], "stage"); got != model.StageSegment {
		t.Fatalf("stage 应为 %s，实际 %s", model.StageSegment, got)
	}
	if got := jsonNum(t, evs[0], "outTimeMs"); got != 1000 {
		t.Fatalf("outTimeMs 应为 1000，实际 %v", got)
	}
	if got := jsonStr(t, evs[0], "speed"); got != "1.2x" {
		t.Fatalf("speed 应为 1.2x，实际 %s", got)
	}

	// 500ms 窗口内：不追加发送；到期补发最新值
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{
		TaskID: "remote_edl_1", Progress: 25, Stage: model.StageSegment,
		SegIndex: 2, SegTotal: 5, OutTimeMs: 2500, Speed: "1.4x",
	})
	if got := rec.count("task_update"); got != 1 {
		t.Fatalf("聚合窗口内不应追加广播，实际 %d 条", got)
	}
	time.Sleep(ws.TaskUpdateAggInterval + 300*time.Millisecond)
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
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{
		TaskID: "remote_edl_1", Progress: 25, Stage: model.StageSegment,
		SegIndex: 2, SegTotal: 5, OutTimeMs: 2500, Speed: "1.4x",
	})
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("内容未变不应重复广播，实际 %d 条", got)
	}
	// 迟到低进度（不带分段信息）：progress 单调不回退，空值字段保持原值
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "remote_edl_1", Progress: 5})
	if snap := mustTask(t, s, "t_edl"); snap.Progress != 25 || snap.SegIndex != 2 || snap.Stage != model.StageSegment {
		t.Fatalf("进度不应回退且空值字段应保持: %+v", snap)
	}
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("迟到低进度不应产生新广播，实际 %d 条", got)
	}

	// 空 TaskID / 未匹配上报：忽略且不广播
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "", Progress: 60})
	sch.HandleRemoteProgress("srv2", remote.RemoteProgress{TaskID: "remote_unknown", Progress: 60})
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("未匹配上报不应广播，实际 %d 条", got)
	}

	// 收口任务不参与匹配：committed 终态后迟到上报被忽略
	s.UpdateTaskStatus("t_edl", model.StatusCompleted, 100, "")
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "remote_edl_1", Progress: 80, Stage: model.StageFinalize})
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

	s.UpsertTask(model.Task{
		ID: "t_tr", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeTranscode,
		ServerID: "srv1", Progress: 0,
	})

	// 合法进度：落库 + 广播
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "t_tr", Progress: 30, Stage: model.StageSegment})
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
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "t_tr", Progress: 20})
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "t_tr", Progress: 130})
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{TaskID: "t_tr", Progress: 0})
	if got := mustTask(t, s, "t_tr"); got.Progress != 30 {
		t.Fatalf("非法进度不应改写，实际 %v", got.Progress)
	}
	if got := rec.count("task_update"); got != 1 {
		t.Fatalf("非法进度不应广播，实际 %d 条", got)
	}

	// 转码任务不被渲染字段污染
	sch.HandleRemoteProgress("srv1", remote.RemoteProgress{
		TaskID: "t_tr", Progress: 50, Stage: model.StageSegment, SegIndex: 3, SegTotal: 6,
	})
	if got := mustTask(t, s, "t_tr"); got.Stage != "" || got.SegIndex != 0 || got.SegTotal != 0 {
		t.Fatalf("转码任务不应写入渲染阶段字段: %+v", got)
	}
}
