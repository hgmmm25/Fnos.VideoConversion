package edl

// P2-1 B轮：审计钩子解耦（07 §7 D-04 拒绝类错误码集中记账）。
// 根包经 SetAuditRejectionHook 注入 security.AuditRejection，避免校验域（edl）
// 反向依赖安全审计域（security）造成 import cycle（security 已依赖 edl 校验符号）。
// 未注入时静默降级（与审计 sink 未设置时的行为一致）。

import "github.com/gin-gonic/gin"

var auditRejectionHook func(c *gin.Context, code string)

// SetAuditRejectionHook 注入拒绝类错误码审计回调（由根包在初始化时调用）。
func SetAuditRejectionHook(fn func(c *gin.Context, code string)) {
	auditRejectionHook = fn
}

// auditRejection 调用已注入的审计回调；未注入时静默跳过。
func auditRejection(c *gin.Context, code string) {
	if auditRejectionHook != nil {
		auditRejectionHook(c, code)
	}
}
