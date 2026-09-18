package main

// M4 代理收尾验收测试（04 §3.4 完成后校验收口 / §4.1 时间轴对齐 P0 硬约束）：
//  1) verifyProxyMeta：同帧率、同时长（≤1 帧）、高度 ≤720、音轨存在性一致；探测不可用时放行；
//  2) 校验通过 → asset_proxies 登记 state=ready + 任务 COMPLETED + WS proxy_ready 广播（含 durationMs）；
//  3) 校验失败 → 标记 state=invalid + 删除代理文件 + 任务 ERROR，且不广播 proxy_ready；
//  4) 轮询入口在缺少远端 taskId 时不做状态变更。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// stubProber 桩探测：按本地绝对路径返回元数据，避免 ffprobe 在环。
type stubProber struct {
	byPath   map[string]VideoInfo
	fallback *VideoInfo
	err      error
}

// Available 实现 MediaProber：桩始终视为可用。
func (s *stubProber) Available() bool { return true }

func (s *stubProber) Probe(path string) (VideoInfo, error) {
	if s.err != nil {
		return VideoInfo{}, s.err
	}
	if s.byPath != nil {
		if v, ok := s.byPath[filepath.Clean(path)]; ok {
			return enrichStubInfo(path, v), nil
		}
	}
	if s.fallback != nil {
		return enrichStubInfo(path, *s.fallback), nil
	}
	return VideoInfo{}, os.ErrNotExist
}

// enrichStubInfo 补齐路径系字段（真实 ffprobe 会回填 Path/FileName，桩需同构），
// 否则依赖扫描结果文件名的用例（如保留目录过滤）会因空名而失配。
func enrichStubInfo(path string, v VideoInfo) VideoInfo {
	if v.Path == "" {
		v.Path = path
	}
	if v.FileName == "" {
		v.FileName = filepath.Base(path)
	}
	if v.Format == "" {
		v.Format = strings.TrimPrefix(filepath.Ext(path), ".")
	}
	if v.Size == 0 {
		if fi, err := os.Stat(path); err == nil {
			v.Size = fi.Size()
		}
	}
	return v
}

type proxyFlowEnv struct {
	sch    *Scheduler
	store  *Store
	hub    *Hub
	root   string
	frames *[]string
	mu     *sync.Mutex
}

// newProxyFlowEnv 构造代理收尾测试环境：Hub 通过 emitHook 捕获全部 WS 帧，免真实连接。
func newProxyFlowEnv(t *testing.T) *proxyFlowEnv {
	t.Helper()
	root := t.TempDir()
	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	st.SaveSettings(Settings{VideoRoot: toPOSIXRoot(root)})

	hub := NewHub()
	var mu sync.Mutex
	var frames []string
	hub.emitHook = func(b []byte) {
		mu.Lock()
		frames = append(frames, string(b))
		mu.Unlock()
	}
	sch := NewScheduler(st, NewRemoteClient(), hub, nil)
	return &proxyFlowEnv{sch: sch, store: st, hub: hub, root: root, frames: &frames, mu: &mu}
}

// submitted 写入一条 RUNNING 态 GEN_PROXY 任务，并返回其载荷。
func (e *proxyFlowEnv) submitted(t *testing.T) (Task, GenProxyPayload) {
	t.Helper()
	payload := GenProxyPayload{
		Type:      "GenProxy",
		AssetID:   "a_0000000a",
		SrcFile:   "demo/a_01.mp4",
		ProxyFile: "demo/a_01.proxy.mp4",
		Template:  DefaultProxyTemplate(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("载荷序列化失败: %v", err)
	}
	task := Task{
		ID:           "t_1757980100_cd34ef",
		TaskType:     TaskTypeGenProxy,
		Status:       StatusTranscoding,
		SourceFile:   payload.SrcFile,
		PayloadJSON:  string(raw),
		ServerID:     "srv-1",
		RemoteTaskID: "r_1",
	}
	e.store.UpsertTask(task)
	return task, payload
}

// framesContain 判断捕获帧中是否存在同时包含全部子串的一帧。
func (e *proxyFlowEnv) framesContain(subs ...string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, f := range *e.frames {
		hit := true
		for _, s := range subs {
			if !strings.Contains(f, s) {
				hit = false
				break
			}
		}
		if hit {
			return true
		}
	}
	return false
}

func TestM4VerifyProxyMeta(t *testing.T) {
	src := VideoInfo{Duration: 10.0, Fps: "30000/1001", Height: 1080, AudioCodec: "aac"}
	cases := []struct {
		name    string
		mutate  func(*VideoInfo)
		wantErr bool
	}{
		{"合规代理", func(p *VideoInfo) {}, false},
		{"时长差 1 帧内", func(p *VideoInfo) { p.Duration = 10.02 }, false},
		{"时长偏差超 1 帧", func(p *VideoInfo) { p.Duration = 10.5 }, true},
		{"帧率不等", func(p *VideoInfo) { p.Fps = "25/1" }, true},
		{"高度超 720", func(p *VideoInfo) { p.Height = 1080 }, true},
		{"音轨缺失", func(p *VideoInfo) { p.AudioCodec = "" }, true},
		{"探测不可用放行", func(p *VideoInfo) { p.Fps = "unknown"; p.Duration = 0; p.Height = 0 }, false},
	}
	for _, tc := range cases {
		px := VideoInfo{Duration: 10.0, Fps: "30000/1001", Height: 720, AudioCodec: "aac"}
		tc.mutate(&px)
		err := verifyProxyMeta(src, px)
		if tc.wantErr && err == nil {
			t.Fatalf("%s：应校验失败，实际通过", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s：应校验通过，实际 %v", tc.name, err)
		}
	}
}

func TestM4FinishGenProxyRegistersReady(t *testing.T) {
	e := newProxyFlowEnv(t)
	m4WriteFile(t, filepath.Join(e.root, "demo", "a_01.mp4"), 4096)
	proxyLocal := filepath.Join(e.root, "_proxy", "demo", "a_01.proxy.mp4")
	m4WriteFile(t, proxyLocal, 500)

	task, payload := e.submitted(t)
	e.sch.SetProxyProbe(&stubProber{byPath: map[string]VideoInfo{
		filepath.Clean(filepath.Join(e.root, "demo", "a_01.mp4")): {Duration: 10.0, Fps: "30000/1001", Height: 1080, AudioCodec: "aac"},
		filepath.Clean(proxyLocal):                                {Duration: 10.0, Fps: "30000/1001", Height: 720, AudioCodec: "aac"},
	}})

	e.sch.finishGenProxy(task)

	// 1) asset_proxies 登记为 ready（04 §4.2 字段）
	p, ok := e.store.GetAssetProxy(payload.SrcFile)
	if !ok {
		t.Fatalf("代理映射未登记: %s", payload.SrcFile)
	}
	if p.State != ProxyStateReady || p.Mode != ProxyModeFull {
		t.Fatalf("代理状态应为 ready/full，实际 %s/%s", p.State, p.Mode)
	}
	if p.ProxyRel != payload.ProxyFile {
		t.Fatalf("proxyRel 应为 %q，实际 %q", payload.ProxyFile, p.ProxyRel)
	}
	if p.SrcFPS != "30000/1001" || p.ProxyFPS != "30000/1001" {
		t.Fatalf("帧率字段不符: src=%q proxy=%q", p.SrcFPS, p.ProxyFPS)
	}
	if p.SrcDurationMs != 10000 || p.ProxyDurationMs != 10000 {
		t.Fatalf("时长字段不符: src=%d proxy=%d", p.SrcDurationMs, p.ProxyDurationMs)
	}
	if p.SizeBytes != 500 {
		t.Fatalf("sizeBytes 应为 500，实际 %d", p.SizeBytes)
	}
	if p.TaskID != task.ID || p.GeneratedAt == "" {
		t.Fatalf("taskId/generatedAt 未回填: %+v", p)
	}

	// 2) 任务收口为 COMPLETED，代理文件保留
	got, ok := e.store.GetTask(task.ID)
	if !ok || got.Status != StatusCompleted {
		t.Fatalf("任务应 COMPLETED，实际 %v/%v", got.Status, ok)
	}
	if _, err := os.Stat(proxyLocal); err != nil {
		t.Fatalf("合规代理文件不应被删除: %v", err)
	}

	// 3) WS proxy_ready 广播（03 §6 负载：assetId/proxyFile/durationMs）
	if !e.framesContain("proxy_ready", `"assetId":"a_0000000a"`, `"durationMs":10000`) {
		t.Fatalf("未捕获合规 proxy_ready 广播，帧: %v", *e.frames)
	}
}

func TestM4FinishGenProxyInvalidMarksState(t *testing.T) {
	e := newProxyFlowEnv(t)
	m4WriteFile(t, filepath.Join(e.root, "demo", "a_01.mp4"), 4096)
	proxyLocal := filepath.Join(e.root, "_proxy", "demo", "a_01.proxy.mp4")
	m4WriteFile(t, proxyLocal, 500)

	task, payload := e.submitted(t)
	e.sch.SetProxyProbe(&stubProber{byPath: map[string]VideoInfo{
		filepath.Clean(filepath.Join(e.root, "demo", "a_01.mp4")): {Duration: 10.0, Fps: "30000/1001", Height: 1080, AudioCodec: "aac"},
		filepath.Clean(proxyLocal):                                {Duration: 12.5, Fps: "30000/1001", Height: 720, AudioCodec: "aac"},
	}})

	e.sch.finishGenProxy(task)

	// 1) 标记 invalid + 删除代理文件（04 §3.4 校验失败处理）
	p, ok := e.store.GetAssetProxy(payload.SrcFile)
	if !ok || p.State != ProxyStateInvalid {
		t.Fatalf("代理应标记 invalid，实际 %+v/%v", p, ok)
	}
	if _, err := os.Stat(proxyLocal); !os.IsNotExist(err) {
		t.Fatalf("时长不一致的代理文件应被删除，stat err=%v", err)
	}

	// 2) 任务收口为 ERROR，错误信息可定位（含时长）
	got, _ := e.store.GetTask(task.ID)
	if got.Status != StatusError {
		t.Fatalf("任务应 ERROR，实际 %v", got.Status)
	}
	if !strings.Contains(got.ErrorMsg, "时长") {
		t.Fatalf("错误信息应指明时长不一致，实际 %q", got.ErrorMsg)
	}

	// 3) 不应广播 proxy_ready
	if e.framesContain("proxy_ready") {
		t.Fatalf("校验失败不得广播 proxy_ready，帧: %v", *e.frames)
	}
}

func TestM4PollGenProxyWithoutRemoteKeepsState(t *testing.T) {
	e := newProxyFlowEnv(t)
	task := Task{
		ID:          "t_poll_keep",
		TaskType:    TaskTypeGenProxy,
		Status:      StatusTranscoding,
		PayloadJSON: `{"type":"GenProxy","assetId":"a_0000000a","srcFile":"demo/a_01.mp4","proxyFile":"demo/a_01.proxy.mp4"}`,
	}
	e.store.UpsertTask(task)

	// 无 remoteTaskId → 轮询直接返回，不得改动任务状态
	e.sch.pollGenProxy(task)
	got, ok := e.store.GetTask(task.ID)
	if !ok || got.Status != StatusTranscoding {
		t.Fatalf("缺远端 taskId 时状态应保持不变，实际 %v/%v", got.Status, ok)
	}
}
