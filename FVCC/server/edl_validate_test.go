package main

// D-02 测试：FVCC 侧 EDL 白名单校验。
// 用例数据来自共用向量 ../../FVCS/pkg/protocol/testdata/edl_vectors.json
// （规则来源 07 §3.2 / §3.5 / §3.6 / §8），与 FVCS/pkg/protocol/edl_validate_test.go 共用，
// 保证两侧「同规则双实现」不漂移。向量文件两侧均不得修改。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

const edlVecWantOK = "OK"

type edlVectorRelPath struct {
	ID       string `json:"id"`
	In       string `json:"in"`
	Decoded  bool   `json:"decoded"`
	WantCode string `json:"wantCode"`
	Note     string `json:"note"`
}

type edlVectorOutput struct {
	ID       string `json:"id"`
	In       string `json:"in"`
	WantCode string `json:"wantCode"`
	Note     string `json:"note"`
}

type edlVectorPayload struct {
	ID       string          `json:"id"`
	WantCode string          `json:"wantCode"`
	Note     string          `json:"note"`
	Raw      json.RawMessage `json:"raw"`
}

type edlVectorSymlink struct {
	ID       string `json:"id"`
	Rel      string `json:"rel"`
	WantCode string `json:"wantCode"`
	Note     string `json:"note"`
}

type edlVectorFile struct {
	Version int                `json:"version"`
	RelPath []edlVectorRelPath `json:"relpath"`
	Output  []edlVectorOutput  `json:"output"`
	Payload []edlVectorPayload `json:"payload"`
	Symlink []edlVectorSymlink `json:"symlink"`
}

// loadEDLVectors 读取与 FVCS 共用的校验向量（测试工作目录为 FVCC/server）。
func loadEDLVectors(t *testing.T) *edlVectorFile {
	t.Helper()
	p := filepath.Join("..", "..", "FVCS", "pkg", "protocol", "testdata", "edl_vectors.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读取共用向量失败（%s）：%v", p, err)
	}
	var v edlVectorFile
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("解析共用向量失败：%v", err)
	}
	if v.Version != 1 {
		t.Fatalf("向量版本不支持：%d", v.Version)
	}
	return &v
}

// TestD02EDLVectorsRelPath 相对路径 L1~L3 与向量一致（S1/S2/S3/S4 + P-*）。
func TestD02EDLVectorsRelPath(t *testing.T) {
	v := loadEDLVectors(t)
	if len(v.RelPath) == 0 {
		t.Fatal("向量 relpath 为空")
	}
	for _, tc := range v.RelPath {
		var e *edlValidationError
		if tc.Decoded {
			e = validateRelPathDecoded(tc.In, edlAllowedSourceExt)
		} else {
			e = validateRelPath(tc.In, edlAllowedSourceExt)
		}
		got := edlVecWantOK
		if e != nil {
			got = e.Code
		}
		if got != tc.WantCode {
			t.Errorf("[%s] %q：期望 %s，实际 %s（%s）", tc.ID, tc.In, tc.WantCode, got, tc.Note)
		}
	}
}

// TestD02EDLVectorsOutputName 输出名白名单与向量一致（S8 + O-*）。
func TestD02EDLVectorsOutputName(t *testing.T) {
	v := loadEDLVectors(t)
	if len(v.Output) == 0 {
		t.Fatal("向量 output 为空")
	}
	for _, tc := range v.Output {
		e := validateOutputNameStrict(tc.In)
		got := edlVecWantOK
		if e != nil {
			got = e.Code
		}
		if got != tc.WantCode {
			t.Errorf("[%s] %q：期望 %s，实际 %s（%s）", tc.ID, tc.In, tc.WantCode, got, tc.Note)
		}
	}
}

// TestD02EDLVectorsPayload 载荷严格解码 + 白名单校验与向量一致（PL-* / S4/S5/S6/S7/S8/S12）。
func TestD02EDLVectorsPayload(t *testing.T) {
	v := loadEDLVectors(t)
	if len(v.Payload) == 0 {
		t.Fatal("向量 payload 为空")
	}
	for _, tc := range v.Payload {
		got := edlVecWantOK
		p, e := decodeRenderPayloadStrict(tc.Raw)
		if e != nil {
			got = e.Code
		} else if e2 := validateRenderEDLPayload(p); e2 != nil {
			got = e2.Code
		}
		if got != tc.WantCode {
			t.Errorf("[%s]：期望 %s，实际 %s（%s）", tc.ID, tc.WantCode, got, tc.Note)
		}
	}
}

// TestD02PayloadOKBackfills 合法载荷解析后应完成 totalMs/speed 回填（FVCC 侧闸门副作用可控）。
func TestD02PayloadOKBackfills(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"RenderEDL","projectId":"p-1","projectRev":1,
		"sourceRoot":"videos","destRoot":"exports","output":"demo.mp4",
		"timeline":{"width":1920,"height":1080,"fps":30,"sampleRate":48000,"audio":true},
		"clips":[{"file":"videos/a.mp4","in":"00:00:00.000","out":"00:00:10.000"}],
		"profile":{"presetKey":"libx264_medium","container":"mp4",
			"video":{"codec":"libx264","crf":23,"preset":"medium","pixFmt":"yuv420p"},
			"audio":{"codec":"aac","bitrate":"192k","channels":2,"sampleRate":48000}},
		"totalMs":0,"checksum":""}`)
	p, e := decodeRenderPayloadStrict(raw)
	if e != nil {
		t.Fatalf("合法载荷不应被拒：%v", e)
	}
	if e := validateRenderEDLPayload(p); e != nil {
		t.Fatalf("合法载荷校验失败：%v", e)
	}
	if p.TotalMs != 10000 {
		t.Fatalf("totalMs 应回填 10000，实际 %d", p.TotalMs)
	}
	if p.Clips[0].Speed != 1.0 {
		t.Fatalf("缺省 speed 应回填 1.0，实际 %v", p.Clips[0].Speed)
	}
}

// TestD02PayloadLimits JSON 体积 / 深度 / 字符串长度 / 结构完整性（07 §3.6）。
func TestD02PayloadLimits(t *testing.T) {
	base := `{"type":"RenderEDL","clips":[{"file":"videos/a.mp4"}]}`
	deep := strings.Repeat(`{"a":`, edlMaxJSONDepth+1) + `1` + strings.Repeat(`}`, edlMaxJSONDepth+1)
	long := `{"s":"` + strings.Repeat("x", edlMaxJSONStrRunes+1) + `"}`

	cases := []struct {
		name     string
		raw      string
		wantCode string
	}{
		{"体积超限", `{"pad":"` + strings.Repeat("x", edlMaxBodyBytes) + `"}`, errCodeEDLTooLarge},
		{"深度超限", deep, errCodePayloadInvalid},
		{"字符串超限", long, errCodePayloadInvalid},
		{"结构不完整", `{"type":"RenderEDL"`, errCodePayloadInvalid},
		{"字符串未闭合", `{"type":"RenderEDL`, errCodePayloadInvalid},
		{"空载荷", ``, errCodePayloadInvalid},
		{"正常", base, ""},
	}
	for _, tc := range cases {
		e := validatePayloadLimits([]byte(tc.raw))
		got := ""
		if e != nil {
			got = e.Code
		}
		if got != tc.wantCode {
			t.Errorf("%s：期望 %q，实际 %q", tc.name, tc.wantCode, got)
		}
	}
}

// TestD02ResolveRelPathAndSymlinkEscape L4 落地校验：真实路径 + 符号链接逃逸（§8 S15）。
func TestD02ResolveRelPathAndSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "videos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "videos", "a.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	abs, e := edlResolveRelPath(root, "videos/a.mp4")
	if e != nil {
		t.Fatalf("合法相对路径应解析成功：%v", e)
	}
	if filepath.Base(abs) != "a.mp4" {
		t.Fatalf("解析结果异常：%s", abs)
	}

	if _, e := edlResolveRelPath(root, "nope/a.mp4"); e == nil || e.Code != errCodeAssetMissing {
		t.Fatalf("父目录不存在应返回 %s，实际 %v", errCodeAssetMissing, e)
	}

	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("当前环境不支持创建符号链接，跳过 S15 用例：%v", err)
	}
	for _, tc := range loadEDLVectors(t).Symlink {
		if _, e := edlResolveRelPath(root, tc.Rel); e == nil || e.Code != tc.WantCode {
			t.Fatalf("[%s] %q：期望 %s，实际 %v（%s）", tc.ID, tc.Rel, tc.WantCode, e, tc.Note)
		}
	}
}

// TestD02PathValidatorValidateRel PathValidator.ValidateRel：根授权 + L1~L4 全链路。
func TestD02PathValidatorValidateRel(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "videos"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "videos", "a.mp4"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "paths.txt")
	if err := os.WriteFile(cfg, []byte(root), 0o644); err != nil {
		t.Fatal(err)
	}
	pv := NewPathValidator(false, cfg)

	abs, e := pv.ValidateRel(root, "videos/a.mp4", edlAllowedSourceExt)
	if e != nil {
		t.Fatalf("授权根下的合法相对路径应通过：%v", e)
	}
	if filepath.Base(abs) != "a.mp4" {
		t.Fatalf("解析结果异常：%s", abs)
	}

	outside := t.TempDir()
	if _, e := pv.ValidateRel(outside, "a.mp4", nil); e == nil || e.Code != errCodeAssetNotInRoot {
		t.Fatalf("授权目录之外的根必须被拒（%s），实际 %v", errCodeAssetNotInRoot, e)
	}
	if _, e := pv.ValidateRel(root, "../../etc/passwd", nil); e == nil {
		t.Fatal("穿越相对路径必须被拒")
	}
	if _, e := pv.ValidateRel(root, "videos/a.txt", edlAllowedSourceExt); e == nil {
		t.Fatal("非白名单扩展名必须被拒")
	}

	pvEmpty := NewPathValidator(false, filepath.Join(t.TempDir(), "missing.txt"))
	if _, e := pvEmpty.ValidateRel(root, "videos/a.mp4", nil); e == nil || e.Code != errCodeAssetNotInRoot {
		t.Fatalf("未配置授权目录时必须拒绝（%s），实际 %v", errCodeAssetNotInRoot, e)
	}
}

// TestD02PathValidatorPrefixBoundary 授权前缀必须带分隔符边界，且不误杀含 .. 的合法文件名。
func TestD02PathValidatorPrefixBoundary(t *testing.T) {
	base := t.TempDir()
	allow := filepath.Join(base, "videos")
	sibling := filepath.Join(base, "videos2")
	for _, d := range []string{filepath.Join(allow, "sub"), sibling} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	inside := filepath.Join(allow, "sub", "y.mp4")
	siblingFile := filepath.Join(sibling, "x.mp4")
	dotdotName := filepath.Join(allow, "a..b.mp4")
	for _, f := range []string{inside, siblingFile, dotdotName} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := filepath.Join(t.TempDir(), "paths.txt")
	if err := os.WriteFile(cfg, []byte(allow), 0o644); err != nil {
		t.Fatal(err)
	}
	pv := NewPathValidator(false, cfg)

	if err := pv.Validate(inside); err != nil {
		t.Fatalf("授权目录内的文件应通过：%v", err)
	}
	if err := pv.Validate(siblingFile); err == nil {
		t.Fatal("videos2 不应被当作 videos 的子目录")
	}
	if err := pv.Validate(dotdotName); err != nil {
		t.Fatalf("a..b.mp4 不应被误判为穿越：%v", err)
	}
	if err := pv.Validate(filepath.Join(allow, "..", "videos2", "x.mp4")); err == nil {
		t.Fatal("含 .. 段的绝对路径必须被拒")
	}
}

// TestD02ValidateStatusMapping 错误码 → HTTP 状态映射（07 §3.2 L2 越权 → 403）。
func TestD02ValidateStatusMapping(t *testing.T) {
	cases := map[string]int{
		errCodeEDLInvalid:     http.StatusBadRequest,
		errCodePayloadInvalid: http.StatusBadRequest,
		errCodeAssetNotInRoot: http.StatusForbidden,
		errCodeAssetMissing:   http.StatusNotFound,
		errCodeEDLTooLarge:    http.StatusRequestEntityTooLarge,
		errCodeProfileInvalid: http.StatusBadRequest,
		errCodeProjectMissing: http.StatusBadRequest,
	}
	for code, want := range cases {
		if got := edlValidateStatus(code); got != want {
			t.Errorf("%s：期望 %d，实际 %d", code, want, got)
		}
	}
}

// TestD02ValidationErrorFieldAndIndex 结构化错误必须可定位 field / index（03 §5.1）。
func TestD02ValidationErrorFieldAndIndex(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"RenderEDL","sourceRoot":"videos","destRoot":"exports","output":"demo.mp4",
		"timeline":{"width":1920,"height":1080,"fps":30,"sampleRate":48000,"audio":true},
		"clips":[{"file":"videos/a.mp4","in":"00:00:00.000","out":"00:00:10.000","speed":1},
		         {"file":"videos/b.txt","in":"00:00:00.000","out":"00:00:10.000","speed":1}],
		"profile":{"presetKey":"libx264_medium","container":"mp4",
			"video":{"codec":"libx264","crf":23,"preset":"medium","pixFmt":"yuv420p"},
			"audio":{"codec":"aac","bitrate":"192k","channels":2,"sampleRate":48000}},
		"totalMs":20000}`)
	p, e := decodeRenderPayloadStrict(raw)
	if e != nil {
		t.Fatalf("载荷解码失败：%v", e)
	}
	e = validateRenderEDLPayload(p)
	if e == nil {
		t.Fatal("第二段 file 扩展名非法，必须被拒")
	}
	if e.Code != errCodeEDLInvalid || e.Field != "file" || e.Index != 1 {
		t.Fatalf("错误定位应为 E_EDL_INVALID/file/1，实际 %v", e)
	}
}

// ============================================================
// D-02 接入：渲染提交侧闸门（07 §3.2 L4 / §3.5）
// ============================================================

// newD02RenderEnv 构造带 PathValidator 的渲染提交环境（授权根 = 临时根目录）：
// 素材根/成品根均落在授权根内，使 precheckAssets 走 D-02 严格 L4 分支。
func newD02RenderEnv(t *testing.T) (r *gin.Engine, s *Store, srcDir, destDir, root string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root = t.TempDir()
	srcDir = filepath.Join(root, "videos")
	destDir = filepath.Join(root, "exports")
	for _, d := range []string{filepath.Join(srcDir, "demo"), destDir, filepath.Join(root, "state")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(srcDir, "demo", "a_01.mp4"), make([]byte, 64), 0o644); err != nil {
		t.Fatalf("写入素材失败: %v", err)
	}

	s = NewStore(filepath.Join(root, "state"))
	if err := s.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	cfg := s.GetSettings()
	cfg.VideoRoot = toPOSIXRoot(srcDir)
	cfg.ExportRoot = toPOSIXRoot(destDir)
	s.SaveSettings(cfg)

	pv := NewPathValidator(false, "")
	pv.SetExtraPaths([]string{root})
	h := &Handlers{store: s, hub: NewHub(), pv: pv}
	return newRouter(Config{}, h), s, srcDir, destDir, root
}

// TestD02RenderSubmitStrictAssetGate D-02 严格分支：素材缺失 404 / 软链接越权 403 / 合法提交放行。
func TestD02RenderSubmitStrictAssetGate(t *testing.T) {
	r, s, srcDir, _, _ := newD02RenderEnv(t)

	// 1) 素材缺失 → 404 E_ASSET_MISSING
	missing := renderTestClips()
	missing[0].File = "demo/missing.mp4"
	missing[1].File = "demo/missing.mp4"
	pMissing := mustCreateRenderProject(t, s, "d02_missing", renderTestTimeline(), missing)
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+pMissing.ID+"/render",
		`{"presetKey":"copy_same_source","outputName":"d02_missing"}`)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), errCodeAssetMissing) {
		t.Fatalf("严格分支缺失素材应 404 E_ASSET_MISSING，实际 %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "clips.file") {
		t.Errorf("错误 detail 必须可定位 clips.file，实际 %s", w.Body.String())
	}

	// 2) 合法提交 → 200，且闸门 7/8 通过后任务入队
	pOK := mustCreateRenderProject(t, s, "d02_ok", renderTestTimeline(), renderTestClips())
	resp := mustSubmitRender(t, r, pOK.ID, `{"presetKey":"copy_same_source","outputName":"d02_ok"}`)
	if resp.Output != "d02_ok.mp4" {
		t.Errorf("输出名应为 d02_ok.mp4，实际 %q", resp.Output)
	}
	if _, ok := s.GetTask(resp.TaskID); !ok {
		t.Errorf("合法提交应入队任务 %s", resp.TaskID)
	}

	// 3) 软链接越权：素材根内软链接指向授权根之外 → 403 E_ASSET_NOT_IN_ROOT
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.mp4"), make([]byte, 32), 0o644); err != nil {
		t.Fatalf("写入外部素材失败: %v", err)
	}
	link := filepath.Join(srcDir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("当前环境不支持创建软链接，跳过越权用例: %v", err)
	}
	escaping := renderTestClips()
	escaping[0].File = "link/secret.mp4"
	escaping[1].File = "link/secret.mp4"
	pEsc := mustCreateRenderProject(t, s, "d02_escape", renderTestTimeline(), escaping)
	w2 := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+pEsc.ID+"/render",
		`{"presetKey":"copy_same_source","outputName":"d02_escape"}`)
	if w2.Code != http.StatusForbidden || !strings.Contains(w2.Body.String(), errCodeAssetNotInRoot) {
		t.Fatalf("软链接越权应 403 E_ASSET_NOT_IN_ROOT，实际 %d %s", w2.Code, w2.Body.String())
	}
}

// TestD02RenderOutputNameStrictGate 最终输出名严格白名单（07 §3.5）：多重扩展名/保留名拒绝，合法名放行。
func TestD02RenderOutputNameStrictGate(t *testing.T) {
	r, s, _, _ := newRenderTestEnv(t)
	p := mustCreateRenderProject(t, s, "d02_outname", renderTestTimeline(), renderTestClips())

	// normalizeOutputBase 放行 a.v2（点号在主名内）→ 最终名闸门必须拒绝 a.v2.mp4
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+p.ID+"/render",
		`{"presetKey":"copy_same_source","outputName":"a.v2"}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), errCodeEDLInvalid) {
		t.Fatalf("多重扩展名输出名应 400 E_EDL_INVALID，实际 %d %s", w.Code, w.Body.String())
	}

	// Windows 保留设备名
	w2 := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+p.ID+"/render",
		`{"presetKey":"copy_same_source","outputName":"CON"}`)
	if w2.Code != http.StatusBadRequest || !strings.Contains(w2.Body.String(), errCodeEDLInvalid) {
		t.Fatalf("保留设备名应 400 E_EDL_INVALID，实际 %d %s", w2.Code, w2.Body.String())
	}

	// 合法名放行
	resp := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"final_cut_01"}`)
	if resp.Output != "final_cut_01.mp4" {
		t.Errorf("合法输出名应通过，实际 %q", resp.Output)
	}
}

// TestD02ShareRelRootMapping 共享根映射层（03 §2.3）：协议内根必须为相对共享根的 POSIX 相对根。
func TestD02ShareRelRootMapping(t *testing.T) {
	cases := []struct {
		name      string
		localRoot string
		shareBase string
		want      string
		wantOK    bool
	}{
		{"本地根在共享内", `D:\media\videos`, `D:\media`, "videos", true},
		{"多级相对", `D:\media\nas\videos`, `D:\media`, "nas/videos", true},
		{"斜杠混用与尾斜杠", "D:/media/videos/", `D:\media\`, "videos", true},
		{"大小写不敏感", `d:\MEDIA\Videos`, `D:\media`, "Videos", true},
		{"UNC 共享根", `\\HOST\share\videos`, `\\HOST\share`, "videos", true},
		{"重复斜杠压缩", `D://media//videos`, `D:\media`, "videos", true},
		{"等于共享根本身", `D:\media`, `D:\media`, "", false},
		{"不在共享内", `E:\other\videos`, `D:\media`, "", false},
		{"共享根为空", `D:\media\videos`, "", "", false},
		{"前缀伪装(media2)", `D:\media2\videos`, `D:\media`, "", false},
	}
	for _, c := range cases {
		got, ok := toShareRelRoot(c.localRoot, c.shareBase)
		if ok != c.wantOK || got != c.want {
			t.Errorf("%s: toShareRelRoot(%q, %q) = (%q, %v)，期望 (%q, %v)",
				c.name, c.localRoot, c.shareBase, got, ok, c.want, c.wantOK)
		}
	}
}

// TestD02RenderPayloadRootMappedToShareRel 配置共享根后，落地载荷的 sourceRoot/destRoot
// 必须是共享相对根（无盘符、无前导 /），并通过 FVCS 侧同源白名单。
func TestD02RenderPayloadRootMappedToShareRel(t *testing.T) {
	r, s, srcDir, destDir, root := newD02RenderEnv(t)
	cfg := s.GetSettings()
	cfg.SMBSharePath = root // 共享根 = 临时根目录，videos/exports 均为其子目录
	s.SaveSettings(cfg)

	p := mustCreateRenderProject(t, s, "d02_mapped", renderTestTimeline(), renderTestClips())
	resp := mustSubmitRender(t, r, p.ID, `{"presetKey":"copy_same_source","outputName":"d02_mapped"}`)

	task, ok := s.GetTask(resp.TaskID)
	if !ok {
		t.Fatalf("任务应入队: %s", resp.TaskID)
	}
	decoded, e := decodeRenderPayloadStrict(json.RawMessage(task.PayloadJSON))
	if e != nil {
		t.Fatalf("载荷应可严格解码: %v", e)
	}
	if decoded.SourceRoot != "videos" || decoded.DestRoot != "exports" {
		t.Fatalf("根应映射为共享相对根 videos/exports，实际 sourceRoot=%q destRoot=%q",
			decoded.SourceRoot, decoded.DestRoot)
	}
	if e := validateRenderEDLPayload(decoded); e != nil {
		t.Fatalf("映射后的载荷必须通过 FVCS 侧同源白名单: %v", e)
	}
	if strings.Contains(decoded.SourceRoot, ":") || strings.HasPrefix(decoded.SourceRoot, "/") {
		t.Errorf("协议根不得出现盘符或前导斜杠: %q", decoded.SourceRoot)
	}
	_ = srcDir
	_ = destDir
}
