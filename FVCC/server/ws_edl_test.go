package main

// B-07 验收测试之一（06 §6 进度与事件聚合）：
//  1) BroadcastTaskUpdateFull：500ms 聚合节流（窗口内不追加发送、到期补发最新值）、
//     进度未变不广播、终态立即广播、progress 越界钳制、空 taskID 丢弃；
//  2) BroadcastProxyReady：同一 assetId 10s 内去重、窗口到期恢复、空 assetId 拒绝；
//  3) BroadcastNodeStatus：状态变化或 healthScore 跨 20 分档广播、健康分未知仅按状态变化。

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// ===== 广播出口录制器（替代真实 WS 连接，捕获事件载荷）=====

type wsEventRecorder struct {
	mu  sync.Mutex
	raw [][]byte
}

func newWSRecorder() *wsEventRecorder { return &wsEventRecorder{} }

func (r *wsEventRecorder) hook(b []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raw = append(r.raw, append([]byte(nil), b...))
}

func (r *wsEventRecorder) count(typ string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, b := range r.raw {
		var m map[string]interface{}
		if json.Unmarshal(b, &m) == nil && m["type"] == typ {
			n++
		}
	}
	return n
}

func (r *wsEventRecorder) events(t *testing.T, typ string) []map[string]interface{} {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []map[string]interface{}
	for _, b := range r.raw {
		var m map[string]interface{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("广播内容不是合法 JSON: %v (%s)", err, string(b))
		}
		if m["type"] == typ {
			out = append(out, m)
		}
	}
	return out
}

func jsonNum(t *testing.T, m map[string]interface{}, key string) float64 {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("事件缺少字段 %s: %+v", key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("字段 %s 不是数字: %T(%v)", key, v, v)
	}
	return f
}

func jsonStr(t *testing.T, m map[string]interface{}, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("事件缺少字段 %s: %+v", key, m)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("字段 %s 不是字符串: %T(%v)", key, v, v)
	}
	return s
}

// ---------- 1) task_update 500ms 聚合节流 ----------

func TestB07TaskUpdateFullAggregation(t *testing.T) {
	hub := NewHub()
	rec := newWSRecorder()
	hub.emitHook = rec.hook

	// 首帧立即广播
	hub.BroadcastTaskUpdateFull("t1", "RUNNING", 10, StagePrepare, nil, "准备中", 0, 0, "")
	if got := rec.count("task_update"); got != 1 {
		t.Fatalf("首帧应立即广播，实际 %d 条", got)
	}

	// 500ms 窗口内：不追加发送
	hub.BroadcastTaskUpdateFull("t1", "RUNNING", 20, StageSegment,
		&SegInfo{Index: 1, Total: 5}, "第 1/5 段", 1000, 30000, "1.2x")
	if got := rec.count("task_update"); got != 1 {
		t.Fatalf("聚合窗口内不应追加广播，实际 %d 条", got)
	}

	// 窗口到期：补发最新值（取最新，不丢帧）
	time.Sleep(taskUpdateAggInterval + 250*time.Millisecond)
	evs := rec.events(t, "task_update")
	if len(evs) != 2 {
		t.Fatalf("窗口到期应补发最新值，实际广播 %d 条", len(evs))
	}
	last := evs[len(evs)-1]
	if got := jsonNum(t, last, "progress"); got != 20 {
		t.Fatalf("补发内容应为最新进度 20，实际 %v", got)
	}
	if got := jsonStr(t, last, "stage"); got != StageSegment {
		t.Fatalf("补发内容 stage 应为 %s，实际 %s", StageSegment, got)
	}
	if got := jsonStr(t, last, "message"); got != "第 1/5 段" {
		t.Fatalf("补发内容 message 应为「第 1/5 段」，实际 %s", got)
	}
	seg, ok := last["seg"].(map[string]interface{})
	if !ok {
		t.Fatalf("补发内容缺少 seg 字段: %+v", last)
	}
	if jsonNum(t, seg, "index") != 1 || jsonNum(t, seg, "total") != 5 {
		t.Fatalf("seg 字段错误: %+v", seg)
	}
	if got := jsonNum(t, last, "outTimeMs"); got != 1000 {
		t.Fatalf("outTimeMs 应为 1000，实际 %v", got)
	}
	if got := jsonNum(t, last, "totalMs"); got != 30000 {
		t.Fatalf("totalMs 应为 30000，实际 %v", got)
	}
	if got := jsonStr(t, last, "speed"); got != "1.2x" {
		t.Fatalf("speed 应为 1.2x，实际 %s", got)
	}

	// 内容完全一致（窗口已过）：进度未变不广播
	hub.BroadcastTaskUpdateFull("t1", "RUNNING", 20, StageSegment,
		&SegInfo{Index: 1, Total: 5}, "第 1/5 段", 1000, 30000, "1.2x")
	if got := rec.count("task_update"); got != 2 {
		t.Fatalf("进度未变不应广播，实际 %d 条", got)
	}

	// 终态：立即广播，不受 500ms 节流约束
	hub.BroadcastTaskUpdateFull("t1", "SUCCESS", 100, StageFinalize,
		&SegInfo{Index: 5, Total: 5}, "收尾中", 30000, 30000, "2.0x")
	if got := rec.count("task_update"); got != 3 {
		t.Fatalf("终态应立即广播，实际 %d 条", got)
	}
	// 终态帧内容：状态与进度均正确落到线协议
	final := rec.events(t, "task_update")[2]
	if got := jsonStr(t, final, "status"); got != "SUCCESS" {
		t.Fatalf("终态 status 应为 SUCCESS，实际 %s", got)
	}
	if got := jsonNum(t, final, "progress"); got != 100 {
		t.Fatalf("终态 progress 应为 100，实际 %v", got)
	}
}

func TestB07TaskUpdateFullGuards(t *testing.T) {
	hub := NewHub()
	rec := newWSRecorder()
	hub.emitHook = rec.hook

	// 空 taskID：直接丢弃
	hub.BroadcastTaskUpdateFull("", "RUNNING", 10, StagePrepare, nil, "", 0, 0, "")
	if got := rec.count("task_update"); got != 0 {
		t.Fatalf("空 taskID 不应广播，实际 %d 条", got)
	}

	// 下界钳制 -5 → 0；上界钳制 150 → 100
	hub.BroadcastTaskUpdateFull("t2", "RUNNING", -5, "", nil, "", 0, 0, "")
	hub.BroadcastTaskUpdateFull("t3", "RUNNING", 150, "", nil, "", 0, 0, "")

	evs := rec.events(t, "task_update")
	if len(evs) != 2 {
		t.Fatalf("应广播 2 条，实际 %d 条", len(evs))
	}
	if got := jsonNum(t, evs[0], "progress"); got != 0 {
		t.Fatalf("进度下界应钳制为 0，实际 %v", got)
	}
	if got := jsonNum(t, evs[1], "progress"); got != 100 {
		t.Fatalf("进度上界应钳制为 100，实际 %v", got)
	}
	for _, ev := range evs {
		if _, ok := ev["stage"]; ok {
			t.Fatalf("stage 为空时不应出现该字段: %+v", ev)
		}
		if _, ok := ev["seg"]; ok {
			t.Fatalf("seg 为 nil 时不应出现该字段: %+v", ev)
		}
	}
}

// ---------- 2) proxy_ready 去重 ----------

func TestB07ProxyReadyDedupe(t *testing.T) {
	hub := NewHub()
	rec := newWSRecorder()
	hub.emitHook = rec.hook

	base := time.Now()
	cur := base
	hub.clock = func() time.Time { return cur }

	if !hub.BroadcastProxyReady("asset_1", map[string]interface{}{"proxyPath": "a_proxy.mp4"}) {
		t.Fatal("首次 proxy_ready 应广播")
	}
	if hub.BroadcastProxyReady("asset_1", nil) {
		t.Fatal("10s 内同一 assetId 应去重")
	}
	if got := rec.count("proxy_ready"); got != 1 {
		t.Fatalf("去重窗口内应只广播 1 条，实际 %d 条", got)
	}

	cur = base.Add(9 * time.Second)
	if hub.BroadcastProxyReady("asset_1", nil) {
		t.Fatal("9s 时仍应去重")
	}

	cur = base.Add(proxyReadyDedupeWindow)
	if !hub.BroadcastProxyReady("asset_1", nil) {
		t.Fatal("去重窗口到期应重新广播")
	}
	if got := rec.count("proxy_ready"); got != 2 {
		t.Fatalf("窗口到期后应共广播 2 条，实际 %d 条", got)
	}

	// 不同 assetId 互不影响
	if !hub.BroadcastProxyReady("asset_2", nil) {
		t.Fatal("不同 assetId 应各自广播")
	}
	// 空 assetId 无法去重：直接拒绝
	if hub.BroadcastProxyReady("", nil) {
		t.Fatal("空 assetId 不应广播")
	}

	evs := rec.events(t, "proxy_ready")
	if len(evs) != 3 {
		t.Fatalf("应共广播 3 条 proxy_ready，实际 %d 条", len(evs))
	}
	if got := jsonStr(t, evs[0], "assetId"); got != "asset_1" {
		t.Fatalf("assetId 应为 asset_1，实际 %s", got)
	}
	if got := jsonStr(t, evs[2], "assetId"); got != "asset_2" {
		t.Fatalf("第三条 assetId 应为 asset_2，实际 %s", got)
	}
}

// ---------- 3) node_status 变化与分档 ----------

func TestB07NodeStatusBroadcast(t *testing.T) {
	hub := NewHub()
	rec := newWSRecorder()
	hub.emitHook = rec.hook

	cases := []struct {
		status string
		score  int
		want   bool
		desc   string
	}{
		{"online", 90, true, "首次上线"},
		{"online", 90, false, "无变化"},
		{"online", 75, true, "健康分跨档 4→3"},
		{"online", 78, false, "同档（3）"},
		{"offline", 80, true, "状态变化 online→offline"},
		{"offline", healthScoreUnknown, false, "同状态且健康分未知"},
		{"online", 85, true, "状态变化 offline→online"},
	}
	for _, c := range cases {
		if got := hub.BroadcastNodeStatus("srv1", c.status, c.score, "test"); got != c.want {
			t.Fatalf("%s：期望广播=%v，实际 %v", c.desc, c.want, got)
		}
	}

	evs := rec.events(t, "node_status")
	if len(evs) != 4 {
		t.Fatalf("应共广播 4 条 node_status，实际 %d 条", len(evs))
	}
	if got := jsonStr(t, evs[0], "serverId"); got != "srv1" {
		t.Fatalf("serverId 应为 srv1，实际 %s", got)
	}
	if got := jsonNum(t, evs[0], "healthScore"); got != 90 {
		t.Fatalf("healthScore 应为 90，实际 %v", got)
	}
	if got := jsonStr(t, evs[4-1], "status"); got != "online" {
		t.Fatalf("最后一条 status 应为 online，实际 %s", got)
	}

	// 空 serverID / status 拒绝
	if hub.BroadcastNodeStatus("", "online", 90, "") {
		t.Fatal("空 serverID 不应广播")
	}
	if hub.BroadcastNodeStatus("srv1", "", 90, "") {
		t.Fatal("空 status 不应广播")
	}
}
