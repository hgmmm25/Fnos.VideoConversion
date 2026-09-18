package server

// 安全响应头与通用接口限流（07 §4.4 / §4.5，D-04）
//
// 落地范围：
//  1. 安全响应头（07 §4.4）：X-Content-Type-Options: nosniff、Referrer-Policy: no-referrer、
//     X-Frame-Options: SAMEORIGIN —— 对 HTTP 与 WS 两条监听链路统一生效；
//  2. 通用接口限流（07 §4.5）：按来源 IP 的令牌桶，速率取 config.qps_limit（默认 100/s，
//     突发额度同速率）；超限返回 429 + Retry-After + E_RATE_LIMITED；
//  3. WS 单节点并发上限（07 §4.5：≤3 条）：超限拒绝升级并返回 429 + E_TOO_MANY_CONNECTIONS；
//  4. 限流拒绝均记 WARN 级日志（含来源 IP 与路径，不含任何凭据/明文）。
//
// 说明：审计表（07 §7）由 NAS 调度端（FVCC）持有，FVCS 侧不落本地审计表，
// 仅以 logger 记录限流事件，供 07 §7 的日志留存策略覆盖。

import (
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/config"
	"Fnos.VC_Service/pkg/logger"
)

const (
	// errCodeRateLimited 通用接口限流拒绝码（07 §4.5）。
	errCodeRateLimited = "E_RATE_LIMITED"
	// errCodeTooManyConns WS 单节点并发超限拒绝码（07 §4.5）。
	errCodeTooManyConns = "E_TOO_MANY_CONNECTIONS"

	// defaultWSConns 默认 WS 单节点并发上限（07 §4.5：单节点 ≤3 条）。
	defaultWSConns = 3

	// 限流器内部约束
	maxLimiterKeys   = 4096             // 单次运行内保留的 IP 桶上限（防内存膨胀）
	limiterKeyTTL    = 10 * time.Minute // 空闲桶回收阈值
	minRetryAfterSec = 1                // Retry-After 下限（秒）
)

// ===== 安全响应头（07 §4.4）=====

// securityHeadersHandler 为所有响应追加 07 §4.4 要求的安全头。
// 显式配置 disable_security_headers=true 时不追加（默认开启）。
func securityHeadersHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg := config.Get(); cfg != nil && !cfg.DisableSecurityHeaders {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("X-Frame-Options", "SAMEORIGIN")
		}
		next.ServeHTTP(w, r)
	})
}

// ===== 令牌桶限流（07 §4.5）=====

// tokenBucket 单个来源的令牌桶；now 由限流器注入以便单测推进时间。
type tokenBucket struct {
	tokens float64
	last   time.Time
}

// ipRateLimiter 按 key（来源 IP）分桶的令牌桶限流器。
type ipRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // 每秒补充令牌数
	burst   float64 // 桶容量（突发额度）
	now     func() time.Time
}

func newIPRateLimiter(ratePerSec, burst int) *ipRateLimiter {
	if ratePerSec <= 0 {
		ratePerSec = 100
	}
	if burst <= 0 {
		burst = ratePerSec
	}
	return &ipRateLimiter{
		buckets: map[string]*tokenBucket{},
		rate:    float64(ratePerSec),
		burst:   float64(burst),
		now:     time.Now,
	}
}

// allow 尝试取用 1 个令牌；令牌不足返回 false。
func (l *ipRateLimiter) allow(key string) bool {
	if key == "" {
		key = "unknown"
	}
	t := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxLimiterKeys {
			l.evictLocked(t)
		}
		b = &tokenBucket{tokens: l.burst, last: t}
		l.buckets[key] = b
	}
	if elapsed := t.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
		b.last = t
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// retryAfter 返回建议重试间隔（令牌补足 1 个所需时间，向下取整到秒，且 ≥1s）。
func (l *ipRateLimiter) retryAfter() time.Duration {
	d := time.Duration(float64(time.Second) / l.rate)
	if d < minRetryAfterSec*time.Second {
		d = minRetryAfterSec * time.Second
	}
	return d
}

// evictLocked 回收空闲桶；需持有 l.mu。
func (l *ipRateLimiter) evictLocked(now time.Time) {
	for k, b := range l.buckets {
		if now.Sub(b.last) > limiterKeyTTL {
			delete(l.buckets, k)
		}
	}
}

// rateLimitMiddleware 通用接口限流：超限 → 429 + Retry-After + E_RATE_LIMITED（07 §4.5）。
func rateLimitMiddleware(l *ipRateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if l != nil {
				if ip := clientIP(r); !l.allow(ip) {
					retry := l.retryAfter()
					w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())))
					logger.Warn("server", "Rate limited: %s %s from %s", r.Method, r.URL.Path, ip)
					writeJSONError(w, http.StatusTooManyRequests, errCodeRateLimited, "请求过于频繁，请稍后重试")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// clientIP 取来源 IP（剥端口）；解析失败时回落原始 RemoteAddr。
func clientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeJSONError 输出统一失败响应（03 §4.1：{ok:false, code, msg}）。
func writeJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "code": code, "msg": msg})
}

// ===== 鉴权失败限流（07 §4.5：10 次/5min/IP → 429 + 冷却 15min）=====

const (
	authFailMax      = 10               // 登录/鉴权失败次数上限
	authFailWindow   = 5 * time.Minute  // 观测窗口
	authFailCooldown = 15 * time.Minute // 超限冷却时长
)

// authFailGuard 按来源 IP 统计鉴权失败次数；窗口内达阈值即进入冷却期。
// 与令牌桶（ipRateLimiter）互补：前者防暴力猜 Key，后者防整体流量洪峰。
type authFailGuard struct {
	mu      sync.Mutex
	fails   map[string][]time.Time
	blocked map[string]time.Time
	now     func() time.Time
}

func newAuthFailGuard() *authFailGuard {
	return &authFailGuard{
		fails:   map[string][]time.Time{},
		blocked: map[string]time.Time{},
		now:     time.Now,
	}
}

// fail 记录一次鉴权失败；窗口内达阈值则进入冷却，返回是否「本次刚进入冷却」。
func (g *authFailGuard) fail(key string) bool {
	t := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	cut := t.Add(-authFailWindow)
	kept := g.fails[key][:0]
	for _, ts := range g.fails[key] {
		if ts.After(cut) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, t)
	g.fails[key] = kept
	if len(kept) < authFailMax {
		return false
	}
	if _, blocked := g.blocked[key]; blocked {
		return false
	}
	g.blocked[key] = t.Add(authFailCooldown)
	return true
}

// inCooldown 判断 key 是否处于冷却期；冷却到期自动解封并清理窗口计数。
func (g *authFailGuard) inCooldown(key string) bool {
	return g.cooldownRemaining(key) > 0
}

// cooldownRemaining 返回剩余冷却时长；0 表示未封控。
func (g *authFailGuard) cooldownRemaining(key string) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.blocked[key]
	if !ok {
		return 0
	}
	if left := until.Sub(g.now()); left > 0 {
		return left
	}
	delete(g.blocked, key)
	delete(g.fails, key) // 冷却期满重置计数，避免解封瞬间再次触发
	return 0
}

// reset 清空全部状态（单测用）。
func (g *authFailGuard) reset() {
	g.mu.Lock()
	g.fails = map[string][]time.Time{}
	g.blocked = map[string]time.Time{}
	g.mu.Unlock()
}

// authFails 鉴权失败守卫实例（upload / download / WS 鉴权共用）。
var authFails = newAuthFailGuard()

// authFailGate 前置校验：来源 IP 处于冷却期 → 429 + Retry-After 并返回 true（调用方直接 return）。
func authFailGate(w http.ResponseWriter, r *http.Request) bool {
	ip := clientIP(r)
	left := authFails.cooldownRemaining(ip)
	if left <= 0 {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
	logger.Warn("server", "Auth-fail cooldown active: %s %s from %s (left=%v)", r.Method, r.URL.Path, ip, left)
	auditSecurity(ip, actionRateLimitAuth, r.URL.Path, errCodeRateLimited, "冷却中")
	writeJSONError(w, http.StatusTooManyRequests, errCodeRateLimited, "鉴权失败次数过多，请 15 分钟后重试")
	return true
}

// authFailNote 记录一次鉴权失败并写审计；返回是否刚进入冷却（调用方据此记 WARN / 断开连接）。
func authFailNote(ip, target string) bool {
	auditSecurity(ip, actionAuthFail, target, "denied", "")
	if authFails.fail(ip) {
		logger.Warn("server", "鉴权失败达阈值(%d 次/%v)，来源 %s 进入冷却 %v",
			authFailMax, authFailWindow, ip, authFailCooldown)
		return true
	}
	return false
}

// ===== WS 连接数上限（07 §4.5）=====

// wsConnLimiter 节点级 WS 并发计数（升级前校验，避免超限连接进入会话）。
type wsConnLimiter struct {
	mu     sync.Mutex
	max    int
	active int
}

func newWSConnLimiter(max int) *wsConnLimiter {
	if max <= 0 {
		max = defaultWSConns
	}
	return &wsConnLimiter{max: max}
}

// setMax 更新上限（Init 时按 config.max_ws_conns 设定）。
func (l *wsConnLimiter) setMax(max int) {
	if max <= 0 {
		max = defaultWSConns
	}
	l.mu.Lock()
	l.max = max
	l.mu.Unlock()
}

// acquire 占用一个连接名额；已达上限返回 false。
func (l *wsConnLimiter) acquire() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.active >= l.max {
		return false
	}
	l.active++
	return true
}

// release 释放一个连接名额。
func (l *wsConnLimiter) release() {
	l.mu.Lock()
	if l.active > 0 {
		l.active--
	}
	l.mu.Unlock()
}

// activeCount 当前占用的连接数（单测/诊断用）。
func (l *wsConnLimiter) activeCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.active
}

// maxCount 当前配置的上限（日志/诊断用）。
func (l *wsConnLimiter) maxCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.max
}

// nodeWSConns 单节点 WS 连接上限实例（Init 时按配置设定）。
var nodeWSConns = newWSConnLimiter(defaultWSConns)

// setWSConnLimit 设定节点级 WS 连接上限（Init / 单测使用）。
func setWSConnLimit(max int) {
	nodeWSConns.setMax(max)
}
