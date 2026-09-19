package api

// B-09 权限中间件验收测试（08 §4.2 / 07 §4.2、§7、§9 S10）：
//  1) 写操作（/render、DELETE 类）非 admin → 403，且响应体为 03 §4.1 统一失败契约（E_FORBIDDEN）；
//  2) admin / root 放行，独立模式（无网关头）放行，行为与改造前一致；
//  3) 项目读写（列表/详情/新建/保存）按 07 §4.2 对只读用户开放，不被 B-09 误挂权限。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// b09Headers 构造网关透传身份头（gateway.go 解析 X-Trim-User-Id / -Name / -Role）。
func b09Headers(uid, name, role string) map[string]string {
	h := map[string]string{}
	if uid != "" {
		h["X-Trim-User-Id"] = uid
	}
	if name != "" {
		h["X-Trim-User-Name"] = name
	}
	if role != "" {
		h["X-Trim-User-Role"] = role
	}
	return h
}

func doB09Request(t *testing.T, r *gin.Engine, method, path, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, gwPrefix+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestB09RenderAndDeleteRequireAdmin(t *testing.T) {
	r, _ := newEDLTestRouter(t, t.TempDir())

	reader := b09Headers("u_reader", "reader", "")
	admin := b09Headers("u_admin", "admin", "admin")
	root := b09Headers("u_root", "root", "root")

	// S10：无 admin 权限调用 /render → 403 E_FORBIDDEN
	w := doB09Request(t, r, http.MethodPost, "/api/edl/projects/p_demo/render", `{}`, reader)
	if w.Code != http.StatusForbidden {
		t.Fatalf("非 admin 提交渲染应 403，实际 %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"ok":false`) || !strings.Contains(w.Body.String(), `"code":"E_FORBIDDEN"`) {
		t.Fatalf("403 响应未对齐统一失败契约: %s", w.Body.String())
	}

	// admin / root 放行到业务层（项目不存在 → 404，而不是 403）
	for name, hdr := range map[string]map[string]string{"admin": admin, "root": root} {
		if got := doB09Request(t, r, http.MethodPost, "/api/edl/projects/p_demo/render", `{}`, hdr); got.Code == http.StatusForbidden {
			t.Fatalf("%s 不应被权限拦截: %d %s", name, got.Code, got.Body.String())
		}
	}

	// 删除类路由（07 §7：DELETE 挂 requireAdmin）
	deletes := []string{
		"/api/edl/projects/p_demo",
		"/api/log",
		"/api/video-cache",
		"/api/servers/s1",
		"/api/profiles/pr1",
		"/api/tasks/t1",
		"/api/history/h1",
	}
	for _, p := range deletes {
		got := doB09Request(t, r, http.MethodDelete, p, "", reader)
		if got.Code != http.StatusForbidden {
			t.Errorf("%s 非 admin 应 403，实际 %d %s", p, got.Code, got.Body.String())
			continue
		}
		if !strings.Contains(got.Body.String(), "E_FORBIDDEN") {
			t.Errorf("%s 403 响应缺少 E_FORBIDDEN: %s", p, got.Body.String())
		}
		if ok := doB09Request(t, r, http.MethodDelete, p, "", admin); ok.Code == http.StatusForbidden {
			t.Errorf("%s admin 不应被拦截: %d %s", p, ok.Code, ok.Body.String())
		}
	}
}

func TestB09ProjectReadWriteOpenToReadonlyUser(t *testing.T) {
	r, _ := newEDLTestRouter(t, t.TempDir())
	reader := b09Headers("u_reader", "reader", "")

	w := doB09Request(t, r, http.MethodPost, "/api/edl/projects",
		`{"name":"b09_团队共用","timeline":`+edlTimelineJSON+`}`, reader)
	if w.Code != http.StatusOK {
		t.Fatalf("只读用户新建项目应放行（07 §4.2 项目读写）: %d %s", w.Code, w.Body.String())
	}
	var created Project
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("响应不可解析: %v", err)
	}

	if got := doB09Request(t, r, http.MethodGet, "/api/edl/projects", "", reader); got.Code != http.StatusOK {
		t.Fatalf("只读用户列项目应放行: %d %s", got.Code, got.Body.String())
	}
	if got := doB09Request(t, r, http.MethodGet, "/api/edl/projects/"+created.ID, "", reader); got.Code != http.StatusOK {
		t.Fatalf("只读用户读项目详情应放行: %d %s", got.Code, got.Body.String())
	}
	if got := doB09Request(t, r, http.MethodPut, "/api/edl/projects/"+created.ID,
		`{"rev":1,"name":"b09_团队共用_v2","timeline":`+edlTimelineJSON+`}`, reader); got.Code != http.StatusOK {
		t.Fatalf("只读用户保存项目应放行: %d %s", got.Code, got.Body.String())
	}
	// 删除项目仍需 admin
	if got := doB09Request(t, r, http.MethodDelete, "/api/edl/projects/"+created.ID, "", reader); got.Code != http.StatusForbidden {
		t.Fatalf("只读用户删除项目应 403: %d %s", got.Code, got.Body.String())
	}
}

func TestB09StandaloneModeBypassesAdmin(t *testing.T) {
	r, _ := newEDLTestRouter(t, t.TempDir())
	// 无网关头（独立 / 内网直连）：放行，与改造前一致
	if got := doB09Request(t, r, http.MethodPost, "/api/edl/projects/p_demo/render", `{}`, nil); got.Code == http.StatusForbidden {
		t.Fatalf("独立模式提交渲染不应被拦截: %d %s", got.Code, got.Body.String())
	}
	if got := doB09Request(t, r, http.MethodDelete, "/api/log", "", nil); got.Code == http.StatusForbidden {
		t.Fatalf("独立模式删除日志不应被拦截: %d %s", got.Code, got.Body.String())
	}
}
