package scheduler

// B-05 验收测试（06 §4.1 分流 / §4.3 冷却退避 / §4.5 锁）：
//  1) 分流：QUEUE 的 RENDER_EDL / GEN_PROXY 走渲染派发，TRANSCODE 仍走既有路径；
//  2) 优先级：GEN_PROXY 强制低优，不得插到 RenderEDL/Transcode 之前，同档位按 OrderID；
//  3) 退避序列：连续失败得到 30/60/120/240/480s，冷却到期转回 QUEUE 且 OrderID 不变，
//     达上限（5 次）后转 FAILED 并由 handleError 归档；
//  4) 错误码白名单：E_EDL_INVALID / E_ASSET_MISSING / E_DISK_FULL / E_FFMPEG_MISSING 直接失败；
//  5) 节点校验：离线节点冷却重试、节点不存在直接失败、未指定节点自动选机（B-08）；
//  6) 侧门：载荷缺失直接失败、下发通道未注入（B-06 未到）时保持 QUEUE、锁派发后立即释放。

import (
	"errors"
	"strings"
	"testing"
	"time"

	"fvcc/internal/edl"
	"fvcc/internal/remote"
	"fvcc/internal/store"
	"fvcc/internal/store/model"
	"fvcc/internal/ws"
)

// fakeDispatcher 记录下发调用，按需返回注入错误。
type fakeDispatcher struct {
	edlCalls   int
	proxyCalls int
	lastServer model.Server
	lastTask   model.Task
	err        error
}

func (f *fakeDispatcher) CreateRenderEDL(server model.Server, t model.Task) (string, error) {
	return f.CreateRenderEDLWithTrace(server, t, t.TraceID)
}

func (f *fakeDispatcher) CreateGenProxy(server model.Server, t model.Task) (string, error) {
	return f.CreateGenProxyWithTrace(server, t, t.TraceID)
}

func (f *fakeDispatcher) CreateRenderEDLWithTrace(server model.Server, t model.Task, traceID string) (string, error) {
	f.edlCalls++
	f.lastServer, f.lastTask = server, t
	if f.err != nil {
		return "", f.err
	}
	return "remote_edl_1", nil
}

func (f *fakeDispatcher) CreateGenProxyWithTrace(server model.Server, t model.Task, traceID string) (string, error) {
	f.proxyCalls++
	f.lastServer, f.lastTask = server, t
	if f.err != nil {
		return "", f.err
	}
	return "remote_proxy_1", nil
}

func newSchedTestEnv(t *testing.T) (*Scheduler, *store.Store) {
	t.Helper()
	s := store.NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertServer(model.Server{ID: "srv1", Name: "节点一", Status: "online"})
	s.UpsertServer(model.Server{ID: "srv_off", Name: "离线节点", Status: "offline"})
	return NewScheduler(s, remote.NewRemoteClient(), ws.NewHub(), nil), s
}

func mustTask(t *testing.T, s *store.Store, id string) model.Task {
	t.Helper()
	got, ok := s.GetTask(id)
	if !ok {
		t.Fatalf("任务 %s 不存在", id)
	}
	return got
}

// ---------- 1) 分流与派发成功 ----------

func TestB05RenderDispatchSuccess(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)

	s.UpsertTask(model.Task{
		ID: "t_edl", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", PayloadJSON: `{"clips":[],"out":"a.mp4"}`, ProjectID: "p_1", Checksum: "abc", TotalMs: 9000,
	})

	sch.handleRenderEDL(mustTask(t, s, "t_edl"))

	got := mustTask(t, s, "t_edl")
	if got.Status != model.StatusTranscoding {
		t.Fatalf("派发成功应进入 RUNNING(model.StatusTranscoding)，实际 %s", got.Status)
	}
	if got.Status.WireName() != "RUNNING" {
		t.Fatalf("线协议状态应为 RUNNING，实际 %s", got.Status.WireName())
	}
	if got.ServerID != "srv1" || got.ServerName != "节点一" {
		t.Fatalf("server_id/server_name 未落库: %+v", got)
	}
	if got.RemoteTaskID != "remote_edl_1" {
		t.Fatalf("remoteTaskId 未落库: %q", got.RemoteTaskID)
	}
	if got.Stage != model.StagePrepare {
		t.Fatalf("进入 RUNNING 应置 stage=prepare，实际 %q", got.Stage)
	}
	if got.CoolDownUntil != nil || got.CoolDownSec != 0 || got.CooldownReason != "" {
		t.Fatalf("派发成功必须清空 next_retry_at/cooldown：%+v", got)
	}
	if d.edlCalls != 1 || d.proxyCalls != 0 {
		t.Fatalf("应且仅应调用 CreateRenderEDL 一次: edl=%d proxy=%d", d.edlCalls, d.proxyCalls)
	}
	if d.lastTask.PayloadJSON == "" || d.lastTask.TaskType != model.TaskTypeRenderEDL {
		t.Fatalf("下发载荷不应为空且须带 tasks 快照: %+v", d.lastTask)
	}
	// 06 §4.5：进入 RUNNING 后锁立即释放（可被其它任务再次获取）。
	if !s.AcquireTransLock("srv1", "t_probe", 30) {
		t.Fatalf("派发完成后转码锁未释放")
	}
	s.ReleaseTransLock("srv1", "t_probe")
}

func TestB05GenProxyDispatchAndLowPriority(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)

	s.UpsertTask(model.Task{ID: "t_proxy", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeGenProxy,
		ServerID: "srv1", PayloadJSON: `{"assetId":"a1"}`})
	sch.handleGenProxy(mustTask(t, s, "t_proxy"))

	if got := mustTask(t, s, "t_proxy"); got.Status != model.StatusTranscoding || got.RemoteTaskID != "remote_proxy_1" {
		t.Fatalf("GEN_PROXY 派发失败: %+v", got)
	}
	if d.proxyCalls != 1 || d.edlCalls != 0 {
		t.Fatalf("GEN_PROXY 应走 CreateGenProxy: edl=%d proxy=%d", d.edlCalls, d.proxyCalls)
	}

	// 优先级：代理任务 OrderID 最小（100），但档位最低，不得插到 RenderEDL/TRANSCODE 之前。
	// P1-1：nextQueueTask 只做只读查询（占位防重已移入 processTask），
	// 连续调用需用 processing.Store 模拟"上一任务已在处理"，才能取到下一个可推进任务。
	queued := sortQueueTasks([]model.Task{
		{ID: "t_proxy", OrderID: 100, Status: model.StatusQueue, TaskType: model.TaskTypeGenProxy},
		{ID: "t_render", OrderID: 200, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL},
		{ID: "t_trans", OrderID: 300, Status: model.StatusQueue, TaskType: model.TaskTypeTranscode},
	})
	first, ok := sch.nextQueueTask(queued)
	if !ok || first.ID != "t_render" {
		t.Fatalf("首个可推进任务应为 RENDER_EDL(OrderID=200)，实际 %q", first.ID)
	}
	sch.processing.Store(first.ID, true)
	second, ok := sch.nextQueueTask(queued)
	if !ok || second.ID != "t_trans" {
		t.Fatalf("第二个应为 TRANSCODE(OrderID=300)，实际 %q", second.ID)
	}
	sch.processing.Store(second.ID, true)
	third, ok := sch.nextQueueTask(queued)
	if !ok || third.ID != "t_proxy" {
		t.Fatalf("代理任务应最后被调度，实际 %q", third.ID)
	}

	// 同档位按 OrderID 升序。
	sameRank := sortQueueTasks([]model.Task{
		{ID: "t_b", OrderID: 20, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL},
		{ID: "t_a", OrderID: 10, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL},
	})
	picked, ok := sch.nextQueueTask(sameRank)
	if !ok || picked.ID != "t_a" {
		t.Fatalf("同档位应按 OrderID 升序，实际 %q", picked.ID)
	}
	sch.processing.Store(picked.ID, true)
	if picked2, ok2 := sch.nextQueueTask(sameRank); !ok2 || picked2.ID != "t_b" {
		t.Fatalf("同档位按 OrderID 升序的次个应为 t_b，实际 %q", picked2.ID)
	}
}

// ---------- 2) 冷却退避序列与到期提升 ----------

func TestB05CooldownBackoffSequence(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	d := &fakeDispatcher{err: errors.New("E_NODE_OFFLINE: 节点心跳超时")}
	sch.SetRenderDispatcher(d)

	s.UpsertTask(model.Task{ID: "t_bk", OrderID: 42, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", PayloadJSON: `{"clips":[]}`})

	wantCool := []int{30, 60, 120, 240, 480} // 06 §4.3：min(30×2^attempt, 600)
	for i, want := range wantCool {
		t0 := time.Now()
		sch.handleRenderEDL(mustTask(t, s, "t_bk"))
		got := mustTask(t, s, "t_bk")

		if got.Status != model.StatusCooldown || got.Status.WireName() != "COOLDOWN" {
			t.Fatalf("第 %d 次失败应进入 COOLDOWN，实际 %s", i+1, got.Status)
		}
		if got.CoolDownSec != want {
			t.Fatalf("第 %d 次失败退避应为 %ds，实际 %ds", i+1, want, got.CoolDownSec)
		}
		if got.RetryCount != i+1 {
			t.Fatalf("attempt 应为 %d，实际 %d", i+1, got.RetryCount)
		}
		if got.CooldownReason != edl.ErrCodeNodeOffline {
			t.Fatalf("cooldown_reason 应为 %s，实际 %s", edl.ErrCodeNodeOffline, got.CooldownReason)
		}
		if got.OrderID != 42 {
			t.Fatalf("冷却期间 OrderID 不得改变，实际 %d", got.OrderID)
		}
		if got.CoolDownUntil == nil || got.CoolDownUntil.Before(t0.Add(time.Duration(want-1)*time.Second)) {
			t.Fatalf("next_retry_at 未按退避设置: %+v", got.CoolDownUntil)
		}

		// 未到期不得转回 QUEUE。
		sch.promoteCooldownTasks(time.Now())
		if still := mustTask(t, s, "t_bk"); still.Status != model.StatusCooldown {
			t.Fatalf("未到期任务不应转回 QUEUE，实际 %s", still.Status)
		}

		// 冷却到期 → QUEUE，保留原 OrderID（06 §4.3）。
		past := time.Now().Add(-time.Second)
		got.CoolDownUntil = &past
		s.UpsertTask(got)
		sch.promoteCooldownTasks(time.Now())
		due := mustTask(t, s, "t_bk")
		if due.Status != model.StatusQueue {
			t.Fatalf("冷却到期应转回 QUEUE，实际 %s", due.Status)
		}
		if due.CoolDownUntil != nil || due.CoolDownSec != 0 {
			t.Fatalf("转回 QUEUE 应清空 next_retry_at，实际 %+v", due)
		}
		if due.OrderID != 42 {
			t.Fatalf("转回 QUEUE 必须保留原 OrderID，实际 %d", due.OrderID)
		}
	}

	// 第 6 次失败：超过 MaxRetry(5) → 直接 FAILED，不再进入冷却。
	sch.handleRenderEDL(mustTask(t, s, "t_bk"))
	over := mustTask(t, s, "t_bk")
	if over.Status != model.StatusError || over.Status.WireName() != "FAILED" {
		t.Fatalf("重试次数用尽应转 FAILED，实际 %s", over.Status)
	}
	if over.RetryType != model.NonRetryable || over.CoolDownUntil != nil {
		t.Fatalf("重试用尽后应为不可重试且无冷却: %+v", over)
	}
	if !strings.Contains(over.ErrorMsg, "重试次数用尽") {
		t.Fatalf("失败原因应说明重试次数用尽: %s", over.ErrorMsg)
	}
	// 收口：既有 handleError 负责归档，B-05 不重复实现。
	sch.handleError(over)
	if _, ok := s.GetTask("t_bk"); ok {
		t.Fatalf("FAILED 任务应由 handleError 归档到历史")
	}
}

// ---------- 3) 错误码白名单 ----------

func TestB05ErrorCodeWhitelist(t *testing.T) {
	nonRetryable := []string{edl.ErrCodeEDLInvalid, edl.ErrCodeAssetMissing, errCodeDiskFull, errCodeFFmpegMissing}
	for _, code := range nonRetryable {
		sch, s := newSchedTestEnv(t)
		sch.SetRenderDispatcher(&fakeDispatcher{err: errors.New(code + ": 用例构造")})
		s.UpsertTask(model.Task{ID: "t_nr", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
			ServerID: "srv1", PayloadJSON: `{"clips":[]}`})

		sch.handleRenderEDL(mustTask(t, s, "t_nr"))
		got := mustTask(t, s, "t_nr")
		if got.Status != model.StatusError || got.RetryType != model.NonRetryable {
			t.Fatalf("%s 应直接失败，实际 status=%s retry=%s", code, got.Status, got.RetryType)
		}
		if got.RetryCount != 0 || got.CoolDownUntil != nil || got.CoolDownSec != 0 {
			t.Fatalf("%s 不计入重试且不得设冷却: %+v", code, got)
		}
		if got.CooldownReason != code {
			t.Fatalf("%s 应记录 cooldown_reason，实际 %s", code, got.CooldownReason)
		}
	}

	// 白名单内错误 → 冷却重试；未识别错误归一为 E_RENDER_FAILED（仍可重试）。
	for _, tc := range []struct{ err, wantCode string }{
		{"E_SMB_MOUNT_FAILED: 挂载失败", remote.ErrCodeSMBMountFailed},
		{"E_TIMEOUT_STALL: 无进度", errCodeTimeoutStall},
		{"E_RENDER_FAILED: ffmpeg 非零退出", remote.ErrCodeRenderFailed},
		{"网络抖动", remote.ErrCodeRenderFailed},
	} {
		code, retryable := classifyRenderError(errors.New(tc.err))
		if code != tc.wantCode || !retryable {
			t.Fatalf("错误 %q 应归类为可重试 %s，实际 %s/%v", tc.err, tc.wantCode, code, retryable)
		}
	}
}

// ---------- 4) 节点校验与侧门 ----------

func TestB05NodeGuards(t *testing.T) {
	// 离线节点 → 冷却重试，且不触碰下发通道。
	sch, s := newSchedTestEnv(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)
	s.UpsertTask(model.Task{ID: "t_off", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv_off", PayloadJSON: `{"clips":[]}`})
	sch.handleRenderEDL(mustTask(t, s, "t_off"))
	off := mustTask(t, s, "t_off")
	if off.Status != model.StatusCooldown || off.CoolDownSec != 30 || off.CooldownReason != edl.ErrCodeNodeOffline {
		t.Fatalf("离线节点应冷却 30s(E_NODE_OFFLINE): %+v", off)
	}
	if d.edlCalls != 0 {
		t.Fatalf("节点离线不得调用下发通道")
	}

	// 节点不存在 → 直接失败（不可重试）。
	s.UpsertTask(model.Task{ID: "t_ghost", OrderID: 2, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv_missing", PayloadJSON: `{"clips":[]}`})
	sch.handleRenderEDL(mustTask(t, s, "t_ghost"))
	ghost := mustTask(t, s, "t_ghost")
	if ghost.Status != model.StatusError || ghost.RetryCount != 0 || ghost.CooldownReason != errCodeNodeNotFound {
		t.Fatalf("节点不存在应直接失败(E_NODE_NOT_FOUND): %+v", ghost)
	}

	// 未指定节点 → B-08 自动选机（在线且空闲的 srv1），落 server_id 并进入 RUNNING。
	s.UpsertTask(model.Task{ID: "t_noserver", OrderID: 3, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		PayloadJSON: `{"clips":[]}`})
	sch.handleRenderEDL(mustTask(t, s, "t_noserver"))
	auto := mustTask(t, s, "t_noserver")
	if auto.Status != model.StatusTranscoding || auto.ServerID != "srv1" || auto.ServerName != "节点一" {
		t.Fatalf("未指定节点应自动选机到 srv1 并进入 RUNNING: %+v", auto)
	}
	if auto.Status.WireName() != "RUNNING" || auto.RemoteTaskID != "remote_edl_1" {
		t.Fatalf("自动选机后应落线协议 RUNNING 与 remoteTaskId: %+v", auto)
	}
	if d.edlCalls != 1 {
		t.Fatalf("自动选机后应调用一次下发通道，实际 %d", d.edlCalls)
	}
}

func TestB05SideGuards(t *testing.T) {
	// 1) 载荷缺失 → 直接失败，不计入重试。
	sch, s := newSchedTestEnv(t)
	sch.SetRenderDispatcher(&fakeDispatcher{})
	s.UpsertTask(model.Task{ID: "t_nopayload", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL, ServerID: "srv1"})
	sch.handleRenderEDL(mustTask(t, s, "t_nopayload"))
	empty := mustTask(t, s, "t_nopayload")
	if empty.Status != model.StatusError || empty.RetryCount != 0 || empty.CooldownReason != remote.ErrCodePayloadMissing {
		t.Fatalf("载荷缺失应直接失败(E_PAYLOAD_MISSING): %+v", empty)
	}

	// 2) processTask 分流：COOLDOWN / RUNNING(渲染) 空转，QUEUE(GEN_PROXY) 正常派发。
	sch2, s2 := newSchedTestEnv(t)
	d := &fakeDispatcher{}
	sch2.SetRenderDispatcher(d)
	s2.UpsertTask(model.Task{ID: "t_wait", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", PayloadJSON: `{"clips":[]}`})
	sch2.processTask(model.Task{ID: "t_cd", Status: model.StatusCooldown, TaskType: model.TaskTypeRenderEDL})
	if d.edlCalls != 0 || d.proxyCalls != 0 {
		t.Fatalf("COOLDOWN 任务不应触发下发")
	}
	sch2.processTask(model.Task{ID: "t_run", Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL})
	if d.edlCalls != 0 {
		t.Fatalf("RUNNING 的渲染任务不应走既有转码探测（B-07 接管）")
	}
	sch2.processTask(mustTask(t, s2, "t_wait"))
	if d.edlCalls != 1 {
		t.Fatalf("processTask 应把 QUEUE 的 RENDER_EDL 分流到渲染派发，实际调用 %d 次", d.edlCalls)
	}
}
