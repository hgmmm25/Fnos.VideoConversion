// Package version 承载版本单一来源（P2-1 阶段 A：version.go 迁入）。
// 依赖方向：仅标准库；禁止反向依赖上层包。
package version

// 版本号唯一来源：本目录 VERSION 文件（go:embed 注入），禁止在代码中另写版本字面量。
//
// 版本发布约定（与 FVCS/pkg/version 同构，2026-09-16 建立；递增版本同步点见 README「版本表」）：
//   - 出新包时同步修改三处真源（缺一不可）：
//     1. 本目录 VERSION（go:embed → AppVer，即 /api/info 与「设置」页显示的值）
//     2. FVCC/manifest 的 version 字段（打包用；自本目录上溯 4 层，不是 ../manifest）
//     3. FVCC/ui-src/package.json 的 version 字段
//     历史缺陷：v1.6.4 出包只改了 2、3，漏改 1 → 已推送的包「设置」页仍显示 1.6.0。
//   - /app/fvcc/api/info 返回的 version 与前端「设置」页展示均取自 AppVer；
//   - 一致性由 version_test.go 自动校验（AppVer vs manifest vs package.json）：
//     该用例从自身源文件位置向上定位仓库根，Windows 本机 `go test ./...` 同样真跑，
//     定位不到或读取失败一律判红（旧实现路径写错 + 失败即 Skipf，导致漂移静默溜过）。

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// AppVer FVCC 应用版本号（形如 1.2.2），必须与 manifest / ui-src/package.json 保持一致。
var AppVer = strings.TrimSpace(versionFile)
