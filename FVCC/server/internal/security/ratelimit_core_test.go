package security

// ratelimit_core_test.go — P2-1 阶段 A：限流器核心单元行为测试。
// 由 main 包 ratelimit_test.go §4 随实现迁入（同包可见私有字段 now/events），
// 方法名对齐 ratelimit_core.go 导出命名（Allow/Count/Exceeded/Record/Fail/...）。
// 覆盖：窗口滚动恢复、空闲 key 回收、key 维度隔离、冷却进入/解封。

import (
	"strconv"
	"testing"
	"time"
)

func TestSlidingWindowTokenRolling(t *testing.T) {
	l := NewSlidingWindowLimiter(3, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if !l.Allow("k") {
			t.Fatalf("第 %d 次应放行", i+1)
		}
	}
	if l.Allow("k") {
		t.Fatalf("超限应拒绝")
	}
	// 窗口滚动：窗口内 3 次事件全部滑出后恢复放行
	now = now.Add(61 * time.Second)
	if !l.Allow("k") {
		t.Fatalf("窗口滚动后应恢复放行")
	}
	// key 隔离
	if !l.Allow("k2") {
		t.Fatalf("不同 key 额度应独立")
	}
	if l.Count("k2") != 1 {
		t.Fatalf("k2 计数应为 1，实际 %d", l.Count("k2"))
	}
}

func TestSlidingWindowEviction(t *testing.T) {
	l := NewSlidingWindowLimiter(30, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < RLMaxKeys; i++ {
		l.events["k"+strconv.Itoa(i)] = []time.Time{now.Add(-2 * time.Minute)}
	}
	if l.Exceeded("new-key") {
		t.Fatalf("新 key 不应超限")
	}
	l.Record("new-key")
	if len(l.events) != 1 {
		t.Fatalf("空闲 key 应被回收，剩余 %d", len(l.events))
	}
}

// 鉴权失败冷却：达阈值进入 15 分钟冷却，冷却到期后解封。
func TestCooldownLimiterLifecycle(t *testing.T) {
	l := NewCooldownLimiter(3, 5*time.Minute, 15*time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if l.InCooldown("ip:1.1.1.1") {
			t.Fatalf("第 %d 次失败不应已处于冷却", i+1)
		}
		l.Fail("ip:1.1.1.1")
	}
	if !l.InCooldown("ip:1.1.1.1") {
		t.Fatalf("达阈值应进入冷却")
	}
	if got := l.CooldownUntil("ip:1.1.1.1"); !got.Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("冷却截止时刻应为 now+15min，实际 %v", got)
	}
	// 冷却期内即使事件窗口已滑出，仍保持封控
	now = now.Add(6 * time.Minute)
	if !l.InCooldown("ip:1.1.1.1") {
		t.Fatalf("窗口滑出但冷却未满，仍应封控")
	}
	// 冷却到期解封，且窗口事件已过期 → 完全恢复
	now = now.Add(15 * time.Minute)
	if l.InCooldown("ip:1.1.1.1") {
		t.Fatalf("冷却到期应解封")
	}
	if l.Exceeded("ip:1.1.1.1") {
		t.Fatalf("冷却到期后窗口内不应残留计数")
	}
}
