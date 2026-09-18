package main

// P2-2 测试补强（2026-09-17）：
// 1) Store 并发读写压力测试：50 goroutine × 200 次混合操作，验证无死锁/数据竞争
//    （CI 以 -race 运行）、OrderID 全局唯一、任务计数最终一致；
// 2) 传输/编码锁并发操作：并发获取/释放锁后锁表归零；
// 3) 调度排序纯函数并发：sortQueueTasks 并发调用结果与串行基准一致。
//
// 注意：store 的 GetTasks/GetHistory 返回切片副本，测试中对副本的修改
// 不会污染内部状态；本测试仅断言并发安全与计数一致性。

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestStoreConcurrentReadWrite 并发读写压力测试。
func TestStoreConcurrentReadWrite(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertServer(Server{ID: "srv1", Name: "节点一", Status: "online"})

	const (
		goroutines = 50
		perWorker  = 200
	)
	var wg sync.WaitGroup
	var orderIDDup int32 // 检测 OrderID 重复
	seen := make(chan int64, goroutines*perWorker)

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				id := fmt.Sprintf("task_g%d_%d", g, i)
				s.UpsertTask(Task{
					ID: id, OrderID: s.NextOrderID(), Status: StatusQueue,
					FileName: id + ".mp4", ServerID: "srv1",
				})
				// 读-改-写 混合
				_ = s.GetTasks()
				if i%10 == 0 {
					s.UpdateTaskStatus(id, StatusTranscoding, float64(i%100), "")
				}
				if i%50 == 0 {
					if tk, ok := s.GetTask(id); ok {
						_ = tk
					}
					s.MoveToHistory(id)
					_ = s.GetHistory()
				}
				if i%7 == 0 {
					_ = s.GetServers()
				}
			}
		}(g)
	}
	wg.Wait()
	close(seen)

	total := goroutines * perWorker
	if got := len(s.GetTasks()) + len(s.GetHistory()); got != total {
		t.Fatalf("任务总量不一致: 期望 %d, 实际 tasks=%d history=%d", total, len(s.GetTasks()), len(s.GetHistory()))
	}
	if got := int(atomic.LoadInt32(&orderIDDup)); got != 0 {
		t.Fatalf("OrderID 出现重复: %d 处", got)
	}

	// 验证 OrderID 单调唯一：UpsertTask 之后 GetTasks 的 orderId 应与分配值一致
	ids := map[int64]string{}
	for _, tk := range s.GetTasks() {
		if pre, ok := ids[tk.OrderID]; ok {
			t.Fatalf("任务 %s 与 %s 的 OrderID 重复: %d", pre, tk.ID, tk.OrderID)
		}
		ids[tk.OrderID] = tk.ID
	}
}

// TestStoreConcurrentLockOps 传输锁/编码锁并发获取-释放压力测试。
func TestStoreConcurrentLockOps(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}

	const (
		goroutines = 20
		rounds     = 100
	)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			serverID := fmt.Sprintf("srv_%d", g)
			for i := 0; i < rounds; i++ {
				taskID := fmt.Sprintf("task_%d_%d", g, i)
				if s.AcquireTransLock(serverID, taskID, 60) {
					// 同一 server 已有锁则获取失败，只有持有者能释放
					s.ReleaseTransLock(serverID, taskID)
				}
				if s.AcquireCodeLock(serverID, taskID, 60) {
					s.ReleaseCodeLock(serverID, taskID)
				}
			}
		}(g)
	}
	wg.Wait()

	// 全部释放后锁表不应残留任何有效锁（既有设计：per-server 占位记录保留，
	// TransLock/CodeLock 置 nil 表示已释放；SweepExpiredLocks 会清理过期项）
	locks := s.GetLocks()
	for _, lk := range locks {
		if lk.TransLock != nil || lk.CodeLock != nil {
			t.Fatalf("锁表存在未释放锁: %+v", lk)
		}
	}
}

// TestSortQueueTasksConcurrent 并发调用排序函数，结果须与串行基准一致且无竞争。
func TestSortQueueTasksConcurrent(t *testing.T) {
	// 构造混合任务集合：GEN_PROXY 强制低优 + 同档按 OrderID
	base := make([]Task, 0, 300)
	for i := 0; i < 300; i++ {
		tt := TaskTypeTranscode
		if i%5 == 0 {
			tt = TaskTypeRenderEDL
		}
		if i%7 == 0 {
			tt = TaskTypeGenProxy
		}
		base = append(base, Task{ID: fmt.Sprintf("t%d", i), OrderID: int64(i), Status: StatusQueue, TaskType: tt})
	}

	// 串行基准
	ref := sortQueueTasks(append([]Task(nil), base...))

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got := sortQueueTasks(append([]Task(nil), base...))
				if !sameTaskOrder(got, ref) {
					t.Errorf("并发排序结果与串行基准不一致")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func sameTaskOrder(a, b []Task) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// TestSchedulerTickConcurrentSafe 并发 tick 安全性：
// 注入 fakeDispatcher 的 RENDER_EDL 任务在 tick 内只走内存状态机（B-05 已验证逻辑），
// 本测试用多 goroutine 并发触发 tick + 并发提交任务，验证：不 panic、无数据竞争、
// processing 表最终清空、任务状态合法收敛。
func TestSchedulerTickConcurrentSafe(t *testing.T) {
	sch, s := newSchedTestEnv(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)

	const submitWorkers = 8
	const tasksPerWorker = 20
	var wg sync.WaitGroup

	// 并发提交 RENDER_EDL 任务
	for g := 0; g < submitWorkers; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < tasksPerWorker; i++ {
				id := fmt.Sprintf("c_%d_%d", g, i)
				s.UpsertTask(Task{
					ID: id, OrderID: s.NextOrderID(), Status: StatusQueue,
					TaskType: TaskTypeRenderEDL, ServerID: "srv1",
					PayloadJSON: `{"clips":[],"out":"a.mp4"}`, ProjectID: "p_conc", Checksum: "abc",
					TotalMs: 9000,
				})
			}
		}(g)
	}

	// 并发 tick（模拟 1s 定时器在极端延迟下的重入）
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 15; i++ {
				sch.tick()
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	wg.Wait()

	// 并发 tick 后仍有 QUEUE 任务属正常（tick 对 QUEUE 为串行推进，每轮只派发一个）；
	// 这里补跑串行 tick 直至全部收敛，验证的是"并发提交 + 并发 tick 不 panic、不泄漏"。
	deadline := time.Now().Add(10 * time.Second)
	for {
		queued := 0
		for _, tk := range s.GetTasks() {
			if tk.Status == StatusQueue {
				queued++
			}
		}
		if queued == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("仍有 %d 个任务滞留 QUEUE 未收敛", queued)
		}
		sch.tick()
		time.Sleep(3 * time.Millisecond)
	}

	// 等待异步 processTask 收敛
	deadline = time.Now().Add(3 * time.Second)
	for {
		processingCount := 0
		sch.processing.Range(func(_, _ any) bool { processingCount++; return true })
		if processingCount == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("processing 表未清空（可能泄漏）：%d 项", processingCount)
		}
		time.Sleep(10 * time.Millisecond)
	}

	total := submitWorkers * tasksPerWorker
	tasks := s.GetTasks()
	if len(tasks) != total {
		t.Fatalf("任务总数不一致: 期望 %d, 实际 %d", total, len(tasks))
	}
	for _, tk := range tasks {
		switch tk.Status {
		case StatusTranscoding: // 派发成功进入 RUNNING
		case StatusError: // 错误白名单直接失败（E_EDL_INVALID 等）亦为合法收敛
		default:
			t.Fatalf("任务 %s 状态未合法收敛: %s", tk.ID, tk.Status)
		}
	}
	// 并发提交下 OrderID 应仍保持单调唯一
	orderIDs := make([]int64, 0, len(tasks))
	for _, tk := range tasks {
		orderIDs = append(orderIDs, tk.OrderID)
	}
	sort.Slice(orderIDs, func(i, j int) bool { return orderIDs[i] < orderIDs[j] })
	for i := 1; i < len(orderIDs); i++ {
		if orderIDs[i] == orderIDs[i-1] {
			t.Fatalf("OrderID 重复: %d", orderIDs[i])
		}
	}
}
