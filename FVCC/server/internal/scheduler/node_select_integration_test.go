package scheduler

// P2-1 B轮：B-08 端到端集成测试（自动选机后派发成功）保留在根包调度域。
// 选机 / 健康分 / 熔断 / 能力落库的单元验收已随实现迁入 internal/node（node_test.go），
// 本文件仅保留涉及 handleRenderEDL 派发链路的端到端用例。

import (
	"testing"

	"fvcc/internal/remote"
	"fvcc/internal/store"
	"fvcc/internal/store/model"
	"fvcc/internal/ws"
)

func newB08EnvSched(t *testing.T) (*Scheduler, *store.Store) {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertServer(model.Server{ID: "srv1", Name: "节点一", Status: "online"})
	s.UpsertServer(model.Server{ID: "srv2", Name: "节点二", Status: "online"})
	s.UpsertServer(model.Server{ID: "srv_off", Name: "离线节点", Status: "offline"})
	return NewScheduler(s, remote.NewRemoteClient(), ws.NewHub(), nil), s
}

// TestB08AutoPickThenDispatch 端到端：自动选机后派发成功。
func TestB08AutoPickThenDispatch(t *testing.T) {
	sch, s := newB08EnvSched(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)
	s.UpsertNodeCaps(model.NodeCaps{ServerID: "srv1", AgentVersion: "1.0.0", MaxConcurrent: 1})

	// srv1 满槽 → 自动选机落到 srv2，任务进入 RUNNING 并落 server_id。
	s.UpsertTask(model.Task{ID: "t_other", OrderID: 1, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL, ServerID: "srv1"})
	s.UpsertTask(model.Task{ID: "t_auto", OrderID: 2, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		PayloadJSON: `{"sourceRoot":"a","destRoot":"b","profile":{"video":{"codec":"libx264"}}}`})
	sch.handleRenderEDL(mustTask(t, s, "t_auto"))

	got := mustTask(t, s, "t_auto")
	if got.Status != model.StatusTranscoding || got.ServerID != "srv2" || got.RemoteTaskID != "remote_edl_1" {
		t.Fatalf("自动选机应落到 srv2 并进入 RUNNING: %+v", got)
	}
	if d.edlCalls != 1 || d.lastServer.ID != "srv2" {
		t.Fatalf("下发通道应收到 srv2 任务: calls=%d server=%s", d.edlCalls, d.lastServer.ID)
	}
}
