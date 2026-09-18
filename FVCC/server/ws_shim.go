package main

// ws_shim.go — P2-1 阶段 B：main 包对 internal/ws 的兼容转发层。
//
// ws.go / ws_limit.go 已迁入 internal/ws（包名 ws），根包业务代码与测试
// 仍以原根包符号引用 Hub / 常量 / 消息类型，此处做类型别名与常量转发，
// 避免大范围调用点改动；后续根包收口时可逐步改直引 internal/ws。

import "fvcc/internal/ws"

// Hub 浏览器 WebSocket 连接管理器（转发 internal/ws.Hub）。
type Hub = ws.Hub

// SegInfo 渲染分段进度（转发 internal/ws.SegInfo）。
type SegInfo = ws.SegInfo

// TaskUpdateFullMsg 完整任务更新消息（转发 internal/ws.TaskUpdateFullMsg）。
type TaskUpdateFullMsg = ws.TaskUpdateFullMsg

// ProxyReadyMsg 代理就绪事件（转发 internal/ws.ProxyReadyMsg）。
type ProxyReadyMsg = ws.ProxyReadyMsg

// NodeStatusMsg 渲染节点状态事件（转发 internal/ws.NodeStatusMsg）。
type NodeStatusMsg = ws.NodeStatusMsg

// TaskUpdateMsg 任务更新消息（转发 internal/ws.TaskUpdateMsg）。
type TaskUpdateMsg = ws.TaskUpdateMsg

// InfoMsg 信息消息（转发 internal/ws.InfoMsg）。
type InfoMsg = ws.InfoMsg

// 常量转发：B-07 聚合窗口 / B-08 健康分档 / D-04 WS 连接上限。
const (
	taskUpdateAggInterval  = ws.TaskUpdateAggInterval
	proxyReadyDedupeWindow = ws.ProxyReadyDedupeWindow
	nodeHealthBandSize     = ws.NodeHealthBandSize
	healthScoreUnknown     = ws.HealthScoreUnknown
	wsBrowserMaxConns      = ws.WSBrowserMaxConns
)

// NewHub 创建 Hub（转发 internal/ws.NewHub）。
func NewHub() *Hub { return ws.NewHub() }
