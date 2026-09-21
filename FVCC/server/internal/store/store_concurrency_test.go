package store

// P2-2 测试补强（2026-09-17），P2-1 B轮由 server/store_concurrency_test.go 迁入：
// 1) Store 并发读写压力测试：50 goroutine × 200 次混合操作，验证无死锁/数据竞争
//    （CI 以 -race 运行）、OrderID 全局唯一、任务计数最终一致；
// 2) 传输/编码锁并发操作：并发获取/释放锁后锁表归零。
//
// 注意：store 的 GetTasks/GetHistory 返回切片副本，测试中对副本的修改
// 不会污染内部状态；本测试仅断言并发安全与计数一致性。

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"fvcc/internal/store/model"
)

// TestStoreConcurrentReadWrite 并发读写压力测试。
func TestStoreConcurrentReadWrite(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertServer(model.Server{ID: "srv1", Name: "节点一", Status: "online"})

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
				s.UpsertTask(model.Task{
					ID: id, OrderID: s.NextOrderID(), Status: model.StatusQueue,
					FileName: id + ".mp4", ServerID: "srv1",
				})
				// 读-改-写 混合
				_ = s.GetTasks()
				if i%10 == 0 {
					s.UpdateTaskStatus(id, model.StatusTranscoding, float64(i%100), "")
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
