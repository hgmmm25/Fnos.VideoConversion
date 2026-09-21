package server

// D-04 验收测试（续）：鉴权失败限流（08 §2 D-04 / 07 §4.5）
//  1) 同一 IP 5 分钟内失败 10 次 → 进入 15 分钟冷却，且窗口滑出后仍封控；
//  2) 不同 IP 独立计量，冷却到期自动解封并清零计数；
//  3) 冷却期内 /upload、/download、/ws 一律 429 + Retry-After + E_RATE_LIMITED；
//  4) 鉴权失败在 download（缺 Key / Key 不符）路径上真实记账。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"Fnos.VC_Service/pkg/config"
)

func TestD04AuthFailGuardCooldown(t *testing.T) {
	g := newAuthFailGuard()
	now := time.Now()
	g.now = func() time.Time { return now }

	const ip = "10.1.1.1"
	for i := 0; i < authFailMax; i++ {
		if g.inCooldown(ip) {
			t.Fatalf("第 %d 次失败前不应处于冷却", i+1)
		}
		g.fail(ip)
	}
	if !g.inCooldown(ip) {
		t.Fatalf("窗口内失败 %d 次应进入冷却", authFailMax)
	}
	if left := g.cooldownRemaining(ip); left <= 0 || left > authFailCooldown {
		t.Fatalf("剩余冷却时长异常: %v", left)
	}

	// 观测窗口已滑出，但冷却未满 → 仍封控（防绕窗试探）
	now = now.Add(authFailWindow + time.Minute)
	if !g.inCooldown(ip) {
		t.Fatalf("冷却期内即使失败窗口滑出也应继续封控")
	}

	// 其他 IP 独立计量
	if g.inCooldown("10.1.1.2") {
		t.Fatalf("其他来源 IP 不应被连带封控")
	}

	// 冷却到期解封并清零计数
	now = now.Add(authFailCooldown)
	if g.inCooldown(ip) {
		t.Fatalf("冷却到期应自动解封")
	}
	if n := len(g.fails[ip]); n != 0 {
		t.Fatalf("解封时应清空失败窗口计数，实际 %d", n)
	}
}

func TestD04AuthFailCooldownBlocksEndpoints(t *testing.T) {
	defer authFails.reset()

	const blockedIP = "203.0.113.9"
	for i := 0; i < authFailMax; i++ {
		authFailNote(blockedIP, "/download")
	}
	if !authFails.inCooldown(blockedIP) {
		t.Fatalf("达到阈值后应进入冷却")
	}

	cases := []struct {
		name   string
		method string
		path   string
		h      http.HandlerFunc
	}{
		{"upload", http.MethodPost, "/upload", handleUpload},
		{"download", http.MethodGet, "/download", handleDownload},
		{"ws", http.MethodGet, "/ws", handleWebSocket},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		req.RemoteAddr = blockedIP + ":4444"
		w := httptest.NewRecorder()
		tc.h(w, req)

		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("%s：冷却期内应 429，实际 %d %s", tc.name, w.Code, w.Body.String())
		}
		if ra := w.Header().Get("Retry-After"); ra == "" {
			t.Fatalf("%s：429 响应缺少 Retry-After", tc.name)
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("%s：429 响应体不可解析: %v", tc.name, err)
		}
		if body["code"] != errCodeRateLimited || body["ok"] != false {
			t.Fatalf("%s：未对齐统一失败契约: %s", tc.name, w.Body.String())
		}
	}

	// 未被封控的 IP 仍可正常进入鉴权流程（缺 Key → 401）
	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	req.RemoteAddr = "203.0.113.10:4444"
	w := httptest.NewRecorder()
	handleDownload(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("其他 IP 应正常进入鉴权（401），实际 %d %s", w.Code, w.Body.String())
	}
}

func TestD04AuthFailCountedOnDownload(t *testing.T) {
	defer authFails.reset()
	defer config.Set(&config.Config{})
	config.Set(&config.Config{})

	const ip = "203.0.113.11"
	do := func(key string) int {
		req := httptest.NewRequest(http.MethodGet, "/download", nil)
		req.RemoteAddr = ip + ":5555"
		if key != "" {
			req.Header.Set("X-Auth-Key", key)
		}
		w := httptest.NewRecorder()
		handleDownload(w, req)
		return w.Code
	}

	if code := do("wrong-key"); code != http.StatusUnauthorized {
		t.Fatalf("Key 不符应 401，实际 %d", code)
	}
	if n := len(authFails.fails[ip]); n != 1 {
		t.Fatalf("Key 不符应记 1 次失败，实际 %d", n)
	}

	if code := do(""); code != http.StatusUnauthorized {
		t.Fatalf("缺 Key 应 401，实际 %d", code)
	}
	if n := len(authFails.fails[ip]); n != 2 {
		t.Fatalf("缺 Key 应累计至 2 次失败，实际 %d", n)
	}
	if authFails.inCooldown(ip) {
		t.Fatalf("未达阈值不应封控")
	}
}
