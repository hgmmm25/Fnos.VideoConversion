package protocol

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// P2-4：统一错误契约 {ok:false, code, msg, detail?} 形状测试。
// 约定：HTTP 失败响应不再输出 "error" 键；code 为稳定机器码；HTTP 状态码语义不变。

func TestFailContractShape(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", func(c *gin.Context) {
		Fail(c, http.StatusBadRequest, "路径不能为空")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["ok"] != false {
		t.Errorf("ok = %v, want false", body["ok"])
	}
	if body["code"] != "E_BAD_REQUEST" {
		t.Errorf("code = %v, want E_BAD_REQUEST", body["code"])
	}
	if body["msg"] != "路径不能为空" {
		t.Errorf("msg = %v, want 路径不能为空", body["msg"])
	}
	if _, has := body["error"]; has {
		t.Errorf("legacy key 'error' must not be present, got %v", body["error"])
	}
	if _, has := body["detail"]; has {
		t.Errorf("detail must be omitted when not provided, got %v", body["detail"])
	}
}

func TestFailContractWithDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", func(c *gin.Context) {
		Fail(c, http.StatusNotFound, "文件不存在", gin.H{"path": "/data/a.mp4"})
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["code"] != "E_NOT_FOUND" {
		t.Errorf("code = %v, want E_NOT_FOUND", body["code"])
	}
	detail, ok := body["detail"].(map[string]any)
	if !ok {
		t.Fatalf("detail = %v, want object", body["detail"])
	}
	if detail["path"] != "/data/a.mp4" {
		t.Errorf("detail.path = %v, want /data/a.mp4", detail["path"])
	}
}

func TestFailWithCodeExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", func(c *gin.Context) {
		FailWithCode(c, http.StatusOK, "E_SERVER_OFFLINE", "连通性测试失败")
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if body["ok"] != false || body["code"] != "E_SERVER_OFFLINE" || body["msg"] != "连通性测试失败" {
		t.Errorf("body = %v, want {ok:false, code:E_SERVER_OFFLINE, msg:连通性测试失败}", body)
	}
}

func TestFailCodeMapping(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{http.StatusBadRequest, "E_BAD_REQUEST"},
		{http.StatusUnauthorized, "E_UNAUTHORIZED"},
		{http.StatusForbidden, "E_FORBIDDEN"},
		{http.StatusNotFound, "E_NOT_FOUND"},
		{http.StatusConflict, "E_CONFLICT"},
		{http.StatusTooManyRequests, "E_RATE_LIMITED"},
		{http.StatusInternalServerError, "E_INTERNAL"},
		{http.StatusOK, "E_INTERNAL"}, // 未显式 code 的 200 兜底为 E_INTERNAL
	}
	for _, tc := range cases {
		if got := FailCode(tc.status); got != tc.want {
			t.Errorf("FailCode(%d) = %s, want %s", tc.status, got, tc.want)
		}
	}
}
