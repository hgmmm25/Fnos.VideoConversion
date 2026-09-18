package main

// 版本号唯一来源：本目录 VERSION 文件（go:embed 注入），禁止在代码中另写版本字面量。
//
// 发布约定（2026-09-16 建立，与 FVCS/pkg/version 同构）：
//   - 出新包时同步修改：本目录 VERSION、../manifest（version 字段）、../ui-src/package.json；
//   - /app/fvcc/api/info 返回的 version 与前端「设置」页展示均取自 appVer；
//   - 一致性由 version_test.go 自动校验（二进制版本 vs manifest vs package.json），防止漂移。

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var versionFile string

// appVer FVCC 应用版本号（形如 1.2.2），必须与 manifest / ui-src/package.json 保持一致。
var appVer = strings.TrimSpace(versionFile)
