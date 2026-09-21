package scheduler

// P1-1 调度器事件驱动优化（2026-09-21）——scheduler 侧专项测试：
//  1) 事件驱动派发：任务创建/入队经 store 钩子投递事件，handleEvent 立即推进调度
//     （无需等待 1s tick 全量扫描）；
//  2) 冷却到期唤醒：failRenderTaskWithCooldown 注册定时器，到期投递事件即时重入队；
//  3) 优雅退出落盘：Stop 后脏任务数据已 Flush，新实例 Load 可见。

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fvcc/internal/remote"
	"fvcc/internal/store"
	"fvcc/internal/store/model"
	"fvcc/internal/ws"
)

// TestP1EventDrivenQueueDispatch 验证事件驱动核心：QUEUE 任务创建即投递事件，
// 事件循环消费后立即派发，调度延迟不受 1s tick 限制。
func TestP1EventDrivenQueueDispatch(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)

	s.UpsertTask(model.Task{ID: "t_ev", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", PayloadJSON: `{"clips":[]}`})

	// NewScheduler 已注册 store 钩子：任务创建应即时投递事件。
	// 事件经 store 钩子异步投递，覆盖率插桩/并行测试下可能延迟数个毫秒，
	// 故用 2s 超时等待而非非阻塞 default（避免 flaky FAIL）。
	select {
	case taskID := <-sch.eventCh:
		if taskID != "t_ev" {
			t.Fatalf("事件应携带任务 ID，实际 %q", taskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("创建 QUEUE 任务应即时投递事件（2s 超时）")
	}

	// 事件循环消费：handleEvent 立即推进（模拟 select 分支，无需 tick）。
	// processTask 为异步协程（go s.processTask），派发调用先于 store 状态更新可见，
	// 故状态断言同样轮询等待，避免插桩/并行下 flaky FAIL。
	sch.handleEvent("t_ev")
	deadline := time.Now().Add(2 * time.Second)
	for d.edlCalls == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if d.edlCalls != 1 {
		t.Fatalf("事件驱动应立即派发 RENDER_EDL，edlCalls=%d", d.edlCalls)
	}
	var got model.Task
	for {
		got = mustTask(t, s, "t_ev")
		if got.Status == model.StatusTranscoding {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("派发成功应进入 RUNNING(StatusTranscoding)，实际 %s", got.Status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestP1CooldownWakeTimer 验证冷却到期唤醒：派发失败进入 COOLDOWN 并注册唤醒 timer，
// 到期投递事件，事件消费后转回 QUEUE 且保留 OrderID。
func TestP1CooldownWakeTimer(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	sch.SetRenderDispatcher(&fakeDispatcher{err: errors.New("E_NODE_OFFLINE: 节点心跳超时")})

	s.UpsertTask(model.Task{ID: "t_cd", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", PayloadJSON: `{"clips":[]}`})
	sch.handleRenderEDL(mustTask(t, s, "t_cd"))

	got := mustTask(t, s, "t_cd")
	if got.Status != model.StatusCooldown {
		t.Fatalf("派发失败应进入 COOLDOWN，实际 %s", got.Status)
	}
	sch.cooldownMu.Lock()
	_, ok := sch.cooldownTimers["t_cd"]
	sch.cooldownMu.Unlock()
	if !ok {
		t.Fatal("进入冷却应注册到期唤醒 timer（P1-1：无需等 1s tick 扫描）")
	}
	defer func() {
		sch.cooldownMu.Lock()
		if tm := sch.cooldownTimers["t_cd"]; tm != nil {
			tm.Stop()
		}
		delete(sch.cooldownTimers, "t_cd")
		sch.cooldownMu.Unlock()
	}()

	// 短延时 timer 到期后应投递唤醒事件（模拟 30s 冷却到期路径）
	sch.scheduleCooldownWake("t_cd", 50*time.Millisecond)
	select {
	case taskID := <-sch.eventCh:
		if taskID != "t_cd" {
			t.Fatalf("唤醒事件应携带任务 ID，实际 %q", taskID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("冷却到期 timer 到期后应投递唤醒事件")
	}

	// 事件消费：到期 COOLDOWN → QUEUE，保留 OrderID
	past := time.Now().Add(-time.Second)
	got.CoolDownUntil = &past
	s.UpsertTask(got)
	sch.handleEvent("t_cd")
	due := mustTask(t, s, "t_cd")
	if due.Status != model.StatusQueue {
		t.Fatalf("到期后应转回 QUEUE，实际 %s", due.Status)
	}
	if due.OrderID != 1 {
		t.Fatalf("转回 QUEUE 应保留 OrderID，实际 %d", due.OrderID)
	}
	if due.CoolDownUntil != nil || due.CoolDownSec != 0 {
		t.Fatalf("转回 QUEUE 应清空冷却字段: %+v", due)
	}
}

// TestP1StopFlushOnExit 验证优雅退出：Start 后 Stop 前任务仅内存标脏，
// Stop 事件循环退出时 Flush，新实例 Load 可见已派发任务。
func TestP1StopFlushOnExit(t *testing.T) {
	dir := t.TempDir()
	s := store.NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertServer(model.Server{ID: "srv1", Name: "节点一", Status: "online"})
	sch := NewScheduler(s, remote.NewRemoteClient(), ws.NewHub(), nil)
	sch.SetRenderDispatcher(&fakeDispatcher{})
	go sch.Start() // Start 为阻塞式事件循环，测试协程异步启动
	defer sch.Stop()

	// 事件驱动创建任务并派发；等待事件循环消费完成（processTask 异步）
	s.UpsertTask(model.Task{ID: "t_flush", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL,
		ServerID: "srv1", PayloadJSON: `{"clips":[]}`})
	time.Sleep(300 * time.Millisecond)
	sch.Stop() // 优雅退出：事件循环退出前 Flush 脏数据（defer 幂等兜底）

	// 新实例 Load：任务应已落盘（Stop 触发 Flush）
	// 注：Load 含崩溃恢复语义，非终态任务会重置为 QUEUE（"was interrupted"），
	// 故此处断言任务存在即可，不校验运行态。
	s2 := store.NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("二次 Load() 失败: %v", err)
	}
	if _, ok := s2.GetTask("t_flush"); !ok {
		t.Fatal("Stop 后脏数据应已 Flush 落盘")
	}

	// 直接校验磁盘文件已写入任务（绕过 Load 崩溃恢复的可见性干扰）
	data, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatalf("读取 tasks.json 失败: %v", err)
	}
	if !strings.Contains(string(data), `"t_flush"`) {
		t.Fatal("tasks.json 应包含已落盘任务 t_flush")
	}
}
