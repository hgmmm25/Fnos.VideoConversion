package main

// P2-1 B轮：Scheduler 已整体迁入 internal/scheduler（含节点选机/代理收尾委托），
// 根包以类型别名 + 构造转发保留装配入口，main.go / handlers.go 调用点零改动。
// 阶段 C 迁入 internal/api 后本文件删除。

import (
	"fvcc/internal/scheduler"
	"fvcc/internal/security"
)

// Scheduler 类型别名：方法集由 internal/scheduler 提供（含 pickNode/NoteNode*/
// HealthScoreOf/NodeFreeSlots/ApplyNodeHello/SetProxyProbe 等 B-08/M4 委托）。
type Scheduler = scheduler.Scheduler

// NewScheduler 创建调度器（转发 internal/scheduler）。
func NewScheduler(store *Store, remote *RemoteClient, hub *Hub, pv *security.PathValidator) *Scheduler {
	return scheduler.NewScheduler(store, remote, hub, pv)
}
