---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 839b5d1d4fff15220193598838e6072d_780a70e0b30c11f1839d525400cd780f
    ReservedCode1: 2ysoiPtuqSp4s94f4ilGwDGkUXgKLrRRmbF1BJSjFKfAPfvCHxO4YJirhsdWfv4CoIIxgSQM9hHqXsgC/AWgUfp206l3qPmtUae6ZT4ehYu4zPE5+uiimfBPckdAFZxFbqAYNIoVLkJ6K73CttRuZspNAMDX06f0VixEZeD6TMhisxPLsqtF7q+99FA=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 839b5d1d4fff15220193598838e6072d_780a70e0b30c11f1839d525400cd780f
    ReservedCode2: 2ysoiPtuqSp4s94f4ilGwDGkUXgKLrRRmbF1BJSjFKfAPfvCHxO4YJirhsdWfv4CoIIxgSQM9hHqXsgC/AWgUfp206l3qPmtUae6ZT4ehYu4zPE5+uiimfBPckdAFZxFbqAYNIoVLkJ6K73CttRuZspNAMDX06f0VixEZeD6TMhisxPLsqtF7q+99FA=
---

# FVCC — 视频转码调度 Web 客户端（fnNAS 应用）

面向 fnNAS 的 Web 调度端：浏览本地素材、下发转码任务到 FVCS 渲染端、实时跟踪进度。服务端 Go（gin），前端 Vite + TypeScript（无框架原生 DOM）。

## 目录结构

```
FVCC/
├── server/        # Go 后端（package main，单模块 fvcc）
│   ├── handlers.go        # HTTP 处理器组
│   ├── store.go           # JSON 文件存储（任务/设置/Profile）
│   ├── scheduler.go       # 任务调度（选机、健康分、熔断）
│   ├── applyjson.go       # Profile 反射白名单更新 helper（P1-1）
│   ├── smb_validate.go    # SMB 路径校验 helper（P1-2）
│   ├── crypto.go          # 凭据落盘加密与 API 脱敏（P0-1）
│   ├── edl_validate.go    # EDL 载荷校验（与 FVCS 双实现）
│   └── VERSION            # 版本号（与 manifest/package.json 三处同步）
├── ui-src/        # 前端源码（Vite + TS）
│   ├── src/               # 页面与逻辑
│   └── public/            # config + images（vite 构建时复制回 app/ui，禁止误删）
├── cmd/           # fnOS 安装脚本
├── app/           # 构建产物目录（git 忽略）
├── temp/          # 打包 stage 等临时目录（git 忽略）
├── manifest       # fnOS 应用清单（UTF-8 无 BOM）
└── .gitignore     # 构建产物/依赖/日志排除
```

## 构建

完整构建流程与坑位见仓库根 [BUILD.md](../BUILD.md)。核心命令：

```powershell
# FVCC 全量构建（前端 + 后端 + fpk 打包）
cd D:\Fnos.VideoConversion
.\build.ps1 -Target FVCC

# 后端测试（单测 90s+）
cd server
$env:GOTOOLCHAIN = "auto"   # go.mod 要求 go 1.27.1，必须 auto 匹配缓存工具链
go test ./...
```

## 设计文档引用速查

代码注释中大量以 `WebVideoEditor_Design/07-§3.2` 形式引用规格文档，**相对仓库根 `D:\Fnos.VideoConversion` 解析**（设计文档在上级目录，不在本目录内）。速查表：

| 编号 | 文档 | 主要覆盖 |
|------|------|----------|
| 01 | [01-产品需求与交互规格.md](../WebVideoEditor_Design/01-产品需求与交互规格.md) | 产品需求、交互规格 |
| 02 | [02-前端架构与时间线编辑器设计.md](../WebVideoEditor_Design/02-前端架构与时间线编辑器设计.md) | 前端架构、时间线编辑器 |
| 03 | [03-EDL数据模型与接口契约.md](../WebVideoEditor_Design/03-EDL数据模型与接口契约.md) | EDL 数据模型、接口契约、错误码 |
| 04 | [04-预览网关与代理工作流设计.md](../WebVideoEditor_Design/04-预览网关与代理工作流设计.md) | 预览网关、代理工作流 |
| 05 | [05-RenderEDL渲染引擎与FFmpeg命令构造器设计.md](../WebVideoEditor_Design/05-RenderEDL渲染引擎与FFmpeg命令构造器设计.md) | 渲染引擎、FFmpeg 命令构造 |
| 06 | [06-调度持久化与渲染节点管理设计.md](../WebVideoEditor_Design/06-调度持久化与渲染节点管理设计.md) | 调度持久化、节点管理 |
| 07 | [07-安全校验与凭据管理细则.md](../WebVideoEditor_Design/07-安全校验与凭据管理细则.md) | 安全校验、凭据管理、路径约束 |
| 08 | [08-P0实施计划与验收清单.md](../WebVideoEditor_Design/08-P0实施计划与验收清单.md) | P0 实施计划、验收清单 |
| 09 | [09-附录-部署与迁移说明.md](../WebVideoEditor_Design/09-附录-部署与迁移说明.md) | 部署与迁移 |
| 10 | [10-实施进度与下一步.md](../WebVideoEditor_Design/10-实施进度与下一步.md) | 实施进度台账 |

注释引用示例：`// 规则唯一来源：WebVideoEditor_Design/07-安全校验与凭据管理细则.md §3.2` → 即上表 07 文档第 3.2 节。

## 版本号单一来源

升级版本必须**三处同步**（构建时以 `server/VERSION` 为准，`scripts/check-versions.ps1` 自动校验）：

| 文件 | 字段 | 当前值 |
|------|------|--------|
| `manifest` | `version` | 1.4.1 |
| `ui-src/package.json` | `version` | 1.4.1 |
| `server/VERSION` | 文件内容 | 1.4.1 |
*（内容由AI生成，仅供参考）*
