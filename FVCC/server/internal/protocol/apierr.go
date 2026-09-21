// Package protocol 承载跨包共享的协议层基元（P2-1 阶段 A：apierr/applyjson 迁入）。
// 依赖方向：本包仅依赖标准库与 gin，禁止反向被下层包依赖。
package protocol

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ===== P2-4：统一 API 错误契约 =====
// 协议层统一错误响应：{ok:false, code, msg, detail?}（与 ui-src/src/api.ts 已声明的
// 统一错误契约对齐）。code 为稳定机器码（前端 ApiError.code），msg 面向用户展示，
// detail 可选透出额外上下文；HTTP 状态码保持语义不变（400/403/404/409/500...）。
//
// 历史兼容（旧接口曾输出两种形态，均已在本轮迁移）：
//   - {"error": "..."}             → 纯 HTTP 错误态（旧）
//   - {"ok":false, "error":"..."}  → HTTP 200 业务失败态（旧，如 testServer）
// 前端适配层（ui-src/src/api.ts request）保留 body.msg || body.error 兜底，
// 新代码一律走本 helper。命名规范详见 docs/API_CONTRACT.md。

// FailCode 按 HTTP 状态码映射通用错误码（P2-4）。
func FailCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "E_BAD_REQUEST"
	case http.StatusUnauthorized:
		return "E_UNAUTHORIZED"
	case http.StatusForbidden:
		return "E_FORBIDDEN"
	case http.StatusNotFound:
		return "E_NOT_FOUND"
	case http.StatusConflict:
		return "E_CONFLICT"
	case http.StatusTooManyRequests:
		return "E_RATE_LIMITED"
	default:
		return "E_INTERNAL"
	}
}

// Fail 输出统一失败契约（P2-4）。msg 面向用户展示；detail 可选透出额外上下文。
func Fail(c *gin.Context, status int, msg string, detail ...any) {
	body := gin.H{"ok": false, "code": FailCode(status), "msg": msg}
	if len(detail) > 0 && detail[0] != nil {
		body["detail"] = detail[0]
	}
	c.JSON(status, body)
}

// FailWithCode 显式指定错误码（用于业务域自定义 code，如 EDL/stream/网关域既有码）。
func FailWithCode(c *gin.Context, status int, code, msg string, detail ...any) {
	body := gin.H{"ok": false, "code": code, "msg": msg}
	if len(detail) > 0 && detail[0] != nil {
		body["detail"] = detail[0]
	}
	c.JSON(status, body)
}
