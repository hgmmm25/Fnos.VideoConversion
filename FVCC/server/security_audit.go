package main

// D-04：07 §7 审计与告警 —— EDL 域拒绝事件的集中记账 + 突增告警。
//
// 挂载点选在 edlErr（03 §4.1 统一失败出口，40+ 处调用点共用），避免逐个 handler 埋点遗漏；
// 审计目标 store 经 globalStore 注入（main 启动时赋值；单测可显式赋值），nil 时仅降级日志。
// 脱敏约定：Detail 只记录「方法 + 路径 + 来源 IP」，不含素材绝对路径、共享凭据等敏感内容。

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
)

const (
	auditActionValidateReject = "validate.reject"
	auditActionSecurityAlert  = "security.alert"

	// 07 §7 告警阈值（5 分钟滑动窗口，按来源 IP 聚合）
	alertAssetScanThreshold    = 3  // E_ASSET_NOT_IN_ROOT ≥ 3 → 越权扫描
	alertPayloadProbeThreshold = 50 // E_EDL_INVALID / E_PAYLOAD_INVALID 突增 → 协议探测
	alertWindow                = 5 * time.Minute
)

var (
	alertAssetScan    = newSlidingWindowLimiter(alertAssetScanThreshold+1, alertWindow)
	alertPayloadProbe = newSlidingWindowLimiter(alertPayloadProbeThreshold+1, alertWindow)
)

// auditRejectCode 判断错误码是否属于 07 §7 需记账的拒绝类（越权 / 载荷校验）。
func auditRejectCode(code string) bool {
	switch code {
	case errCodeEDLInvalid, errCodePayloadInvalid, errCodeAssetNotInRoot:
		return true
	}
	return false
}

// auditRejection 记账一次拒绝事件（action=validate.reject），并按阈值判定突增告警。
func auditRejection(c *gin.Context, code string) {
	if !auditRejectCode(code) {
		return
	}
	actor, target, detail := auditRejectContext(c)
	store := globalStore
	logger.Warn("audit", "拒绝事件 code=%s actor=%s detail=%s", code, actor, detail)
	if store != nil {
		store.AppendAudit(AuditEntry{
			Actor:  actor,
			Action: auditActionValidateReject,
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
	if store != nil {
		store.AppendAudit(AuditEntry{
			Actor:  actor,
			Action: auditActionSecurityAlert,
			Target: target,
			Detail: reason + " | " + detail,
			Result: code,
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
	case errCodeAssetNotInRoot:
		alertAssetScan.record(key)
		if alertAssetScan.count(key) == alertAssetScanThreshold {
			return fmt.Sprintf("E_ASSET_NOT_IN_ROOT 5 分钟内达 %d 次（疑似路径越权扫描）", alertAssetScanThreshold)
		}
	case errCodeEDLInvalid, errCodePayloadInvalid:
		alertPayloadProbe.record(key)
		if alertPayloadProbe.count(key) == alertPayloadProbeThreshold {
			return fmt.Sprintf("载荷/EDL 校验失败 5 分钟内达 %d 次（疑似协议探测）", alertPayloadProbeThreshold)
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
	actor = edlActor(c)
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
