package remote

// P2-1 B轮：remote 域独立包导出层。
// 收敛调度下发域的错误码、下发接口与共享辅助（错误码原定义于根包 scheduler.go，
// 辅助函数原定义于 handlers_render.go / remote.go），根包经 remote_shim.go 转发保持引用点零改动；
// 阶段 C 迁入 api 层后内联清理。

import (
	"encoding/json"
	"strings"

	"fvcc/internal/store/model"
)

// 调度下发错误码（06 §4.3 可重试白名单，随 remote 域收敛；E_EDL_INVALID 见 internal/edl）。
const (
	ErrCodeSMBMountFailed = "E_SMB_MOUNT_FAILED"
	ErrCodeRenderFailed   = "E_RENDER_FAILED"
	ErrCodePayloadMissing = "E_PAYLOAD_MISSING"
)

// RenderDispatcher 渲染类任务的下发通道（06 §4.2）。
// 由 RemoteClient 实现（CreateRenderEDL / CreateGenProxy，仅传 credentialId，07 号文档）；
// B-05 只负责"何时下发"，不关心线协议细节，故以接口注入，便于单测替身。
type RenderDispatcher interface {
	// CreateRenderEDL 向节点下发 RENDER_EDL 任务，返回节点侧 taskId。
	CreateRenderEDL(server model.Server, t model.Task) (string, error)
	// CreateGenProxy 向节点下发 GEN_PROXY 任务，返回节点侧 taskId。
	CreateGenProxy(server model.Server, t model.Task) (string, error)
	// CreateRenderEDLWithTrace 同 CreateRenderEDL，额外透传链路追踪 ID（P2-1）。
	CreateRenderEDLWithTrace(server model.Server, t model.Task, traceID string) (string, error)
	// CreateGenProxyWithTrace 同 CreateGenProxy，额外透传链路追踪 ID（P2-1）。
	CreateGenProxyWithTrace(server model.Server, t model.Task, traceID string) (string, error)
}

// ParseHelloCaps 解析 Hello 能力上报为 NodeCaps（B-08）。供根包集成测试复用。
func ParseHelloCaps(serverID string, data json.RawMessage) (model.NodeCaps, bool) {
	return parseHelloCaps(serverID, data)
}

// NormalizeRootForCompare 归一化根路径用于前缀比较（供根包 handler 层复用）。
func NormalizeRootForCompare(p string) string {
	return normalizeRootForCompare(p)
}

// normalizeRootForCompare 归一化根路径用于前缀比较：反斜杠→斜杠、压缩重复斜杠（保留 UNC 前缀）、
// 去尾斜杠。大小写在比较处以 ToLower 处理，返回值保留原大小写以免污染真实路径。
func normalizeRootForCompare(p string) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if strings.HasPrefix(p, "//") {
		p = "//" + strings.TrimLeft(strings.TrimPrefix(p, "//"), "/")
		p = "//" + strings.ReplaceAll(strings.TrimPrefix(p, "//"), "//", "/")
	} else {
		for strings.Contains(p, "//") {
			p = strings.ReplaceAll(p, "//", "/")
		}
	}
	p = strings.TrimRight(p, "/")
	if p == "//" {
		return ""
	}
	return p
}
