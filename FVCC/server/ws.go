package main

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"fvcc/logger"
)

// Hub 管理浏览器 WebSocket 连接，向前端推送任务状态更新。
type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]bool
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
	return &Hub{clients: make(map[*websocket.Conn]bool)}
}

// HandleWS 处理浏览器 WebSocket 连接。
func (h *Hub) HandleWS(c *gin.Context) {
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
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		_ = conn.WriteMessage(websocket.TextMessage, data)
	}
}

// BroadcastInfo 向所有客户端推送信息消息。
func (h *Hub) BroadcastInfo(msgType string, data interface{}) {
	msg := InfoMsg{Type: msgType, Data: data, Time: time.Now().Format(time.RFC3339)}
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		_ = conn.WriteMessage(websocket.TextMessage, b)
	}
}

// CloseAll 关闭所有浏览器连接。
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for conn := range h.clients {
		conn.Close()
		delete(h.clients, conn)
	}
}
