package main

// 安全响应头与限流中间件（07 §4.4 / §4.5，D-04）
//
// 落地范围：
//  1. 安全响应头（07 §4.4）：X-Content-Type-Options: nosniff、Referrer-Policy: no-referrer、
//     X-Frame-Options: SAMEORIGIN —— 全链路生效（REST / 静态资源 / /stream / SPA 兜底）；
//  2. 鉴权失败限流（07 §4.5：登录失败 10 次 / 5 分钟，按来源 IP + 用户名独立计量）：
//     fnOS 网关在到达应用前即拦截的未登录请求不在本层可见，本层覆盖应用侧
//     401（未认证 / 凭据无效）响应，达阈值后返回 429；
//  3. 渲染提交限流（07 §4.5：30 次 / 分钟 / 用户）：按网关 UID 计量，无网关身份
//     （独立 / 内网直连模式）回落来源 IP；
//  4. 限流拒绝均写审计（07 §7：Action=ratelimit.*，Result=denied）与 WARN 日志，
//     审计 Detail 仅记 key 与路径，不含口令 / 凭据。
//
// 说明：/stream 票据限流（60 次/分钟/IP）由 stream.go 的 ticketStore 既有实现承担，
// 预览并发上限由 streamLimiter 承担，本文件不重复实现。

import (
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
	"fvcc/internal/security"
)

const (
	// errCodeRateLimited 限流拒绝错误码（07 §4.5；03 §5.3 未定义，实现补充）。
	errCodeRateLimited = "E_RATE_LIMITED"

	// 07 §4.5 阈值
	authFailMax      = 10               // 登录/鉴权失败次数
	authFailWindow   = 5 * time.Minute  // 观测窗口
	authFailCooldown = 15 * time.Minute // 超限冷却（07 §4.5）
	renderSubmitMax  = 30               // 渲染提交次数
	renderWindow     = time.Minute      // 观测窗口

)

// ===== 安全响应头（07 §4.4）=====

// securityHeaders 为所有响应追加 07 §4.4 要求的安全头。
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		c.Next()
	}
}

// ===== 限流器实例 =====

var (
	// authFailLimiter 鉴权失败限流（07 §4.5：10 次 / 5 分钟 / IP → 冷却 15 分钟）
	authFailLimiter = security.NewCooldownLimiter(authFailMax, authFailWindow, authFailCooldown)
	// renderSubmitLimiter 渲染提交限流（07 §4.5：30 次 / 分钟 / 用户）
	renderSubmitLimiter = security.NewSlidingWindowLimiter(renderSubmitMax, renderWindow)
)

// ===== 中间件 =====

// auditReject 生成限流拒绝回调：写审计（Result=denied）+ WARN 日志。
func (h *Handlers) auditReject(action string) func(c *gin.Context, key string) {
	return func(c *gin.Context, key string) {
		actor := security.GetEDLActor(c)
		path := ""
		if c.Request != nil && c.Request.URL != nil {
			path = c.Request.URL.Path
		}
		logger.Warn("ratelimit", "%s 触发限流 actor=%v key=%v path=%v", action, actor, key, path)
		if h == nil || h.store == nil {
			return
		}
		h.store.AppendAudit(AuditEntry{
			Actor:  actor,
			Action: action,
			Target: path,
			Detail: "key=" + key,
			Result: "denied",
		})
	}
}

// rateLimited 输出统一失败契约的 429（03 §4.1）。
func rateLimited(c *gin.Context, msg string) {
	c.JSON(http.StatusTooManyRequests, gin.H{
		"ok":   false,
		"code": errCodeRateLimited,
		"msg":  msg,
	})
}

// authFailThrottle 鉴权失败限流（07 §4.5：10 次 / 5min / IP → 429 + 冷却 15min）：
//   - 前置：key 处于冷却期（或窗口内已达阈值）→ 429（不再进入后续处理，冷却期内持续拒绝）；
//   - 后置：响应为 401（未认证 / 凭据无效）→ 记一次登录失败；达到阈值即进入冷却。
//
// 口径说明：403（已登录但无权限，如 requireAdmin 拒绝）不计入「登录失败」，
// 避免正常越权尝试与账号安全限流相互污染；网关在到达应用前直接拒绝的请求
// 不在本层可见，由网关侧承担。
func authFailThrottle(l *security.SlidingWindowLimiter, onReject func(c *gin.Context, key string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := authFailKey(c)
		if l.InCooldown(key) || l.Exceeded(key) {
			onReject(c, key)
			rateLimited(c, "鉴权失败次数过多，请 15 分钟后重试")
			c.Abort()
			return
		}
		c.Next()
		if st := c.Writer.Status(); st == http.StatusUnauthorized {
			if l.Fail(key) {
				logger.Warn("ratelimit", "鉴权失败达阈值，进入冷却: key=%v cooldown=%v", key, l.CooldownUntil(key))
			}
		}
	}
}

// renderSubmitLimit 渲染提交限流（07 §4.5：30 次 / 分钟 / 用户）。
// 仅对成功受理（2xx）的提交计数，校验失败 / 权限拒绝不占用配额。
func renderSubmitLimit(l *security.SlidingWindowLimiter, onReject func(c *gin.Context, key string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := rateKey(c)
		if l.Exceeded(key) {
			onReject(c, key)
			rateLimited(c, "渲染提交过于频繁，请稍后重试")
			c.Abort()
			return
		}
		c.Next()
		if st := c.Writer.Status(); st >= 200 && st < 300 {
			l.Record(key)
		}
	}
}

// ===== key 取值 =====

// authFailKey 鉴权失败限流的 key：来源 IP（07 §4.5 口径为 10 次/5min/IP）。
// 同 IP 的不同账号合并计量，避免攻击者轮换用户名绕过。
func authFailKey(c *gin.Context) string {
	if ip := clientIP(c); ip != "" {
		return "ip:" + ip
	}
	return "ip:unknown"
}

// rateKey 用户维度限流的 key：网关 UID → 用户名 → 来源 IP → anonymous（07 §4.5）。
func rateKey(c *gin.Context) string {
	u := security.GetGatewayUser(c)
	if u.UID != "" {
		return "uid:" + u.UID
	}
	if u.Username != "" {
		return "user:" + u.Username
	}
	if ip := clientIP(c); ip != "" {
		return "ip:" + ip
	}
	return "anonymous"
}

// clientIP 取来源 IP（剥端口）；解析失败回落原始 RemoteAddr。
func clientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	addr := c.Request.RemoteAddr
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}
