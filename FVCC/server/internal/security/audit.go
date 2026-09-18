package security

// audit.go — P2-1 阶段 A：审计与告警（安全任务 D-04，见 WebVideoEditor_Design/07-安全校验与凭据管理细则.md §7）。
//
// 由原 main 包 security_audit.go 迁入：EDL 域拒绝事件的集中记账 + 突增告警。
// 挂载点选在 edlErr（03 §4.1 统一失败出口，40+ 处调用点共用），避免逐个 handler 埋点遗漏；
// 审计目标经 AuditSink 注入（main 启动时 SetAuditSink(store.AppendAudit)；单测可显式设置），
// nil 时仅降级日志（与原 globalStore nil 降级语义一致）。
// 脱敏约定：Detail 只记录「方法 + 路径 + 来源 IP」，不含素材绝对路径、共享凭据等敏感内容。

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/internal/protocol"
	"fvcc/internal/store/model"
	"fvcc/logger"
)

const (
	AuditActionValidateReject = "validate.reject"
	AuditActionSecurityAlert  = "security.alert"

	// P2-8：破坏性操作审计动作（删除/清缓存/清日志/清空回收站统一记账）
	AuditActionDestructiveDelete = "destructive.delete"      // 删除（视频→回收站 / 服务器 / 方案 / 任务 / 历史）
	AuditActionDestructiveClear  = "destructive.clear"       // 清空日志 / 清空视频缓存
	AuditActionDestructiveEmpty  = "destructive.empty_trash" // 清空回收站

	// 07 §7 告警阈值（5 分钟滑动窗口，按来源 IP 聚合）
	AlertAssetScanThreshold    = 3  // E_ASSET_NOT_IN_ROOT ≥ 3 → 越权扫描
	AlertPayloadProbeThreshold = 50 // E_EDL_INVALID / E_PAYLOAD_INVALID 突增 → 协议探测
	AlertWindow                = 5 * time.Minute
)

var (
	alertAssetScan    = NewSlidingWindowLimiter(AlertAssetScanThreshold+1, AlertWindow)
	alertPayloadProbe = NewSlidingWindowLimiter(AlertPayloadProbeThreshold+1, AlertWindow)

	// AuditSink 审计落库回调（main 启动时注入 store.AppendAudit；nil 时仅降级日志）。
	AuditSink func(model.AuditEntry)
)

// SetAuditSink 注入审计落库回调；传 nil 表示恢复降级日志模式。
func SetAuditSink(fn func(model.AuditEntry)) { AuditSink = fn }

// ResetAlertCounters 重建告警滑动窗口（单测隔离用）。
func ResetAlertCounters() {
	alertAssetScan = NewSlidingWindowLimiter(AlertAssetScanThreshold+1, AlertWindow)
	alertPayloadProbe = NewSlidingWindowLimiter(AlertPayloadProbeThreshold+1, AlertWindow)
}

// GetEDLActor 审计主体：优先取网关用户名，独立模式记为 local。
func GetEDLActor(c *gin.Context) string {
	if u := GetGatewayUser(c); u.Username != "" {
		return u.Username
	}
	return "local"
}

// AuditRejectCode 判断错误码是否属于 07 §7 需记账的拒绝类（越权 / 载荷校验）。
func AuditRejectCode(code string) bool {
	switch code {
	case protocol.ErrCodeEDLInvalid, protocol.ErrCodePayloadInvalid, protocol.ErrCodeAssetNotInRoot:
		return true
	}
	return false
}

// AuditRejection 记账一次拒绝事件（action=validate.reject），并按阈值判定突增告警。
func AuditRejection(c *gin.Context, code string) {
	if !AuditRejectCode(code) {
		return
	}
	actor, target, detail := auditRejectContext(c)
	logger.Warn("audit", "拒绝事件 code=%s actor=%s detail=%s", code, actor, detail)
	if AuditSink != nil {
		AuditSink(model.AuditEntry{
			Actor:  actor,
			Action: AuditActionValidateReject,
			Target: target,
			Detail: detail,
			Result: code,
		})
	}

	reason := alertReject(c, actor, code)
	if reason == "" {
		return
	}
	logger.Error("security", "告警阈值触发: %s actor=%s detail=%s", reason, actor, detail)
	if AuditSink != nil {
		AuditSink(model.AuditEntry{
			Actor:  actor,
			Action: AuditActionSecurityAlert,
			Target: target,
			Detail: reason + " | " + detail,
			Result: code,
		})
	}
}

// AuditDestructive 记账一次破坏性操作（删除/清缓存/清日志/清空回收站），
// 与 AuditRejection 共用审计通道。脱敏约定同拒绝类：Detail 只记录
// 「方法 + 路径 + 来源 IP」；资源标识经调用方显式传入 Target（文件路径 / id）。
func AuditDestructive(c *gin.Context, action, target, result string) {
	actor := "local"
	detail := "-"
	if c != nil && c.Request != nil {
		if u := GetGatewayUser(c); u.Username != "" {
			actor = u.Username
		}
		method, path := c.Request.Method, c.Request.URL.Path
		detail = method + " " + path
		if ip := c.ClientIP(); ip != "" {
			detail = fmt.Sprintf("%s ip=%s", detail, ip)
		}
	}
	logger.Warn("audit", "破坏性操作 action=%s actor=%s target=%s detail=%s result=%s", action, actor, target, detail, result)
	if AuditSink != nil {
		AuditSink(model.AuditEntry{
			Actor:  actor,
			Action: action,
			Target: target,
			Detail: detail,
			Result: result,
		})
	}
}

// alertReject 计数并按「首次跨越阈值」返回告警原因（未触发返回空串）。
func alertReject(c *gin.Context, actor, code string) string {
	key := clientKey(c, actor)
	if key == "" {
		return ""
	}
	switch code {
	case protocol.ErrCodeAssetNotInRoot:
		alertAssetScan.Record(key)
		if alertAssetScan.Count(key) == AlertAssetScanThreshold {
			return fmt.Sprintf("E_ASSET_NOT_IN_ROOT 5 分钟内达 %d 次（疑似路径越权扫描）", AlertAssetScanThreshold)
		}
	case protocol.ErrCodeEDLInvalid, protocol.ErrCodePayloadInvalid:
		alertPayloadProbe.Record(key)
		if alertPayloadProbe.Count(key) == AlertPayloadProbeThreshold {
			return fmt.Sprintf("载荷/EDL 校验失败 5 分钟内达 %d 次（疑似协议探测）", AlertPayloadProbeThreshold)
		}
	}
	return ""
}

// clientKey 告警聚合键：优先来源 IP，缺失时退回 actor。
func clientKey(c *gin.Context, actor string) string {
	if c != nil && c.Request != nil {
		if ip := c.ClientIP(); ip != "" {
			return ip
		}
	}
	return actor
}

// auditRejectContext 生成审计三元组（脱敏）：target 取资源 id 或路径，detail 记方法 + 路径 + IP。
func auditRejectContext(c *gin.Context) (actor, target, detail string) {
	actor, target = "local", "-"
	if c == nil {
		return
	}
	actor = GetEDLActor(c)
	id := c.Param("id")
	method, path := "", ""
	if c.Request != nil && c.Request.URL != nil {
		method, path = c.Request.Method, c.Request.URL.Path
	}
	detail = method + " " + path
	if id != "" {
		target = id
	} else if path != "" {
		target = path
	}
	if c.Request != nil {
		if ip := c.ClientIP(); ip != "" {
			detail = fmt.Sprintf("%s ip=%s", detail, ip)
		}
	}
	return
}
