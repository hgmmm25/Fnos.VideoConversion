package security

// ratelimit_core.go — P2-1 阶段 A：滑动窗口限流器核心（07 §4.5）。
//
// 原实现位于 main 包 ratelimit.go，因 security_audit.go 迁移需要共用，
// 随迁至 internal/security 并导出；main 包 ratelimit.go 仅保留中间件
// 组合与实例化（authFailLimiter / renderSubmitLimiter）。

import (
	"sync"
	"time"
)

// RLMaxKeys 单限流器保留的 key 上限（超出触发空闲回收）。
const RLMaxKeys = 4096

// SlidingWindowLimiter 按 key 独立的滑动窗口计数限流器（now 可注入，便于单测推进时间）。
// cooldown > 0 时，窗口内达阈值即进入冷却，冷却期内该 key 一律拒绝（07 §4.5 登录失败）。
type SlidingWindowLimiter struct {
	mu       sync.Mutex
	events   map[string][]time.Time
	blocked  map[string]time.Time // key → 冷却截止时刻
	limit    int
	window   time.Duration
	cooldown time.Duration
	now      func() time.Time
}

// NewSlidingWindowLimiter 构造基础滑动窗口限流器。
func NewSlidingWindowLimiter(limit int, window time.Duration) *SlidingWindowLimiter {
	if limit <= 0 {
		limit = 1
	}
	if window <= 0 {
		window = time.Minute
	}
	return &SlidingWindowLimiter{
		events:  map[string][]time.Time{},
		blocked: map[string]time.Time{},
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

// NewCooldownLimiter 构造「达阈值即冷却」的限流器（如 07 §4.5 登录失败：10 次/5min → 冷却 15min）。
func NewCooldownLimiter(limit int, window, cooldown time.Duration) *SlidingWindowLimiter {
	l := NewSlidingWindowLimiter(limit, window)
	l.cooldown = cooldown
	return l
}

// SetNow 注入时间源（默认 time.Now；测试推进时钟 / 冷却到期验证用）。
// now 在限流判定前读取，属配置项，测试场景下无并发写，直接赋值即可。
func (l *SlidingWindowLimiter) SetNow(now func() time.Time) {
	if now != nil {
		l.now = now
	}
}

// Exceeded 判断 key 在窗口内是否已达阈值（只读，不记录）。
func (l *SlidingWindowLimiter) Exceeded(key string) bool {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, t)) >= l.limit
}

// Record 记录一次事件（超限后仍记录，保证窗口滚动期间持续受限）。
func (l *SlidingWindowLimiter) Record(key string) {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.events[key]; !ok && len(l.events) >= RLMaxKeys {
		l.evictLocked(t)
	}
	l.events[key] = append(l.pruneLocked(key, t), t)
}

// Allow 记录一次事件并回报是否未超限（限流判定为「先记录后判定」，用于消费型配额）。
func (l *SlidingWindowLimiter) Allow(key string) bool {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.events[key]; !ok && len(l.events) >= RLMaxKeys {
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

// Fail 记录一次失败并进入冷却（07 §4.5 登录失败路径）。
func (l *SlidingWindowLimiter) Fail(key string) bool {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.events[key]; !ok && len(l.events) >= RLMaxKeys {
		l.evictLocked(t)
	}
	kept := append(l.pruneLocked(key, t), t)
	l.events[key] = kept
	if l.cooldown <= 0 || len(kept) < l.limit {
		return true
	}
	if _, blocked := l.blocked[key]; blocked {
		return false
	}
	l.blocked[key] = t.Add(l.cooldown)
	return true
}

// InCooldown 判断 key 是否处于冷却期；冷却到期自动解封。
func (l *SlidingWindowLimiter) InCooldown(key string) bool {
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

// CooldownUntil 返回冷却截止时刻（无冷却则零值；诊断 / 单测用）。
func (l *SlidingWindowLimiter) CooldownUntil(key string) time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.blocked[key]
}

// Count 返回 key 当前窗口内的事件数（诊断 / 单测用）。
func (l *SlidingWindowLimiter) Count(key string) int {
	t := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pruneLocked(key, t))
}

// pruneLocked 丢弃窗口外的历史事件；key 不存在时不创建条目；需持有 l.mu。
func (l *SlidingWindowLimiter) pruneLocked(key string, now time.Time) []time.Time {
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
func (l *SlidingWindowLimiter) evictLocked(now time.Time) {
	cut := now.Add(-l.window)
	for k, ev := range l.events {
		if len(ev) == 0 || ev[len(ev)-1].Before(cut) {
			delete(l.events, k)
		}
	}
}
