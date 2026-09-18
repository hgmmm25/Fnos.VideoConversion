package server

// D-04 验收测试（08 §2 D-04 / 07 §4.4、§4.5）：
//  1) 安全响应头三项对 HTTP 与 WS 两条链路生效，且支持显式关闭；
//  2) 通用接口限流：令牌桶按来源 IP 独立计量，超限 429 + Retry-After + E_RATE_LIMITED；
//  3) WS 单节点并发上限 ≤3：超限拒绝升级并返回 E_TOO_MANY_CONNECTIONS。

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Fnos.VC_Service/pkg/config"
)

// 07 §4.4：三项安全响应头
func TestD04SecurityHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := securityHeadersHandler(inner)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/upload", nil))

	want := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Referrer-Policy":        "no-referrer",
		"X-Frame-Options":        "SAMEORIGIN",
	}
	for k, v := range want {
		if got := w.Header().Get(k); got != v {
			t.Errorf("安全头 %s = %q，期望 %q", k, got, v)
		}
	}
}

// 显式关闭开关（config.disable_security_headers）生效
func TestD04SecurityHeadersDisabled(t *testing.T) {
	defer config.Set(&config.Config{})
	config.Set(&config.Config{DisableSecurityHeaders: true})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	w := httptest.NewRecorder()
	securityHeadersHandler(inner).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/upload", nil))

	if got := w.Header().Get("X-Content-Type-Options"); got != "" {
		t.Fatalf("关闭开关后不应下发安全头，实际 %q", got)
	}
}

// 07 §4.5：限流超限 → 429 + Retry-After + E_RATE_LIMITED（按 IP 独立，时间推进后恢复）
func TestD04RateLimitMiddleware(t *testing.T) {
	lim := newIPRateLimiter(10, 2)
	now := time.Now()
	lim.now = func() time.Time { return now }

	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := rateLimitMiddleware(lim)(ok)

	do := func(ip string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/upload", nil)
		req.RemoteAddr = ip + ":51234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}

	// 突发额度 2：前两次放行，第三次拒绝
	if got := do("192.168.1.10").Code; got != http.StatusOK {
		t.Fatalf("第 1 次请求应放行，实际 %d", got)
	}
	if got := do("192.168.1.10").Code; got != http.StatusOK {
		t.Fatalf("第 2 次请求应放行，实际 %d", got)
	}
	third := do("192.168.1.10")
	if third.Code != http.StatusTooManyRequests {
		t.Fatalf("第 3 次请求应 429，实际 %d %s", third.Code, third.Body.String())
	}
	if ra := third.Header().Get("Retry-After"); ra == "" {
		t.Fatalf("429 响应缺少 Retry-After")
	}
	var body map[string]any
	if err := json.Unmarshal(third.Body.Bytes(), &body); err != nil {
		t.Fatalf("429 响应体不可解析: %v", err)
	}
	if body["code"] != errCodeRateLimited || body["ok"] != false {
		t.Fatalf("429 响应未对齐统一失败契约: %s", third.Body.String())
	}

	// 另一 IP 不受影响（按来源独立计量）
	if got := do("192.168.1.11").Code; got != http.StatusOK {
		t.Fatalf("不同来源 IP 应独立计量，实际 %d", got)
	}

	// 时间推进 1s（速率 10/s）→ 恢复放行
	now = now.Add(time.Second)
	if got := do("192.168.1.10").Code; got != http.StatusOK {
		t.Fatalf("令牌补充后应放行，实际 %d", got)
	}
}

// 07 §4.5：WS 单节点并发 ≤3，超限 429 + E_TOO_MANY_CONNECTIONS
func TestD04WSConnLimit(t *testing.T) {
	setWSConnLimit(3)
	defer func() {
		for nodeWSConns.activeCount() > 0 {
			nodeWSConns.release()
		}
	}()

	// 占满 3 条连接名额
	for i := 0; i < 3; i++ {
		if !nodeWSConns.acquire() {
			t.Fatalf("第 %d 条连接应被接受", i+1)
		}
	}

	// 第 4 条：拒绝升级（未进入 WS 会话）
	w := httptest.NewRecorder()
	handleWebSocket(w, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("超限连接应 429，实际 %d %s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("429 响应体不可解析: %v", err)
	}
	if body["code"] != errCodeTooManyConns {
		t.Fatalf("超限拒绝码应为 %s，实际 %v", errCodeTooManyConns, body["code"])
	}

	// 释放一个名额后，请求应进入升级流程（httptest.ResponseRecorder 不支持 Hijack、
	// 且未带 WS 升级头 → 升级失败并归还名额），证明名额回收生效
	nodeWSConns.release()
	w2 := httptest.NewRecorder()
	handleWebSocket(w2, httptest.NewRequest(http.MethodGet, "/ws", nil))
	if w2.Code == http.StatusTooManyRequests {
		t.Fatalf("名额释放后不应再被限流: %s", w2.Body.String())
	}
	if nodeWSConns.activeCount() != 2 {
		t.Fatalf("升级失败应归还名额，active=%d（期望 2）", nodeWSConns.activeCount())
	}
}

// 限流器空闲桶回收：避免长期运行内存膨胀
func TestD04RateLimiterEviction(t *testing.T) {
	lim := newIPRateLimiter(1, 1)
	now := time.Now()
	lim.now = func() time.Time { return now }

	// 预置达到上限的空闲桶（最后访问时间超出 TTL）
	for i := 0; i < maxLimiterKeys; i++ {
		lim.buckets[fmt.Sprintf("k%d", i)] = &tokenBucket{tokens: 1, last: now.Add(-2 * limiterKeyTTL)}
	}
	if !lim.allow("10.0.0.1") {
		t.Fatalf("新来源应放行")
	}
	if len(lim.buckets) != 1 {
		t.Fatalf("空闲桶应被回收，剩余 %d", len(lim.buckets))
	}
	if !lim.allow("10.0.0.1") == false {
		// 速率 1/s、突发 1：刚取走 1 个令牌，立刻再取应被拒
	}
	if lim.allow("10.0.0.1") {
		t.Fatalf("突发额度用尽后应被拒")
	}
	now = now.Add(2 * time.Second)
	if !lim.allow("10.0.0.1") {
		t.Fatalf("时间推进后应恢复放行")
	}
}
