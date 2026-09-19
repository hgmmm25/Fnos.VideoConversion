package main

// M4 预览网关验收测试（04 §2.2 Range / §2.3 票据 / §2.4 缩略图 / §2.6 三根白名单）：
//  1) 票据签发：路径越界 / 素材缺失拒绝，正常签发 tk_<32hex> 且带 expiresAt；
//  2) Range：200 全量 / 206 中段·后缀·开放端 / 416 越界；ETag 与 Accept-Ranges 齐备；
//  3) 票据复用（非一次性）：播放期间多次 Range 共用同一票据；换 path / 换 root / 显式删除后 → 403；
//  4) 单 IP 60s 内 ≤60 张，超出 429（04 §2.3 发放量限制）；
//  5) 缩略图：抽帧不可用时走 1×1 占位（200 image/jpeg），不返回 5xx 阻塞素材列表。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"fvcc/internal/security"
)

type m4Env struct {
	r     *gin.Engine
	store *Store
	h     *Handlers
	root  string
}

// newM4Env 构造 M4 测试环境：素材根（videoRoot）与 dataDir 分离，
// PathValidator 授权素材根（供 scanDirectory 用例），抽帧通道指向不存在的 ffmpeg。
func newM4Env(t *testing.T) *m4Env {
	t.Helper()
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	st.SaveSettings(Settings{VideoRoot: toPOSIXRoot(root)})

	pv := security.NewPathValidator(false, "")
	extra := []string{root}
	if realRoot, err := filepath.EvalSymlinks(root); err == nil && realRoot != root {
		extra = append(extra, realRoot)
	}
	pv.SetExtraPaths(extra)

	h := &Handlers{store: st, hub: NewHub(), pv: pv}
	// 扫描用例需要探测通道（main.go 装配 *FFprobe；此处注入桩以脱离 ffprobe）。
	probeInfo := VideoInfo{Duration: 1, Fps: "30/1", Width: 16, Height: 16}
	h.probe = &stubProber{fallback: &probeInfo}
	// 缩略图用例确定性走"抽帧失败"兜底分支（04 §2.4），不依赖宿主机 ffmpeg。
	h.thumbFFmpeg = filepath.Join(t.TempDir(), "no-such-ffmpeg")
	return &m4Env{r: newRouter(Config{}, h), store: st, h: h, root: root}
}

// writeFile 在素材根内写入指定大小的占位文件（内容无关，仅测 Range 字节语义）。
func (e *m4Env) writeFile(t *testing.T, rel string, size int) {
	t.Helper()
	m4WriteFile(t, filepath.Join(e.root, filepath.FromSlash(rel)), size)
}

func m4WriteFile(t *testing.T, abs string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("MkdirAll 失败: %v", err)
	}
	if err := os.WriteFile(abs, make([]byte, size), 0o644); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
}

// m4Ticket 申请一次票据并返回 ticket 串。
func m4Ticket(t *testing.T, e *m4Env, relPath string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"path": relPath})
	w := doEDLRequest(t, e.r, http.MethodPost, "/api/stream/ticket", string(body))
	if w.Code != http.StatusOK {
		t.Fatalf("申请票据失败: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Ticket    string `json:"ticket"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("票据响应解析失败: %v (%s)", err, w.Body.String())
	}
	if out.ExpiresAt == "" {
		t.Fatalf("票据响应缺少 expiresAt: %s", w.Body.String())
	}
	return out.Ticket
}

// m4StreamReq 构造带可选 Range 的 GET/HEAD 预览请求。
func m4StreamReq(t *testing.T, r *gin.Engine, method, urlPath, rangeHdr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, gwPrefix+urlPath, nil)
	if rangeHdr != "" {
		req.Header.Set("Range", rangeHdr)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestM4StreamTicketContract(t *testing.T) {
	e := newM4Env(t)
	e.writeFile(t, "demo/a_01.mp4", 3000)
	e.writeFile(t, "demo/b_02.mp4", 128)

	// 1) 路径越界 → 403 E_ASSET_NOT_IN_ROOT
	w := doEDLRequest(t, e.r, http.MethodPost, "/api/stream/ticket", `{"path":"../secret.mp4"}`)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "E_ASSET_NOT_IN_ROOT") {
		t.Fatalf("越界路径应 403 E_ASSET_NOT_IN_ROOT，实际 %d %s", w.Code, w.Body.String())
	}

	// 2) 素材缺失 → 404 E_ASSET_MISSING
	w = doEDLRequest(t, e.r, http.MethodPost, "/api/stream/ticket", `{"path":"demo/missing.mp4"}`)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "E_ASSET_MISSING") {
		t.Fatalf("缺失素材应 404 E_ASSET_MISSING，实际 %d %s", w.Code, w.Body.String())
	}

	// 3) 正常签发：tk_<32hex>
	tk := m4Ticket(t, e, "demo/a_01.mp4")
	if !strings.HasPrefix(tk, "tk_") || len(tk) != len("tk_")+32 {
		t.Fatalf("票据形态应为 tk_<32hex>，实际 %q", tk)
	}

	// 4) 无票据 → 403 E_TICKET_INVALID
	w = m4StreamReq(t, e.r, http.MethodGet, "/api/stream?path=demo/a_01.mp4&root=src", "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "E_TICKET_INVALID") {
		t.Fatalf("无票据应 403 E_TICKET_INVALID，实际 %d %s", w.Code, w.Body.String())
	}

	base := "/api/stream?path=demo/a_01.mp4&root=src&ticket=" + tk

	// 5) 200 全量：Content-Type / Content-Length / Accept-Ranges / ETag
	w = m4StreamReq(t, e.r, http.MethodGet, base, "")
	if w.Code != http.StatusOK {
		t.Fatalf("无 Range 应 200，实际 %d %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 3000 {
		t.Fatalf("全量响应体应为 3000 字节，实际 %d", w.Body.Len())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "video/mp4") {
		t.Fatalf("Content-Type 应为 video/mp4，实际 %q", ct)
	}
	if ar := w.Header().Get("Accept-Ranges"); ar != "bytes" {
		t.Fatalf("Accept-Ranges 应为 bytes，实际 %q", ar)
	}
	if w.Header().Get("ETag") == "" || w.Header().Get("Last-Modified") == "" {
		t.Fatalf("响应必须带 ETag 与 Last-Modified: %v", w.Header())
	}

	// 6) 非一次性：同一票据可重复使用（播放期间多次 Range）
	for i := 0; i < 3; i++ {
		w = m4StreamReq(t, e.r, http.MethodGet, base, "bytes=0-99")
		if w.Code != http.StatusPartialContent {
			t.Fatalf("第 %d 次复用票据应 206，实际 %d %s", i+1, w.Code, w.Body.String())
		}
	}

	// 7) 票据改绑 path → 403
	w = m4StreamReq(t, e.r, http.MethodGet, "/api/stream?path=demo/b_02.mp4&root=src&ticket="+tk, "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "E_TICKET_INVALID") {
		t.Fatalf("票据换 path 应 403，实际 %d %s", w.Code, w.Body.String())
	}

	// 8) 票据换 root → 403
	w = m4StreamReq(t, e.r, http.MethodGet, "/api/stream?path=demo/a_01.mp4&root=dest&ticket="+tk, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("票据换 root 应 403，实际 %d %s", w.Code, w.Body.String())
	}

	// 9) 显式 DELETE → 失效
	w = doEDLRequest(t, e.r, http.MethodDelete, "/api/stream/ticket?ticket="+tk, "")
	if w.Code != http.StatusOK {
		t.Fatalf("删除票据应 200，实际 %d %s", w.Code, w.Body.String())
	}
	w = m4StreamReq(t, e.r, http.MethodGet, base, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("票据已删除应 403，实际 %d %s", w.Code, w.Body.String())
	}

	// 10) 路径越界优先于票据校验 → E_ASSET_NOT_IN_ROOT
	w = m4StreamReq(t, e.r, http.MethodGet, "/api/stream?path=../a.mp4&root=src&ticket=tk_deadbeef", "")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "E_ASSET_NOT_IN_ROOT") {
		t.Fatalf("越界路径应 403 E_ASSET_NOT_IN_ROOT，实际 %d %s", w.Code, w.Body.String())
	}
}

func TestM4StreamRangeSemantics(t *testing.T) {
	e := newM4Env(t)
	e.writeFile(t, "demo/a_01.mp4", 3000)
	tk := m4Ticket(t, e, "demo/a_01.mp4")
	base := "/api/stream?path=demo/a_01.mp4&root=src&ticket=" + tk

	cases := []struct {
		name       string
		rangeHdr   string
		wantStatus int
		wantLen    int
		wantRange  string
	}{
		{"中段", "bytes=100-199", http.StatusPartialContent, 100, "bytes 100-199/3000"},
		{"开放端", "bytes=2000-", http.StatusPartialContent, 1000, "bytes 2000-2999/3000"},
		{"后缀", "bytes=-100", http.StatusPartialContent, 100, "bytes 2900-2999/3000"},
		{"单字节", "bytes=0-0", http.StatusPartialContent, 1, "bytes 0-0/3000"},
		{"越界", "bytes=5000-", http.StatusRequestedRangeNotSatisfiable, 0, "bytes */3000"},
	}
	for _, tc := range cases {
		w := m4StreamReq(t, e.r, http.MethodGet, base, tc.rangeHdr)
		if w.Code != tc.wantStatus {
			t.Fatalf("%s：Range=%q 应 %d，实际 %d %s", tc.name, tc.rangeHdr, tc.wantStatus, w.Code, w.Body.String())
		}
		if tc.wantLen > 0 && w.Body.Len() != tc.wantLen {
			t.Fatalf("%s：响应体应为 %d 字节，实际 %d", tc.name, tc.wantLen, w.Body.Len())
		}
		if tc.wantRange != "" {
			if cr := w.Header().Get("Content-Range"); cr != tc.wantRange {
				t.Fatalf("%s：Content-Range 应为 %q，实际 %q", tc.name, tc.wantRange, cr)
			}
		}
	}

	// HEAD：只读方法头，状态与 Content-Type 可用
	w := m4StreamReq(t, e.r, http.MethodHead, base, "")
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD 应 200，实际 %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "video/mp4") {
		t.Fatalf("HEAD Content-Type 应为 video/mp4，实际 %q", ct)
	}
}

func TestM4StreamTicketPerIPRateLimit(t *testing.T) {
	e := newM4Env(t)
	e.writeFile(t, "demo/a_01.mp4", 16)
	body, _ := json.Marshal(map[string]string{"path": "demo/a_01.mp4"})

	// 04 §2.3：单 IP 60s 内最多 60 张，第 61 张拒绝。
	for i := 0; i < 60; i++ {
		w := doEDLRequest(t, e.r, http.MethodPost, "/api/stream/ticket", string(body))
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 张票据应 200，实际 %d %s", i+1, w.Code, w.Body.String())
		}
	}
	w := doEDLRequest(t, e.r, http.MethodPost, "/api/stream/ticket", string(body))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超出单 IP 发放量应 429，实际 %d %s", w.Code, w.Body.String())
	}
}

func TestM4ThumbFallbackAndGuards(t *testing.T) {
	e := newM4Env(t)
	e.writeFile(t, "demo/a_01.mp4", 3000)
	tk := m4Ticket(t, e, "demo/a_01.mp4")

	// 1) 抽帧不可用 → 200 + 1×1 透明 JPEG 占位（04 §2.4：禁止 5xx）
	w := m4StreamReq(t, e.r, http.MethodGet,
		"/api/thumb?path=demo/a_01.mp4&t=1000&root=src&ticket="+tk, "")
	if w.Code != http.StatusOK {
		t.Fatalf("缩略图失败兜底应 200，实际 %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/jpeg") {
		t.Fatalf("缩略图 Content-Type 应为 image/jpeg，实际 %q", ct)
	}
	b := w.Body.Bytes()
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		t.Fatalf("缩略图响应应为 JPEG（SOI 0xFFD8），实际前几字节 % x", b[:min(4, len(b))])
	}

	// 2) 缓存根推导：<videoRoot>/_wve_cache（04 §2.4）
	posixRoot, localRoot := e.h.resolveCacheRoot()
	if !strings.HasSuffix(posixRoot, "/_wve_cache") {
		t.Fatalf("缩略图缓存根应缺省为 <videoRoot>/_wve_cache，实际 %q", posixRoot)
	}
	if strings.TrimSpace(localRoot) == "" {
		t.Fatalf("缩略图缓存本地根不应为空")
	}

	// 3) 路径越界 → 403（与 /stream 同规则）
	w = m4StreamReq(t, e.r, http.MethodGet, "/api/thumb?path=../x.mp4&t=0&root=src", "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("缩略图越界路径应 403，实际 %d %s", w.Code, w.Body.String())
	}

	// 4) 非法 root → 400 E_ROOT_UNKNOWN
	w = m4StreamReq(t, e.r, http.MethodGet, "/api/thumb?path=demo/a_01.mp4&t=0&root=weird", "")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "E_ROOT_UNKNOWN") {
		t.Fatalf("非法 root 应 400 E_ROOT_UNKNOWN，实际 %d %s", w.Code, w.Body.String())
	}
}
