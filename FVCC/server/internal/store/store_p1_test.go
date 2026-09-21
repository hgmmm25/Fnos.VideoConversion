package store

// P1-1 调度器事件驱动优化（2026-09-21）——store 侧专项测试：
//  1) 任务变更钩子：创建/状态变更触发回调，纯进度更新不触发（避免事件风暴）；
//  2) 脏标记 + Flush：高频任务状态变更仅标脏，Flush 一次性落盘；
//  3) checksum 内存索引：活动/成功任务按 checksum 定位，删除/变更后索引正确失效重建。

import (
	"encoding/json"
	"testing"

	"fvcc/internal/store/model"
)

type hookEvent struct {
	id     string
	status model.TaskStatus
}

// TestP1StoreEventHook 验证事件钩子语义：创建与状态变更触发，进度更新不触发。
func TestP1StoreEventHook(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	var got []hookEvent
	s.SetTaskChangeHook(func(taskID string, status model.TaskStatus) {
		got = append(got, hookEvent{taskID, status})
	})

	// 创建任务 → 触发一次 QUEUE 事件
	s.UpsertTask(model.Task{ID: "t1", OrderID: 1, Status: model.StatusQueue})
	if len(got) != 1 || got[0].id != "t1" || got[0].status != model.StatusQueue {
		t.Fatalf("创建任务应触发一次 QUEUE 事件，实际 %+v", got)
	}

	// 状态变更 → 触发 RUNNING 事件
	s.UpdateTaskStatus("t1", model.StatusTranscoding, 5, "")
	if len(got) != 2 || got[1].id != "t1" || got[1].status != model.StatusTranscoding {
		t.Fatalf("状态变更应触发 RUNNING 事件，实际 %+v", got)
	}

	// 同状态纯进度更新 → 不触发（避免高频进度上报刷爆事件通道）
	s.UpdateTaskStatus("t1", model.StatusTranscoding, 50, "")
	if len(got) != 2 {
		t.Fatalf("纯进度更新不应触发事件，实际 %+v", got)
	}

	// 同值 UpsertTask（状态/进度/错误均未变）→ 仍触发（幂等收敛场景，调度器据此确认状态）
	s.UpsertTask(model.Task{ID: "t1", OrderID: 1, Status: model.StatusTranscoding, Progress: 50})
	if len(got) != 3 || got[2].status != model.StatusTranscoding {
		t.Fatalf("UpsertTask 应触发事件，实际 %+v", got)
	}
}

// TestP1StoreDirtyFlush 验证脏标记 + Flush 定时落盘：未 Flush 前新实例读不到，
// Flush 后新实例可见且数据为最新状态。
func TestP1StoreDirtyFlush(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertTask(model.Task{ID: "t1", OrderID: 1, Status: model.StatusQueue})
	s.UpdateTaskStatus("t1", model.StatusTranscoding, 10, "")

	// 未 Flush：磁盘仍无数据（脏数据仅存内存）
	s2 := NewStore(dir)
	if err := s2.Load(); err != nil {
		t.Fatalf("二次 Load() 失败: %v", err)
	}
	if _, ok := s2.GetTask("t1"); ok {
		t.Fatalf("未 Flush 前任务不应落盘（P1-1 定时落盘：高频变更不再逐次写盘）")
	}

	// Flush 后：新实例 Load 可见任务（崩溃恢复会把运行态重置为 QUEUE，存在即可）
	s.Flush()
	s3 := NewStore(dir)
	if err := s3.Load(); err != nil {
		t.Fatalf("三次 Load() 失败: %v", err)
	}
	if _, ok := s3.GetTask("t1"); !ok {
		t.Fatalf("Flush 后任务应落盘")
	}

	// 直接校验 SQLite 落盘内容为最新状态（P0-2 提交 3：主模式落 SQLite，
	// 绕过 Load 崩溃恢复对运行态的可见性干扰）
	sq, err := openSQLite(dir)
	if err != nil {
		t.Fatalf("openSQLite 失败: %v", err)
	}
	defer sq.Close()
	var data string
	if err := sq.db.QueryRow(`SELECT data FROM tasks WHERE id='t1'`).Scan(&data); err != nil {
		t.Fatalf("读取 SQLite tasks.t1 失败: %v", err)
	}
	var rec struct {
		ID       string `json:"id"`
		Status   string `json:"status"`
		Progress int    `json:"progress"`
	}
	if err := json.Unmarshal([]byte(data), &rec); err != nil {
		t.Fatalf("解析落盘数据失败: %v", err)
	}
	if rec.ID != "t1" || rec.Status != "TRANSCODING" {
		t.Fatalf("落盘数据应为最新状态 TRANSCODING，实际: %s", data)
	}
	if rec.Progress != 10 {
		t.Fatalf("落盘数据应包含进度 10，实际: %s", data)
	}

	// 无脏数据时 Flush 为 no-op（不产生 IO，返回即可）
	s.Flush()
}

// TestP1StoreChecksumIndex 验证 checksum 内存索引：活动/成功查询命中、历史兜底、
// 结构变动与 checksum 变更后索引正确失效重建。
func TestP1StoreChecksumIndex(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	// 预热索引（首次 GetTask 触发 ensureIndexLocked 重建）
	if _, ok := s.GetTask("none"); ok {
		t.Fatal("空库不应命中")
	}

	s.UpsertTask(model.Task{ID: "t_a", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL, Checksum: "cs1"})
	s.UpsertTask(model.Task{ID: "t_b", OrderID: 2, Status: model.StatusTranscoding, TaskType: model.TaskTypeRenderEDL, Checksum: "cs2"})
	s.UpsertTask(model.Task{ID: "t_c", OrderID: 3, Status: model.StatusCompleted, TaskType: model.TaskTypeRenderEDL, Checksum: "cs3"})

	// 活动任务命中（QUEUE / RUNNING）
	if got, ok := s.FindActiveTaskByChecksum("cs1"); !ok || got.ID != "t_a" {
		t.Fatalf("QUEUE 任务应命中活跃索引，实际 id=%q ok=%v", got.ID, ok)
	}
	if got, ok := s.FindActiveTaskByChecksum("cs2"); !ok || got.ID != "t_b" {
		t.Fatalf("RUNNING 任务应命中活跃索引，实际 id=%q ok=%v", got.ID, ok)
	}
	// 成功索引命中已完成任务，但不命中活动任务
	if got, ok := s.FindSuccessTaskByChecksum("cs3"); !ok || got.ID != "t_c" {
		t.Fatalf("已完成任务应命中成功索引，实际 id=%q ok=%v", got.ID, ok)
	}
	if _, ok := s.FindSuccessTaskByChecksum("cs1"); ok {
		t.Fatal("活动任务不应命中成功索引")
	}

	// 移入历史：活动表不可见，但成功索引经历史兜底仍可命中
	s.MoveToHistory("t_c")
	if _, ok := s.GetTask("t_c"); ok {
		t.Fatal("MoveToHistory 后活动表不应再有 t_c")
	}
	if got, ok := s.FindSuccessTaskByChecksum("cs3"); !ok || got.ID != "t_c" {
		t.Fatalf("历史中已完成任务应命中成功索引，实际 id=%q ok=%v", got.ID, ok)
	}
	if _, ok := s.FindActiveTaskByChecksum("cs3"); ok {
		t.Fatal("移历史后活动索引不应命中 cs3（索引应失效重建）")
	}

	// checksum 变更：旧值失效、新值命中（索引置脏后重建）
	s.UpsertTask(model.Task{ID: "t_a", OrderID: 1, Status: model.StatusQueue, TaskType: model.TaskTypeRenderEDL, Checksum: "cs1-new"})
	if _, ok := s.FindActiveTaskByChecksum("cs1"); ok {
		t.Fatal("checksum 更新后旧值不应命中")
	}
	if got, ok := s.FindActiveTaskByChecksum("cs1-new"); !ok || got.ID != "t_a" {
		t.Fatalf("checksum 更新后新值应命中，实际 id=%q ok=%v", got.ID, ok)
	}
}
