package main

// P2-1 可观测性端点验证（2026-09-17）：
// 1) GET /metrics 扩展字段（activeTasks/queuedTasks/failedTasks/completedTotal/
//    nodeCapsCount/offlineServers）返回正确；
// 2) GET /metrics/prometheus 输出 Prometheus 文本格式，指标名/标签/类型齐全。
// 仅依赖 store 字段，无需启动完整服务。

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newMetricsTestEnv(t *testing.T) *Handlers {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	// 2 个活跃任务（queue + transcoding）+ 1 个错误任务
	s.UpsertTask(Task{ID: "t1", OrderID: s.NextOrderID(), Status: StatusQueue, FileName: "a.mp4", ServerID: "srv1"})
	s.UpsertTask(Task{ID: "t2", OrderID: s.NextOrderID(), Status: StatusTranscoding, FileName: "b.mp4", ServerID: "srv1"})
	s.UpsertTask(Task{ID: "t3", OrderID: s.NextOrderID(), Status: StatusError, FileName: "c.mp4", ServerID: "srv1"})
	// 1 个已完成历史任务
	s.UpsertTask(Task{ID: "t4", OrderID: s.NextOrderID(), Status: StatusCompleted, FileName: "d.mp4", ServerID: "srv1"})
	s.MoveToHistory("t4")
	// 1 在线 + 1 离线节点
	s.UpsertServer(Server{ID: "srv1", Name: "节点一", Status: "online"})
	s.UpsertServer(Server{ID: "srv2", Name: "节点二", Status: "offline"})
	return &Handlers{store: s}
}

func TestMetricsEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newMetricsTestEnv(t)
	r := gin.New()
	r.GET("/metrics", h.metrics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("GET /metrics 状态码 = %d, 期望 200", w.Code)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应非 JSON: %v", err)
	}
	if m["totalTasks"].(float64) != 3 {
		t.Errorf("totalTasks = %v, 期望 3", m["totalTasks"])
	}
	if m["totalHistory"].(float64) != 1 {
		t.Errorf("totalHistory = %v, 期望 1", m["totalHistory"])
	}
	// StatusError 非终态（可重试，IsTerminal 仅认 Completed/Cancelled），故活跃=3
	if m["activeTasks"].(float64) != 3 {
		t.Errorf("activeTasks = %v, 期望 3", m["activeTasks"])
	}
	if m["queuedTasks"].(float64) != 1 {
		t.Errorf("queuedTasks = %v, 期望 1", m["queuedTasks"])
	}
	if m["failedTasks"].(float64) != 1 {
		t.Errorf("failedTasks = %v, 期望 1", m["failedTasks"])
	}
	if m["completedTotal"].(float64) != 1 {
		t.Errorf("completedTotal = %v, 期望 1", m["completedTotal"])
	}
	if m["onlineServers"].(float64) != 1 {
		t.Errorf("onlineServers = %v, 期望 1", m["onlineServers"])
	}
	if m["offlineServers"].(float64) != 1 {
		t.Errorf("offlineServers = %v, 期望 1", m["offlineServers"])
	}
	if m["nodeCapsCount"].(float64) != 0 {
		t.Errorf("nodeCapsCount = %v, 期望 0", m["nodeCapsCount"])
	}
}

func TestMetricsPrometheusEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := newMetricsTestEnv(t)
	r := gin.New()
	r.GET("/metrics/prometheus", h.metricsPrometheus)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics/prometheus", nil)
	r.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("GET /metrics/prometheus 状态码 = %d, 期望 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`# TYPE fvcc_tasks_total gauge`,
		`fvcc_tasks_total{status="QUEUE"} 1`,
		`fvcc_tasks_total{status="RUNNING"} 1`,
		`fvcc_tasks_total{status="FAILED"} 1`,
		`fvcc_tasks_active 3`,
		`fvcc_tasks_failed 1`,
		`# TYPE fvcc_tasks_completed_total counter`,
		`fvcc_tasks_completed_total 1`,
		`fvcc_servers_total{status="online"} 1`,
		`fvcc_servers_total{status="offline"} 1`,
		`fvcc_node_caps_count 0`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("Prometheus 输出缺少 %q\n--- 实际输出 ---\n%s", want, body)
		}
	}
}
