package scheduler

// P2-2 测试补强（2026-09-17），P2-1 B轮由 server/store_concurrency_test.go 迁入：
// 1) 调度排序纯函数并发：sortQueueTasks 并发调用结果与串行基准一致；
// 2) 并发 tick 安全性：注入 fakeDispatcher 的 RENDER_EDL 任务在 tick 内只走内存状态机，
//    多 goroutine 并发触发 tick + 并发提交任务，验证：不 panic、无数据竞争、
//    processing 表最终清空、任务状态合法收敛。

import (
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"fvcc/internal/store/model"
)

// TestSortQueueTasksConcurrent 并发调用排序函数，结果须与串行基准一致且无竞争。
func TestSortQueueTasksConcurrent(t *testing.T) {
	// 构造混合任务集合：GEN_PROXY 强制低优 + 同档按 OrderID
	base := make([]model.Task, 0, 300)
	for i := 0; i < 300; i++ {
		tt := model.TaskTypeTranscode
		if i%5 == 0 {
			tt = model.TaskTypeRenderEDL
		}
		if i%7 == 0 {
			tt = model.TaskTypeGenProxy
		}
		base = append(base, model.Task{ID: fmt.Sprintf("t%d", i), OrderID: int64(i), Status: model.StatusQueue, TaskType: tt})
	}

	// 串行基准
	ref := sortQueueTasks(append([]model.Task(nil), base...))

	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				got := sortQueueTasks(append([]model.Task(nil), base...))
				if !sameTaskOrder(got, ref) {
					t.Errorf("并发排序结果与串行基准不一致")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func sameTaskOrder(a, b []model.Task) bool {
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
				s.UpsertTask(model.Task{
					ID: id, OrderID: s.NextOrderID(), Status: model.StatusQueue,
					TaskType: model.TaskTypeRenderEDL, ServerID: "srv1",
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
			if tk.Status == model.StatusQueue {
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
		case model.StatusTranscoding: // 派发成功进入 RUNNING
		case model.StatusError: // 错误白名单直接失败（E_EDL_INVALID 等）亦为合法收敛
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
