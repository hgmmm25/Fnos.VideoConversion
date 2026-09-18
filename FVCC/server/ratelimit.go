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
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"fvcc/logger"
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

	// 限流器内存约束
	rlMaxKeys = 4096 // 单限流器保留的 key 上限（超出触发空闲回收）
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

// ===== 滑动窗口限流器（07 §4.5）=====

// slidingWindowLimiter 按 key 独立的滑动窗口计数限流器（now 可注入，便于单测推进时间）。
// cooldown > 0 时，窗口内达阈值即进入冷却，冷却期内该 key 一律拒绝（07 §4.5 登录失败）。
type slidingWindowLimiter struct {
	mu       sync.Mutex
	events   map[string][]time.Time
	blocked  map[string]time.Time // key → 冷却截止时刻
	limit    int
	window   time.Duration
	cooldown time.Duration
	now      func() time.Time
}

func newSlidingWindowLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &slidingWindowLimiter{
		events:  map[string][]time.Time{},
		blocked: map[string]time.Time{},
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// newCooldownLimiter 构造「达阈值即冷却」的限流器（如 07 §4.5 登录失败：10 次/5min → 冷却 15min）。
func newCooldownLimiter(limit int, window, cooldown time.Duration) *slidingWindowLimiter {
	l := newSlidingWindowLimiter(limit, window)
	l.cooldown = cooldown
	return l
}

// exceeded 判断 key 在窗口内是否已达阈值（只读，不记录）。
func (l *slidingWindowLimiter) exceeded(key string) bool {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, t)) >= l.limit
}

// record 记录一次事件（超限后仍记录，保证窗口滚动期间持续受限）。
func (l *slidingWindowLimiter) record(key string) {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.events[key]; !ok && len(l.events) >= rlMaxKeys {
		l.evictLocked(t)
	}
	l.events[key] = append(l.pruneLocked(key, t), t)
}

// allow 记录一次事件并回报是否未超限（限流判定为「先记录后判定」，用于消费型配额）。
func (l *slidingWindowLimiter) allow(key string) bool {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.events[key]; !ok && len(l.events) >= rlMaxKeys {
		l.evictLocked(t)
	}
	kept := l.pruneLocked(key, t)
	if len(kept) >= l.limit {
		l.events[key] = kept
		return false
	}
	l.events[key] = append(kept, t)
	return true
}

// fail 记录一次鉴权失败；若窗口内达到阈值则进入冷却，返回是否「本次刚进入冷却」。
func (l *slidingWindowLimiter) fail(key string) bool {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.events[key]; !ok && len(l.events) >= rlMaxKeys {
		l.evictLocked(t)
	}
	kept := append(l.pruneLocked(key, t), t)
	l.events[key] = kept
	if l.cooldown <= 0 || len(kept) < l.limit {
		return false
	}
	if _, blocked := l.blocked[key]; blocked {
		return false
	}
	l.blocked[key] = t.Add(l.cooldown)
	return true
}

// inCooldown 判断 key 是否处于冷却期；冷却到期自动解封。
func (l *slidingWindowLimiter) inCooldown(key string) bool {
	if l.cooldown <= 0 {
		return false
	}
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	until, ok := l.blocked[key]
	if !ok {
		return false
	}
	if !t.Before(until) {
		delete(l.blocked, key)
		return false
	}
	return true
}

// cooldownUntil 返回冷却截止时刻（无冷却则零值；诊断 / 单测用）。
func (l *slidingWindowLimiter) cooldownUntil(key string) time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.blocked[key]
}

// pruneLocked 丢弃窗口外的历史事件；key 不存在时不创建条目；需持有 l.mu。
func (l *slidingWindowLimiter) pruneLocked(key string, now time.Time) []time.Time {
	ev := l.events[key]
	if len(ev) == 0 {
		return ev
	}
	cut := now.Add(-l.window)
	kept := ev[:0]
	for _, t := range ev {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	l.events[key] = kept
	return kept
}

// evictLocked 回收窗口内无事件的 key；需持有 l.mu。
func (l *slidingWindowLimiter) evictLocked(now time.Time) {
	cut := now.Add(-l.window)
	for k, ev := range l.events {
		if len(ev) == 0 || ev[len(ev)-1].Before(cut) {
			delete(l.events, k)
		}
	}
}

// count 返回 key 当前窗口内的事件数（诊断 / 单测用）。
func (l *slidingWindowLimiter) count(key string) int {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, t))
}

// ===== 限流器实例 =====

var (
	// authFailLimiter 鉴权失败限流（07 §4.5：10 次 / 5 分钟 / IP → 冷却 15 分钟）
	authFailLimiter = newCooldownLimiter(authFailMax, authFailWindow, authFailCooldown)
	// renderSubmitLimiter 渲染提交限流（07 §4.5：30 次 / 分钟 / 用户）
	renderSubmitLimiter = newSlidingWindowLimiter(renderSubmitMax, renderWindow)
)

// ===== 中间件 =====

// auditReject 生成限流拒绝回调：写审计（Result=denied）+ WARN 日志。
func (h *Handlers) auditReject(action string) func(c *gin.Context, key string) {
	return func(c *gin.Context, key string) {
		actor := edlActor(c)
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
func authFailThrottle(l *slidingWindowLimiter, onReject func(c *gin.Context, key string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := authFailKey(c)
		if l.inCooldown(key) || l.exceeded(key) {
			onReject(c, key)
			rateLimited(c, "鉴权失败次数过多，请 15 分钟后重试")
			c.Abort()
			return
		}
		c.Next()
		if st := c.Writer.Status(); st == http.StatusUnauthorized {
			if l.fail(key) {
				logger.Warn("ratelimit", "鉴权失败达阈值，进入冷却: key=%v cooldown=%v", key, l.cooldown)
			}
		}
	}
}

// renderSubmitLimit 渲染提交限流（07 §4.5：30 次 / 分钟 / 用户）。
// 仅对成功受理（2xx）的提交计数，校验失败 / 权限拒绝不占用配额。
func renderSubmitLimit(l *slidingWindowLimiter, onReject func(c *gin.Context, key string)) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := rateKey(c)
		if l.exceeded(key) {
			onReject(c, key)
			rateLimited(c, "渲染提交过于频繁，请稍后重试")
			c.Abort()
			return
		}
		c.Next()
		if st := c.Writer.Status(); st >= 200 && st < 300 {
			l.record(key)
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
	u := getGatewayUser(c)
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
