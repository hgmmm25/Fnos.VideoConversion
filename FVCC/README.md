# FVCC — 视频转码调度 Web 客户端（fnNAS 应用）

面向 fnNAS 的 Web 调度端：浏览本地素材、下发转码任务到 FVCS 渲染端、实时跟踪进度。服务端 Go（gin），前端 Vite + TypeScript（无框架原生 DOM）。

## 目录结构

```
FVCC/
├── server/              # Go 后端（单模块 fvcc）
│   ├── main.go              # 入口与装配（组件注入、优雅退出）
│   ├── internal/            # 分层内部包（P2-1 后端分层收口）
│   │   ├── api/             # HTTP 处理器组/路由/票据流/缩略图/回收站/限流（handlers*、router、stream、trash、security_roots、hashutil、ratelimit）
│   │   ├── edl/             # EDL 载荷校验（与 FVCS 双实现）
│   │   ├── media/           # ffprobe 探测 / 本地转码 / 代理流程 / 流媒体与缩略图工具
│   │   ├── node/            # 节点选择策略
│   │   ├── protocol/        # API 错误码与 JSON 应用 helper
│   │   ├── remote/          # FVCS 远程连接（WSS 数据面）与渲染分发
│   │   ├── scheduler/       # 任务调度（选机、健康分、熔断）
│   │   ├── security/        # 凭据加密/审计/网关鉴权/限流核心/路径校验
│   │   ├── store/           # JSON 文件存储 + model 数据模型域
│   │   ├── version/         # 版本信息（go:embed，VERSION 单一来源）
│   │   └── ws/              # WebSocket 推送与限流
│   ├── logger/              # 日志
│   ├── smbshare/            # SMB 共享能力
│   └── go.mod / go.sum
├── ui-src/              # 前端源码（Vite + TS，零框架 el() 手工 DOM）
│   ├── src/                 # 页面与逻辑
│   │   ├── pages/           # 页面（含时间线编辑器）
│   │   ├── api.ts / ws.ts   # HTTP / WS 客户端
│   │   ├── store.ts         # 前端状态
│   │   ├── command.ts       # 命令面板
│   │   ├── theme.ts         # 主题切换
│   │   ├── style.css        # 设计令牌真源（--c-* / --ease-*）
│   │   └── ui.ts            # el() DOM 工具
│   ├── scripts/             # check-design.mjs 等门禁脚本
│   └── public/              # config + images（vite 构建时复制回 app/ui，禁止误删）
├── docs/                # 项目内文档
│   ├── API_CONTRACT.md      # 前后端 API 契约
│   ├── DESIGN.md            # 设计契约（Named Rules + check:design 门禁）
│   └── PRODUCT.md           # 产品定位与反参照
├── cmd/                 # fnOS 安装脚本
├── app/                 # 构建产物目录（git 忽略）
├── temp/                # 打包 stage 等临时目录（git 忽略）
├── manifest             # fnOS 应用清单（UTF-8 无 BOM）
└── .gitignore           # 构建产物/依赖/日志排除
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

升级版本必须**三处同步**（构建时以 `server/internal/version/VERSION` 为准，`scripts/check-versions.ps1` 自动校验）：

| 文件 | 字段 | 当前值 |
|------|------|--------|
| `manifest` | `version` | 1.4.4 |
| `ui-src/package.json` | `version` | 1.4.4 |
| `server/internal/version/VERSION` | 文件内容 | 1.4.4 |

## 项目内文档体系

| 文档 | 位置 | 说明 |
|------|------|------|
| API_CONTRACT.md | `FVCC/docs/` | 前后端 API 契约（错误码、路由、载荷） |
| DESIGN.md | `FVCC/docs/` | 前端设计契约（Named Rules + 反例清单 + check:design 门禁） |
| PRODUCT.md | `FVCC/docs/` | 前端产品定位、用户与反参照 |
| 设计规格 01~10 | `../WebVideoEditor_Design/` | 见上表"设计文档引用速查" |
| 混乱度评价报告 | `../docs/FVCC_混乱度评价报告_细化版.md` | 项目治理台账（P0/P1/P2 计划与状态） |
| TASK_REFERENCE.md | `../docs/TASK_REFERENCE.md` | 任务编号语义登记（B/C/D/M4/修复/Px-x） |

## 前端公共模块速查（ui-src/src/lib/，P2-2 新增）

| 模块 | 职责 | 消费方 |
|------|------|--------|
| `crudActions.ts` | CRUD 四件套统一封装（confirmDialog → api → toast → reload），只依赖 ApiError | profiles / servers / tasks / history |
| `scrollPos.ts` | 按 key 隔离保存/恢复列表滚动位置 | profiles |
| `useListPage.ts` | 列表页公共流程（订阅 store → 拉数据 → 渲染 → 错误处理 → refresh/dispose） | tasks / servers / history / profiles / scanner |
| `formBuilder.ts` | 表单 label+input 生成、collect/fill/validate（a11y：label 关联 input） | settings / profiles |

> P2-2 落地状态：2026-09-18 已收口（6 页 CRUD/滚动/订阅/表单样板收敛；main.ts 路由切换接入 dispose 防泄漏）。

## 治理计划状态（P1-1 ~ P1-5）

依据 `FVCC_混乱度评价报告_细化版.md` §4 的 P1 组计划项，2026-09-18 复核状态：

| 计划项 | 内容 | 状态 | 说明 / 剩余动作 |
|--------|------|------|------------------|
| P1-1 | Profile 反射白名单更新 | ✅ 已落地 | `server/internal/protocol/applyjson.go` + 单测（P2-1 分层后位置） |
| P1-2 | SMB 路径校验 | ✅ 已落地 | `server/internal/security/smb_validate.go` + 单测（P2-1 分层后位置） |
| P1-3 | 文档落地（代码注释引用设计规格） | ✅ 已落地 | 注释以 `WebVideoEditor_Design/07-§3.2` 形式引用 |
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地 | 三处 1.4.4 已同步（2026-09-19 复核）；本表已更新，消除 README 滞后 |
| P1-5 | 依赖瘦身（前端字体 / 后端 indirect） | 🟡 部分落地 | 字体 latin 子集已瘦身（app/ui/assets 12 文件）；go.mod 已执行 `go mod tidy` 复核：29 个 indirect 均为 gin v1.12 传递依赖（mongo-driver v2 由 gin 直接 require、quic-go 由 gin http3 引入），无进一步精简空间；node_modules 全字符集不进入构建产物，建议保持不动 |

## 治理计划状态（P2-1 ~ P2-8）

依据 `FVCC_混乱度评价报告_细化版.md` §4 的 P2 组计划项，2026-09-19 复核状态：

| 计划项 | 内容 | 状态 | 说明 / 剩余动作 |
|---|---|---|---|
| P2-1 | 后端 internal 分层 | ✅ 已落地（2026-09-19） | `server/internal/` 11 包 + store/model 子包，根包仅剩 main.go（82 个 .go）；阶段 A/B/C 完成、8 shim 内联删除；基线 tag v1.4.3；详见 [docs/P2-1_后端分层_阶段A实施记录.md](<FVCC/docs/P2-1_后端分层_阶段A实施记录.md>) |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | `src/lib/` 4 件套接入 6 页、样板残留 0；main.ts 路由切换接入 dispose 防订阅泄漏；详见 [docs/P2-2_细化细则.md](<FVCC/docs/P2-2_细化细则.md>)（Checklist 全勾） |
| P2-3 | 建立 CI 流水线 | ✅ 已落地 | `.github/workflows/fvcc-ci.yml`（paths 限定 FVCC/**）+ `scripts/check-coverage.ps1`（覆盖率门槛 ≥55%，基线 56.1%，后续上调 60%） |
| P2-4 | 统一 API 契约与命名 | ✅ 已收口 | 统一错误契约（apierr.go + API_CONTRACT.md + api.ts 适配层）已落地；存量 `{error}` 裸格式 grep 为 0；remote.go snake_case 结构体均为 FVCS 对等协议段（API_CONTRACT §3.2 已登记边界），无剩余动作 |
| P2-5 | 删除操作回收站化 | ✅ 已落地 | `server/internal/store/trash.go`（P2-1 分层后位置）：视频删除移入授权根下 `_trash`，同名时间戳后缀；listTrash 列表接口 |
| P2-6 | 清理 AIGC 残留与任务编号注释 | ✅ 已收口 | AIGC frontmatter 已清（README / API_CONTRACT / ui-src PRODUCT / 根目录 2 份 / 混乱报告自身 / docs/ 3 份补清）；编号映射见 docs/TASK_REFERENCE.md |
| P2-7 | 测试文件命名规范化 | ✅ 已落地 | `genproxy_share_cred_126_test.go` → `genproxy_share_cred_test.go`、`handlers_edl_list_contract_test.go` → `handlers_edl_list_test.go`（git mv 保留历史） |
| P2-8 | 破坏性操作审计闭环 | ✅ 已落地 | security_audit.go 新增 `auditDestructive`（destructive.delete / destructive.clear / destructive.empty_trash），已接入清日志/清缓存/删除视频/服务器/方案/任务/历史/清空回收站 8 处调用点；配套单测 TestP28DestructiveOpsAudited |
