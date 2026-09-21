package ws

import "sync"

// WSBrowserMaxConns 单个浏览器的 WS 连接上限（07 §4.5：浏览器 ≤ 5 条）。
const WSBrowserMaxConns = 5

// WSConnRegistry 按「浏览器」维度统计 WS 连接数（key 取网关 UID，回落来源 IP）。
// 计数归零时移除条目，避免 map 随连接抖动无限增长。
type WSConnRegistry struct {
	mu    sync.Mutex
	conns map[string]int
}

func NewWSConnRegistry() *WSConnRegistry {
	return &WSConnRegistry{conns: map[string]int{}}
}

// Acquire 尝试占用一个连接位；返回 false 表示该 key 已达上限。
func (r *WSConnRegistry) Acquire(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[key] >= WSBrowserMaxConns {
		return false
	}
	r.conns[key]++
	return true
}

// Release 释放一个连接位。
func (r *WSConnRegistry) Release(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := r.conns[key] - 1
	if n <= 0 {
		delete(r.conns, key)
		return
	}
	r.conns[key] = n
}

// Count 返回 key 当前连接数（诊断 / 单测用）。
func (r *WSConnRegistry) Count(key string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[key]
}

// Len 返回当前登记的 key 数量（诊断 / 单测用）。
func (r *WSConnRegistry) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.conns)
}

// ConnRegistry 懒初始化 Hub 的连接计数表（兼容直接构造 &Hub{} 的调用方）。
func (h *Hub) ConnRegistry() *WSConnRegistry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.wsConns == nil {
		h.wsConns = NewWSConnRegistry()
	}
	return h.wsConns
}
