package main

import "sync"

// wsBrowserMaxConns 单个浏览器的 WS 连接上限（07 §4.5：浏览器 ≤ 5 条）。
const wsBrowserMaxConns = 5

// wsConnRegistry 按「浏览器」维度统计 WS 连接数（key 取网关 UID，回落来源 IP）。
// 计数归零时移除条目，避免 map 随连接抖动无限增长。
type wsConnRegistry struct {
	mu    sync.Mutex
	conns map[string]int
}

func newWSConnRegistry() *wsConnRegistry {
	return &wsConnRegistry{conns: map[string]int{}}
}

// acquire 尝试占用一个连接位；返回 false 表示该 key 已达上限。
func (r *wsConnRegistry) acquire(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[key] >= wsBrowserMaxConns {
		return false
	}
	r.conns[key]++
	return true
}

// release 释放一个连接位。
func (r *wsConnRegistry) release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.conns[key] - 1
	if n <= 0 {
		delete(r.conns, key)
		return
	}
	r.conns[key] = n
}

// count 返回 key 当前连接数（诊断 / 单测用）。
func (r *wsConnRegistry) count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[key]
}

// connRegistry 懒初始化 Hub 的连接计数表（兼容直接构造 &Hub{} 的调用方）。
func (h *Hub) connRegistry() *wsConnRegistry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsConns == nil {
		h.wsConns = newWSConnRegistry()
	}
	return h.wsConns
}
