package server

// D-04 审计与告警埋点（07 §7）：鉴权失败、限流拒绝、渲染/代理提交、载荷校验拒绝，
// 以及 §7 表格中的阈值告警（越权扫描 E_ASSET_NOT_IN_ROOT、注入试探 E_PAYLOAD_INVALID/E_EDL_INVALID）。
// 统一经 smb.AuditEvent 落 audit_log（actor 取来源 IP / 'local'，detail 脱敏）；
// 审计库未就绪时降级为 WARN 日志，属旁路能力，不影响请求可用性。

import (
	"fmt"
	"sync"
	"time"

	"Fnos.VC_Service/pkg/logger"
	"Fnos.VC_Service/pkg/protocol"
	"Fnos.VC_Service/pkg/smb"
)

// 审计动作名（与 FVCC 侧 Action 命名对齐，便于跨端按 Action 聚合）
const (
	actionAuthFail       = "auth.fail"       // 鉴权失败（Key 缺失 / 不符）
	actionRateLimitAuth  = "ratelimit.auth"  // 鉴权失败达阈值触发冷却拒绝
	actionValidateReject = "validate.reject" // 载荷校验 / 凭据 Scope 前置拒绝
	actionTaskSubmit     = "task.submit"     // 直通任务提交
	actionRenderSubmit   = "render.submit"   // RenderEDL 提交
	actionProxySubmit    = "proxy.submit"    // GenProxy 提交
	actionSecurityAlert  = "security.alert"  // 阈值告警事件（07 §7 表格）
)

// 07 §7 告警阈值
const (
	alertAssetScanThreshold    = 3  // 5min 内 E_ASSET_NOT_IN_ROOT ≥ 3 次（P0 仅日志 + 审计）
	alertPayloadProbeThreshold = 50 // 5min 内 E_PAYLOAD_INVALID/E_EDL_INVALID > 50 次
	alertWindow                = 5 * time.Minute
)

// auditSecurity 安全 / 渲染事件审计入口：actor 取来源 IP（07 §7）。
func auditSecurity(ip, action, target, result, detail string) {
	if ip == "" {
		ip = "local"
	}
	smb.AuditEvent(ip, action, target, result, detail)
}

// slidingCounter 滑动窗口计数（进程内），用于 07 §7 的突增型告警。
type slidingCounter struct {
	mu     sync.Mutex
	window time.Duration
	hits   []time.Time
}

func newSlidingCounter(window time.Duration) *slidingCounter {
	return &slidingCounter{window: window}
}

// add 记一次命中并返回窗口内累计次数。
func (c *slidingCounter) add() int {
	now := time.Now()
	cutoff := now.Add(-c.window)
	c.mu.Lock()
	defer c.mu.Unlock()
	kept := c.hits[:0]
	for _, t := range c.hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	c.hits = append(kept, now)
	return len(c.hits)
}

// reset 清空窗口（测试用）。
func (c *slidingCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hits = nil
}

var (
	alertAssetScan    = newSlidingCounter(alertWindow)
	alertPayloadProbe = newSlidingCounter(alertWindow)
)

// auditReject 记录一次载荷校验/前置拒绝，并按 07 §7 表格做阈值告警。
func auditReject(ip, target, code, detail string) {
	auditSecurity(ip, actionValidateReject, target, code, detail)

	switch code {
	case protocol.ErrCodeAssetNotInRoot:
		// 疑似越权扫描：达阈值记 ERROR 并落告警审计（P0 仅日志 + 审计，不自动封禁）
		if n := alertAssetScan.add(); n == alertAssetScanThreshold {
			logger.Error("server", "告警：5min 内 E_ASSET_NOT_IN_ROOT 达 %d 次，疑似越权扫描，最近来源 IP=%s", n, ip)
			auditSecurity(ip, actionSecurityAlert, target, code, fmt.Sprintf("count=%d/%v", n, alertWindow))
		}
	case protocol.ErrCodePayloadInvalid, protocol.ErrCodeEDLInvalid:
		// 疑似注入试探
		if n := alertPayloadProbe.add(); n == alertPayloadProbeThreshold {
			logger.Warn("server", "告警：5min 内载荷校验失败(P/EDL) 达 %d 次，疑似注入试探", n)
			auditSecurity(ip, actionSecurityAlert, target, code, fmt.Sprintf("count=%d/%v", n, alertWindow))
		}
	}
}
