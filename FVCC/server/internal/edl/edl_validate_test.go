package edl

// D-02 测试：FVCC 侧 EDL 白名单校验（纯校验单测）。
// P2-1 B轮：随 edl_validate.go 迁入 internal/edl；集成测试留在 server 根包。
// 用例数据来自共用向量 ../../FVCS/pkg/protocol/testdata/edl_vectors.json
// （规则来源 07 §3.2 / §3.5 / §3.6 / §8），与 FVCS/pkg/protocol/edl_validate_test.go 共用，
// 保证两侧「同规则双实现」不漂移。向量文件两侧均不得修改。

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const edlVecWantOK = "OK"

// TestEDLWithinRootBoundary 覆盖授权根边界判定：根自身、根下子路径、同前缀歧义路径。
// 重点回归 Windows 盘符根（D:\）Clean 后仍以分隔符结尾导致的「子路径永不命中」bug。
func TestEDLWithinRootBoundary(t *testing.T) {
	sep := string(filepath.Separator)

	// 通用（平台无关）用例：非分隔符结尾的根
	root := filepath.Join("vol", "media")
	within := filepath.Join(root, "a.mp4")
	ambig := filepath.Join("vol", "media2", "a.mp4")
	if !edlWithinRoot(root, root) {
		t.Errorf("根自身应命中: root=%q abs=%q", root, root)
	}
	if !edlWithinRoot(root, within) {
		t.Errorf("根下子路径应命中: root=%q abs=%q", root, within)
	}
	if edlWithinRoot(root, ambig) {
		t.Errorf("同前缀歧义路径不应命中: root=%q abs=%q", root, ambig)
	}

	// 分隔符结尾的根（Windows 盘符根场景，跨平台用显式根+sep 模拟同形态）
	driveRoot := "D:" + sep // 仅 Windows 上真实为盘符根；其它平台验证「根以分隔符结尾」分支
	if runtime.GOOS != "windows" {
		driveRoot = sep // 非 Windows 无盘符概念，用根分隔符等价模拟
	}
	if !edlWithinRoot(driveRoot, driveRoot) {
		t.Errorf("根自身应命中: root=%q", driveRoot)
	}
	child := driveRoot
	if runtime.GOOS == "windows" {
		child = driveRoot + "Fnos.VideoConversion" + sep + "FVCC"
	} else {
		child = driveRoot + "mnt" + sep + "data"
	}
	if !edlWithinRoot(driveRoot, child) {
		t.Errorf("以分隔符结尾的根下子路径应命中（回归盘符根 bug）: root=%q abs=%q", driveRoot, child)
	}

	// Windows 专属：盘符根下任意路径都在授权内；同前缀目录也在内（盘符根即全盘）
	if runtime.GOOS == "windows" {
		if !edlWithinRoot(`D:\`, `D:\Fnos.VideoConversion\FVCC\server`) {
			t.Errorf("D:\\ 授权下子路径应命中: D:\\Fnos.VideoConversion\\FVCC\\server")
		}
		if !edlWithinRoot(`D:\Fnos.VideoConversion`, `D:\Fnos.VideoConversion\FVCC`) {
			t.Errorf("非盘符根子路径应命中")
		}
		if edlWithinRoot(`D:\Fnos.VideoConversion`, `D:\Fnos.VideoConversion2`) {
			t.Errorf("同前缀歧义路径不应命中: D:\\Fnos.VideoConversion2")
		}
	}
}

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
	p := filepath.Join("..", "..", "..", "..", "FVCS", "pkg", "protocol", "testdata", "edl_vectors.json")
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
func TestD02ValidateStatusMapping(t *testing.T) {
	cases := map[string]int{
		errCodeEDLInvalid:     http.StatusBadRequest,
		errCodePayloadInvalid: http.StatusBadRequest,
		errCodeAssetNotInRoot: http.StatusForbidden,
		errCodeAssetMissing:   http.StatusNotFound,
		errCodeEDLTooLarge:    http.StatusRequestEntityTooLarge,
		errCodeProfileInvalid: http.StatusBadRequest,
		"E_PROJECT_NOT_FOUND": http.StatusBadRequest,
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
