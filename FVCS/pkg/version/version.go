// Package version 是 FVCS 节点版本号的唯一来源。
//
// 发布约定（2026-09-16 修复"上报版本与交付包不一致"缺陷后建立）：
//   - 版本号只写在本目录的 VERSION 文件里，编译期由 go:embed 注入，禁止在代码中另写版本字面量；
//   - 出新包时同步修改：本文件 VERSION、交付包文件名 FVCS_v<版本>_*.fpk、下一步.md；
//   - node 侧 WS Hello 上报的 agentVersion 直接取本值（见 pkg/server/hello.go），
//     调度端（FVCC）用它做能力/兼容性判断，因此必须与实际交付包版本严格一致。
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// Version FVCS 节点版本号（形如 1.2.1）。供 WS Hello 上报与日志展示使用。
var Version = strings.TrimSpace(versionFile)
