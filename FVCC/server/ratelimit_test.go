package main

// D-04 FVCC 侧验收测试（07 §4.4 / §4.5 / §7）：
//  1) 安全响应头全链路生效（含 404 / SPA 兜底等异常路径）；
//  2) 鉴权失败限流：401 累计达 10 次/5 分钟/IP → 第 11 次 429（E_RATE_LIMITED）并冷却 15 分钟；
//  3) 渲染提交限流：30 次/分钟/用户，第 31 次 429；仅 2xx 计数；
//  4) 限流拒绝写审计（Result=denied）；
//  5) 限流器窗口滚动恢复、空闲回收、key 维度隔离、冷却到期解封；
//  6) 浏览器 WS 连接上限（07 §4.5：浏览器 ≤ 5 条）。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// resetRateLimiters 每个用例前重置全局限流器，避免用例间相互污染。
func resetRateLimiters() {
	authFailLimiter = newCooldownLimiter(authFailMax, authFailWindow, authFailCooldown)
	renderSubmitLimiter = newSlidingWindowLimiter(renderSubmitMax, renderWindow)
}

// d04Router 构造带 D-04 中间件的最小路由（/api/info 正常 200，/api/protected 返回 401）。
func d04Router(t *testing.T) (*gin.Engine, *Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	resetRateLimiters()

	st := NewStore(t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}
	h := &Handlers{store: st}

	r := gin.New()
	r.Use(securityHeaders())
	r.Use(authFailThrottle(authFailLimiter, h.auditReject("ratelimit.auth")))
	r.Use(gatewayUser())
	g := r.Group(gwPrefix)
	g.GET("/api/info", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	g.GET("/api/protected", func(c *gin.Context) {
		c.JSON(http.StatusUnauthorized, gin.H{"ok": false, "code": "E_UNAUTHORIZED"})
	})
	g.POST("/api/edl/projects/:id/render",
		renderSubmitLimit(renderSubmitLimiter, h.auditReject("ratelimit.render")),
		func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return r, st
}

func d04Request(r *gin.Engine, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	return d04RequestFrom(r, method, path, hdr, "")
}

// d04RequestFrom 允许指定来源 IP（限流按 IP 分桶的用例需要）。
func d04RequestFrom(r *gin.Engine, method, path string, hdr map[string]string, remote string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, gwPrefix+path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if remote != "" {
		req.RemoteAddr = remote
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ===== 1. 安全响应头 =====

func TestD04FVCCSecurityHeaders(t *testing.T) {
	r, _ := d04Router(t)

	// 正常响应
	w := d04Request(r, http.MethodGet, "/api/info", nil)
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "SAMEORIGIN",
	} {
		if got := w.Header().Get(k); got != want {
			t.Fatalf("安全头 %s = %q，期望 %q", k, got, want)
		}
	}

	// 未认证响应同样带全
	if w := d04Request(r, http.MethodGet, "/api/protected", nil); w.Header().Get("X-Frame-Options") != "SAMEORIGIN" {
		t.Fatalf("401 响应缺少安全头")
	}

	// 未匹配路由（NoRoute → 404）同样带全（securityHeaders 早于业务中间件）
	if w := d04Request(r, http.MethodGet, "/api/not-exist", nil); w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("404 响应缺少安全头")
	}
}

// ===== 2. 鉴权失败限流 =====

func TestD04FVCCAuthFailThrottle(t *testing.T) {
	r, st := d04Router(t)
	hdr := b09Headers("u_bad", "bad", "")

	// 前 10 次 401 均应正常放行（未触发限流）
	for i := 0; i < authFailMax; i++ {
		if w := d04Request(r, http.MethodGet, "/api/protected", hdr); w.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次未认证请求应 401，实际 %d", i+1, w.Code)
		}
	}
	// 第 11 次达到阈值 → 429 + 统一失败契约
	w := d04Request(r, http.MethodGet, "/api/protected", hdr)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超阈值应 429，实际 %d %s", w.Code, w.Body.String())
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("429 响应非 JSON: %s", w.Body.String())
	}
	if body["code"] != errCodeRateLimited || body["ok"] != false {
		t.Fatalf("429 响应未对齐统一失败契约: %s", w.Body.String())
	}

	// 审计留痕：Result=denied，Action=ratelimit.auth
	if len(st.ListAudit(200, "ratelimit.auth")) == 0 {
		t.Fatalf("限流拒绝未写审计")
	}

	// 其他来源 IP 不受影响（07 §4.5 按 IP 分桶）
	otherIP := b09Headers("u_other", "other", "")
	if w := d04RequestFrom(r, http.MethodGet, "/api/protected", otherIP, "198.51.100.7:9000"); w.Code != http.StatusUnauthorized {
		t.Fatalf("其他来源 IP 不应被连带限流，实际 %d", w.Code)
	}
	// 同一 IP 换用户名同样被拒（防止轮换用户名绕过阈值）
	if w := d04Request(r, http.MethodGet, "/api/protected", otherIP); w.Code != http.StatusTooManyRequests {
		t.Fatalf("同 IP 换账号应仍受限，实际 %d", w.Code)
	}
	// 冷却期内该 IP 的任意请求一律 429
	if w := d04Request(r, http.MethodGet, "/api/info", hdr); w.Code != http.StatusTooManyRequests {
		t.Fatalf("冷却期内该 IP 的请求应 429，实际 %d", w.Code)
	}

	// 冷却 15 分钟后解封（推进限流器时钟）
	authFailLimiter.now = func() time.Time { return time.Now().Add(authFailCooldown + time.Minute) }
	if w := d04Request(r, http.MethodGet, "/api/protected", hdr); w.Code != http.StatusUnauthorized {
		t.Fatalf("冷却到期后应恢复放行（401 而非 429），实际 %d", w.Code)
	}
}

// ===== 3. 渲染提交限流 =====

func TestD04FVCCRenderSubmitThrottle(t *testing.T) {
	r, st := d04Router(t)
	hdr := b09Headers("u_editor", "editor", "admin")

	for i := 0; i < renderSubmitMax; i++ {
		if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render", hdr); w.Code != http.StatusOK {
			t.Fatalf("第 %d 次提交应 200，实际 %d %s", i+1, w.Code, w.Body.String())
		}
	}
	if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render", hdr); w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 %d 次提交应 429，实际 %d", renderSubmitMax+1, w.Code)
	}

	// 另一用户独立额度
	if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render", b09Headers("u_other", "other", "admin")); w.Code != http.StatusOK {
		t.Fatalf("其他用户额度应独立，实际 %d", w.Code)
	}

	if len(st.ListAudit(200, "ratelimit.render")) == 0 {
		t.Fatalf("渲染限流拒绝未写审计")
	}
}

// 仅 2xx 计入渲染配额：失败提交不占用额度
func TestD04FVCCRenderLimitCountsSuccessOnly(t *testing.T) {
	resetRateLimiters()
	gin.SetMode(gin.TestMode)
	l := newSlidingWindowLimiter(2, time.Minute)
	h := &Handlers{}
	r := gin.New()
	r.Use(gatewayUser())
	r.POST(gwPrefix+"/api/edl/projects/:id/render",
		renderSubmitLimit(l, h.auditReject("ratelimit.render")),
		func(c *gin.Context) {
			if c.Query("fail") == "1" {
				c.JSON(http.StatusBadRequest, gin.H{"ok": false})
				return
			}
			c.JSON(http.StatusOK, gin.H{"ok": true})
		})

	for i := 0; i < 5; i++ {
		if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render?fail=1", nil); w.Code != http.StatusBadRequest {
			t.Fatalf("失败提交应 400，实际 %d", w.Code)
		}
	}
	if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render", nil); w.Code != http.StatusOK {
		t.Fatalf("失败提交不应占用配额，实际 %d", w.Code)
	}
	if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render", nil); w.Code != http.StatusOK {
		t.Fatalf("第 2 次成功提交应 200，实际 %d", w.Code)
	}
	if w := d04Request(r, http.MethodPost, "/api/edl/projects/p1/render", nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次成功提交应 429，实际 %d", w.Code)
	}
}

// ===== 4. 限流器单元行为 =====

func TestD04FVCCTokenWindowRolling(t *testing.T) {
	l := newSlidingWindowLimiter(3, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.allow("k") {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if l.allow("k") {
		t.Fatalf("超限应拒绝")
	}
	// 窗口滚动：窗口内 3 次事件全部滑出后恢复放行
	now = now.Add(61 * time.Second)
	if !l.allow("k") {
		t.Fatalf("窗口滚动后应恢复放行")
	}
	// key 隔离
	if !l.allow("k2") {
		t.Fatalf("不同 key 额度应独立")
	}
	if l.count("k2") != 1 {
		t.Fatalf("k2 计数应为 1，实际 %d", l.count("k2"))
	}
}

func TestD04FVCCLimiterEviction(t *testing.T) {
	l := newSlidingWindowLimiter(30, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < rlMaxKeys; i++ {
		l.events["k"+strconv.Itoa(i)] = []time.Time{now.Add(-2 * time.Minute)}
	}
	if l.exceeded("new-key") {
		t.Fatalf("新 key 不应超限")
	}
	l.record("new-key")
	if len(l.events) != 1 {
		t.Fatalf("空闲 key 应被回收，剩余 %d", len(l.events))
	}
}

func TestD04FVCCRateKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 网关身份：UID 优先
	c1, _ := gin.CreateTestContext(httptest.NewRecorder())
	c1.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	c1.Request.Header.Set("X-Trim-User-Id", "u1")
	c1.Request.Header.Set("X-Trim-User-Name", "alice")
	c1.Request.RemoteAddr = "10.0.0.9:1234"
	if k := rateKey(c1); k != "uid:u1" {
		t.Fatalf("rateKey 应优先 UID，实际 %s", k)
	}
	if k := authFailKey(c1); k != "ip:10.0.0.9" {
		t.Fatalf("authFailKey 应为来源 IP，实际 %s", k)
	}

	// 无网关身份（独立模式）：回落 IP
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = httptest.NewRequest(http.MethodGet, "/x", nil)
	c2.Request.RemoteAddr = "192.168.1.7:5555"
	if k := rateKey(c2); k != "ip:192.168.1.7" {
		t.Fatalf("独立模式应回落 IP，实际 %s", k)
	}
	if k := authFailKey(c2); k != "ip:192.168.1.7" {
		t.Fatalf("authFailKey 应剥离端口，实际 %s", k)
	}
}

// 鉴权失败冷却：达阈值进入 15 分钟冷却，冷却到期后解封。
func TestD04FVCCAuthFailCooldown(t *testing.T) {
	l := newCooldownLimiter(3, 5*time.Minute, authFailCooldown)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if l.inCooldown("ip:1.1.1.1") {
			t.Fatalf("第 %d 次失败不应已处于冷却", i+1)
		}
		l.fail("ip:1.1.1.1")
	}
	if !l.inCooldown("ip:1.1.1.1") {
		t.Fatalf("达阈值应进入冷却")
	}
	if got := l.cooldownUntil("ip:1.1.1.1"); !got.Equal(now.Add(authFailCooldown)) {
		t.Fatalf("冷却截止时刻应为 now+15min，实际 %v", got)
	}
	// 冷却期内即使事件窗口已滑出，仍保持封控
	now = now.Add(6 * time.Minute)
	if !l.inCooldown("ip:1.1.1.1") {
		t.Fatalf("窗口滑出但冷却未满，仍应封控")
	}
	// 冷却到期解封，且窗口事件已过期 → 完全恢复
	now = now.Add(authFailCooldown)
	if l.inCooldown("ip:1.1.1.1") {
		t.Fatalf("冷却到期应解封")
	}
	if l.exceeded("ip:1.1.1.1") {
		t.Fatalf("冷却到期后窗口内不应残留计数")
	}
}

// ===== 6. 浏览器 WS 连接上限 =====

func TestD04FVCCWSConnRegistry(t *testing.T) {
	reg := newWSConnRegistry()
	key := "uid:u1"
	for i := 0; i < wsBrowserMaxConns; i++ {
		if !reg.acquire(key) {
			t.Fatalf("第 %d 条连接应放行", i+1)
		}
	}
	if reg.acquire(key) {
		t.Fatalf("超出 %d 条的连接应被拒绝", wsBrowserMaxConns)
	}
	// 其他浏览器（key）额度独立
	if !reg.acquire("ip:10.0.0.2") {
		t.Fatalf("不同 key 额度应独立")
	}
	// 释放后可重新占用；计数归零时条目被清理
	reg.release(key)
	if !reg.acquire(key) {
		t.Fatalf("释放后应可重新占用")
	}
	if reg.count(key) != wsBrowserMaxConns {
		t.Fatalf("释放再占用后计数应为 %d，实际 %d", wsBrowserMaxConns, reg.count(key))
	}
	for i := 0; i < wsBrowserMaxConns; i++ {
		reg.release(key)
	}
	if reg.count(key) != 0 || len(reg.conns) != 1 {
		t.Fatalf("全部释放后应清理该 key，剩余条目 %d", len(reg.conns))
	}
}

func TestD04FVCCHandleWSOverLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewHub()
	reg := h.connRegistry()
	for i := 0; i < wsBrowserMaxConns; i++ {
		if !reg.acquire("ip:192.0.2.1") {
			t.Fatalf("预置第 %d 条连接失败", i+1)
		}
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, gwPrefix+"/ws", nil)
	c.Request.RemoteAddr = "192.0.2.1:1234"
	h.HandleWS(c)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超限的 WS 握手应 429，实际 %d", w.Code)
	}
	var body map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("429 响应非 JSON: %s", w.Body.String())
	}
	if body["code"] != errCodeRateLimited {
		t.Fatalf("429 响应错误码应为 %s，实际 %v", errCodeRateLimited, body["code"])
	}
}
