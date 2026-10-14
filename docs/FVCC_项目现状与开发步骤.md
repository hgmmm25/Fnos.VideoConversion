# FVCC 项目现状与开发步骤

> 调查日期：2026-09-20（首版）；**2026-10-14 压缩更新**：历史版本快照段落收拢为 §1.0 版本线表，每轮细节与提交链见 git 历史与 `docs/IMPROVEMENT_LOG.md`。
> 调查范围：`D:\Fnos.VideoConversion\FVCC`（视频转码调度端 Web 客户端，fnNAS 应用）
> 数据来源：实际目录扫描 + 源码阅读（server 后端 / ui-src 前端 / docs 文档）+ git 核验，非记忆推断。

---

## 1. 现状概述

FVCC（Fnos Video Conversion Client）是 fnNAS 生态的视频转码调度端 Web 客户端：浏览本地素材、把转码/代理/渲染任务下发给 FVCS 渲染端、通过 WebSocket 实时跟踪进度。服务端 Go（gin），前端 Vite + TypeScript 零框架原生 DOM，产物以 fpk 形式打包供 fnOS 安装。

**当前基线（2026-10-14）**：**FVCC v1.6.9 / FVCS v1.3.1**——三真源一致（`FVCC/manifest`、`FVCC/ui-src/package.json`、`FVCC/server/internal/version/VERSION`），`scripts/check-versions.ps1` 绿（v1.6.5 起校验为「漂移即红」）。v1.6.9 为**低风险收尾批次（第三十七轮）**：store 备份/恢复前端入口 + P3-6 勾选 + `.commandcode/` 收编；已出 fpk（11,014,297 B）、已提交（主体 `dedd124` + 后记笔打注解 tag `v1.6.9`）并推送 `fork/baseline-snapshot`，NAS 真机安装由用户验证。

### 1.0 版本线摘要（git 核验）

| 版本 | tag | 一句话结论 |
|---|---|---|
| v1.5.7 | `v1.5.7`→68ebdec | Windows 盘符根路径边界修复（stream/edl/trash/smbshare/handlers 五处）+ 多根兜底 + roots 回落 + 素材递归扫描 |
| v1.5.8 | `v1.5.8` | P3-5 store 定时备份 / 版本化迁移链 / 快照回滚三阶段 + `/api/store/*` API（仅后端，前端入口缓做） |
| v1.5.9~1.5.12 | `v1.5.12` | 步骤 8 e2e A 档缺陷修复（#5/#6/#10/#11）+ **#8 渲染全链路四层收口**：①FVCS `srcBase=mountPath` ②FVCC `EDLClip.sourceRoot` 有界单根 `resolveRenderRoots` ③FVCS `makeSrcResolver` 对齐 `BuildSMBPath` ④FVCC RENDER_EDL 终态轮询 `pollRenderTerminal`/`completeRenderTask`；2026-10-12 真机复验通过（Success 收口 + 进度平滑）；配套 FVCS v1.2.6 |
| v1.6.0 | `v1.6.0`→b1a0d93 | 视觉打磨版：方向 A「精修冷灰」令牌重排 + 控件自绘 + 外壳导航分组/搜索胶囊/状态栏 + V1/V5-V8 逐页修复 + 微交互与 WCAG（V9）；同批 FVCS 1.2.6→1.3.0（Fyne 托盘 + 设置窗口，`build.ps1 -Target FVCS-UI`） |
| v1.6.4 | `v1.6.4`→b007a3d | 任务页视觉改进批次 A+B+C（方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`；进度条 `scaleX(--p)` 合成层 + 热路径签名守卫，硬约束「视觉改进不增加 WS tick 重绘」） |
| v1.6.5 | `v1.6.5`→5166cbe | VERSION 第三真源漂移修复 + 校验加固（Windows 静默 Skip 根治为漂移即红 + `TestAppVerIsNotPinnedLiteral` 反硬编码守卫 + 脚本去硬编码路径） |
| v1.6.6 | `v1.6.6`→1dcb774 | 第三十轮低风险依赖刷新 + 配套 FVCS 1.3.1 |
| v1.6.7 | `v1.6.7`→4154d31 | `modernc.org/sqlite` v1.14.0→**v1.60.1** 跨 46 minor 专项（真实 fvcc.db 迁移验证，14 包全绿）+ Node 运行时 v25.9.0→**v26.11.1**（第三十三轮；`engines.node>=26`、CI 22→26、`@types/node` ^26，asset 哈希逐项不变） |
| v1.6.8 | `v1.6.8` | `tailwindcss` 3.4.19→**4.3.3** 大版本专项（CSS-first、`tailwind.config.js` 删除、token 入 `src/style.css` `@theme inline`、`npm audit`/`npm outdated` 双清零、gzip 10.00→9.78 kB、fpk 11,013,500 B）；产品裁决：接受 v4 移除按钮举手型默认 |
| v1.6.9 | `v1.6.9`（后记笔，主体 `dedd124`） | 低风险收尾批次（第三十七轮）：**store 备份/恢复前端入口**（设置页新面板接 `/api/store/*`，dev 真实后端 + 浏览器全链路实测）+ P3-6 台账正式勾选 + `.commandcode/` 入 .gitignore；纯前端 + 治理，后端零改动；fpk 11,014,297 B（出包改由 `pwsh` 7 执行，见 IMPROVEMENT_LOG §28.9） |

**易错点**：`1.6.1`/`1.6.2`/`1.6.3` 只是批次开发过程中的本地中间 bump，**从未进入 git 历史、未出包、未打 tag**；方案文件名里的 "v1.6.1" 是**方案稿编号**、不是交付版本号——不可写成"1.6.1~1.6.4 版本线"，应写"**1.6.4**（方案稿 v1.6.1 的落地批次）"。

**治理进展**：P0-2 SQLite 迁移已落地（2026-09-21，a72b8ff/9e122d5/5dd373e）；P1 组落地（P1-1 事件驱动调度 eab7688）；P2 组（P2-1~P2-8）全部收口（后端 internal 分层、前端组件化、CI 流水线、API 契约统一、回收站化、AIGC 清理、测试命名规范化、破坏性操作审计）。设计文档引用速查见 `FVCC/README.md`，治理台账见 `docs/FVCC_混乱度评价报告_细化版.md`。

**总体判断**：主干功能完整、工程质量较高（分层清晰、测试齐全、契约规范、SQLite 持久化落地），处于"功能已闭环、局部待打磨"阶段。P3 组自 v1.6.9 起**仅剩 P3-8**（远期决策门，随多轨立项评审触发）；P3-6 已于第三十七轮正式勾选关闭（NAS `/info` `ffprobe:true`，2026-10-11），P3-7/P3-9 已于第二十六轮关闭，P3-5 的前端入口亦随 v1.6.9 还清。

**开放项清单（2026-10-14 口径）**：

| 开放项 | 状态 |
|---|---|
| 概览 Filter Chips 状态过滤 | ⬜ 待产品拍板 |
| `?fx=stripes\|breathe` 实验动画 | ⬜ 待真机并发裁决转正/弃用 |
| 其余页面骨架迁移 `listSkeleton()` | ⬜ 目前仅 tasks 页切换 |
| 手动备份删除入口（v1.6.9 新登记） | ⬜ 待裁决：轮转池跳过 manual，后端无 `DELETE /api/store/backups/:id`，面板只增不减 |
| e2e #7 `judgeVisibility` 忽略分辨率 | ⬜ 需用户给阈值 |
| e2e #9 跨多授权根混合素材时间线 | ⬜ 当前明确拒绝 `E_EDL_INVALID`，需跨端 payload 改造 + 真机验证 |
| 渲染 Failed/Cancelled 终态分支 | ⬜ 无自然复现，遇真实失败顺带验证 |
| 结构性立项：多轨 `{videoTracks,audioTracks}` + EDL 转场 | ⬜ P1-2 freecut 框架试点绑于此 |
| NAS 真机视觉对照（Tailwind 4 淡色/悬停/触屏/forced-colors） | ⬜ v1.6.8 后用户实测中 |

### 1.1 代码规模（实测）

| 维度 | 文件数 | 行数 | 说明 |
|---|---|---|---|
| Go 后端（非测试） | 58 | 17,197 | `server/` 根包 + `internal/` 12 包 + `logger/` + `smbshare/`（2026-10-13 实测，2026-10-14 在新 SQLite 驱动 + Node 26 下复测逐项一致） |
| Go 测试 | 52 | 9,183 | 含 e2e / 并发 / SQLite 迁移 / P3-5 备份-迁移-回滚 / #8 render_poll 回归 |
| TypeScript 前端 | 33 | 11,622 | `ui-src/src/` 全部页面与逻辑（v1.6.9 +148 行：store 备份/恢复面板与类型） |
| **合计** | **143** | **38,002** | 不含 node_modules、构建产物、fpk 包（Go 合计 110 个 `.go`） |

### 1.2 构建产物与运行形态

- 根目录：构建产物与历史 fpk 包已于 2026-09-21 归档至根仓库 `archive/fpk/`（完整收纳 v1.1.0~v1.5.3）；`manifest`（应用清单，version=1.6.9，2026-10-14 实测）。根目录**无** `fvcc.exe` / `fvcs-service.exe` / `fvcc.fpk` 物理堆积。
- 运行时数据（按禁删原则保留原位）：`config.enc.json`（加密配置）、`master.key` / `secret.key`（凭据密钥）、`credentials.db`（凭据库 SQLite，含 -shm/-wal）、`tasks.json` / `history_tasks.json`（任务存储）、`locks.json`（锁表）。根目录**无** `task.db`（仅 `archive/` 下存 12KB 备份，为 FVCS 侧运行产物）。
- `config/`：fnOS 应用配置目录（privilege / resource）；`logs/`：运行时日志（按日期 + app_*.log）。
- `app/`：前端构建产物（vite 输出 + 静态资源），由 fpk 打包；`app/ui/assets` 为 hash 资源目录（以最新构建的 index.html 引用为准核对）。
- `server/` 监听：生产走 fnOS 网关 Unix Socket（TRIM_APPDEST 注入），开发模式 TCP `127.0.0.1:8088`，网关前缀 `/app/fvcc`。
- 打包链路：`D:\Fnos.VideoConversion\build.ps1 -Target FVCC`（前端 build → 后端 build → fnpack 打包 fpk）。

---

## 2. 目录结构

```
D:\Fnos.VideoConversion\FVCC\
├── server/                    # Go 后端（单模块 fvcc，go 1.27.1）
│   ├── main.go                # 入口与装配（组件注入、优雅退出、Unix Socket / dev TCP）
│   ├── internal/              # P2-1 分层后的内部包
│   │   ├── api/               # HTTP 处理器/路由/票据流/回收站/限流/装配（handlers*.go、router.go、stream.go、trash_handlers.go、security_roots.go、ratelimit.go、hashutil.go、aliases.go、config.go）
│   │   ├── edl/               # EDL 载荷校验/导出（edl_validate.go 18K、edl_support.go、export.go、audit_hook.go）
│   │   ├── media/             # ffprobe 探测/本地转码/代理工作流/流媒体与缩略图（ffprobe.go、proxy.go、stream_media.go、roots.go、localtranscode*.go）
│   │   ├── node/              # 节点选择策略（node.go 16K）
│   │   ├── protocol/          # 错误码契约与 JSON 应用 helper（apierr.go、errcode.go、applyjson.go）
│   │   ├── remote/            # FVCS 远程连接（WSS 数据面）与渲染分发（remote.go 42K、export.go）
│   │   ├── scheduler/         # 任务调度（scheduler.go 46K/1268 行：队列分发、转码/编码锁、健康分、熔断）
│   │   ├── security/          # 凭据加密/审计/网关鉴权/限流核心/路径校验（audit.go、crypto.go、gateway.go、smb_validate.go、ratelimit_core.go）
│   │   ├── store/             # 持久化（SQLite 主载体 + JSON 回退 + 内存索引 + Flush 批量落盘）：store.go、store_edl.go、store_proxy.go、trash.go、sqlite.go、sqlite_import.go、sqlite_load.go、sqlite_persist.go、backup.go、migrate.go、restore.go、model/models.go 32K
│   │   ├── version/           # 版本信息（go:embed VERSION 单一来源）
│   │   └── ws/                # WebSocket Hub 推送与限流（ws.go、ws_limit.go）
│   ├── logger/                # 日志
│   ├── smbshare/              # SMB 共享能力
│   └── go.mod / go.sum
├── ui-src/                    # 前端源码（Vite + TS，零框架 el() 手工 DOM）
│   ├── src/
│   │   ├── main.ts            # hash 路由 + 7 页面挂载 + 全局状态条 + 命令面板
│   │   ├── api.ts             # 统一 ApiError 契约 + 全部 REST/SSE 封装（254 行）
│   │   ├── ws.ts              # WebSocket 客户端
│   │   ├── store.ts           # 全局响应式单例（tasks/servers/profiles/history/metrics/settings/currentProjectId）
│   │   ├── command.ts / theme.ts / types.ts / ui.ts / style.css / vite-env.d.ts
│   │   ├── lib/               # P2-2 组件化成果：crudActions / formBuilder / scrollPos / useListPage
│   │   └── pages/             # tasks / scanner / servers / profiles / history / settings + editor/
│   │       └── editor/        # 剪辑编辑器 14 个 TS 模块（见 §4.2）
│   ├── scripts/               # check-design.mjs、check-versions.ps1 等门禁脚本
│   ├── public/                # config + images（vite 构建时复制回 app/ui，禁止误删）
│   └── package.json / vite.config.ts / postcss.config.js / tsconfig.json   # v1.6.8 起无 tailwind.config.js（Tailwind 4 CSS-first，token 映射在 src/style.css 的 @theme inline）
├── cmd/                       # fnOS 应用生命周期脚本（install / upgrade / uninstall / config，各含 init/callback）
├── config/                    # fnOS 应用配置（privilege / resource）
├── app/                       # 前端构建产物（git 忽略，vite outDir）
├── docs/                      # 项目内文档（7 份）：API_CONTRACT / DESIGN / PRODUCT / P2-1 实施记录 / P2-2 细化细则 / SECURITY / STORE_PERSISTENCE_ROADMAP
├── logs/ / temp/              # 运行时日志与打包 stage 目录（git 忽略）
├── manifest                   # fnOS 应用清单（UTF-8 无 BOM，version=1.6.9）
├── config.enc.json            # 加密配置（凭据相关）
├── credentials.db / -shm / -wal / master.key / secret.key / locks.json / tasks.json / history_tasks.json   # 运行时数据（禁删原则保留原位）
└── .gitignore
```

> 上层目录 `D:\Fnos.VideoConversion\docs\` 另有治理文档：`FVCC_混乱度评价报告_细化版.md`、`TASK_REFERENCE.md`、`IMPROVEMENT_LOG.md`、`SECURITY.md`、`项目分析与改进方向.md`、`FVCC_UI设计优化方向.md`、`FVCC_项目现状与开发步骤.md`（本文档）。
> 设计规格 01~10 位于 `D:\Fnos.VideoConversion\WebVideoEditor_Design\`（代码注释以 `WebVideoEditor_Design/07-§3.2` 形式引用）。

---

## 3. 技术栈

### 3.1 后端（server/）

| 项 | 版本/内容 |
|---|---|
| 语言 | Go 1.27.1（go.mod `go 1.27.1`，需 `GOTOOLCHAIN=auto` 匹配模块缓存工具链；本机 PATH Go 1.27.2 但 build.ps1 走 sdk\go1.25.3 启动器 + auto，交付产物恒由 go.mod 声明的 1.27.1 编译） |
| Web 框架 | gin-gonic/gin v1.12.0 |
| WebSocket | gorilla/websocket v1.5.3 |
| 间接依赖 | goccy/go-yaml、quic-go（gin http3 引入）、golang.org/x/crypto、mongo-driver v2（gin 直接 require） |
| 存储 | SQLite 主载体（P0-2 已落地：modernc.org/sqlite **v1.60.1** 纯 Go 驱动（v1.6.7 自 v1.14.0 升级），运行时 `fvcc.db`，12 张实体表 + schema v1 迁移链）+ JSON 文件回退（settings.json / tasks.json / history_tasks.json / servers.json / profiles.json / projects.json 等）+ `credentials.db` 凭据库（FVCS 侧运行产物）+ `_trash` 回收站目录 |
| 网络形态 | 生产：fnOS 网关 Unix Socket；开发：TCP 127.0.0.1:8088，前缀 `/app/fvcc` |

### 3.2 前端（ui-src/）

| 项 | 版本/内容 |
|---|---|
| 构建 | Vite 8.3（`base='/app/fvcc/'`，`outDir='../app/ui'`，`emptyOutDir: true`） |
| 语言 | TypeScript 7.0 |
| 样式 | TailwindCSS 4.3（CSS-first，`@tailwindcss/postcss`，无 `tailwind.config.js`）；设计令牌真源 `src/style.css`（`--c-*` 为完整 `rgb()` 颜色，`@theme inline` 注册 token 类，`/NN` 透明度编译期 `color-mix`） |
| UI 范式 | 零框架，`el()` 手工 DOM（`src/ui.ts`）；hash 路由（`#/editor/:id`） |
| 图表 | lightweight-charts（指标/图表） |
| 字体 | @fontsource/ibm-plex-mono / ibm-plex-sans（latin 子集，已瘦身） |
| 实时 | WebSocket（`/app/fvcc/ws`）+ SSE（`/video/scan-stream`） |
| 质量门禁 | `npm run check:design` / `check:design:diff`（scripts/check-design.mjs）、`check-versions.ps1` |

### 3.3 构建/CI

| 项 | 内容 |
|---|---|
| 全量构建 | `D:\Fnos.VideoConversion\build.ps1 -Target FVCC`（前端 + 后端 + fnpack 打 fpk） |
| CI | `.github/workflows/fvcc-ci.yml`（paths 限定 FVCC/**）+ `scripts/check-coverage.ps1`（覆盖率门槛 ≥60%，P3-7 关闭：实测 60.7%，第二十六轮） |
| 版本单一来源 | `server/internal/version/VERSION`，构建时 `check-versions.ps1` 校验三处一致（v1.6.5 加固：定位失败 / 读取失败 / 字段缺失 / 版本不一致一律判红，移除 `t.Skipf`；新增 `TestAppVerIsNotPinnedLiteral` 扫硬编码版本字面量；脚本由 `$PSScriptRoot` 推导仓库根、支持 `-RepoRoot` 覆盖） |

---

## 4. 核心模块说明

### 4.1 后端核心模块

| 模块 | 位置 | 职责 |
|---|---|---|
| **api** | `server/internal/api/` | HTTP 全部入口：handlers（settings/video/trash/servers/profiles/tasks/history/projects/render/edl）、router.go 路由挂载、stream.go 票据流、trash_handlers.go 回收站、ratelimit.go 限流、security_roots.go、aliases.go（P2-1 内联 shim 转发符号）、config.go 装配入口（NewHandlers / NewRouter） |
| **store** | `server/internal/store/` | 持久化域（SQLite 主载体 + JSON 回退 + 内存索引 + 脏标记 Flush 批量落盘）：store.go（核心 + 定时落盘）、sqlite.go / sqlite_import.go / sqlite_load.go / sqlite_persist.go（SQLite 迁移四件套）、store_edl.go（EDL 项目）、store_proxy.go（代理任务）、trash.go（回收站，删除移入 `_trash` 同名时间戳后缀）；P3-5（v1.5.8）新增 backup.go（定时/写前/迁移前/回滚前/手动备份 + manifest 验签 + 保留轮转）、migrate.go（data_version 锚点 + 版本化迁移链）、restore.go（快照整包回滚）；`model/models.go` 数据模型域（32K，含 Task/Server/Profile/Settings/Project/EDLClip 等 40+ 类型） |
| **scheduler** | `server/internal/scheduler/` | 调度核心：任务队列分发（RENDER_EDL / GEN_PROXY 共用 dispatchRenderLike）、节点选机（resolveRenderNode）、转码锁 + 编码锁（同节点/同 checksum 互斥）、健康分与失败率记账（B-08）、熔断与 COOLDOWN、重试策略（failRenderTaskWithCooldown / failRenderTaskPermanent） |
| **remote** | `server/internal/remote/` | 与 FVCS 渲染端的 WSS 数据面连接：Hello 能力上报（ParseHelloCaps）、进度推送消费、`RenderDispatcher` 接口（CreateRenderEDLWithTrace / CreateGenProxyWithTrace）、错误码归类（classifyRenderError） |
| **ws** | `server/internal/ws/` | WebSocket Hub：任务进度广播、proxy_ready / node_status / snapshot 事件；限流（ws_limit.go）；SetClock / SetEmitHook 可注入 |
| **security** | `server/internal/security/` | 网关用户中间件（gateway.go，fnOS 网关透传用户）、requireAdmin、凭据加密（crypto.go，DPAPI）、审计日志（audit.go，含 auditDestructive 破坏性操作闭环）、限流核心（ratelimit_core.go：登录失败限流、渲染提交限流）、SMB 路径校验（smb_validate.go）、路径遍历防护 |
| **edl** | `server/internal/edl/` | EDL 载荷校验（与 FVCS 双实现，edl_validate.go）、共享常量、导出包装 |
| **media** | `server/internal/media/` | ffprobe 探测（ffprobe.go，缺二进制时降级 stub）、本地转码（localtranscode.go）、代理工作流（proxy.go + roots.go 三根解析）、流媒体票据与缩略图（stream_media.go） |
| **node** | `server/internal/node/` | 节点选择策略（健康分、熔断、失败率） |
| **protocol** | `server/internal/protocol/` | API 错误码常量（errcode.go）、错误响应契约（apierr.go）、JSON 白名单应用（applyjson.go，P1-1） |
| **version** | `server/internal/version/` | go:embed VERSION 单一来源 |

### 4.2 前端核心模块

| 模块 | 位置 | 职责 |
|---|---|---|
| **main.ts** | `ui-src/src/main.ts` | hash 路由外壳，渲染 7 个页面（tasks/scanner/servers/profiles/history/settings + editor）；全局状态条、命令面板、移动端适配；路由切换调用 dispose 防订阅泄漏（P2-2） |
| **store.ts** | `ui-src/src/store.ts` | 全局响应式单例：tasks/servers/profiles/history/metrics/appInfo/settings/currentProjectId；subscribe/notify；消费 WS 事件（task_update / proxy_ready / node_status / snapshot）；sameList 去重防循环通知 |
| **api.ts** | `ui-src/src/api.ts` | 统一错误契约 `ApiError`（{ok:false, code, msg, detail?}，兼容旧 {error}）；全部 REST + SSE（scan-stream 事件流含 cancel）封装 |
| **ws.ts** | `ui-src/src/ws.ts` | WebSocket 客户端（断线重连、事件分发） |
| **lib/** | `ui-src/src/lib/` | P2-2 组件化 4 件套：`crudActions.ts`（CRUD 四件套 confirmDialog→api→toast→reload）、`useListPage.ts`（列表页公共流程）、`scrollPos.ts`（滚动位置保持）、`formBuilder.ts`（表单 label+input 生成与校验） |
| **editor/** | `ui-src/src/pages/editor/` | 剪辑编辑器，14 个 TS 模块：`index.ts`（路由入口 + 项目选择页 + 面板装配）、`editorStore.ts`（状态机 + 撤销/重做栈 50 步 + 2s 防抖保存 + 重试 [1s,3s,9s] + E_REV_CONFLICT 冲突检测）、`layout.ts`（三段布局，拖拽持久化 wve.layout.v1）、`timeline.ts`（25K 时间线渲染与交互）、`assets.ts`（26K 素材库）、`preview.ts`（17K 预览器，含代理映射）、`clipOps.ts`、`dialog.ts`（渲染导出对话框）、`taskDrawer.ts` / `taskView.ts`、`shortcuts.ts`、`ruler.ts`、`format.ts`（时间码/时长/EDL_LIMITS）、`preset.ts`（渲染预设） |

### 4.3 后端 REST/WS 接口一览（router.go + api.ts）

| 分组 | 接口 |
|---|---|
| 系统 | `GET /info`、`GET /metrics`、`GET /metrics/prometheus`、日志查看/清空 |
| 视频 | `POST /video/scan`、`GET /video/scan-stream`（SSE 流式）、`POST /video/probe`、`GET /video/preview/:path`（票据+Range 流）、`GET /video/thumb/:path`（抽帧缩略图）、`POST /video/rename|move|delete`、清视频缓存 |
| 目录 | `GET /dirs`（目录浏览） |
| 回收站 | `GET /trash`、`POST /trash/restore`、`POST /trash/empty`（P2-5） |
| 服务器 | `GET/POST /servers`、`PUT/DELETE /servers/:id`、`POST /servers/:id/test`（连通性） |
| 方案 | `GET/POST /profiles`、`PUT/DELETE /profiles/:id` |
| 任务 | `GET/POST /tasks`、`PUT/DELETE /tasks/:id`、`POST /tasks/:id/pause|resume|cancel|retry` |
| 历史 | `GET /history`、`POST /history/clear` |
| EDL | `GET/POST /projects`、`GET/PUT/DELETE /projects/:id`、`POST /render/submit`（渲染提交，限流）、`POST /genproxy`（代理生成） |
| 流媒体 | `POST /stream/ticket`、`GET /stream/:ticket` |
| 存储 | `POST /api/store/backup`、`GET /api/store/backups`、`POST /api/store/restore`（v1.5.8，均挂 requireAdmin） |
| WS | `GET /app/fvcc/ws`（task_update / proxy_ready / node_status / snapshot） |

---

## 5. 功能清单（已实现）

| 功能域 | 状态 | 说明 |
|---|---|---|
| 视频素材管理 | ✅ | 目录浏览、SSE 流式扫描、ffprobe 探测、预览（票据 + Range 流）、抽帧缩略图、重命名/移动/删除（回收站化） |
| 转码服务器管理 | ✅ | 渲染节点 CRUD、连通性测试、健康分/熔断、节点状态实时推送 |
| 转码方案管理 | ✅ | 转码方案 CRUD（ffmpeg 参数全量配置）、反射白名单 JSON 应用（P1-1） |
| 任务队列 | ✅ | 创建/排序/暂停/恢复/取消/重试/删除、RENDER_EDL 与 GEN_PROXY 分发、转码锁/编码锁互斥、失败冷却与永久失败分类、WS 实时进度 |
| 历史记录 | ✅ | 历史任务查询、清空（审计） |
| EDL 剪辑项目 | ✅ | 项目 CRUD、单轨时间线（P0 单轨）、编辑器三段布局、撤销/重做、2s 防抖自动保存 + 乐观锁冲突检测、渲染提交（限流） |
| 代理生成工作流 | ✅ | GEN_PROXY 任务、proxy_ready 事件、代理文件映射（04 §4.3） |
| 安全 | ✅ | 网关用户中间件、requireAdmin、登录失败限流、渲染提交限流、凭据 DPAPI 加密、SMB 路径校验、审计日志（含破坏性操作 8 处接入）、回收站 |
| 可观测性 | ✅ | /metrics + Prometheus 导出、ANSI 日志查看/清空、任务/节点/方案指标 |
| 前端工程 | ✅ | 设计令牌体系 + check:design 门禁、组件化 4 件套、命令面板、主题切换、移动端触控适配（≥44px） |
| 打包/部署 | ✅ | fpk 打包、cmd/ 生命周期脚本、版本三处单一来源、CI 流水线 |

---

## 6. 待办与缺口

### 6.1 代码级真实 TODO（需开发处理）

| 位置 | 内容 | 建议 |
|---|---|---|
| ~~`server/internal/store/store.go:19`~~ | ~~`TODO(P1)`：定时备份、配置迁移、快照回滚~~ | ✅ 已关闭（2026-10-11，v1.5.8，P3-5 阶段 A~D，提交 efef4bd/16f1043/59e4004） |
| ~~`server/internal/scheduler/scheduler.go:487/667`~~ | ~~`checkUploadProgress`/`checkDownloadProgress` 空占位~~ | ✅ 已清理（2026-09-21） |
| ~~`server/internal/scheduler/scheduler.go:828`~~ | ~~`TODO(B-06)` RenderDispatcher 未注入死分支~~ | ✅ 已清理（2026-09-21，main.go 已注入） |
| `server/internal/store/model/models.go:463` | Transition 片段转场字段 P0 为 nil 仅占位 | 与 FVCS 同步评估转场支持；已登记为结构性立项「多轨 + EDL 转场」 |
| `ui-src/src/pages/editor/index.ts:24` | `placeholder()` 占位函数 | ✅ 注释已更新为"空态兜底"，功能保留 |
| `server/internal/media/ffprobe.go:49` | ffprobe 缺失时降级 stub 数据 | 运行时环境安装 ffmpeg/ffprobe（NAS 已满足，`/info` ffprobe:true） |

### 6.2 环境依赖缺口

| 项 | 现状 | 动作 |
|---|---|---|
| ffprobe/ffmpeg | 本机实测 ffmpeg/ffprobe **9.0.1-essentials**，PATH 解析到 `D:\YTSage\ffmpeg.exe`；fnOS 运行时已核验（ffprobe:true）。**2026-10-14 裁决：ffmpeg 属另一体系，不纳入本仓库工具链升级口径**（FVCS 经配置项 `ffmpeg_path` 调用外部 ffmpeg，不进 fpk） | 部署/安装脚本补 ffmpeg 依赖 |
| Go 工具链 | go.mod 要求 go 1.27.1；本机 PATH Go 已升 **1.27.2**（2026-10-14），但 `build.ps1` 优先命中 `sdk\go1.25.3` 启动器 + auto，交付产物仍由 **go1.27.1** 编译；fnOS 跑预编译二进制不涉及 | 保持 auto；想让产物吃到 1.27.2 须同步抬 go.mod `go` 指令与 CI `go-version` |

### 6.3 文档缺口

| 项 | 现状 |
|---|---|
| `FVCC/docs/SECURITY.md` | ✅ 已补写（2026-09-21），消除 main.go/models.go/gateway.go 悬空引用 |
| `FVCC/README.md` 版本表 | 已对齐 v1.5.12（2026-10-12 文档全面对齐轮）；后续版本线以 README 自身为准 |
| 上层 `docs/IMPROVEMENT_LOG.md` 等 | 与代码现状定期核对，纳入版本发布 checklist |
| 本份长台账 | 已对齐 v1.6.9（2026-10-14 第三十七轮低风险收尾批次回写）；同日压缩：历史版本快照段落收拢为 §1.0 版本线表 |

### 6.4 清理项（已收口，仅保留待裁决项）

| 项 | 处置 |
|---|---|
| 调试重定向残留 / CDP 缓存 / 打包 stage / 调试脚本与二进制 / 历史 fpk | ✅ 2026-09-21 已清理或归档（18 项，约 560.3MB；server/ 根 0 字节文件随 P3-9 归零，2026-10-12） |
| `FVCC/.commandcode/` 工具产物 | ✅ 已处置（2026-10-14 v1.6.9 第三十七轮）：用户裁决为 AI 工具遗留，追加 `.gitignore` 条目 `FVCC/.commandcode/`（不入库、不删除），`git check-ignore -v` 核验命中 |
| `app/ui/assets` 旧 hash | 保持核对（vite `emptyOutDir:true`，以 index.html 引用为准） |

### 6.5 规划中未触发项（决策门保留）

| 项 | 说明 |
|---|---|
| ~~P1-2 前端收敛（freecut 双轨）~~ | ✅ 已定案关闭（2026-10-11，基线 1.5.8）：方案 A——维持零框架，借 freecut 设计不借底座（本地解码 vs 票据流+NAS 渲染架构冲突） |
| P2-2 决策门 | 组件化收敛已达标；渐进式框架（Svelte 5 试点）评估未触发，改绑「多轨 `{videoTracks,audioTracks}` 立项」时一并评审 |
| CI 覆盖率门槛 | **≥60%**（P3-7 已关闭，实测 60.7%） |

---

## 7. 剪辑页功能改进（实现状态总览）

> 适用范围：`ui-src/src/pages/editor/`（剪辑编辑器，14 个 TS 模块）；功能 1/2/4/5 均为前端层改动，无架构性风险。
> 现状（2026-10-13 复核）：功能 1「视频栏改造」已实现 3/4——标题半透明浮层（`buildClipEl` titleBar）、时长信息移除（移入 tooltip）、随时间滚动指示条（`playBarByClip` + `updatePlayhead`）；**未实现**：视频/音频双栏（仍单轨 v1，无 `{videoTracks,audioTracks}` 数据模型，已登记为结构性立项）。功能 2「ALT+滚轮/双指缩放」已完整实现（`onWheel` 校验 `e.altKey`、`onPinchDown/Move/End`、scale 状态入 editorStore）。功能 4「J/K/L 变速播放」已实现（1.5.4，080754c）。功能 5「素材递归子文件夹扫描」已实现（1.5.7，68ebdec）。

| 功能 | 难度 | 关键点 | 状态 |
|---|---|---|---|
| 1. 视频栏改造 | 低~中 | 标题半透明浮层（纯 CSS）；删除时长信息；随时间滚动指示条（播放进度 + rAF/节流）；改"视频/音频"双栏（唯一涉及数据模型 `{videoTracks,audioTracks}`，后端/渲染端需兼容） | ⚠️ 已实现 3/4（仅双栏未实现） |
| 2. ALT+滚轮 / 双指缩放时间轴 | 高 | wheel 监听 `e.altKey` 以鼠标为锚点缩放；触屏 pointer 双触点距离比值驱动；共用 scale 状态（editorStore），`touch-action:none` | ✅ 已完整实现 |
| 4. 预览变速播放 J/K/L | 中 | preview.ts `setSpeed(dir)` 驱动 `video.playbackRate`（同向 2x→3x→4x、最大 4x、反向重置）；shortcuts.ts 接入 J/K/L；控制按钮图标化 | ✅ 已实现（1.5.4，080754c） |
| 5. 素材递归子文件夹扫描 | 低 | assets.ts `recursive=true` 使子文件夹视频汇入列表；依赖 Windows 盘符根路径边界修复（盘符根 `D:\\` 双分隔符）才能命中子路径 | ✅ 已实现（1.5.7，68ebdec） |

**踩坑预警（基于 v1.2.3 白屏经验）**：指示条回调订阅注意变量声明顺序避免 TDZ 白屏；组件销毁时移除监听防泄漏（复用 P2-2 dispose 模式）。

---

## 8. 下一步开发步骤（状态总览）

> 按"环境就绪 → 数据安全 → 代码清理 → 文档同步 → 质量门禁 → 端到端验收"顺序执行，每步含验收标准。各步骤均已完成收口或按决策门延后；开放项见 §1 开放项清单。

| 步骤 | 状态 |
|---|---|
| 1 运行时环境就绪核验 | ✅ 本机已就绪（GOTOOLCHAIN=auto；ffmpeg/ffprobe 9.0.1-essentials）；部署机待核验 |
| 2 ffprobe 探测真实性 | ✅ 启动自检已存在（main.go `ffprobe=%v`），stub 降级保留；NAS 运行时已满足（`/info` ffprobe:true，2026-10-11） |
| 3 调度器占位与过时注释清理 | ✅ 2026-09-21（RenderDispatcher 注入、死分支/空占位删除、placeholder 注释更新） |
| 4 store 备份/迁移/回滚 | ✅ 2026-10-11，v1.5.8（阶段 A 备份 efef4bd → B 迁移链 16f1043 → C 整包回滚 59e4004 → D 收口；`/api/store/*` 挂 requireAdmin + snapshot.restore 审计；方案稿 `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md`）；**前端入口随 v1.6.9 补齐**（设置页「数据备份与恢复」面板，第三十七轮） |
| 5 文档同步 | ✅ 2026-09-21 首轮 + 2026-10-12 v1.5.12 全量对齐 + 2026-10-13 本份回写 v1.6.5 + 2026-10-14 回写 v1.6.8 并压缩 + 2026-10-14 第三十七轮回写 v1.6.9（4 份根台账 + README/BUILD 版本表） |
| 6 清理低风险残留 | ✅ 2026-09-21（18 项约 560.3MB 入回收站；历史 fpk 归档 archive/fpk/ v1.1.0~v1.5.3） |
| 7 质量门禁与 CI 上调 | ✅ 覆盖率口径修复 56.7%→59.0%→补测 60.7%，门禁 55%→60%（P3-7，第二十六轮）；v1.6.5 校验加固（漂移即红） |
| 8 端到端验收 | 🟡 已执行（2026-10-11，真实 NAS+Windows）：①转码/④回收站/⑤断联 pass；②代理/③EDL 渲染缺陷经 A 档 #5/#6/#10/#11 + #8 四层收口，2026-10-12 真机复验通过并推送 fork/baseline-snapshot；B 档 #7/#9 延后未完成（见 §1 开放项） |

**环境验证命令**：

```powershell
$env:GOTOOLCHAIN = "auto"
cd D:\Fnos.VideoConversion\FVCC\server
go version     # 应解析到 go 1.27.x 而非系统旧版
go build ./... ; go vet ./... ; go test ./...
cd ..\ui-src ; npm ci ; npm run check:design ; npm run build
D:\Fnos.VideoConversion\build.ps1 -Target FVCC   # 全量打包回归，产出新 fpk
```

**端到端走查闭环**（每项需 WS 实时进度可观察）：①素材→转码（扫描→probe→创建→下发 FVCS→RUNNING→完成→历史落库）②代理闭环（GEN_PROXY→proxy_ready→编辑器用代理）③EDL 渲染闭环（新建项目→加片段→保存乐观锁→渲染提交限流→进度→成品落库）④回收站闭环（删除→`_trash`→恢复/清空→审计）⑤异常路径（断网重连、节点宕机熔断+COOLDOWN、并发保存 E_REV_CONFLICT）。

---

## 附：调查证据索引

- 代码规模：Go 110 文件 / 26,380 行（非测试 58 / 17,197 + 测试 52 / 9,183，本轮零后端改动）；TS 33 文件 / 11,622 行（2026-10-14 第三十七轮实测，v1.6.9 后 +148）
- 版本核验：三真源一致 = **1.6.9**（2026-10-14 第三十七轮 bump 后实测，`check-versions.ps1` 绿；`ui-src/package-lock.json` 根 version 已同步；FVCS `pkg/version/VERSION` = 1.3.1）；**v1.6.9 已提交（主体 `dedd124` + 本后记笔）、已出 fpk（11,014,297 B）、注解 tag 打在后记笔**；1.6.x 已打注解 tag（v1.6.0→b1a0d93、v1.6.4→b007a3d、v1.6.5→5166cbe、v1.6.6→1dcb774、v1.6.7→4154d31、v1.6.8→65c79e4 即 `e1ed35c` 批次的后记笔）；推送目标 `fork` = hgmmm25/Fnos.VideoConversion，分支 `baseline-snapshot`（另有 `origin` = zdongno01 非推送目标）
- 覆盖率：60.7%（第二十六轮实测，门禁 60%）
- TODO 扫描：`server/`、`ui-src/src/`、`cmd/` 全量关键词扫描；1.6.x 遗留项均为刻意未做或待裁决，非新增代码级 TODO
- 关键文件：`server/main.go`、`server/internal/api/router.go`、`server/internal/scheduler/scheduler.go`、`server/internal/store/store.go`、`ui-src/src/main.ts`、`ui-src/src/store.ts`、`ui-src/src/api.ts`、`ui-src/src/pages/editor/*.ts`
