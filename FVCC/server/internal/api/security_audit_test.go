package api

// D-04 验收测试（FVCC 侧审计与告警）：07 §7 拒绝记账 + 突增告警。
//  1) EDL 域拒绝类错误码（E_EDL_INVALID / E_PAYLOAD_INVALID / E_ASSET_NOT_IN_ROOT）经 edlErr 集中写 audit_log；
//  2) 非拒绝类错误码（如 E_PROJECT_NOT_FOUND）不产生 validate.reject 记录；
//  3) E_ASSET_NOT_IN_ROOT 5 分钟内达 3 次 → 恰好 1 条 security.alert；
//  4) 未达阈值（载荷类 50 次）不产生告警。

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

// testAuditStore 审计测试的包级 store 引用（替代原 main 包 globalStore，测试时 main 不运行）。
var testAuditStore *Store

// withGlobalStore 在测试期间把审计目标指向指定 store。
func withGlobalStore(t *testing.T, s *Store) {
	t.Helper()
	prev := testAuditStore
	testAuditStore = s
	security.SetAuditSink(s.AppendAudit)
	t.Cleanup(func() {
		testAuditStore = prev
		security.SetAuditSink(nil)
	})
}

func TestD04ValidateRejectAudited(t *testing.T) {
	r, s := newEDLTestRouter(t, t.TempDir())
	withGlobalStore(t, s)

	// 1) 名称超长 → 400 E_EDL_INVALID（拒绝类，必须记账）
	longName := strings.Repeat("超", edlMaxNameLen+1)
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects",
		`{"name":"`+longName+`","timeline":`+edlTimelineJSON+`}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("名称超长应 400，实际 %d %s", w.Code, w.Body.String())
	}
	rejects := s.ListAudit(0, security.AuditActionValidateReject)
	if len(rejects) != 1 {
		t.Fatalf("拒绝类错误应恰好记 1 条 %s，实际 %d 条", security.AuditActionValidateReject, len(rejects))
	}
	if rejects[0].Result != errCodeEDLInvalid {
		t.Fatalf("Result 应为 %s，实际 %q", errCodeEDLInvalid, rejects[0].Result)
	}
	if rejects[0].Detail == "" || !strings.Contains(rejects[0].Detail, "/api/edl/projects") {
		t.Fatalf("Detail 应记录方法+路径（脱敏），实际 %q", rejects[0].Detail)
	}

	// 2) 项目不存在 → 404 E_PROJECT_NOT_FOUND（非拒绝类，不记账）
	if w := doEDLRequest(t, r, http.MethodPut, "/api/edl/projects/p_notexist",
		`{"rev":1,"name":"x","timeline":`+edlTimelineJSON+`}`); w.Code != http.StatusNotFound {
		t.Fatalf("不存在项目应 404，实际 %d %s", w.Code, w.Body.String())
	}
	if got := len(s.ListAudit(0, security.AuditActionValidateReject)); got != 1 {
		t.Fatalf("非拒绝类错误不应记账，实际累计 %d 条", got)
	}
}

func TestD04AssetNotInRootRejectedAndAudited(t *testing.T) {
	r, s, srcDir, _, _ := newD02RenderEnv(t)
	withGlobalStore(t, s)
	security.ResetAlertCounters()
	t.Cleanup(security.ResetAlertCounters)

	// 素材根内软链接指向授权根之外 → 严格分支（闸门 6）判为 E_ASSET_NOT_IN_ROOT。
	// 说明：闸门 2 的项目内容校验按 03 §4.2 契约把路径类错误统一折叠为 E_EDL_INVALID，
	// 因此越权码只会出现在 precheckAssets 严格分支（03 §5.2）。
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.mp4"), make([]byte, 32), 0o644); err != nil {
		t.Fatalf("写入外部素材失败: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(srcDir, "link")); err != nil {
		t.Skipf("当前环境不支持创建软链接，跳过越权审计用例: %v", err)
	}

	escaping := renderTestClips()
	escaping[0].File = "link/secret.mp4"
	escaping[1].File = "link/secret.mp4"
	p := mustCreateRenderProject(t, s, "scan_probe", renderTestTimeline(), escaping)
	w := doEDLRequest(t, r, http.MethodPost, "/api/edl/projects/"+p.ID+"/render",
		`{"presetKey":"copy_same_source","outputName":"scan_probe"}`)

	var resp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不可解析: %v (%s)", err, w.Body.String())
	}
	if resp.Code != errCodeAssetNotInRoot {
		t.Fatalf("期望 %s，实际 %q（status=%d body=%s）",
			errCodeAssetNotInRoot, resp.Code, w.Code, w.Body.String())
	}

	rejects := s.ListAudit(0, security.AuditActionValidateReject)
	if len(rejects) != 1 || rejects[0].Result != errCodeAssetNotInRoot {
		t.Fatalf("越权扫描应写 1 条 %s(E_ASSET_NOT_IN_ROOT)，实际 %+v",
			security.AuditActionValidateReject, rejects)
	}
	if rejects[0].Target != p.ID {
		t.Fatalf("target 应为项目 id %s，实际 %q", p.ID, rejects[0].Target)
	}
	// 单次不触发告警
	if alerts := s.ListAudit(0, security.AuditActionSecurityAlert); len(alerts) != 0 {
		t.Fatalf("单次越权不应告警，实际 %d 条", len(alerts))
	}
}

func TestD04AlertOnAssetScanThreshold(t *testing.T) {
	_, s := newEDLTestRouter(t, t.TempDir())
	withGlobalStore(t, s)
	security.ResetAlertCounters()
	t.Cleanup(security.ResetAlertCounters)

	c := newAuditGinContext(t, "203.0.113.77")
	for i := 0; i < security.AlertAssetScanThreshold; i++ {
		security.AuditRejection(c, errCodeAssetNotInRoot)
	}
	alerts := s.ListAudit(0, security.AuditActionSecurityAlert)
	if len(alerts) != 1 {
		t.Fatalf("达阈值应恰好 1 条 %s，实际 %d 条", security.AuditActionSecurityAlert, len(alerts))
	}
	if alerts[0].Actor != "local" || alerts[0].Result != errCodeAssetNotInRoot {
		t.Fatalf("告警审计字段异常: %+v", alerts[0])
	}
	if !strings.Contains(alerts[0].Detail, "疑似路径越权扫描") {
		t.Fatalf("告警 detail 应说明原因，实际 %q", alerts[0].Detail)
	}

	// 越权计数独立的载荷桶：未达 50 次不得告警
	for i := 0; i < security.AlertPayloadProbeThreshold-1; i++ {
		security.AuditRejection(c, errCodePayloadInvalid)
	}
	if got := len(s.ListAudit(0, security.AuditActionSecurityAlert)); got != 1 {
		t.Fatalf("载荷类未达阈值不应新增告警，实际 %d 条", got)
	}
	security.AuditRejection(c, errCodePayloadInvalid)
	if got := len(s.ListAudit(0, security.AuditActionSecurityAlert)); got != 2 {
		t.Fatalf("载荷类达阈值应新增 1 条告警，实际 %d 条", got)
	}
}

// newAuditGinContext 造一个带来源 IP 的测试上下文（不进入路由）。
func newAuditGinContext(t *testing.T, ip string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	req := httptest.NewRequest(http.MethodPost, "/api/edl/projects", nil)
	req.RemoteAddr = ip + ":51000"
	c.Request = req
	return c
}

func TestP28DestructiveOpsAudited(t *testing.T) {
	_, s := newEDLTestRouter(t, t.TempDir())
	withGlobalStore(t, s)

	c := newAuditGinContext(t, "203.0.113.99")
	c.Request.Header.Set("X-Trim-User-Name", "ops")
	security.AuditDestructive(c, security.AuditActionDestructiveDelete, "server:srv1", "ok")
	security.AuditDestructive(c, security.AuditActionDestructiveClear, "video_cache", "ok")
	security.AuditDestructive(c, security.AuditActionDestructiveEmpty, "_trash", "removed=3")

	deletes := s.ListAudit(0, security.AuditActionDestructiveDelete)
	if len(deletes) != 1 || deletes[0].Target != "server:srv1" {
		t.Fatalf("删除类应记 1 条 destructive.delete，实际 %+v", deletes)
	}
	if deletes[0].Actor != "ops" {
		t.Fatalf("actor 应取网关用户名 ops，实际 %q", deletes[0].Actor)
	}
	if !strings.Contains(deletes[0].Detail, "/api/edl/projects") || !strings.Contains(deletes[0].Detail, "203.0.113.99") {
		t.Fatalf("Detail 应脱敏记录方法+路径+IP，实际 %q", deletes[0].Detail)
	}
	if got := len(s.ListAudit(0, security.AuditActionDestructiveClear)); got != 1 {
		t.Fatalf("清空类应记 1 条 destructive.clear，实际 %d", got)
	}
	if got := len(s.ListAudit(0, security.AuditActionDestructiveEmpty)); got != 1 {
		t.Fatalf("清空回收站应记 1 条 destructive.empty_trash，实际 %d", got)
	}
}
