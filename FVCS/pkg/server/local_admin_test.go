package server

// D-03 单测：本机凭据管理接口（07 §5.4）
//
// 覆盖项：
//  1. 非环回来源一律 403（即使凭据库就绪）
//  2. 创建/查询/列表/删除全链路，响应体不含任何口令字段
//  3. 错误码与 HTTP 状态映射（not found → 404、invalid → 400、in use → 409）
//  4. 请求体上限与非法 JSON 拒绝

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
)

const (
	adminTestShare    = `\\nas-01\media`
	adminTestScope    = `\\nas-01\media\footage`
	adminTestPassword = "P@ssw0rd-接口明文禁回传"
)

func withAdminStore(t *testing.T) *smb.CredStore {
	t.Helper()

	prev := smb.DefaultCredStore()
	store, err := smb.OpenCredStore(filepath.Join(t.TempDir(), "credentials.db"))
	if err != nil {
		t.Fatalf("OpenCredStore 失败: %v", err)
	}
	smb.SetDefaultCredStore(store)
	t.Cleanup(func() {
		smb.SetDefaultCredStore(prev)
		_ = store.Close()
	})
	return store
}

func doCredentialRequest(t *testing.T, method, target, body, remoteAddr string) (int, map[string]interface{}, string) {
	t.Helper()

	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, target, reader)
	req.RemoteAddr = remoteAddr

	rec := httptest.NewRecorder()
	handleLocalCredentials(rec, req)

	raw := rec.Body.String()
	out := map[string]interface{}{}
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return rec.Code, out, raw
}

func putBody(id string) string {
	b, _ := json.Marshal(smb.PutCredentialRequest{
		ID:        id,
		ShareBase: adminTestShare,
		Username:  "nasuser",
		Password:  adminTestPassword,
		Scope:     []string{adminTestScope},
	})
	return string(b)
}

// D-03-#1 非环回来源拒绝（含凭据库已就绪的情形）
func TestLocalCredentialsRejectsNonLoopback(t *testing.T) {
	withAdminStore(t)

	for _, addr := range []string{"192.168.1.9:50001", "10.0.0.2:8080", "[fe80::1]:9999"} {
		code, resp, raw := doCredentialRequest(t, http.MethodGet, "/local/credentials", "", addr)
		if code != http.StatusForbidden {
			t.Errorf("来源 %s 应 403，实际 %d", addr, code)
		}
		if resp["code"] != protocol.ErrCodeCredentialScopeDenied {
			t.Errorf("来源 %s 错误码 = %v, 期望 %s", addr, resp["code"], protocol.ErrCodeCredentialScopeDenied)
		}
		if strings.Contains(raw, adminTestPassword) {
			t.Errorf("响应体出现口令明文: %s", raw)
		}
	}
}

// D-03-#2 环回来源全链路：创建 → 列表 → 单查 → 删除
func TestLocalCredentialsLoopbackLifecycle(t *testing.T) {
	withAdminStore(t)

	// 创建（PUT）
	code, resp, raw := doCredentialRequest(t, http.MethodPut, "/local/credentials", putBody("cred_nas01"), "127.0.0.1:51000")
	if code != http.StatusOK {
		t.Fatalf("创建应 200，实际 %d，body=%s", code, raw)
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Fatalf("创建返回 ok=false: %s", raw)
	}
	if strings.Contains(raw, adminTestPassword) {
		t.Fatalf("创建响应含口令明文: %s", raw)
	}
	if strings.Contains(raw, `"password"`) {
		t.Fatalf("创建响应含 password 字段: %s", raw)
	}

	// 列表
	code, resp, raw = doCredentialRequest(t, http.MethodGet, "/local/credentials", "", "127.0.0.1:51000")
	if code != http.StatusOK {
		t.Fatalf("列表应 200，实际 %d", code)
	}
	data, _ := resp["data"].([]interface{})
	if len(data) != 1 {
		t.Fatalf("列表长度 = %d, 期望 1，body=%s", len(data), raw)
	}
	if strings.Contains(raw, adminTestPassword) {
		t.Fatalf("列表响应含口令明文: %s", raw)
	}

	// 单查
	code, resp, raw = doCredentialRequest(t, http.MethodGet, "/local/credentials?id=cred_nas01", "", "::1")
	if code != http.StatusOK {
		t.Fatalf("单查应 200，实际 %d，body=%s", code, raw)
	}
	item, _ := resp["data"].(map[string]interface{})
	if item["credentialId"] != "cred_nas01" {
		t.Errorf("credentialId = %v, 期望 cred_nas01", item["credentialId"])
	}
	if _, exists := item["secret"]; exists {
		t.Error("详情不应含 secret 字段")
	}

	// 删除
	code, _, raw = doCredentialRequest(t, http.MethodDelete, "/local/credentials?id=cred_nas01", "", "127.0.0.1:51000")
	if code != http.StatusOK {
		t.Fatalf("删除应 200，实际 %d，body=%s", code, raw)
	}

	// 删除后单查 → 404
	code, resp, _ = doCredentialRequest(t, http.MethodGet, "/local/credentials?id=cred_nas01", "", "127.0.0.1:51000")
	if code != http.StatusNotFound {
		t.Errorf("删除后单查应 404，实际 %d", code)
	}
	if resp["code"] != protocol.ErrCodeCredentialNotFound {
		t.Errorf("错误码 = %v, 期望 %s", resp["code"], protocol.ErrCodeCredentialNotFound)
	}
}

// D-03-#3 删除被运行中任务引用 → 409 E_CREDENTIAL_IN_USE
func TestLocalCredentialsDeleteInUse(t *testing.T) {
	store := withAdminStore(t)

	if code, _, raw := doCredentialRequest(t, http.MethodPut, "/local/credentials", putBody("cred_nas01"), "127.0.0.1:1"); code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", code, raw)
	}
	store.SetInUseChecker(func(string) []string { return []string{"task_x"} })

	code, resp, _ := doCredentialRequest(t, http.MethodDelete, "/local/credentials?id=cred_nas01", "", "127.0.0.1:1")
	if code != http.StatusConflict {
		t.Errorf("占用中删除应 409，实际 %d", code)
	}
	if resp["code"] != protocol.ErrCodeCredentialInUse {
		t.Errorf("错误码 = %v, 期望 %s", resp["code"], protocol.ErrCodeCredentialInUse)
	}
	store.SetInUseChecker(nil)
}

// D-03-#4 入参拒绝：非法 JSON / 缺 id / 非法 scope / 方法不支持 / 超大 body
func TestLocalCredentialsBadRequests(t *testing.T) {
	withAdminStore(t)

	cases := []struct {
		name   string
		method string
		target string
		body   string
		want   int
	}{
		{"非法 JSON", http.MethodPut, "/local/credentials", "{not-json", http.StatusBadRequest},
		{"DELETE 缺 id", http.MethodDelete, "/local/credentials", "", http.StatusBadRequest},
		{"method 不支持", http.MethodPatch, "/local/credentials", "", http.StatusMethodNotAllowed},
		{"scope 越出 shareBase", http.MethodPut, "/local/credentials",
			`{"credentialId":"cred_x","shareBase":"\\\\nas-01\\media","username":"u","password":"p","scope":["\\\\nas-02\\media"]}`,
			http.StatusBadRequest},
		{"空口令", http.MethodPut, "/local/credentials",
			`{"credentialId":"cred_y","shareBase":"\\\\nas-01\\media","username":"u","password":""}`,
			http.StatusBadRequest},
	}
	for _, c := range cases {
		code, resp, raw := doCredentialRequest(t, c.method, c.target, c.body, "127.0.0.1:52000")
		if code != c.want {
			t.Errorf("%s: 状态码 = %d, 期望 %d（body=%s）", c.name, code, c.want, raw)
		}
		if code >= 400 && resp["ok"] != false {
			t.Errorf("%s: 失败响应 ok 字段应为 false，实际 %v", c.name, resp["ok"])
		}
	}

	// 超大 body → 413
	big := `{"credentialId":"cred_big","shareBase":"\\\\nas-01\\media","username":"u","password":"` +
		strings.Repeat("x", maxCredentialBodyBytes+16) + `"}`
	if code, _, _ := doCredentialRequest(t, http.MethodPut, "/local/credentials", big, "127.0.0.1:52000"); code != http.StatusRequestEntityTooLarge {
		t.Errorf("超大 body 应 413，实际 %d", code)
	}
}

// D-03-#5 凭据库未初始化 → 503（环回请求亦不可用）
func TestLocalCredentialsStoreUnavailable(t *testing.T) {
	prev := smb.DefaultCredStore()
	smb.SetDefaultCredStore(nil)
	t.Cleanup(func() { smb.SetDefaultCredStore(prev) })

	code, resp, _ := doCredentialRequest(t, http.MethodGet, "/local/credentials", "", "127.0.0.1:53000")
	if code != http.StatusServiceUnavailable {
		t.Errorf("库未初始化应 503，实际 %d", code)
	}
	if resp["code"] != codeCredentialStoreUnavailable {
		t.Errorf("错误码 = %v, 期望 %s", resp["code"], codeCredentialStoreUnavailable)
	}
}

// D-03 辅助：环回判定覆盖 IPv4/IPv6/非法地址
func TestIsLoopbackRequest(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:80":   true,
		"127.0.0.5:80":   true,
		"[::1]:80":       true,
		"192.168.1.2:80": false,
		"8.8.8.8:80":     false,
		"":               false,
	}
	for addr, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/local/credentials", nil)
		req.RemoteAddr = addr
		if got := isLoopbackRequest(req); got != want {
			t.Errorf("isLoopbackRequest(%q) = %v, 期望 %v", addr, got, want)
		}
	}
}
