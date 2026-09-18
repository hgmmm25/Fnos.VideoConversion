package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"fvcc/logger"
)

// ===== B-07：进度与事件聚合（06 §6）=====

const (
	// taskUpdateAggInterval task_update 服务端聚合窗口：同一任务在窗口内最多广播一次（取最新值）。
	taskUpdateAggInterval = 500 * time.Millisecond
	// proxyReadyDedupeWindow proxy_ready 去重窗口：同一 assetId 在窗口内只广播一次。
	proxyReadyDedupeWindow = 10 * time.Second
	// nodeHealthBandSize node_status 的 healthScore 分档粒度（跨 20 分档才广播）。
	nodeHealthBandSize = 20
	// healthScoreUnknown 健康分未知（B-08 落地前由调用方传入，不参与分档判断）。
	healthScoreUnknown = -1
)

// SegInfo 渲染分段进度（06 §6 BroadcastTaskUpdateFull 的 seg 参数）。
type SegInfo struct {
	Index int `json:"index"` // 当前分段序号（1-based，0=未开始）
	Total int `json:"total"` // 分段总数
}

// TaskUpdateFullMsg 完整任务更新消息（B-07）：在既有 TaskUpdateMsg 之上补齐
// stage / seg / outTimeMs / totalMs / speed，供前端渲染「第 x/y 段 · stage」文案。
type TaskUpdateFullMsg struct {
	Type      string   `json:"type"` // "task_update"
	TaskID    string   `json:"taskId"`
	Status    string   `json:"status"`
	Progress  int      `json:"progress"`
	Stage     string   `json:"stage,omitempty"`
	Seg       *SegInfo `json:"seg,omitempty"`
	Message   string   `json:"message,omitempty"`
	OutTimeMs int64    `json:"outTimeMs,omitempty"`
	TotalMs   int64    `json:"totalMs,omitempty"`
	Speed     string   `json:"speed,omitempty"`
	Time      string   `json:"time"`
}

// ProxyReadyMsg 代理就绪事件（04 §3.5 / 06 §6）。
type ProxyReadyMsg struct {
	Type    string      `json:"type"` // "proxy_ready"
	AssetID string      `json:"assetId"`
	Data    interface{} `json:"data,omitempty"`
	Time    string      `json:"time"`
}

// NodeStatusMsg 渲染节点状态事件（06 §6）。
type NodeStatusMsg struct {
	Type        string `json:"type"` // "node_status"
	ServerID    string `json:"serverId"`
	Status      string `json:"status"`
	HealthScore int    `json:"healthScore"`
	Reason      string `json:"reason,omitempty"`
	Time        string `json:"time"`
}

// taskUpdateAgg 单个任务在聚合窗口内的状态。
type taskUpdateAgg struct {
	lastSentAt time.Time
	lastKey    string // 上次广播内容指纹（进度未变不广播）
	pending    *TaskUpdateFullMsg
	timer      *time.Timer
}

// nodeStatusState 节点上次已广播的状态与健康分档位。
type nodeStatusState struct {
	status string
	band   int
}

// Hub 管理浏览器 WebSocket 连接，向前端推送任务状态更新。
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool

	// wsConns 浏览器维度连接计数（07 §4.5：浏览器 ≤ 5 条）；由 connRegistry 懒初始化。
	wsConns *wsConnRegistry

	// ===== B-07 事件聚合状态（06 §6）=====
	aggMu     sync.Mutex
	agg       map[string]*taskUpdateAgg  // taskID → task_update 聚合窗口状态
	proxySeen map[string]time.Time       // assetId → 上次 proxy_ready 广播时刻
	nodeState map[string]nodeStatusState // serverID → 上次广播的节点状态

	// clock 时间源（单测可注入）；nil 时使用 time.Now。
	clock func() time.Time
	// emitHook 广播出口钩子（单测注入以捕获事件）；nil 时走真实 WS 写。
	emitHook func([]byte)
}

// TaskUpdateMsg 推送给前端的任务更新消息。
type TaskUpdateMsg struct {
	Type     string  `json:"type"` // "task_update"
	TaskID   string  `json:"taskId"`
	Status   string  `json:"status"`
	Progress float64 `json:"progress"`
	Message  string  `json:"message"`
	Time     string  `json:"time"`
}

// InfoMsg 推送给前端的信息消息。
type InfoMsg struct {
	Type string      `json:"type"`
	Data interface{} `json:"data"`
	Time string      `json:"time"`
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func NewHub() *Hub {
	return &Hub{
		clients:   make(map[*websocket.Conn]bool),
		agg:       make(map[string]*taskUpdateAgg),
		proxySeen: make(map[string]time.Time),
		nodeState: make(map[string]nodeStatusState),
	}
}

// HandleWS 处理浏览器 WebSocket 连接。
// 07 §4.5：单浏览器（网关用户 / 来源 IP）并发连接 ≤ 5，超限直接 429。
func (h *Hub) HandleWS(c *gin.Context) {
	key := rateKey(c)
	reg := h.connRegistry()
	if !reg.acquire(key) {
		logger.Warn("ws", "浏览器 WS 连接数超限: key=%v limit=%d", key, wsBrowserMaxConns)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"ok":   false,
			"code": errCodeRateLimited,
			"msg":  "WebSocket 连接数过多，请复用已有连接",
		})
		c.Abort()
		return
	}
	defer reg.release(key)

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		logger.Warn("ws", "upgrade failed: %v", err)
		return
	}

	h.mu.Lock()
	h.clients[conn] = true
	h.mu.Unlock()

	// 发送欢迎消息
	user := getGatewayUser(c)
	hello := InfoMsg{
		Type: "hello",
		Data: map[string]interface{}{
			"uid":  user.UID,
			"user": user.Username,
		},
		Time: time.Now().Format(time.RFC3339),
	}
	if data, err := json.Marshal(hello); err == nil {
		conn.WriteMessage(websocket.TextMessage, data)
	}

	// 推送当前任务快照
	h.sendTaskSnapshot(conn)

	defer func() {
		h.mu.Lock()
		delete(h.clients, conn)
		h.mu.Unlock()
		conn.Close()
	}()

	// 读取循环（处理客户端心跳/请求）
	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// sendTaskSnapshot 发送当前任务列表快照。
func (h *Hub) sendTaskSnapshot(conn *websocket.Conn) {
	// 由 router 注入 store，通过全局变量访问
	if globalStore == nil {
		return
	}
	tasks := globalStore.GetTasks()
	msg := InfoMsg{
		Type: "snapshot",
		Data: tasks,
		Time: time.Now().Format(time.RFC3339),
	}
	if data, err := json.Marshal(msg); err == nil {
		conn.WriteMessage(websocket.TextMessage, data)
	}
}

// now 返回当前时间（单测可注入 clock）。
func (h *Hub) now() time.Time {
	if h.clock != nil {
		return h.clock()
	}
	return time.Now()
}

// emit 把已序列化的消息发给所有浏览器客户端；单测通过 emitHook 捕获（不产生真实连接写）。
func (h *Hub) emit(data []byte) {
	if h.emitHook != nil {
		h.emitHook(data)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
}

// broadcastJSON 序列化并推送一条事件。
func (h *Hub) broadcastJSON(v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		return
	}
	h.emit(data)
}

// BroadcastTaskUpdate 向所有浏览器客户端推送任务更新。
func (h *Hub) BroadcastTaskUpdate(taskID, status string, progress float64, message string) {
	msg := TaskUpdateMsg{
		Type:     "task_update",
		TaskID:   taskID,
		Status:   status,
		Progress: progress,
		Message:  message,
		Time:     time.Now().Format(time.RFC3339),
	}
	h.broadcastJSON(msg)
}

// BroadcastTaskUpdateFull 广播完整任务更新（06 §6，B-07）。
//
// 聚合规则：同一任务在 taskUpdateAggInterval（500ms）内最多广播一次，窗口内的多次调用
// 只保留最新值并在窗口到期后补发（保证最终值不丢）；内容（状态/进度/阶段/分段/消息）
// 与上次广播完全一致时不广播。
// 线协议终态（SUCCESS/FAILED/CANCELED）为任务生命周期最后一帧，立即广播且不计入节流，
// 避免 Hub 关闭或前端离线时终态被节流吞掉。
func (h *Hub) BroadcastTaskUpdateFull(taskID, status string, progress int, stage string,
	seg *SegInfo, msg string, outTimeMs, totalMs int64, speed string) {
	if taskID == "" {
		return
	}
	if progress < 0 {
		progress = 0
	} else if progress > 100 {
		progress = 100
	}
	update := TaskUpdateFullMsg{
		Type:      "task_update",
		TaskID:    taskID,
		Status:    status,
		Progress:  progress,
		Stage:     stage,
		Seg:       seg,
		Message:   msg,
		OutTimeMs: outTimeMs,
		TotalMs:   totalMs,
		Speed:     speed,
		Time:      h.now().Format(time.RFC3339),
	}
	key := taskUpdateKey(update)

	h.aggMu.Lock()
	st := h.agg[taskID]
	if st == nil {
		st = &taskUpdateAgg{}
		h.agg[taskID] = st
	}
	now := h.now()
	// 进度未变不广播：内容与上次已广播一致，且窗口内没有待发内容。
	if st.lastKey == key && st.pending == nil {
		h.aggMu.Unlock()
		return
	}
	terminal := isTerminalWireStatus(status)
	if terminal || st.lastSentAt.IsZero() || now.Sub(st.lastSentAt) >= taskUpdateAggInterval {
		// 首帧 / 窗口已过 / 终态：立即广播，并丢弃更旧的待发内容。
		st.lastSentAt = now
		st.lastKey = key
		st.pending = nil
		if st.timer != nil {
			st.timer.Stop()
			st.timer = nil
		}
		if terminal {
			delete(h.agg, taskID)
		}
		h.aggMu.Unlock()
		h.broadcastJSON(update)
		return
	}
	// 窗口内：暂存最新值，窗口到期后补发。
	st.pending = &update
	if st.timer == nil {
		delay := taskUpdateAggInterval - now.Sub(st.lastSentAt)
		st.timer = time.AfterFunc(delay, func() { h.flushTaskUpdate(taskID) })
	}
	h.aggMu.Unlock()
}

// flushTaskUpdate 聚合窗口到期后补发最近一次暂存的任务更新（取最新值）。
func (h *Hub) flushTaskUpdate(taskID string) {
	h.aggMu.Lock()
	st := h.agg[taskID]
	if st == nil {
		h.aggMu.Unlock()
		return
	}
	st.timer = nil
	pending := st.pending
	if pending == nil {
		h.aggMu.Unlock()
		return
	}
	st.pending = nil
	st.lastKey = taskUpdateKey(*pending)
	st.lastSentAt = h.now()
	msg := *pending
	h.aggMu.Unlock()
	h.broadcastJSON(msg)
}

// BroadcastProxyReady 广播代理就绪事件（06 §6）：同一 assetId 在 proxyReadyDedupeWindow
// （10s）内只广播一次。返回是否真正广播（去重命中返回 false）。
func (h *Hub) BroadcastProxyReady(assetID string, data interface{}) bool {
	if assetID == "" {
		return false
	}
	now := h.now()
	h.aggMu.Lock()
	for id, at := range h.proxySeen {
		if now.Sub(at) >= proxyReadyDedupeWindow {
			delete(h.proxySeen, id)
		}
	}
	if at, ok := h.proxySeen[assetID]; ok && now.Sub(at) < proxyReadyDedupeWindow {
		h.aggMu.Unlock()
		return false
	}
	h.proxySeen[assetID] = now
	h.aggMu.Unlock()

	h.broadcastJSON(ProxyReadyMsg{
		Type:    "proxy_ready",
		AssetID: assetID,
		Data:    data,
		Time:    now.Format(time.RFC3339),
	})
	return true
}

// BroadcastNodeStatus 广播渲染节点状态变化（06 §6）：status（online↔offline）变化，
// 或 healthScore 跨 nodeHealthBandSize（20 分）档位时广播；healthScore 传
// healthScoreUnknown 时不参与分档判断（B-08 健康分落地前）。返回是否真正广播。
func (h *Hub) BroadcastNodeStatus(serverID, status string, healthScore int, reason string) bool {
	if serverID == "" || status == "" {
		return false
	}
	band := -1
	if healthScore >= 0 {
		if healthScore > 100 {
			healthScore = 100
		}
		band = healthScore / nodeHealthBandSize
	}
	h.aggMu.Lock()
	prev, seen := h.nodeState[serverID]
	// 健康分未知（band<0）时不参与分档判断，仅按状态变化广播。
	if seen && prev.status == status && (band < 0 || prev.band == band) {
		h.aggMu.Unlock()
		return false
	}
	next := nodeStatusState{status: status, band: prev.band}
	if band >= 0 {
		next.band = band
	}
	h.nodeState[serverID] = next
	h.aggMu.Unlock()

	h.broadcastJSON(NodeStatusMsg{
		Type:        "node_status",
		ServerID:    serverID,
		Status:      status,
		HealthScore: healthScore,
		Reason:      reason,
		Time:        h.now().Format(time.RFC3339),
	})
	return true
}

// BroadcastInfo 向所有客户端推送信息消息。
func (h *Hub) BroadcastInfo(msgType string, data interface{}) {
	msg := InfoMsg{Type: msgType, Data: data, Time: time.Now().Format(time.RFC3339)}
	h.broadcastJSON(msg)
}

// taskUpdateKey 任务更新内容指纹（进度未变不广播的判据）：
// 状态 + 进度 + 阶段 + 分段 + 消息。
func taskUpdateKey(m TaskUpdateFullMsg) string {
	segIdx, segTotal := 0, 0
	if m.Seg != nil {
		segIdx, segTotal = m.Seg.Index, m.Seg.Total
	}
	return fmt.Sprintf("%s|%d|%s|%d|%d|%s", m.Status, m.Progress, m.Stage, segIdx, segTotal, m.Message)
}

// isTerminalWireStatus 线协议终态（06 §2.3）：终态帧立即广播，不计入 500ms 节流。
func isTerminalWireStatus(status string) bool {
	switch status {
	case "SUCCESS", "FAILED", "CANCELED":
		return true
	}
	return false
}

// CloseAll 关闭所有浏览器连接。
func (h *Hub) CloseAll() {
	h.mu.Lock()
	for conn := range h.clients {
		conn.Close()
		delete(h.clients, conn)
	}
	h.mu.Unlock()

	// 停止所有聚合定时器，避免关闭后仍触发补发。
	h.aggMu.Lock()
	for _, st := range h.agg {
		if st.timer != nil {
			st.timer.Stop()
			st.timer = nil
		}
	}
	h.agg = make(map[string]*taskUpdateAgg)
	h.aggMu.Unlock()
}
