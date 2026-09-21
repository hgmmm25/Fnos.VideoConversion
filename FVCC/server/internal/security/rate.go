package security

// rate.go — P2-1 阶段 B：限流 key 公共计算下沉（原 main 包 ratelimit.go）。
//
// 网关用户维度 / 来源 IP 维度的 key 计算被根包 ratelimit.go 与 internal/ws
// 共同使用，收口到 security 避免跨包重复实现。

import (
	"net"

	"github.com/gin-gonic/gin"
)

// RateKey 用户维度限流的 key：网关 UID → 用户名 → 来源 IP → anonymous（07 §4.5）。
func RateKey(c *gin.Context) string {
	u := GetGatewayUser(c)
	if u.UID != "" {
		return "uid:" + u.UID
	}
	if u.Username != "" {
		return "user:" + u.Username
	}
	if ip := ClientIP(c); ip != "" {
		return "ip:" + ip
	}
	return "anonymous"
}

// ClientIP 取来源 IP（剥端口）；解析失败回落原始 RemoteAddr。
func ClientIP(c *gin.Context) string {
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
