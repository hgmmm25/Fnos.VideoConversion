package main

// B-08：渲染节点选机 / 健康分 / 熔断 / 能力落库（06 §5.1~§5.4）。
//
// 设计要点：
//  1. 选机（§5.3）：显式 serverId 优先（离线/维护→E_NODE_OFFLINE 冷却，忙→排队不换机）；
//     未指定则「打分降级」：0.5×空闲槽占比 + 0.3×健康分 + 0.2×编码能力匹配，
//     全忙时退化为「最早可能空闲」节点排队。
//  2. 健康分（§5.2）：100 − 40×近 1h 失败率 − 15×(24h 有卡死) − 10×(1h 挂载失败≥2) − 20×(离线)。
//  3. 熔断（§5.4）：同一节点 5 分钟内连续 3 次失败 → 熔断 10 分钟（自动选机跳过）；
//     熔断到期后允许接 1 个探测任务，成功即恢复（清空失败窗口）。
//  4. 能力落库（§5.1）：节点 Hello 上报 → node_caps（UpsertNodeCaps）+ 按真实健康分广播 node_status。
//
// 指标窗口（尝试/失败/卡死/挂载失败时刻）保存在内存：节点重启或 FVCC 重启后自愈重算，
// 不落盘，避免为「近 N 分钟」语义引入额外持久化表（06 §2.1 未定义该表）。

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"fvcc/logger"
)

const (
	// minAgentVersionForRender 支持 RENDER_EDL 的最低节点 Agent 版本（06 §5.3「版本不匹配」）。
	minAgentVersionForRender = "0.2.0"
	// defaultNodeMaxConcurrent node_caps.maxConcurrent 缺失时的并发缺省值。
	defaultNodeMaxConcurrent = 1
	// proxySlotCost 代理任务额外占用的槽位数（04 §3.5：代理任务再减 1）。
	proxySlotCost = 1
	// 熔断参数（06 §5.4）：同一节点 5 分钟内连续 3 次失败 → 熔断 10 分钟。
	nodeCircuitWindow        = 5 * time.Minute
	nodeCircuitFailThreshold = 3
	nodeCircuitDuration      = 10 * time.Minute
	// 健康分窗口（06 §5.2）。
	healthFailWindow         = time.Hour
	healthStallWindow        = 24 * time.Hour
	healthMountFailWindow    = time.Hour
	healthMountFailThreshold = 2
	healthOfflinePenalty     = 20.0
	healthFailPenalty        = 40.0
	healthStallPenalty       = 15.0
	healthMountFailPenalty   = 10.0
	// 选机打分权重（06 §5.3）。
	pickWeightFreeSlots  = 0.5
	pickWeightHealth     = 0.3
	pickWeightCapability = 0.2
	capabilityHitScore   = 100.0
	capabilityMissScore  = 20.0
	// proxyEncoder 代理生成所需的软编编码器（04 §3.5）。
	proxyEncoder = "libx264"
)

var (
	// errNodeUnavailable 目标节点不可用（离线/维护/版本过低），按 E_NODE_OFFLINE 冷却重试。
	errNodeUnavailable = errors.New(errCodeNodeOffline)
	// errNoSelectableNode 无任何可选节点（全部离线/熔断/忙且无排队候选）。
	errNoSelectableNode = errors.New(errCodeNodeOffline)
)

// nodeMetrics 节点运行指标的内存窗口（B-08）。
type nodeMetrics struct {
	attempts     []time.Time // 派发尝试时刻
	failures     []time.Time // 派发失败时刻
	stalls       []time.Time // 卡死时刻（E_TIMEOUT_STALL）
	mountFails   []time.Time // SMB 挂载失败时刻（E_SMB_MOUNT_FAILED）
	circuitUntil time.Time   // 熔断截止时刻
	probing      bool        // 熔断到期后等待探测任务
}

// withMetrics 在锁内访问（必要时创建）节点指标，避免 WS 回传与调度 tick 并发读写。
func (s *Scheduler) withMetrics(serverID string, fn func(m *nodeMetrics)) {
	if serverID == "" {
		return
	}
	s.nodeMu.Lock()
	defer s.nodeMu.Unlock()
	if s.nodes == nil {
		s.nodes = make(map[string]*nodeMetrics)
	}
	m := s.nodes[serverID]
	if m == nil {
		m = &nodeMetrics{}
		s.nodes[serverID] = m
	}
	fn(m)
}

// trimBefore 返回窗口内（now-window, now] 的时刻切片（原地截断，依赖时刻单调递增）。
func trimBefore(ts []time.Time, now time.Time, window time.Duration) []time.Time {
	cut := now.Add(-window)
	out := ts[:0]
	for _, t := range ts {
		if t.After(cut) {
			out = append(out, t)
		}
	}
	return out
}

// countWithin 统计窗口内时刻数（只读）。
func countWithin(ts []time.Time, now time.Time, window time.Duration) int {
	cut := now.Add(-window)
	n := 0
	for _, t := range ts {
		if t.After(cut) {
			n++
		}
	}
	return n
}

// ===== 指标记录 =====

// NoteNodeAttempt 记录一次派发尝试（06 §5.2 失败率分母）。
func (s *Scheduler) NoteNodeAttempt(serverID string, now time.Time) {
	s.withMetrics(serverID, func(m *nodeMetrics) {
		m.attempts = append(trimBefore(m.attempts, now, healthFailWindow), now)
	})
}

// NoteNodeFailure 记录一次派发失败，并按错误码归集卡死/挂载失败，必要时触发熔断（06 §5.4）。
func (s *Scheduler) NoteNodeFailure(serverID, code string, now time.Time) {
	s.withMetrics(serverID, func(m *nodeMetrics) {
		m.failures = append(trimBefore(m.failures, now, healthFailWindow), now)
		switch code {
		case errCodeTimeoutStall:
			m.stalls = append(trimBefore(m.stalls, now, healthStallWindow), now)
		case errCodeSMBMountFailed:
			m.mountFails = append(trimBefore(m.mountFails, now, healthMountFailWindow), now)
		}
		// 熔断判定：同一节点 5 分钟内连续 3 次失败 → 熔断 10 分钟。
		if len(trimBefore(m.failures, now, nodeCircuitWindow)) >= nodeCircuitFailThreshold {
			m.circuitUntil = now.Add(nodeCircuitDuration)
			m.probing = false
		}
	})
}

// NoteNodeSuccess 记录一次派发成功：探测任务成功即恢复（清空失败窗口并解除熔断）。
func (s *Scheduler) NoteNodeSuccess(serverID string, now time.Time) {
	s.withMetrics(serverID, func(m *nodeMetrics) {
		m.failures = nil
		m.stalls = nil
		m.mountFails = nil
		m.circuitUntil = time.Time{}
		m.probing = false
	})
}

// nodeCircuitOpen 判断节点是否处于熔断期（06 §5.4：熔断期内不被自动选机命中）。
// 熔断到期后放行并标记 probing，允许接 1 个探测任务（成功后由 NoteNodeSuccess 恢复）。
func (s *Scheduler) nodeCircuitOpen(serverID string, now time.Time) bool {
	open := false
	s.withMetrics(serverID, func(m *nodeMetrics) {
		if m.circuitUntil.IsZero() {
			return
		}
		if now.Before(m.circuitUntil) {
			open = true
			return
		}
		m.probing = true
	})
	return open
}

// ===== 健康分（06 §5.2）=====

// HealthScoreOf 计算节点健康分（0~100），供 WS node_status 与选机打分使用。
func (s *Scheduler) HealthScoreOf(serverID string) int {
	return s.healthScoreAt(serverID, time.Now())
}

func (s *Scheduler) healthScoreAt(serverID string, now time.Time) int {
	var attempts, failures, stalls, mountFails int
	s.withMetrics(serverID, func(m *nodeMetrics) {
		attempts = countWithin(m.attempts, now, healthFailWindow)
		failures = countWithin(m.failures, now, healthFailWindow)
		stalls = countWithin(m.stalls, now, healthStallWindow)
		mountFails = countWithin(m.mountFails, now, healthMountFailWindow)
	})

	score := 100.0
	if attempts > 0 {
		rate := float64(failures) / float64(attempts)
		if rate > 1 {
			rate = 1
		}
		score -= healthFailPenalty * rate
	}
	if stalls >= 1 {
		score -= healthStallPenalty
	}
	if mountFails >= healthMountFailThreshold {
		score -= healthMountFailPenalty
	}
	if sv, ok := s.store.GetServer(serverID); !ok || sv.Status != "online" {
		score -= healthOfflinePenalty
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	return int(math.Round(score))
}

// ===== 槽位与能力 =====

// nodeMaxConcurrent 节点最大并发（node_caps.maxConcurrent，缺失回落到缺省 1）。
func (s *Scheduler) nodeMaxConcurrent(serverID string) int {
	if caps, ok := s.store.GetNodeCaps(serverID); ok && caps.MaxConcurrent > 0 {
		return caps.MaxConcurrent
	}
	return defaultNodeMaxConcurrent
}

// runningCount 统计节点上占用槽位的任务数（上传/待转码/转码/待下载/下载）。
func (s *Scheduler) runningCount(serverID string) int {
	n := 0
	for _, t := range s.store.GetTasks() {
		if t.ServerID != serverID || t.Status.IsTerminal() {
			continue
		}
		switch t.Status {
		case StatusUploading, StatusWaitingTrans, StatusTranscoding, StatusWaitingDown, StatusDownloading:
			n++
		}
	}
	return n
}

// NodeFreeSlots 节点空闲槽位 = maxConcurrent − running（代理任务再减 1，04 §3.5）。
func (s *Scheduler) NodeFreeSlots(serverID string, t Task) int {
	free := s.nodeMaxConcurrent(serverID) - s.runningCount(serverID)
	if t.TaskType == TaskTypeGenProxy {
		free -= proxySlotCost
	}
	if free < 0 {
		free = 0
	}
	return free
}

// requiredEncoder 返回任务所需的视频编码器；`copy` 与未知取值为空（不参与能力扣分）。
func requiredEncoder(t Task) string {
	if t.TaskType == TaskTypeGenProxy {
		return proxyEncoder
	}
	if strings.TrimSpace(t.PayloadJSON) == "" {
		return ""
	}
	var p RenderTaskPayload
	if err := json.Unmarshal([]byte(t.PayloadJSON), &p); err != nil {
		return ""
	}
	codec := strings.TrimSpace(p.Profile.Video.Codec)
	if codec == "" || strings.EqualFold(codec, "copy") {
		return ""
	}
	return codec
}

// capabilityMatch 能力匹配分：命中所需编码器=100；未命中=20（可软编兜底）；
// 所需编码器未知或节点能力未上报时不惩罚（按命中计）。
func (s *Scheduler) capabilityMatch(serverID string, t Task) float64 {
	need := requiredEncoder(t)
	if need == "" {
		return capabilityHitScore
	}
	caps, ok := s.store.GetNodeCaps(serverID)
	if !ok || len(caps.Encoders) == 0 {
		return capabilityHitScore
	}
	for _, e := range caps.Encoders {
		if strings.EqualFold(strings.TrimSpace(e), need) {
			return capabilityHitScore
		}
	}
	return capabilityMissScore
}

// agentVersionAtLeast 语义化版本比较（段数不等按 0 补齐）；空版本视为满足（不排除节点）。
func agentVersionAtLeast(got, want string) bool {
	got, want = strings.TrimSpace(got), strings.TrimSpace(want)
	if got == "" || want == "" {
		return true
	}
	gs, ws := strings.Split(got, "."), strings.Split(want, ".")
	for i := 0; i < len(gs) || i < len(ws); i++ {
		g, w := 0, 0
		if i < len(gs) {
			g = leadingInt(gs[i])
		}
		if i < len(ws) {
			w = leadingInt(ws[i])
		}
		if g != w {
			return g > w
		}
	}
	return true
}

// leadingInt 取版本段的前导数字（"3-beta" → 3），无数字返回 0。
func leadingInt(seg string) int {
	seg = strings.TrimSpace(seg)
	i := 0
	for i < len(seg) && seg[i] >= '0' && seg[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0
	}
	n, err := strconv.Atoi(seg[:i])
	if err != nil {
		return 0
	}
	return n
}

// nodeVersionOK 节点 Agent 版本是否支持渲染（06 §5.3）。
func (s *Scheduler) nodeVersionOK(serverID string) bool {
	caps, ok := s.store.GetNodeCaps(serverID)
	if !ok {
		return true // 能力未上报：不排除，交由下发结果兜底
	}
	return agentVersionAtLeast(caps.AgentVersion, minAgentVersionForRender)
}

// nodeSelectable 自动选机的节点准入：在线 + 版本达标 + 非熔断。
func (s *Scheduler) nodeSelectable(sv Server, now time.Time) bool {
	if sv.IsLocal || sv.Status != "online" {
		return false
	}
	if !s.nodeVersionOK(sv.ID) {
		return false
	}
	return !s.nodeCircuitOpen(sv.ID, now)
}

// onlineNodes 返回可参与自动选机的节点（06 §5.3）。
func (s *Scheduler) onlineNodes(now time.Time) []Server {
	var out []Server
	for _, sv := range s.store.GetServers() {
		if s.nodeSelectable(sv, now) {
			out = append(out, sv)
		}
	}
	return out
}

// earliestFreeNode 全忙时的排队候选：按已占用槽位升序，取最早可能空闲的节点（06 §5.3）。
func (s *Scheduler) earliestFreeNode(cands []Server, t Task, now time.Time) *Server {
	var best *Server
	bestRunning := math.MaxInt32
	for i := range cands {
		n := cands[i]
		if !s.nodeSelectable(n, now) {
			continue
		}
		r := s.runningCount(n.ID)
		if r < bestRunning {
			c := n
			best = &c
			bestRunning = r
		}
	}
	return best
}

// pickNode 选机（06 §5.3）：显式 serverId 优先（忙则排队不换机），否则打分降级选优。
// 返回 error 时，调用方按 classifyRenderError 归码（E_NODE_OFFLINE 属可重试白名单）。
func (s *Scheduler) pickNode(t Task, now time.Time) (Server, error) {
	if t.ServerID != "" {
		sv, found := s.store.GetServer(t.ServerID)
		if !found {
			return Server{}, errCodeToError(errCodeNodeNotFound)
		}
		if sv.IsLocal || sv.Status != "online" || !s.nodeVersionOK(sv.ID) {
			return Server{}, errNodeUnavailable
		}
		// 空闲槽为 0 时仍返回该节点：由传输/编码锁排队等待，尊重用户显式选择（不换机）。
		return sv, nil
	}

	cands := s.onlineNodes(now)
	if len(cands) == 0 {
		return Server{}, errNoSelectableNode
	}

	var best *Server
	bestScore := -1.0
	for i := range cands {
		n := cands[i]
		free := s.NodeFreeSlots(n.ID, t)
		if free <= 0 {
			continue
		}
		maxC := s.nodeMaxConcurrent(n.ID)
		if maxC <= 0 {
			maxC = defaultNodeMaxConcurrent
		}
		sc := pickWeightFreeSlots*float64(free)/float64(maxC)*100 +
			pickWeightHealth*float64(s.healthScoreAt(n.ID, now)) +
			pickWeightCapability*s.capabilityMatch(n.ID, t)
		if sc > bestScore {
			c := n
			best = &c
			bestScore = sc
		}
	}
	if best != nil {
		return *best, nil
	}
	if n := s.earliestFreeNode(cands, t, now); n != nil {
		return *n, nil
	}
	return Server{}, errNoSelectableNode
}

// errCodeToError 把线协议错误码包装为 error（供 classifyRenderError 解析）。
func errCodeToError(code string) error { return errors.New(code) }

// ===== 节点 Hello 能力落库（06 §5.1）=====

// ApplyNodeHello 处理节点能力上报：刷新 node_caps，并按真实健康分广播 node_status。
func (s *Scheduler) ApplyNodeHello(caps NodeCaps, now time.Time) {
	if caps.ServerID == "" {
		return
	}
	if caps.UpdatedAt.IsZero() {
		caps.UpdatedAt = now
	}
	s.store.UpsertNodeCaps(caps)

	status := "online"
	if sv, ok := s.store.GetServer(caps.ServerID); ok && sv.Status != "" {
		status = sv.Status
	}
	reason := "能力上报"
	if !agentVersionAtLeast(caps.AgentVersion, minAgentVersionForRender) {
		reason = "能力上报（Agent 版本低于 " + minAgentVersionForRender + "，不参与渲染选机）"
	}
	s.hub.BroadcastNodeStatus(caps.ServerID, status, s.healthScoreAt(caps.ServerID, now), reason)
	logger.Info("scheduler", "节点能力已落库: server=%s ver=%s maxConcurrent=%d encoders=%d",
		caps.ServerID, caps.AgentVersion, caps.MaxConcurrent, len(caps.Encoders))
}
