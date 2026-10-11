package scheduler

// RENDER_EDL 终态轮询收口（06 §4.2）：WS 只推进度，终态靠 QueryTaskDetail 轮询落地。
// 本文件覆盖不依赖真实节点的收口分支：成功落库、缺 ID 空转、节点消失的不可重试失败，
// 以及轮询窗口节流。（Success/Failed/Cancelled 的 QueryTaskDetail 结果分支需真实 WS 或
// httptest 服务端，交由真机复验收口；此处与既有 checkTranscodeProgress/proxyWf.Poll 口径一致。）

import (
	"testing"
	"time"

	"fvcc/internal/store/model"
)

// TestCompleteRenderTaskSuccess RENDER_EDL 成功收口：置 COMPLETED + 进度 100。
func TestCompleteRenderTaskSuccess(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	s.UpsertTask(model.Task{
		ID: "t_ok", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "remote_edl_1", Progress: 100, Stage: "finalize",
	})

	sch.completeRenderTask(mustTask(t, s, "t_ok"))

	got := mustTask(t, s, "t_ok")
	if got.Status != model.StatusCompleted {
		t.Fatalf("成功收口应为 COMPLETED，实际 %s", got.Status)
	}
	if got.Progress != 100 {
		t.Fatalf("成功收口进度应锁定 100，实际 %v", got.Progress)
	}
	// 轮询窗口状态应被清理，避免 sync.Map 泄漏。
	if _, ok := sch.renderPollAt.Load("t_ok"); ok {
		t.Fatal("完成后应清除 renderPollAt 窗口记录")
	}
}

// TestPollRenderTerminalNoopWithoutRemoteID 缺 RemoteTaskID/ServerID 时直接空转，不改状态。
func TestPollRenderTerminalNoopWithoutRemoteID(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	s.UpsertTask(model.Task{
		ID: "t_noid", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", // RemoteTaskID 空
	})

	sch.pollRenderTerminal(mustTask(t, s, "t_noid"))

	if got := mustTask(t, s, "t_noid"); got.Status != model.StatusTranscoding {
		t.Fatalf("缺 remoteTaskId 应空转保持 RUNNING，实际 %s", got.Status)
	}
}

// TestPollRenderTerminalNodeGoneFailsPermanent 节点记录已消失 → 不可重试失败（E_NODE_NOT_FOUND）。
func TestPollRenderTerminalNodeGoneFailsPermanent(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	s.UpsertTask(model.Task{
		ID: "t_ghost", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv_missing", RemoteTaskID: "remote_edl_1",
	})

	sch.pollRenderTerminal(mustTask(t, s, "t_ghost"))

	got := mustTask(t, s, "t_ghost")
	if got.Status != model.StatusError {
		t.Fatalf("节点消失应转 ERROR，实际 %s", got.Status)
	}
	if got.RetryType != model.NonRetryable {
		t.Fatalf("节点消失应判不可重试，实际 %s", got.RetryType)
	}
}

// TestPollRenderTerminalWindowThrottled 窗口内二次轮询应被节流跳过（不打 QueryTaskDetail）。
// srv1 在线但无真实 WS 连接：若未被节流会走到 QueryTaskDetail 报错分支（不改状态，难区分），
// 故以 renderPollAt 命中 + 提前返回作为节流判据。
func TestPollRenderTerminalWindowThrottled(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	s.UpsertTask(model.Task{
		ID: "t_thr", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "remote_edl_1",
	})
	// 预置"刚刚轮询过"，下一次调用应落在窗口内直接返回。
	sch.renderPollAt.Store("t_thr", time.Now())

	sch.pollRenderTerminal(mustTask(t, s, "t_thr"))

	if got := mustTask(t, s, "t_thr"); got.Status != model.StatusTranscoding {
		t.Fatalf("窗口内应跳过轮询保持 RUNNING，实际 %s", got.Status)
	}
	// 窗口命中路径不应误清理计时记录（仍保留上次时刻）。
	if _, ok := sch.renderPollAt.Load("t_thr"); !ok {
		t.Fatal("节流命中不应清除 renderPollAt，破坏窗口语义")
	}
}
