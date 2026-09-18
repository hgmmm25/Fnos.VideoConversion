package main

import (
	"strings"

	"github.com/gin-gonic/gin"
)

// GatewayUser 统一网关透传的用户信息。
// fnOS 网关在转发请求时注入 X-Trim-* 请求头。
type GatewayUser struct {
	UID      string // 用户 UID
	IsAdmin  bool   // 是否管理员
	Username string // 用户名
	Present  bool   // 是否存在有效登录态
}

// getGatewayUser 从请求头解析网关用户信息。
func getGatewayUser(c *gin.Context) GatewayUser {
	u := GatewayUser{
		UID:      c.GetHeader("X-Trim-User-Id"),
		Username: c.GetHeader("X-Trim-User-Name"),
	}
	role := strings.ToLower(c.GetHeader("X-Trim-User-Role"))
	if role == "admin" || role == "root" {
		u.IsAdmin = true
	}
	u.Present = u.UID != "" || u.Username != ""
	return u
}

// gatewayUser 中间件：解析网关用户并注入到 context。
func gatewayUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := getGatewayUser(c)
		c.Set("gatewayUser", user)
		c.Next()
	}
}

// errCodeForbidden 权限拒绝错误码（07 §4.2；03 §5.3 未定义，实现补充，见 10 号台账）。
const errCodeForbidden = "E_FORBIDDEN"

// requireAdmin 中间件：要求管理员权限（独立模式放行）。
// 依据 07 §4.2 / §7：提交渲染（/render）、生成代理（/proxy）、删除类（DELETE）等写操作仅 admin；
// 无网关身份（独立 / 内网直连模式）时放行，行为与改造前一致；拒绝响应对齐 03 §4.1 统一失败契约。
func requireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		user := getGatewayUser(c)
		// 独立模式（无网关头）放行，仅内网使用
		if !user.Present {
			c.Next()
			return
		}
		if !user.IsAdmin {
			c.JSON(403, gin.H{"ok": false, "code": errCodeForbidden, "msg": "需要管理员权限"})
			c.Abort()
			return
		}
		c.Next()
	}
}
