package server

// D-04 验收测试（审计与告警）：07 §7 审计落库 + 阈值告警。
//  1) /upload、/download 鉴权失败写 audit_log（action=auth.fail，actor=来源 IP）；
//  2) 冷却期拒绝写 audit_log（action=ratelimit.auth，result=E_RATE_LIMITED）；
//  3) 任务提交成功写 audit_log（action=task.submit，result=ok）；
//  4) 载荷/凭据前置拒绝写 audit_log（action=validate.reject，result=错误码）；
//  5) E_ASSET_NOT_IN_ROOT 达阈值（5min/3 次）触发 security.alert 告警审计。
//
// 审计依赖 audit_log（go-sqlite3，需 CGO）；无 CGO 环境自动 skip，不影响其余 D-04 用例。

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
)

// setupAuditStore 建临时凭据库并设为默认审计目标；go-sqlite3 不可用时 skip。
func setupAuditStore(t *testing.T) *smb.CredStore {
	t.Helper()
	store, err := smb.OpenCredStore(filepath.Join(t.TempDir(), "cred_audit.db"))
	if err != nil {
		t.Skipf("跳过：审计库不可用（需 CGO/go-sqlite3）: %v", err)
	}
	prev := smb.DefaultCredStore()
	smb.SetDefaultCredStore(store)
	t.Cleanup(func() {
		smb.SetDefaultCredStore(prev)
		_ = store.Close()
	})
	return store
}

// newAuditTestConn 构造可 safeSend 的连接（不涉及真实 websocket）。
func newAuditTestConn() *ClientConnection {
	return &ClientConnection{
		ClientID: "audit-test",
		ConnID:   "conn-audit",
		Send:     make(chan []byte, 16),
		done:     make(chan struct{}),
	}
}

func TestD04AuditAuthFailWritten(t *testing.T) {
	store := setupAuditStore(t)
	defer authFails.reset()
	defer config.Set(&config.Config{})
	config.Set(&config.Config{})

	const ip = "198.51.100.7"
	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	req.RemoteAddr = ip + ":6666"
	req.Header.Set("X-Auth-Key", "wrong-key")
	w := httptest.NewRecorder()
	handleDownload(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Key 不符应 401，实际 %d", w.Code)
	}

	rows, err := store.ListAudit(smb.AuditFilter{Action: actionAuthFail})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("鉴权失败应写 audit_log(action=%s)", actionAuthFail)
	}
	if rows[0].Actor != ip {
		t.Fatalf("审计 actor 应为来源 IP %s，实际 %q", ip, rows[0].Actor)
	}
	if rows[0].Result != "denied" {
		t.Fatalf("鉴权失败 result 应为 denied，实际 %q", rows[0].Result)
	}
	if rows[0].Target != "/download" {
		t.Fatalf("target 应记为 /download，实际 %q", rows[0].Target)
	}
}

func TestD04AuditRateLimitRejectAudited(t *testing.T) {
	store := setupAuditStore(t)
	defer authFails.reset()
	defer config.Set(&config.Config{})
	config.Set(&config.Config{})

	const ip = "198.51.100.8"
	for i := 0; i < authFailMax; i++ {
		authFailNote(ip, "/upload")
	}
	if !authFails.inCooldown(ip) {
		t.Fatalf("达阈值应进入冷却")
	}

	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.RemoteAddr = ip + ":7777"
	w := httptest.NewRecorder()
	handleUpload(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("冷却期应 429，实际 %d", w.Code)
	}

	rows, err := store.ListAudit(smb.AuditFilter{Action: actionRateLimitAuth})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("冷却拒绝应写 audit_log(action=%s)", actionRateLimitAuth)
	}
	if rows[0].Result != errCodeRateLimited {
		t.Fatalf("冷却拒绝 result 应为 %s，实际 %q", errCodeRateLimited, rows[0].Result)
	}
}

func TestD04AuditSubmitAndReject(t *testing.T) {
	store := setupAuditStore(t)
	c := newAuditTestConn()

	// 提交成功：RenderEDL/GenProxy/直通任务的提交埋点向 audit_log 写 ok
	// （此处直连埋点校验；命令级成功路径需任务管理器初始化，属集成测试范畴）
	auditSecurity(c.remoteIP(), actionRenderSubmit, "task-submit-render", "ok", "project=p1 rev=1 fastCopy=true")
	auditSecurity(c.remoteIP(), actionProxySubmit, "task-submit-proxy", "ok", "preset=proxy_540p")
	for _, action := range []string{actionRenderSubmit, actionProxySubmit} {
		rows, err := store.ListAudit(smb.AuditFilter{Action: action})
		if err != nil {
			t.Fatalf("ListAudit(%s) 失败: %v", action, err)
		}
		if len(rows) == 0 || rows[0].Result != "ok" {
			t.Fatalf("提交成功应写 audit_log(action=%s, result=ok)", action)
		}
	}
	if rows, err := store.ListAudit(smb.AuditFilter{Action: actionRenderSubmit}); err == nil {
		if rows[0].Target != "task-submit-render" {
			t.Fatalf("submit 审计 target 应为 taskId，实际 %q", rows[0].Target)
		}
	}

	// 前置拒绝：凭据档案不存在 → validate.reject 记错误码（真实 handler 链路）
	if err := c.handleCreateRenderEDL(&protocol.WebSocketRequest{
		TaskId:       "task-audit-reject",
		CredentialID: "cred_not_exist",
		SMBPath:      `\\nas\media`,
	}); err != nil {
		t.Fatalf("handleCreateRenderEDL 不应返回错误（拒绝经响应体透出）: %v", err)
	}
	rejects, err := store.ListAudit(smb.AuditFilter{Action: actionValidateReject})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(rejects) == 0 {
		t.Fatalf("前置拒绝应写 audit_log(action=%s)", actionValidateReject)
	}
	if rejects[0].Result != protocol.ErrCodeCredentialNotFound {
		t.Fatalf("拒绝应为 %s，实际 %q（detail=%s）",
			protocol.ErrCodeCredentialNotFound, rejects[0].Result, rejects[0].Detail)
	}
	if rejects[0].Target != "task-audit-reject" {
		t.Fatalf("target 应为 taskId，实际 %q", rejects[0].Target)
	}
}

func TestD04AlertOnAssetScan(t *testing.T) {
	store := setupAuditStore(t)
	alertAssetScan.reset()
	alertPayloadProbe.reset()
	t.Cleanup(func() {
		alertAssetScan.reset()
		alertPayloadProbe.reset()
	})

	const ip = "198.51.100.9"
	for i := 0; i < alertAssetScanThreshold; i++ {
		auditReject(ip, "task-scan", protocol.ErrCodeAssetNotInRoot, "RenderEDL")
	}

	alerts, err := store.ListAudit(smb.AuditFilter{Action: actionSecurityAlert})
	if err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	}
	if len(alerts) != 1 {
		t.Fatalf("越权扫描达阈值应恰好写 1 条 security.alert，实际 %d", len(alerts))
	}
	if alerts[0].Actor != ip {
		t.Fatalf("告警审计应记来源 IP %s，实际 %q", ip, alerts[0].Actor)
	}

	// 未达阈值的载荷校验失败不应产生告警
	for i := 0; i < alertPayloadProbeThreshold-1; i++ {
		auditReject(ip, "task-probe", protocol.ErrCodePayloadInvalid, "RenderEDL")
	}
	if alerts, err = store.ListAudit(smb.AuditFilter{Action: actionSecurityAlert}); err != nil {
		t.Fatalf("ListAudit 失败: %v", err)
	} else if len(alerts) != 1 {
		t.Fatalf("未达阈值不应新增告警，实际 %d 条", len(alerts))
	}
}

func TestD04AuditSkippedWhenStoreUnavailable(t *testing.T) {
	prev := smb.DefaultCredStore()
	smb.SetDefaultCredStore(nil)
	t.Cleanup(func() { smb.SetDefaultCredStore(prev) })

	// 审计库不可用时仅降级日志，不得 panic 或阻断请求
	auditReject("198.51.100.10", "task-x", protocol.ErrCodePayloadInvalid, "RenderEDL")
	auditSecurity("198.51.100.10", actionTaskSubmit, "task-x", "ok", "type=transcode")
}
