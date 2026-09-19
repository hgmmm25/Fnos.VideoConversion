package api

// B-04 验收测试（03 §4.4 / §5、06 §4.4 / §5.3、07 §3.5）：
//  1) 提交契约：200 + {taskId,status,serverId,output,totalMs,fastCopyAllowed}，任务入队 QUEUE 并冻结 rev；
//  2) totalMs = Σ(outMs-inMs) 与 fastCopyAllowed（同源首尾相接 + copy_same_source 才为 true）；
//  3) 幂等：相同内容重复提交复用同一 taskId；改名/改 rev 不改变 checksum；force:true 跳过幂等；
//  4) 输出名规范化与重名追加 _1/_2（物理文件与已占用任务输出名都算占用）；
//  5) 契约化错误响应：E_PROJECT_NOT_FOUND / E_PROFILE_INVALID / E_EDL_INVALID / E_ASSET_MISSING / E_NODE_OFFLINE。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// ===== 测试辅助 =====

// writeRenderTestFile 写入占位文件（内容不参与校验，仅需存在且非目录）。
func writeRenderTestFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}
}

// newRenderTestEnv 构建 B-04 测试环境：素材根/成品根均为本地可见目录，
// 以便覆盖 precheckAssets（E_ASSET_MISSING）与重名检测（os.Stat）两条分支。
func newRenderTestEnv(t *testing.T) (r *gin.Engine, s *Store, srcDir, destDir string) {
	t.Helper()
	root := t.TempDir()
	srcDir = filepath.Join(root, "videos")
	destDir = filepath.Join(root, "exports")
	writeRenderTestFile(t, filepath.Join(srcDir, "demo", "a_01.mp4"), 64)
	writeRenderTestFile(t, filepath.Join(srcDir, "demo", "a_02.mp4"), 64)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatalf("创建成品根失败: %v", err)
	}
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("创建状态目录失败: %v", err)
	}

	r, s = newEDLTestRouter(t, stateDir)
	cfg := s.GetSettings()
	cfg.VideoRoot = srcDir
	cfg.ExportRoot = destDir
	s.SaveSettings(cfg)
	return r, s, srcDir, destDir
}

// renderTestTimeline 合法时间线（03 §2.5）。
func renderTestTimeline() Timeline {
	return Timeline{Width: 1920, Height: 1080, FPS: 30, SampleRate: 48000, Audio: true}
}

// renderTestClips 两段同源首尾相接片段（in=0→5000→12000），totalMs=12000。
func renderTestClips() []EDLClip {
	return []EDLClip{
		{ClipID: "c_00000001", AssetID: "a_0000000a", File: "demo/a_01.mp4",
			InMs: 0, OutMs: 5000, Speed: 1.0, SourceDurationMs: 600000},
		{ClipID: "c_00000002", AssetID: "a_0000000a", File: "demo/a_01.mp4",
			InMs: 5000, OutMs: 12000, Speed: 1.0, SourceDurationMs: 600000},
	}
}

// mustCreateRenderProject 直接经 store 建项目（绕过 B-03 闸门，便于单测构造边界）。
func mustCreateRenderProject(t *testing.T, s *Store, name string, tl Timeline, clips []EDLClip) Project {
	t.Helper()
	p, err := s.CreateProject(Project{Name: name, Timeline: tl, Clips: clips})
	if err != nil {
		t.Fatalf("CreateProject(%s) 失败: %v", name, err)
	}
	return p
}

// renderSubmitResp POST /render 响应体（03 §4.4）。
type renderSubmitResp struct {
	TaskID          string `json:"taskId"`
	Status          string `json:"status"`
	ServerID        string `json:"serverId"`
	Output          string `json:"output"`
	TotalMs         int64  `json:"totalMs"`
	FastCopyAllowed bool   `json:"fastCopyAllowed"`
	Reused          bool   `json:"reused"`
}

func mustSubmitRender(t *testing.T, r *gin.Engine, projectID, body string) renderSubmitResp {
	t.Helper()
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+projectID+"/render", body)
	if w.Code != http.StatusOK {
		t.Fatalf("渲染提交应成功，实际 %d: %s", w.Code, w.Body.String())
	}
	var resp renderSubmitResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不可解析: %v (%s)", err, w.Body.String())
	}
	return resp
}

// ===== 1. 提交契约与计算字段 =====

func TestB04RenderSubmitContract(t *testing.T) {
	r, s, _, destDir := newRenderTestEnv(t)
	p := mustCreateRenderProject(t, s, "demo_粗剪", renderTestTimeline(), renderTestClips())

	resp := mustSubmitRender(t, r, p.ID,
		`{"presetKey":"copy_same_source","outputName":"demo_cujian_20260910"}`)

	if !strings.HasPrefix(resp.TaskID, "t_") {
		t.Errorf("taskId 形态应为 t_<unix>_<hex>: %q", resp.TaskID)
	}
	if resp.Status != "QUEUE" {
		t.Errorf("初始状态应为 QUEUE: %q", resp.Status)
	}
	if resp.ServerID != "" {
		t.Errorf("未指定 serverId 时应留空由 B-05 选机: %q", resp.ServerID)
	}
	if resp.Output != "demo_cujian_20260910.mp4" {
		t.Errorf("output 应补 .mp4: %q", resp.Output)
	}
	if resp.TotalMs != 12000 {
		t.Errorf("totalMs 应为 Σ(outMs-inMs)=12000: %d", resp.TotalMs)
	}
	if !resp.FastCopyAllowed {
		t.Errorf("同源首尾相接 + copy_same_source 应允许直通: %v", resp.FastCopyAllowed)
	}
	if resp.Reused {
		t.Errorf("首次提交不应标记 reused")
	}

	// 任务已入队且冻结 rev / 载荷可解析
	tasks := s.GetTasks()
	if len(tasks) != 1 {
		t.Fatalf("应入队 1 个任务，实际 %d", len(tasks))
	}
	task := tasks[0]
	if task.ID != resp.TaskID || task.Status != StatusQueue {
		t.Errorf("任务落库与响应不一致: %+v", task)
	}
	if task.TaskType != TaskTypeRenderEDL {
		t.Errorf("taskType 应为 RENDER_EDL: %q", task.TaskType)
	}
	if task.ProjectID != p.ID || task.ProjectRev != p.Rev {
		t.Errorf("应冻结提交时刻 rev: projectID=%s projectRev=%d (期望 %s/%d)",
			task.ProjectID, task.ProjectRev, p.ID, p.Rev)
	}
	if task.SegTotal != len(p.Clips) {
		t.Errorf("segTotal 应为片段数 %d: %d", len(p.Clips), task.SegTotal)
	}
	// OutputFile 指向成品根下规范化后的文件名
	if task.OutputFile != filepath.Join(destDir, resp.Output) {
		t.Errorf("outputFile 应为成品根下绝对路径: %q", task.OutputFile)
	}

	var payload RenderTaskPayload
	if err := json.Unmarshal([]byte(task.PayloadJSON), &payload); err != nil {
		t.Fatalf("payloadJSON 不可解析: %v", err)
	}
	if payload.Type != renderPayloadType || payload.Output != resp.Output {
		t.Errorf("载荷 type/output 不符: %+v", payload)
	}
	if payload.Profile.PresetKey != "copy_same_source" || payload.Profile.Container != renderContainerMP4 {
		t.Errorf("载荷 profile 不符: %+v", payload.Profile)
	}
	if payload.Clips[0].In != "00:00:00.000" || payload.Clips[1].Out != "00:00:12.000" {
		t.Errorf("片段应转为时间码: %+v", payload.Clips)
	}
	if payload.TotalMs != resp.TotalMs || !payload.Profile.FastCopyAllowed {
		t.Errorf("载荷 totalMs/fastCopyAllowed 应与响应一致: %+v", payload)
	}
	// 项目登记最近渲染任务（不推进 rev）
	if got, _ := s.GetProject(p.ID); got.LastRenderTaskID != task.ID || got.Rev != p.Rev {
		t.Errorf("应登记 lastRenderTaskId 且不改 rev: rev=%d last=%q", got.Rev, got.LastRenderTaskID)
	}
	// 审计埋点
	audits := s.ListAudit(10, "render.submit")
	if len(audits) != 1 || audits[0].Target != task.ID {
		t.Errorf("应写入 render.submit 审计: %+v", audits)
	}
}

func TestB04RenderTotalMsAndFastCopyVariants(t *testing.T) {
	r, s, _, _ := newRenderTestEnv(t)

	// 非同源（跨文件）→ fastCopyAllowed=false，两段各自计时
	clips := []EDLClip{
		{ClipID: "c_00000001", AssetID: "a_0000000a", File: "demo/a_01.mp4",
			InMs: 1000, OutMs: 4000, Speed: 1.0, SourceDurationMs: 600000},
		{ClipID: "c_00000002", AssetID: "a_0000000b", File: "demo/a_02.mp4",
			InMs: 0, OutMs: 2000, Speed: 1.0, SourceDurationMs: 600000},
	}
	p := mustCreateRenderProject(t, s, "cross_source", renderTestTimeline(), clips)
	resp := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"cross"}`)
	if resp.TotalMs != 5000 {
		t.Errorf("totalMs 应为 (4000-1000)+(2000-0)=5000: %d", resp.TotalMs)
	}
	if resp.FastCopyAllowed {
		t.Errorf("跨素材不应允许直通")
	}

	// 同源但中间断点（非首尾相接）→ fastCopyAllowed=false
	broken := []EDLClip{
		{ClipID: "c_00000001", AssetID: "a_0000000a", File: "demo/a_01.mp4",
			InMs: 0, OutMs: 5000, Speed: 1.0, SourceDurationMs: 600000},
		{ClipID: "c_00000002", AssetID: "a_0000000a", File: "demo/a_01.mp4",
			InMs: 6000, OutMs: 9000, Speed: 1.0, SourceDurationMs: 600000},
	}
	p2 := mustCreateRenderProject(t, s, "broken_source", renderTestTimeline(), broken)
	resp2 := mustSubmitRender(t, r, p2.ID, `{"presetKey":"copy_same_source","outputName":"broken"}`)
	if resp2.FastCopyAllowed {
		t.Errorf("同源但非首尾相接不应允许直通")
	}

	// 非 copy 方案（需转码）即使同源 → fastCopyAllowed=false
	p3 := mustCreateRenderProject(t, s, "transcode_case", renderTestTimeline(), renderTestClips())
	resp3 := mustSubmitRender(t, r, p3.ID, `{"presetKey":"libx264_medium","outputName":"transcode_out"}`)
	if resp3.FastCopyAllowed {
		t.Errorf("libx264_medium 不应允许直通")
	}
	if resp3.TotalMs != 12000 {
		t.Errorf("转码路径 totalMs 仍为剪辑总长: %d", resp3.TotalMs)
	}
}

// ===== 2. 幂等键复用 =====

func TestB04RenderIdempotentReuse(t *testing.T) {
	r, s, _, destDir := newRenderTestEnv(t)
	p := mustCreateRenderProject(t, s, "idem_project", renderTestTimeline(), renderTestClips())

	first := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"idem"}`)
	if first.Output != "idem.mp4" {
		t.Fatalf("首次输出名错误: %q", first.Output)
	}

	// 1) 完全相同提交 → 复用同一 taskId
	second := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"idem"}`)
	if second.TaskID != first.TaskID || !second.Reused {
		t.Errorf("相同内容应复用 taskId: first=%s second=%s reused=%v",
			first.TaskID, second.TaskID, second.Reused)
	}
	if second.Output != first.Output {
		t.Errorf("复用应返回既有任务输出名: %q", second.Output)
	}
	if len(s.GetTasks()) != 1 {
		t.Errorf("幂等复用不应新增任务，实际 %d", len(s.GetTasks()))
	}

	// 2) 仅改输出名 / 项目 rev → checksum 不含 output 与 projectRev，仍复用
	renamed := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"idem_renamed"}`)
	if renamed.TaskID != first.TaskID || !renamed.Reused {
		t.Errorf("改名提交应命中幂等（checksum 不含 output）: %s reused=%v", renamed.TaskID, renamed.Reused)
	}
	if renamed.Output != first.Output {
		t.Errorf("复用不得改写既有任务输出名: %q", renamed.Output)
	}

	// 3) force:true 跳过幂等 → 新任务，且输出名追加 _1（原 output 已被占用）
	forced := mustSubmitRender(t, r, p.ID,
		`{"presetKey":"copy_same_source","outputName":"idem","force":true}`)
	if forced.TaskID == first.TaskID {
		t.Errorf("force 应生成新任务: %s", forced.TaskID)
	}
	if forced.Output != "idem_1.mp4" {
		t.Errorf("重名应追加 _1: %q", forced.Output)
	}
	if len(s.GetTasks()) != 2 {
		t.Errorf("force 后应有 2 个任务，实际 %d", len(s.GetTasks()))
	}
	if fileExists(filepath.Join(destDir, "idem_1.mp4")) {
		t.Errorf("提交阶段不应产生物理成品文件")
	}
}

func TestB04RenderReuseSuccessTaskWithArtifact(t *testing.T) {
	r, s, _, destDir := newRenderTestEnv(t)
	p := mustCreateRenderProject(t, s, "success_reuse", renderTestTimeline(), renderTestClips())

	first := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"done"}`)

	// 模拟任务完成：状态置 COMPLETED 且成品仍在
	task, ok := s.GetTask(first.TaskID)
	if !ok {
		t.Fatalf("任务未找到: %s", first.TaskID)
	}
	task.Status = StatusCompleted
	task.Progress = 100
	s.UpsertTask(task)
	writeRenderTestFile(t, task.OutputFile, 32)

	again := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"done"}`)
	if again.TaskID != first.TaskID || !again.Reused {
		t.Errorf("成品仍在的成功任务应被复用: %s reused=%v", again.TaskID, again.Reused)
	}
	if len(s.GetTasks()) != 1 {
		t.Errorf("复用成功任务不应新增任务，实际 %d", len(s.GetTasks()))
	}
	if !fileExists(filepath.Join(destDir, "done.mp4")) {
		t.Errorf("测试布置的成品文件应存在")
	}
}

func TestB04ChecksumStableAndScope(t *testing.T) {
	env := func(projRev int, output, preset string, clips []EDLClip) *RenderTaskPayload {
		spec := edlRenderPresetTable[preset]
		p := buildRenderPayload(Project{
			ID: "p_test", Rev: projRev, Timeline: renderTestTimeline(), Clips: clips,
		}, "/media/videos", "/media/exports", output, preset, spec)
		p.Profile.FastCopyAllowed = computeFastCopyAllowed(&p)
		p.Checksum = computeRenderChecksum(&p)
		return &p
	}

	base := env(1, "out.mp4", "copy_same_source", renderTestClips())
	if len(base.Checksum) != 16 {
		t.Fatalf("checksum 应为 sha256 前 16 位十六进制: %q", base.Checksum)
	}
	if again := env(1, "out.mp4", "copy_same_source", renderTestClips()); again.Checksum != base.Checksum {
		t.Errorf("相同输入应稳定同值: %s vs %s", again.Checksum, base.Checksum)
	}
	// output 不参与
	if renamed := env(1, "out_renamed.mp4", "copy_same_source", renderTestClips()); renamed.Checksum != base.Checksum {
		t.Errorf("output 不应参与幂等键: %s vs %s", renamed.Checksum, base.Checksum)
	}
	// projectRev 不参与
	if bumped := env(9, "out.mp4", "copy_same_source", renderTestClips()); bumped.Checksum != base.Checksum {
		t.Errorf("projectRev 不应参与幂等键: %s vs %s", bumped.Checksum, base.Checksum)
	}
	// profile 变化 → 新键
	if other := env(1, "out.mp4", "libx264_medium", renderTestClips()); other.Checksum == base.Checksum {
		t.Errorf("presetKey 变化应产生新键")
	}
	// clips 变化 → 新键
	changed := renderTestClips()
	changed[1].OutMs = 13000
	if other := env(1, "out.mp4", "copy_same_source", changed); other.Checksum == base.Checksum {
		t.Errorf("clips 变化应产生新键")
	}
	// FastCopyAllowed 为服务端回填字段，置 true 不改变键
	withFast := env(1, "out.mp4", "copy_same_source", renderTestClips())
	if !withFast.Profile.FastCopyAllowed {
		t.Fatalf("前置条件：同源应允许直通")
	}
	if withFast.Checksum != base.Checksum {
		t.Errorf("FastCopyAllowed 不应参与幂等键")
	}
}

// ===== 3. 输出名规范化与重名追加 =====

func TestB04NormalizeOutputBase(t *testing.T) {
	long := strings.Repeat("a", renderOutputBaseMax+1)
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "demo_cujian_20260910", "demo_cujian_20260910"},
		{"with_ext", "demo_cujian.mp4", "demo_cujian"},
		{"chinese", "成片_第一版", "成片_第一版"},
		{"dot_inside", "v1.2.final", "v1.2.final"},
		{"trim_space", "  demo  ", "demo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeOutputBase(tc.in)
			if err != nil {
				t.Fatalf("应通过: %v", err)
			}
			if got != tc.want {
				t.Errorf("want %q got %q", tc.want, got)
			}
		})
	}

	bad := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"blank", "   "},
		{"other_ext", "demo_cujian.mov"},
		{"path_sep", "demo/cujian"},
		{"backslash", `demo\cujian`},
		{"traversal", "../secret"},
		{"illegal_char", `demo:cujian`},
		{"reserved", "con"},
		{"reserved_with_ext", "NUL.mp4"},
		{"space_inside", "demo cujian"},
		{"too_long", long},
		{"leading_dash", "-demo"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := normalizeOutputBase(tc.in); err == nil {
				t.Errorf("应拒绝 %q，实际返回 %q", tc.in, got)
			}
		})
	}
}

func TestB04UniqueOutputNameAppends(t *testing.T) {
	r, s, _, destDir := newRenderTestEnv(t)

	// 物理文件已存在 → 追加 _1
	writeRenderTestFile(t, filepath.Join(destDir, "taken.mp4"), 16)
	p := mustCreateRenderProject(t, s, "unique_name", renderTestTimeline(), renderTestClips())
	resp := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"taken"}`)
	if resp.Output != "taken_1.mp4" {
		t.Errorf("物理重名应追加 _1: %q", resp.Output)
	}

	// 已占用任务输出名（不同 profile → 不同 checksum，绕过幂等）→ 继续追加
	resp2 := mustSubmitRender(t, r, p.ID, `{"presetKey":"libx264_medium","outputName":"taken"}`)
	if resp2.Output != "taken_2.mp4" {
		t.Errorf("任务输出名占用应继续追加 _2: %q", resp2.Output)
	}
	if resp2.TaskID == resp.TaskID {
		t.Errorf("不同 profile 应生成不同任务")
	}
}

// ===== 4. 契约化错误响应 =====

func TestB04RenderErrorContract(t *testing.T) {
	r, s, _, _ := newRenderTestEnv(t)
	valid := mustCreateRenderProject(t, s, "err_valid", renderTestTimeline(), renderTestClips())
	empty := mustCreateRenderProject(t, s, "err_empty", renderTestTimeline(), nil)

	lowRate := mustCreateRenderProject(t, s, "err_rate",
		Timeline{Width: 1920, Height: 1080, FPS: 30, SampleRate: 22050, Audio: true}, renderTestClips())

	missing := renderTestClips()
	missing[0].File = "demo/missing.mp4"
	missingProject := mustCreateRenderProject(t, s, "err_missing", renderTestTimeline(), missing)

	s.UpsertServer(Server{ID: "s_off", Name: "节点B", Status: "offline"})
	s.UpsertServer(Server{ID: "s_on", Name: "节点A", Status: "online"})

	cases := []struct {
		name     string
		project  string // 覆盖默认 project id
		body     string
		wantCode int
		wantErr  string
	}{
		{"project_not_found", "p_missing", `{"presetKey":"copy_same_source","outputName":"x"}`,
			http.StatusNotFound, "E_PROJECT_NOT_FOUND"},
		{"preset_unknown", "", `{"presetKey":"nope","outputName":"x"}`,
			http.StatusBadRequest, errCodeProfileInvalid},
		{"preset_proxy_only", "", `{"presetKey":"proxy_720p_h264","outputName":"x"}`,
			http.StatusBadRequest, errCodeProfileInvalid},
		{"preset_empty", "", `{"presetKey":"","outputName":"x"}`,
			http.StatusBadRequest, errCodeProfileInvalid},
		{"output_empty", "", `{"presetKey":"copy_same_source","outputName":""}`,
			http.StatusBadRequest, errCodeEDLInvalid},
		{"output_bad_ext", "", `{"presetKey":"copy_same_source","outputName":"a.mov"}`,
			http.StatusBadRequest, errCodeEDLInvalid},
		{"empty_timeline", "", `{"presetKey":"copy_same_source","outputName":"x"}`,
			http.StatusBadRequest, errCodeEDLInvalid},
		{"asset_missing", "", `{"presetKey":"copy_same_source","outputName":"x"}`,
			http.StatusNotFound, errCodeAssetMissing},
		{"node_unknown", "", `{"presetKey":"copy_same_source","outputName":"x","serverId":"s_nope"}`,
			http.StatusConflict, errCodeNodeOffline},
		{"node_offline", "", `{"presetKey":"copy_same_source","outputName":"x","serverId":"s_off"}`,
			http.StatusConflict, errCodeNodeOffline},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pid := tc.project
			if pid == "" {
				switch tc.name {
				case "empty_timeline":
					pid = empty.ID
				case "asset_missing":
					pid = missingProject.ID
				default:
					pid = valid.ID
				}
			}
			w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+pid+"/render", tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("状态码应为 %d，实际 %d: %s", tc.wantCode, w.Code, w.Body.String())
			}
			var resp struct {
				OK     bool   `json:"ok"`
				Code   string `json:"code"`
				Msg    string `json:"msg"`
				Detail gin.H  `json:"detail"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("错误响应不可解析: %v (%s)", err, w.Body.String())
			}
			if resp.Code != tc.wantErr {
				t.Errorf("错误码应为 %s，实际 %s（%s）", tc.wantErr, resp.Code, resp.Msg)
			}
			if resp.Msg == "" {
				t.Errorf("错误响应必须带 msg")
			}
		})
	}

	// 采样率白名单单独覆盖（项目可建但提交应被第三道闸门拒绝）
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+lowRate.ID+"/render",
		`{"presetKey":"copy_same_source","outputName":"rate"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errCodeEDLInvalid) {
		t.Errorf("非白名单采样率应 400 E_EDL_INVALID: %d %s", w.Code, w.Body.String())
	}

	// 校验失败不得留下任务
	if n := len(s.GetTasks()); n != 0 {
		t.Errorf("被拒绝的提交不应产生任务，实际 %d 个", n)
	}
}

func TestB04RenderServerIDPinned(t *testing.T) {
	r, s, _, _ := newRenderTestEnv(t)
	s.UpsertServer(Server{ID: "s_on", Name: "节点A", Status: "online"})
	p := mustCreateRenderProject(t, s, "pin_node", renderTestTimeline(), renderTestClips())

	resp := mustSubmitRender(t, r, p.ID,
		`{"presetKey":"copy_same_source","outputName":"pinned","serverId":"s_on"}`)
	if resp.ServerID != "s_on" {
		t.Errorf("指定节点应回填 serverId: %q", resp.ServerID)
	}
	task, ok := s.GetTask(resp.TaskID)
	if !ok || task.ServerID != "s_on" || task.ServerName != "节点A" {
		t.Errorf("任务应记录指定节点: %+v", task)
	}
}
