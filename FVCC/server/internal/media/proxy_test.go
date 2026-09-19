package media

// P2-1 B轮：M4 代理收尾验收测试（04 §3.4 完成后校验收口 / §4.1 时间轴对齐 P0 硬约束）。
// 原根包 proxy_flow_test.go 迁入 internal/media，Scheduler 依赖替换为
// ProxyWorkflow + Store/Hub/Finalizer 桩：
//  1) VerifyProxyMeta：同帧率、同时长（≤1 帧）、高度 ≤720、音轨存在性一致；探测不可用时放行；
//  2) 校验通过 → asset_proxies 登记 state=ready + 任务 COMPLETED + WS proxy_ready 广播（含 durationMs）；
//  3) 校验失败 → 标记 state=invalid + 删除代理文件 + 任务 ERROR（经 Finalizer），且不广播 proxy_ready；
//  4) 轮询入口在缺少远端 taskId 时不做状态变更。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fvcc/internal/store"
	"fvcc/internal/store/model"
)

// stubProber 桩探测：按本地绝对路径返回元数据，避免 ffprobe 在环。
type stubProber struct {
	byPath   map[string]model.VideoInfo
	fallback *model.VideoInfo
	err      error
}

func (s *stubProber) Probe(path string) (model.VideoInfo, error) {
	if s.err != nil {
		return model.VideoInfo{}, s.err
	}
	if s.byPath != nil {
		if v, ok := s.byPath[filepath.Clean(path)]; ok {
			return v, nil
		}
	}
	if s.fallback != nil {
		return *s.fallback, nil
	}
	return model.VideoInfo{}, os.ErrNotExist
}

// stubHub 捕获 WS 广播帧与 proxy_ready 负载。
type stubHub struct {
	mu           sync.Mutex
	frames       []string
	taskUpdates  []string
	proxyReadies []map[string]interface{}
}

func (h *stubHub) BroadcastTaskUpdate(taskID, status string, progress float64, message string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.taskUpdates = append(h.taskUpdates, taskID+":"+status+":"+message)
	h.frames = append(h.frames, `{"type":"task_update","taskId":"`+taskID+`","status":"`+status+`"}`)
}

func (h *stubHub) BroadcastProxyReady(assetID string, data interface{}) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	m, _ := data.(map[string]interface{})
	h.proxyReadies = append(h.proxyReadies, m)
	// 与原 Hub emitHook 同构：整帧 JSON 含 assetId/proxyFile/durationMs。
	h.frames = append(h.frames, fmt.Sprintf(`{"type":"proxy_ready","assetId":%q,"proxyFile":%q,"durationMs":%v}`,
		m["assetId"], m["proxyFile"], m["durationMs"]))
	return true
}

// stubFinalizer 记录不可重试失败收口调用（原 failRenderTaskPermanent 的行为替身）。
type stubFinalizer struct {
	mu    sync.Mutex
	calls []string // "taskID|code|msg"
}

func (f *stubFinalizer) FailPermanent(taskID string, code, msg string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, taskID+"|"+code+"|"+msg)
}

func (f *stubFinalizer) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		return ""
	}
	return f.calls[len(f.calls)-1]
}

// proxyFlowEnv 代理收尾测试环境。
type proxyFlowEnv struct {
	wf       *ProxyWorkflow
	store    *store.Store
	hub      *stubHub
	final    *stubFinalizer
	root     string
	frames   *[]string
	mu       *sync.Mutex
}

func newProxyFlowEnv(t *testing.T) *proxyFlowEnv {
	t.Helper()
	root := t.TempDir()
	st := store.NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	hub := &stubHub{}
	final := &stubFinalizer{}
	resolver := ProxyPathResolverFunc(func(p model.GenProxyPayload) (string, string, string, string) {
		return filepath.Join(root, filepath.FromSlash(p.SrcFile)),
			filepath.Join(root, "_proxy", filepath.FromSlash(p.ProxyFile)), "", ""
	})
	wf := NewProxyWorkflow(st, hub, nil, final, resolver)
	return &proxyFlowEnv{wf: wf, store: st, hub: hub, final: final, root: root,
		frames: &hub.frames, mu: &hub.mu}
}

// submitted 写入一条 RUNNING 态 GEN_PROXY 任务，并返回其载荷。
func (e *proxyFlowEnv) submitted(t *testing.T) (model.Task, model.GenProxyPayload) {
	t.Helper()
	payload := model.GenProxyPayload{
		Type:      "GenProxy",
		AssetID:   "a_0000000a",
		SrcFile:   "demo/a_01.mp4",
		ProxyFile: "demo/a_01.proxy.mp4",
		Template:  model.DefaultProxyTemplate(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("载荷序列化失败: %v", err)
	}
	task := model.Task{
		ID:           "t_1757980100_cd34ef",
		TaskType:     model.TaskTypeGenProxy,
		Status:       model.StatusTranscoding,
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

// m4WriteFile 落盘指定大小文件。
func m4WriteFile(t *testing.T, abs string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	f, err := os.Create(abs)
	if err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	defer f.Close()
	if _, err := f.Write(make([]byte, size)); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

func TestVerifyProxyMeta(t *testing.T) {
	src := model.VideoInfo{Duration: 10.0, Fps: "30000/1001", Height: 1080, AudioCodec: "aac"}
	cases := []struct {
		name    string
		mutate  func(*model.VideoInfo)
		wantErr bool
	}{
		{"合规代理", func(p *model.VideoInfo) {}, false},
		{"时长差 1 帧内", func(p *model.VideoInfo) { p.Duration = 10.02 }, false},
		{"时长偏差超 1 帧", func(p *model.VideoInfo) { p.Duration = 10.5 }, true},
		{"帧率不等", func(p *model.VideoInfo) { p.Fps = "25/1" }, true},
		{"高度超 720", func(p *model.VideoInfo) { p.Height = 1080 }, true},
		{"音轨缺失", func(p *model.VideoInfo) { p.AudioCodec = "" }, true},
		{"探测不可用放行", func(p *model.VideoInfo) { p.Fps = "unknown"; p.Duration = 0; p.Height = 0 }, false},
	}
	for _, tc := range cases {
		px := model.VideoInfo{Duration: 10.0, Fps: "30000/1001", Height: 720, AudioCodec: "aac"}
		tc.mutate(&px)
		err := VerifyProxyMeta(src, px)
		if tc.wantErr && err == nil {
			t.Fatalf("%s：应校验失败，实际通过", tc.name)
		}
		if !tc.wantErr && err != nil {
			t.Fatalf("%s：应校验通过，实际 %v", tc.name, err)
		}
	}
}

func TestFinishGenProxyRegistersReady(t *testing.T) {
	e := newProxyFlowEnv(t)
	m4WriteFile(t, filepath.Join(e.root, "demo", "a_01.mp4"), 4096)
	proxyLocal := filepath.Join(e.root, "_proxy", "demo", "a_01.proxy.mp4")
	m4WriteFile(t, proxyLocal, 500)

	task, payload := e.submitted(t)
	e.wf.SetProbe(&stubProber{byPath: map[string]model.VideoInfo{
		filepath.Clean(filepath.Join(e.root, "demo", "a_01.mp4")): {Duration: 10.0, Fps: "30000/1001", Height: 1080, AudioCodec: "aac"},
		filepath.Clean(proxyLocal):                                {Duration: 10.0, Fps: "30000/1001", Height: 720, AudioCodec: "aac"},
	}})

	e.wf.Finish(task)

	// 1) asset_proxies 登记为 ready（04 §4.2 字段）
	p, ok := e.store.GetAssetProxy(payload.SrcFile)
	if !ok {
		t.Fatalf("代理映射未登记: %s", payload.SrcFile)
	}
	if p.State != model.ProxyStateReady || p.Mode != model.ProxyModeFull {
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
	if !ok || got.Status != model.StatusCompleted {
		t.Fatalf("任务应 COMPLETED，实际 %v/%v", got.Status, ok)
	}
	if _, err := os.Stat(proxyLocal); err != nil {
		t.Fatalf("合规代理文件不应被删除: %v", err)
	}
	if e.final.last() != "" {
		t.Fatalf("成功路径不应触发失败收口，实际 %q", e.final.last())
	}

	// 3) WS proxy_ready 广播（03 §6 负载：assetId/proxyFile/durationMs）
	if !e.framesContain("proxy_ready", `"assetId":"a_0000000a"`, `"durationMs":10000`) {
		t.Fatalf("未捕获合规 proxy_ready 广播，帧: %v", *e.frames)
	}
}

func TestFinishGenProxyInvalidMarksState(t *testing.T) {
	e := newProxyFlowEnv(t)
	m4WriteFile(t, filepath.Join(e.root, "demo", "a_01.mp4"), 4096)
	proxyLocal := filepath.Join(e.root, "_proxy", "demo", "a_01.proxy.mp4")
	m4WriteFile(t, proxyLocal, 500)

	task, payload := e.submitted(t)
	e.wf.SetProbe(&stubProber{byPath: map[string]model.VideoInfo{
		filepath.Clean(filepath.Join(e.root, "demo", "a_01.mp4")): {Duration: 10.0, Fps: "30000/1001", Height: 1080, AudioCodec: "aac"},
		filepath.Clean(proxyLocal):                                {Duration: 12.5, Fps: "30000/1001", Height: 720, AudioCodec: "aac"},
	}})

	e.wf.Finish(task)

	// 1) 标记 invalid + 删除代理文件（04 §3.4 校验失败处理）
	p, ok := e.store.GetAssetProxy(payload.SrcFile)
	if !ok || p.State != model.ProxyStateInvalid {
		t.Fatalf("代理应标记 invalid，实际 %+v/%v", p, ok)
	}
	if _, err := os.Stat(proxyLocal); !os.IsNotExist(err) {
		t.Fatalf("时长不一致的代理文件应被删除，stat err=%v", err)
	}

	// 2) 任务经 Finalizer 收口为 ERROR，错误信息可定位（含时长）
	if last := e.final.last(); last == "" || !strings.Contains(last, "时长") {
		t.Fatalf("失败收口应指明时长不一致，实际 %q", last)
	}

	// 3) 不应广播 proxy_ready
	if e.framesContain("proxy_ready") {
		t.Fatalf("校验失败不得广播 proxy_ready，帧: %v", *e.frames)
	}
}

func TestPollGenProxyWithoutRemoteKeepsState(t *testing.T) {
	e := newProxyFlowEnv(t)
	task := model.Task{
		ID:          "t_poll_keep",
		TaskType:    model.TaskTypeGenProxy,
		Status:      model.StatusTranscoding,
		PayloadJSON: `{"type":"GenProxy","assetId":"a_0000000a","srcFile":"demo/a_01.mp4","proxyFile":"demo/a_01.proxy.mp4"}`,
	}
	e.store.UpsertTask(task)

	// 无 remoteTaskId → 轮询直接返回，不得改动任务状态
	e.wf.Poll(task)
	got, ok := e.store.GetTask(task.ID)
	if !ok || got.Status != model.StatusTranscoding {
		t.Fatalf("缺远端 taskId 时状态应保持不变，实际 %v/%v", got.Status, ok)
	}
}
