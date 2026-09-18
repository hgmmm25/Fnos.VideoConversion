package main

// B-08 验收测试（06 §5.1~§5.4）：
//  1) 选机（§5.3）：显式 serverId 优先（离线/不存在归码、满槽排队不换机）；
//     自动选机按 0.5×空闲槽占比 + 0.3×健康分 + 0.2×能力匹配打分；全忙退化为最早空闲节点；
//  2) 健康分（§5.2）：失败率 −40、卡死 −15、挂载失败 −10、离线 −20，clamp[0,100]；
//  3) 熔断（§5.4）：5 分钟内 3 次失败 → 熔断 10 分钟（自动选机跳过、显式指定仍排队）；
//     到期后放行 1 个探测任务，成功即恢复；窗口外失败不计入；
//  4) 能力落库（§5.1）：Hello → node_caps 落库 + node_status 广播（20 分档去重），
//     低版本节点不参与渲染选机；Hello 载荷 camelCase/snake_case/嵌套三种形态兼容。

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func newB08Env(t *testing.T) (*Scheduler, *Store, *wsEventRecorder) {
	t.Helper()
	s := NewStore(t.TempDir())
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	s.UpsertServer(Server{ID: "srv1", Name: "节点一", Status: "online"})
	s.UpsertServer(Server{ID: "srv2", Name: "节点二", Status: "online"})
	s.UpsertServer(Server{ID: "srv_off", Name: "离线节点", Status: "offline"})
	hub := NewHub()
	rec := newWSRecorder()
	hub.emitHook = rec.hook
	return NewScheduler(s, NewRemoteClient(), hub, nil), s, rec
}

func b08Caps(id string, maxConcurrent int, encoders ...string) NodeCaps {
	return NodeCaps{ServerID: id, AgentVersion: "1.0.0", MaxConcurrent: maxConcurrent, Encoders: encoders}
}

func b08Probing(sch *Scheduler, id string) bool {
	v := false
	sch.withMetrics(id, func(m *nodeMetrics) { v = m.probing })
	return v
}

// ---------- 1) 显式节点优先 ----------

func TestB08PickNodeExplicitPreference(t *testing.T) {
	sch, s, _ := newB08Env(t)
	now := time.Now()

	// 显式指定在线节点 → 原样返回，不做打分比较（即便另一节点更空闲）。
	s.UpsertNodeCaps(b08Caps("srv1", 1))
	s.UpsertNodeCaps(b08Caps("srv2", 8))
	got, err := sch.pickNode(Task{ID: "t1", TaskType: TaskTypeRenderEDL, ServerID: "srv1"}, now)
	if err != nil || got.ID != "srv1" {
		t.Fatalf("显式指定在线节点应原样返回 srv1: %+v err=%v", got, err)
	}

	// 显式指定离线节点 → E_NODE_OFFLINE（可重试白名单）。
	if _, err := sch.pickNode(Task{ServerID: "srv_off"}, now); !errors.Is(err, errNodeUnavailable) {
		t.Fatalf("离线节点应返回 E_NODE_OFFLINE，实际 %v", err)
	}

	// 节点不存在 → E_NODE_NOT_FOUND（不可重试）。
	_, err = sch.pickNode(Task{ServerID: "srv_ghost"}, now)
	if err == nil || err.Error() != errCodeNodeNotFound {
		t.Fatalf("节点不存在应返回 %s，实际 %v", errCodeNodeNotFound, err)
	}

	// 显式节点满槽 → 仍返回该节点排队，不换机。
	s.UpsertTask(Task{ID: "t_busy", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "r1"})
	if free := sch.NodeFreeSlots("srv1", Task{TaskType: TaskTypeRenderEDL}); free != 0 {
		t.Fatalf("srv1 应无空闲槽，实际 %d", free)
	}
	got, err = sch.pickNode(Task{ID: "t2", TaskType: TaskTypeRenderEDL, ServerID: "srv1"}, now)
	if err != nil || got.ID != "srv1" {
		t.Fatalf("满槽的显式节点应排队不换机: %+v err=%v", got, err)
	}
}

// ---------- 2) 自动选机（空闲槽 / 健康分 / 能力）----------

func TestB08AutoSelectPrefersFreeCapacity(t *testing.T) {
	sch, s, _ := newB08Env(t)
	s.UpsertNodeCaps(b08Caps("srv1", 2))
	s.UpsertNodeCaps(b08Caps("srv2", 2))
	// srv1 已占 1 槽（free 1/2），srv2 全空（2/2）→ srv2 胜出。
	s.UpsertTask(Task{ID: "t_use", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeRenderEDL,
		ServerID: "srv1", RemoteTaskID: "r1"})

	got, err := sch.pickNode(Task{ID: "t_auto", TaskType: TaskTypeRenderEDL}, time.Now())
	if err != nil || got.ID != "srv2" {
		t.Fatalf("空闲槽占比高者应胜出(srv2): %+v err=%v", got, err)
	}
}

func TestB08AutoSelectPrefersHealthyNode(t *testing.T) {
	sch, s, _ := newB08Env(t)
	s.UpsertNodeCaps(b08Caps("srv1", 2))
	s.UpsertNodeCaps(b08Caps("srv2", 2))
	now := time.Now()

	// srv1 近 1h 失败率 100% → 健康分 60（同空闲槽下退居 srv2）。
	sch.NoteNodeAttempt("srv1", now.Add(-time.Minute))
	sch.NoteNodeFailure("srv1", errCodeRenderFailed, now.Add(-30*time.Second))
	if got := sch.healthScoreAt("srv1", now); got != 60 {
		t.Fatalf("100%% 失败率健康分应为 60，实际 %d", got)
	}
	if got := sch.healthScoreAt("srv2", now); got != 100 {
		t.Fatalf("无指标节点健康分应为 100，实际 %d", got)
	}

	got, err := sch.pickNode(Task{ID: "t_auto", TaskType: TaskTypeRenderEDL}, now)
	if err != nil || got.ID != "srv2" {
		t.Fatalf("健康分高者应胜出(srv2): %+v err=%v", got, err)
	}
}

func TestB08AutoSelectPrefersCapabilityMatch(t *testing.T) {
	sch, s, _ := newB08Env(t)
	s.UpsertNodeCaps(b08Caps("srv1", 2, "libx264"))
	s.UpsertNodeCaps(b08Caps("srv2", 2, "hevc_videotoolbox"))
	payload := `{"sourceRoot":"a","destRoot":"b","profile":{"video":{"codec":"hevc_videotoolbox"}}}`

	got, err := sch.pickNode(Task{ID: "t_cap", TaskType: TaskTypeRenderEDL, PayloadJSON: payload}, time.Now())
	if err != nil || got.ID != "srv2" {
		t.Fatalf("编码能力命中者应胜出(srv2): %+v err=%v", got, err)
	}
	if m := sch.capabilityMatch("srv1", Task{TaskType: TaskTypeRenderEDL, PayloadJSON: payload}); m != capabilityMissScore {
		t.Fatalf("未命中编码器应得 %.0f 分，实际 %.0f", capabilityMissScore, m)
	}
	// codec=copy 或能力未上报 → 不惩罚（按命中计）。
	if m := sch.capabilityMatch("srv1", Task{TaskType: TaskTypeRenderEDL,
		PayloadJSON: `{"profile":{"video":{"codec":"copy"}}}`}); m != capabilityHitScore {
		t.Fatalf("copy 不应参与能力扣分，实际 %.0f", m)
	}
	if m := sch.capabilityMatch("srv_no_caps", Task{TaskType: TaskTypeRenderEDL, PayloadJSON: payload}); m != capabilityHitScore {
		t.Fatalf("能力未上报不应惩罚，实际 %.0f", m)
	}
	// 代理任务固定要求软编 libx264。
	if enc := requiredEncoder(Task{TaskType: TaskTypeGenProxy}); enc != proxyEncoder {
		t.Fatalf("GEN_PROXY 所需编码器应为 %s，实际 %s", proxyEncoder, enc)
	}
}

func TestB08AllBusyFallsBackToEarliestFree(t *testing.T) {
	sch, s, _ := newB08Env(t)
	s.UpsertNodeCaps(b08Caps("srv1", 1))
	s.UpsertNodeCaps(b08Caps("srv2", 3))
	// srv1: 1/1 占用；srv2: 3/3 占用 → 全忙，取已占用槽位更少的 srv1。
	s.UpsertTask(Task{ID: "t1", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeRenderEDL, ServerID: "srv1"})
	for i, st := range []TaskStatus{StatusUploading, StatusTranscoding, StatusDownloading} {
		s.UpsertTask(Task{ID: "t2_" + string(rune('a'+i)), OrderID: int64(2 + i), Status: st,
			TaskType: TaskTypeRenderEDL, ServerID: "srv2"})
	}
	got, err := sch.pickNode(Task{ID: "t_auto", TaskType: TaskTypeRenderEDL}, time.Now())
	if err != nil || got.ID != "srv1" {
		t.Fatalf("全忙时应取最早可能空闲的节点(srv1): %+v err=%v", got, err)
	}

	// 所有在线节点均不可用（离线）→ E_NODE_OFFLINE（保持 QUEUE 由调用方处置）。
	s.UpsertServer(Server{ID: "srv1", Name: "节点一", Status: "offline"})
	s.UpsertServer(Server{ID: "srv2", Name: "节点二", Status: "offline"})
	if _, err := sch.pickNode(Task{ID: "t_none", TaskType: TaskTypeRenderEDL}, time.Now()); err == nil {
		t.Fatalf("无在线节点应返回错误")
	}
}

// ---------- 3) 健康分公式 ----------

func TestB08HealthScoreFormula(t *testing.T) {
	sch, _, _ := newB08Env(t)
	now := time.Now()

	// 基线：在线、无指标 → 100。
	if got := sch.healthScoreAt("srv1", now); got != 100 {
		t.Fatalf("基线健康分应为 100，实际 %d", got)
	}

	// 失败率 2/5=40% → −16；追加 1 次卡死（同时计入失败率 3/5=60% → −24）⇒ 61。
	for i := 0; i < 5; i++ {
		sch.NoteNodeAttempt("srv1", now.Add(-time.Duration(i+1)*time.Minute))
	}
	sch.NoteNodeFailure("srv1", errCodeRenderFailed, now.Add(-5*time.Minute))
	sch.NoteNodeFailure("srv1", errCodeRenderFailed, now.Add(-4*time.Minute))
	if got := sch.healthScoreAt("srv1", now); got != 84 {
		t.Fatalf("40%% 失败率应为 84，实际 %d", got)
	}
	sch.NoteNodeFailure("srv1", errCodeTimeoutStall, now.Add(-3*time.Minute))
	if got := sch.healthScoreAt("srv1", now); got != 61 {
		t.Fatalf("叠加卡死应为 61，实际 %d", got)
	}
	// 再追加 2 次挂载失败：失败率 5/5=100% → −40，卡死 −15，挂载失败 −10 ⇒ 35。
	sch.NoteNodeFailure("srv1", errCodeSMBMountFailed, now.Add(-2*time.Minute))
	sch.NoteNodeFailure("srv1", errCodeSMBMountFailed, now.Add(-time.Minute))
	if got := sch.healthScoreAt("srv1", now); got != 35 {
		t.Fatalf("叠加挂载失败应为 35，实际 %d", got)
	}

	// 离线节点叠加 −20；窗口外指标不计入。
	if got := sch.healthScoreAt("srv_off", now); got != 80 {
		t.Fatalf("离线无指标应为 80，实际 %d", got)
	}
	old := now.Add(-48 * time.Hour)
	sch.NoteNodeFailure("srv2", errCodeTimeoutStall, old)
	if got := sch.healthScoreAt("srv2", now); got != 100 {
		t.Fatalf("48h 前的卡死不应扣分，实际 %d", got)
	}

	// 四类扣分项齐备时的最低分：100 −40(失败率100%) −15(卡死) −10(挂载失败) −20(离线) = 15，
	// clamp[0,100] 保证不会出现负分。
	for i := 0; i < 3; i++ {
		at := now.Add(-time.Duration(i+1) * time.Minute)
		sch.NoteNodeAttempt("srv_off", at)
		sch.NoteNodeFailure("srv_off", errCodeTimeoutStall, at)
	}
	sch.NoteNodeFailure("srv_off", errCodeSMBMountFailed, now.Add(-2*time.Minute))
	sch.NoteNodeFailure("srv_off", errCodeSMBMountFailed, now.Add(-time.Minute))
	if got := sch.healthScoreAt("srv_off", now); got != 15 {
		t.Fatalf("四类指标齐备时为最低分 15，实际 %d", got)
	}
}

// ---------- 4) 熔断 ----------

func TestB08CircuitBreaker(t *testing.T) {
	sch, s, _ := newB08Env(t)
	s.UpsertNodeCaps(b08Caps("srv1", 4))
	s.UpsertNodeCaps(b08Caps("srv2", 4))
	base := time.Now().Add(-time.Minute)

	// 5 分钟内连续 3 次失败 → 熔断 10 分钟。
	for i := 0; i < nodeCircuitFailThreshold; i++ {
		at := base.Add(time.Duration(i) * 10 * time.Second)
		sch.NoteNodeAttempt("srv1", at)
		sch.NoteNodeFailure("srv1", errCodeRenderFailed, at)
	}
	now := base.Add(40 * time.Second)
	if !sch.nodeCircuitOpen("srv1", now) {
		t.Fatalf("3 次失败后应处于熔断期")
	}

	// 熔断节点不被自动选机命中；显式指定仍尊重用户选择（排队等待）。
	got, err := sch.pickNode(Task{ID: "t_auto", TaskType: TaskTypeRenderEDL}, now)
	if err != nil || got.ID != "srv2" {
		t.Fatalf("自动选机应跳过熔断节点: %+v err=%v", got, err)
	}
	if got, err := sch.pickNode(Task{ID: "t_pin", TaskType: TaskTypeRenderEDL, ServerID: "srv1"}, now); err != nil || got.ID != "srv1" {
		t.Fatalf("显式指定熔断节点应排队而非报错: %+v err=%v", got, err)
	}

	// 唯一候选熔断 → 无候选（调用方据此保持 QUEUE）。
	s.UpsertServer(Server{ID: "srv2", Name: "节点二", Status: "offline"})
	if _, err := sch.pickNode(Task{ID: "t_none", TaskType: TaskTypeRenderEDL}, now); !errors.Is(err, errNoSelectableNode) {
		t.Fatalf("唯一候选熔断应返回无候选错误，实际 %v", err)
	}
	s.UpsertServer(Server{ID: "srv2", Name: "节点二", Status: "online"})

	// 熔断到期 → 放行并标记探测，成功后恢复。
	later := base.Add(nodeCircuitDuration + time.Minute)
	if sch.nodeCircuitOpen("srv1", later) {
		t.Fatalf("熔断到期后应放行")
	}
	if !b08Probing(sch, "srv1") {
		t.Fatalf("熔断到期应标记探测中")
	}
	if _, err := sch.pickNode(Task{ID: "t_probe", TaskType: TaskTypeRenderEDL}, later); err != nil {
		t.Fatalf("熔断到期后应可被自动选机命中: %v", err)
	}
	sch.NoteNodeSuccess("srv1", later)
	if b08Probing(sch, "srv1") || sch.nodeCircuitOpen("srv1", later) {
		t.Fatalf("探测成功后应解除熔断与探测标记")
	}
	if got := sch.healthScoreAt("srv1", later); got != 100 {
		t.Fatalf("成功后失败窗口应清空，健康分回到 100，实际 %d", got)
	}

	// 5 分钟窗口外的失败不计入熔断统计。
	sch2, _, _ := newB08Env(t)
	old := time.Now().Add(-30 * time.Minute)
	for i := 0; i < nodeCircuitFailThreshold; i++ {
		at := old.Add(time.Duration(i) * 10 * time.Minute)
		sch2.NoteNodeAttempt("srv1", at)
		sch2.NoteNodeFailure("srv1", errCodeRenderFailed, at)
	}
	if sch2.nodeCircuitOpen("srv1", time.Now()) {
		t.Fatalf("分散在 30 分钟内的失败不应触发熔断")
	}
}

// ---------- 5) Hello 能力落库与广播 ----------

func TestB08ApplyNodeHello(t *testing.T) {
	sch, s, rec := newB08Env(t)
	caps := NodeCaps{
		ServerID: "srv1", AgentVersion: "1.2.0", OS: "windows", CPUCores: 16,
		GPU:           []GPUInfo{{Vendor: "nvidia", Name: "RTX 4070", Encoders: []string{"h264_nvenc"}}},
		Encoders:      []string{"h264_nvenc", "libx264"},
		MaxConcurrent: 3, FFmpegPath: `D:\ffmpeg.exe`,
	}
	sch.ApplyNodeHello(caps, time.Now())

	got, ok := s.GetNodeCaps("srv1")
	if !ok {
		t.Fatalf("Hello 应把能力落库到 node_caps")
	}
	if got.AgentVersion != "1.2.0" || got.MaxConcurrent != 3 || got.OS != "windows" ||
		len(got.Encoders) != 2 || len(got.GPU) != 1 || got.FFmpegPath != `D:\ffmpeg.exe` {
		t.Fatalf("node_caps 落库字段不符: %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("UpdatedAt 应被填充")
	}
	if free := sch.NodeFreeSlots("srv1", Task{TaskType: TaskTypeRenderEDL}); free != 3 {
		t.Fatalf("空闲槽应取 node_caps.maxConcurrent=3，实际 %d", free)
	}

	evs := rec.events(t, "node_status")
	if len(evs) != 1 {
		t.Fatalf("Hello 应广播一次 node_status，实际 %d", len(evs))
	}
	if evs[0]["serverId"] != "srv1" || evs[0]["status"] != "online" {
		t.Fatalf("node_status 内容不符: %+v", evs[0])
	}
	if hs, ok := evs[0]["healthScore"].(float64); !ok || hs != 100 {
		t.Fatalf("node_status 应携带真实健康分 100: %+v", evs[0])
	}

	// 同状态同档位重复上报 → 去重不重复广播。
	sch.ApplyNodeHello(caps, time.Now())
	if n := rec.count("node_status"); n != 1 {
		t.Fatalf("同档位重复上报不应重复广播，实际 %d", n)
	}

	// 版本过低：能力仍落库，但不参与渲染选机，广播文案注明。
	sch.ApplyNodeHello(NodeCaps{ServerID: "srv1", AgentVersion: "0.1.0", MaxConcurrent: 3}, time.Now())
	if sch.nodeVersionOK("srv1") {
		t.Fatalf("0.1.0 节点不应参与渲染选机")
	}
	picked, err := sch.pickNode(Task{ID: "t_ver", TaskType: TaskTypeRenderEDL}, time.Now())
	if err != nil || picked.ID != "srv2" {
		t.Fatalf("低版本节点应被自动选机排除: %+v err=%v", picked, err)
	}
	if _, ok := s.GetNodeCaps("srv1"); !ok {
		t.Fatalf("低版本能力仍应落库")
	}
}

// ---------- 6) Hello 载荷解析 ----------

func TestB08ParseHelloCaps(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantID  string
		wantVer string
		wantMax int
	}{
		{"camelCase", `{"serverId":"srv9","agentVersion":"1.2.0","maxConcurrent":4,"encoders":["h264_nvenc"]}`, "srv9", "1.2.0", 4},
		{"snakeCase", `{"agent_version":"1.3.0","max_concurrent":2,"encoders":["libx264"],"cpu_cores":8}`, "srv_sock", "1.3.0", 2},
		{"nestedCaps", `{"caps":{"agentVersion":"1.1.0","maxConcurrent":6}}`, "srv_sock", "1.1.0", 6},
		{"nestedSnake", `{"nodeCaps":{"agent_version":"1.0.5","max_concurrent":5}}`, "srv_sock", "1.0.5", 5},
	}
	for _, tc := range cases {
		caps, ok := parseHelloCaps("srv_sock", json.RawMessage(tc.raw))
		if !ok {
			t.Fatalf("%s: 应解析成功 (%s)", tc.name, tc.raw)
		}
		if caps.ServerID != tc.wantID || caps.AgentVersion != tc.wantVer || caps.MaxConcurrent != tc.wantMax {
			t.Fatalf("%s: 解析结果不符: %+v", tc.name, caps)
		}
	}

	// 无能力信息 / 非法 JSON → 忽略（不落库）。
	for _, raw := range []string{`{}`, `{"foo":1}`, `not-json`} {
		if _, ok := parseHelloCaps("srv1", json.RawMessage(raw)); ok {
			t.Fatalf("载荷 %s 不应被识别为有效能力", raw)
		}
	}
}

// ---------- 7) 端到端：自动选机后派发成功 ----------

func TestB08AutoPickThenDispatch(t *testing.T) {
	sch, s, _ := newB08Env(t)
	d := &fakeDispatcher{}
	sch.SetRenderDispatcher(d)
	s.UpsertNodeCaps(b08Caps("srv1", 1))

	// srv1 满槽 → 自动选机落到 srv2，任务进入 RUNNING 并落 server_id。
	s.UpsertTask(Task{ID: "t_other", OrderID: 1, Status: StatusTranscoding, TaskType: TaskTypeRenderEDL, ServerID: "srv1"})
	s.UpsertTask(Task{ID: "t_auto", OrderID: 2, Status: StatusQueue, TaskType: TaskTypeRenderEDL,
		PayloadJSON: `{"sourceRoot":"a","destRoot":"b","profile":{"video":{"codec":"libx264"}}}`})
	sch.handleRenderEDL(mustTask(t, s, "t_auto"))

	got := mustTask(t, s, "t_auto")
	if got.Status != StatusTranscoding || got.ServerID != "srv2" || got.RemoteTaskID != "remote_edl_1" {
		t.Fatalf("自动选机应落到 srv2 并进入 RUNNING: %+v", got)
	}
	if d.edlCalls != 1 || d.lastServer.ID != "srv2" {
		t.Fatalf("下发通道应收到 srv2 任务: calls=%d server=%s", d.edlCalls, d.lastServer.ID)
	}
}
