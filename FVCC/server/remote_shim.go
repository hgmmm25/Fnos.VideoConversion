package main

// P2-1 B轮：remote 域收敛到 internal/remote 后的根包兼容层。
// 本文件把 internal/remote 导出符号转发为根包原名，使根包与既有测试引用点零改动；
// 阶段 C 迁入 api 层后随接口收口内联删除。

import (
	"encoding/json"

	"fvcc/internal/remote"
	"fvcc/internal/store/model"
)

// 错误码转发（06 §4.3 白名单；E_EDL_INVALID 见 edl_shim.go）。
const (
	errCodeSMBMountFailed = remote.ErrCodeSMBMountFailed
	errCodeRenderFailed   = remote.ErrCodeRenderFailed
	errCodePayloadMissing = remote.ErrCodePayloadMissing
)

// 类型别名转发。
type (
	RemoteClient     = remote.RemoteClient
	RemoteProgress   = remote.RemoteProgress
	RemoteTaskStatus = remote.RemoteTaskStatus
	RenderDispatcher = remote.RenderDispatcher
)

// 变量转发。
var NewRemoteClient = remote.NewRemoteClient

// 函数转发。
func normalizeRootForCompare(p string) string {
	return remote.NormalizeRootForCompare(p)
}

func parseHelloCaps(serverID string, data json.RawMessage) (model.NodeCaps, bool) {
	return remote.ParseHelloCaps(serverID, data)
}
