---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 839b5d1d4fff15220193598838e6072d_965a8716b4f811f1a816525400cd780f
    ReservedCode1: 0KILMEVm3062Le9Y4rRmQXawhGQGWfAF1yt4FXRFKkO/n5Y/YAwLUjZ3r5lrPXsIIKI6+PONrLYBfry287M/j1qiKyTMPdF3AWzzi0FdF3zc3qvfBdUnnwilsVYNZsgey17iLpHV/yGmwHY+7npJYUJa/MGSXCsksEpYkGvbSYMWPvhIDbUtx37mYZg=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 839b5d1d4fff15220193598838e6072d_965a8716b4f811f1a816525400cd780f
    ReservedCode2: 0KILMEVm3062Le9Y4rRmQXawhGQGWfAF1yt4FXRFKkO/n5Y/YAwLUjZ3r5lrPXsIIKI6+PONrLYBfry287M/j1qiKyTMPdF3AWzzi0FdF3zc3qvfBdUnnwilsVYNZsgey17iLpHV/yGmwHY+7npJYUJa/MGSXCsksEpYkGvbSYMWPvhIDbUtx37mYZg=
---



# FVCC 项目现状与开发步骤

> 调查日期：2026-09-20
> 调查范围：`D:\Fnos.VideoConversion\FVCC`（视频转码调度端 Web 客户端，fnNAS 应用）
> 数据来源：实际目录扫描 + 源码阅读（server 后端 / ui-src 前端 / docs 文档），非记忆推断。

---

## 1. 现状概述

FVCC（Fnos Video Conversion Client）是 fnNAS 生态的视频转码调度端 Web 客户端：浏览本地素材、把转码/代理/渲染任务下发给 FVCS 渲染端、通过 WebSocket 实时跟踪进度。服务端 Go（gin），前端 Vite + TypeScript 零框架原生 DOM，产物以 fpk 形式打包供 fnOS 安装。

当前版本 **v1.4.9**（manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，2026-09-20 实测复核）。

治理进展：P1 组（P1-1 ~ P1-5）全部落地或部分落地；P2 组（P2-1 ~ P2-8）全部收口（后端 internal 分层、前端组件化、CI 流水线、API 契约统一、回收站化、AIGC 清理、测试命名规范化、破坏性操作审计）。设计文档引用速查见 `FVCC/README.md`，治理台账见 `docs/FVCC_混乱度评价报告_细化版.md`。

**总体判断**：项目主干功能完整、工程质量较高（分层清晰、测试齐全、契约规范），处于"功能已闭环、局部待打磨"阶段。真实待办集中在：store 持久化健壮性（定时备份/配置迁移/快照回滚）、ffprobe 运行时环境依赖、少数占位/防御性代码清理、以及文档同步滞后。

### 1.1 代码规模（实测）

| 维度 | 文件数 | 行数 | 说明 |
|---|---|---|---|
| Go 后端（非测试） | 49 | 13,107 | `server/` 根包 + `internal/` 11 包 + `logger/` + `smbshare/` |
| Go 测试 | 38 | 6,208 | 含 e2e / 并发 / 迁移回归 |
| TypeScript 前端 | 33 | 10,035 | `ui-src/src/` 全部页面与逻辑 |
| **合计** | **120** | **29,350** | 不含 node_modules、构建产物、fpk 包 |

### 1.2 构建产物与运行形态

- 根目录：`fvcc.exe`（本地构建）、`fvcs-service.exe`（FVCS 渲染端二进制）、历史 fpk 包（v1.4.0 ~ v1.4.9，10 个）+ 最近打包产物 `fvcc.fpk`、`manifest`（应用清单，version=1.4.9）。
- 运行时数据：`config.enc.json`（加密配置）、`master.key` / `secret.key`（凭据密钥）、`credentials.db`（凭据库 SQLite，含 -shm/-wal）、`tasks.json` / `history_tasks.json`（任务存储）、`locks.json`（锁表）、`task.db`（疑似早期 SQLite 遗留，当前存储为 JSON 文件，需确认是否仍被引用）。
- `config/`：fnOS 应用配置目录（privilege / resource）。
- `logs/`：运行时日志（按日期 + app_*.log）。
- `app/`：前端构建产物（vite 输出 + 静态资源），由 fpk 打包；`app/ui/assets` 为 hash 资源目录（当前 13 个文件）。
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
│   │   ├── store/             # JSON 文件存储 + model 数据模型域（store.go、store_edl.go、store_proxy.go、trash.go、model/models.go 32K）
│   │   ├── version/           # 版本信息（go:embed VERSION 单一来源）
│   │   └── ws/                # WebSocket Hub 推送与限流（ws.go、ws_limit.go）
│   ├── logger/                # 日志
│   ├── smbshare/              # SMB 共享能力
│   ├── temp/                  # 调试重定向残留（build*/commit*/fmt_out.txt 等，见 §6.4）
│   └── go.mod / go.sum
├── ui-src/                    # 前端源码（Vite + TS，零框架 el() 手工 DOM）
│   ├── src/
│   │   ├── main.ts            # hash 路由 + 7 页面挂载 + 全局状态条 + 命令面板
│   │   ├── api.ts             # 统一 ApiError 契约 + 全部 REST/SSE 封装（254 行）
│   │   ├── ws.ts              # WebSocket 客户端
│   │   ├── store.ts           # 全局响应式单例（tasks/servers/profiles/history/metrics/settings/currentProjectId）
│   │   ├── command.ts         # 命令面板
│   │   ├── theme.ts           # 主题切换
│   │   ├── types.ts / ui.ts / style.css / vite-env.d.ts
│   │   ├── lib/               # P2-2 组件化成果：crudActions / formBuilder / scrollPos / useListPage
│   │   └── pages/             # tasks / scanner / servers / profiles / history / settings + editor/
│   │       └── editor/        # 剪辑编辑器 14 个 TS 模块（见 §4.2）
│   ├── scripts/               # check-design.mjs、check-versions.ps1 等门禁脚本
│   ├── public/                # config + images（vite 构建时复制回 app/ui，禁止误删）
│   └── package.json / vite.config.ts / tailwind.config.js / postcss.config.js / tsconfig.json
├── cmd/                       # fnOS 应用生命周期脚本（install / upgrade / uninstall / config，各含 init/callback）
├── config/                    # fnOS 应用配置（privilege / resource）
├── app/                       # 前端构建产物（git 忽略，vite outDir）
├── docs/                      # 项目内文档
│   ├── API_CONTRACT.md        # 前后端 API 契约（错误码、路由、载荷）
│   ├── DESIGN.md              # 前端设计契约（Named Rules + check:design 门禁）
│   ├── PRODUCT.md             # 产品定位与反参照
│   ├── P2-1_后端分层_阶段A实施记录.md
│   └── P2-2_细化细则.md
├── logs/                      # 运行时日志（git 忽略）
├── temp/                      # 打包 stage（fpk_stage*、CDP 调试缓存、调试脚本，git 忽略）
├── manifest                   # fnOS 应用清单（UTF-8 无 BOM，version=1.4.9）
├── config.enc.json            # 加密配置（凭据相关）
├── credentials.db / -shm / -wal   # 凭据库（SQLite）
├── master.key / secret.key    # 凭据密钥
├── locks.json / tasks.json / history_tasks.json / task.db   # 运行时数据
├── fvcc.exe / fvcs-service.exe / fvcc.fpk / FVCC_v1.4.0~1.4.9_fnos_x86.fpk   # 构建产物
└── .gitignore
```

> 上层目录 `D:\Fnos.VideoConversion\docs\` 另有治理文档：`FVCC_混乱度评价报告_细化版.md`、`TASK_REFERENCE.md`、`IMPROVEMENT_LOG.md`、`SECURITY.md`、`项目分析与改进方向.md`、`FVCC_UI设计优化方向.md`、`FVCC_项目现状与开发步骤.md`（本文档）。
> 设计规格 01~10 位于 `D:\Fnos.VideoConversion\WebVideoEditor_Design\`（代码注释以 `WebVideoEditor_Design/07-§3.2` 形式引用）。

---

## 3. 技术栈

### 3.1 后端（server/）

| 项 | 版本/内容 |
|---|---|
| 语言 | Go 1.27.1（go.mod `go 1.27.1`，需 `GOTOOLCHAIN=auto` 匹配模块缓存工具链） |
| Web 框架 | gin-gonic/gin v1.12.0 |
| WebSocket | gorilla/websocket v1.5.3 |
| 间接依赖 | goccy/go-yaml、quic-go（gin http3 引入）、golang.org/x/crypto、mongo-driver v2（gin 直接 require） |
| 存储 | JSON 文件原子写（settings.json / tasks.json / history_tasks.json / servers.json / profiles.json / projects.json 等）+ `credentials.db` 凭据库 + `_trash` 回收站目录 |
| 网络形态 | 生产：fnOS 网关 Unix Socket；开发：TCP 127.0.0.1:8088，前缀 `/app/fvcc` |

### 3.2 前端（ui-src/）

| 项 | 版本/内容 |
|---|---|
| 构建 | Vite 8.3（`base='/app/fvcc/'`，`outDir='../app/ui'`，`emptyOutDir: true`） |
| 语言 | TypeScript 7.0 |
| 样式 | TailwindCSS 3.4 + PostCSS；设计令牌真源 `src/style.css`（`--c-*` / `--ease-*`） |
| UI 范式 | 零框架，`el()` 手工 DOM（`src/ui.ts`）；hash 路由（`#/editor/:id`） |
| 图表 | lightweight-charts（指标/图表） |
| 字体 | @fontsource/ibm-plex-mono / ibm-plex-sans（latin 子集，已瘦身） |
| 实时 | WebSocket（`/app/fvcc/ws`）+ SSE（`/video/scan-stream`） |
| 质量门禁 | `npm run check:design` / `check:design:diff`（scripts/check-design.mjs）、`check-versions.ps1` |

### 3.3 构建/CI

| 项 | 内容 |
|---|---|
| 全量构建 | `D:\Fnos.VideoConversion\build.ps1 -Target FVCC`（前端 + 后端 + fnpack 打 fpk） |
| CI | `.github/workflows/fvcc-ci.yml`（paths 限定 FVCC/**）+ `scripts/check-coverage.ps1`（覆盖率门槛 ≥55%，基线 56.1%） |
| 版本单一来源 | `server/internal/version/VERSION`，构建时 `check-versions.ps1` 校验三处一致 |

---

## 4. 核心模块说明

### 4.1 后端核心模块

| 模块 | 位置 | 职责 |
|---|---|---|
| **api** | `server/internal/api/` | HTTP 全部入口：handlers（settings/video/trash/servers/profiles/tasks/history/projects/render/edl）、router.go 路由挂载、stream.go 票据流、trash_handlers.go 回收站、ratelimit.go 限流、security_roots.go、aliases.go（P2-1 内联 shim 转发符号）、config.go 装配入口（NewHandlers / NewRouter） |
| **store** | `server/internal/store/` | JSON 文件持久化（原子写）；EDL 项目（store_edl.go）、代理任务（store_proxy.go）、回收站（trash.go，删除移入 `_trash` 同名时间戳后缀）；`model/models.go` 数据模型域（32K，含 Task/Server/Profile/Settings/Project/EDLClip 等 40+ 类型） |
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
| **editor/** | `ui-src/src/pages/editor/` | 剪辑编辑器，14 个 TS 模块：`index.ts`（606 行：路由入口 + 项目选择页 + 面板装配）、`editorStore.ts`（452 行：状态机 + 撤销/重做栈 50 步 + 2s 防抖保存 + 重试 [1s,3s,9s] + E_REV_CONFLICT 冲突检测 + 订阅切片）、`layout.ts`（三段布局：顶栏/素材库+预览器/时间线，拖拽持久化 wve.layout.v1，预设 default/compact/preview）、`timeline.ts`（25K 时间线渲染与交互）、`assets.ts`（26K 素材库）、`preview.ts`（17K 预览器，含代理映射）、`clipOps.ts`（片段操作）、`dialog.ts`（渲染导出对话框）、`taskDrawer.ts` / `taskView.ts`（任务抽屉/视图）、`shortcuts.ts`（快捷键）、`ruler.ts`（标尺）、`format.ts`（时间码/时长/EDL_LIMITS）、`preset.ts`（渲染预设） |

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

> 2026-09-20 复核：以下代码级 TODO 经全量关键词扫描确认**全部仍存在**，无新关闭项。

### 6.1 代码级真实 TODO（需开发处理）

| 位置 | 内容 | 影响 | 建议 |
|---|---|---|---|
| `server/internal/store/store.go:19` | `TODO(P1)`：定时备份、配置迁移、快照回滚 | JSON 文件存储无备份机制，升级或误操作后不可回滚 | 高优先级：实现备份/迁移/快照三件套（见 §8 步骤 4） |
| `server/internal/scheduler/scheduler.go:487 / 667` | `checkUploadProgress` / `checkDownloadProgress` 为空操作占位 | 上传/下载阶段的进度不做主动轮询（仅依赖远端推送推进阶段） | 确认远端是否推送阶段内进度；若无通道则明确注释或移除占位 |
| `server/internal/scheduler/scheduler.go:828` | `TODO(B-06)` 注释：RenderDispatcher 未注入时保持 QUEUE | 经查 `main.go` 已注入 `scheduler.SetRenderDispatcher`，该分支为**防御性死代码**，非功能缺口 | 清理过时注释 + 防注入守卫（可选），不阻塞功能 |
| `server/internal/store/model/models.go:463` | Transition 片段转场字段 P0 为 nil 仅占位固化 JSON 形状 | EDL 转场能力未启用，数据结构已预留 | 与 FVCS 渲染端同步评估转场支持（跨端特性，建议单独立项） |
| `ui-src/src/pages/editor/index.ts:24` | `placeholder()` 占位函数（"各面板接入前的占位说明"） | assets/preview/timeline 真实面板已接入，placeholder 现仅用于空态兜底与未启用面板 | 保留为空态兜底即可，注释可更新为"空态兜底" |
| `server/internal/media/ffprobe.go:49` | ffprobe 二进制缺失时降级 stub 数据 | 探测结果非真实（功能可用但数据失真） | 运行时环境安装 ffmpeg/ffprobe（见 §8 步骤 1） |

### 6.2 环境依赖缺口

| 项 | 现状 | 动作 |
|---|---|---|
| ffprobe/ffmpeg | 本机构建机与 fnOS 运行时若未安装，探测走 stub | 部署/安装脚本补 ffmpeg 依赖；`check-versions.ps1` 或启动自检提示 |
| Go 工具链 | go.mod 要求 go 1.27.1，本机 Go 分散，需 `GOTOOLCHAIN=auto` 从模块缓存/代理拉取 | 保持 auto；勿依赖固定路径（历史踩坑：台账路径 `D:\fntv-tools\go\bin\go.exe` 已失效、系统 Go std 不完整） |

### 6.3 文档缺口

| 项 | 现状 | 动作 |
|---|---|---|
| `FVCC/docs/SECURITY.md` | **不存在**（2026-09-20 复核仍缺），但 `server/internal/security/gateway.go` 注释引用 `docs/SECURITY.md` | 按 07 号安全设计规格补写（网关鉴权/凭据加密/审计/限流/路径约束） |
| `FVCC/README.md` 版本表 | 「版本号单一来源」表仍写 **1.4.4**（2026-09-20 复核），实际三处已同步 **1.4.9** | 更新 README 版本表 |
| `FVCC/README.md` 目录树 | 未列出 `ui-src/src/lib/`（P2-2 新增）与 `FVCC/docs/` 下 P2-1/P2-2 文档 | 同步目录树 |
| 上层 `docs/IMPROVEMENT_LOG.md` 等 | 与代码现状需定期核对（如 P2 状态表） | 纳入版本发布 checklist |

### 6.4 清理项（低风险，不阻塞功能）

| 项 | 位置 | 说明 |
|---|---|---|
| 调试重定向残留 | `server/temp/` 下 `build*.txt` / `commit*.txt` / `fmt_out.txt`（61KB）/ `c2~c6.txt` / `t.txt` / `v.txt` 等 | 历史构建/提交重定向残留（含 0 字节与非 0 字节），移入回收站 |
| CDP 调试缓存 | `temp/chrome-profile-9223/`、`temp/chrome-profile-9224/` | 调试用 Chrome 用户数据，确认无用时清理 |
| 打包 stage 残留 | `temp/fpk_stage/`、`fpk_stage_145/146/147/149/` | 历史打包 stage 目录，确认无用时清理 |
| 调试脚本/二进制 | `temp/cdp-fine-test.mjs`、`cdp-real-test.mjs`、`fvcc-win-debug.exe`、`temp/repro-data/`、`temp/secret.key`、`temp/locks.json` | 调试期产物，确认无用时清理 |
| 历史 fpk 包 | FVCC 根目录 `FVCC_v1.4.0_fnos_x86.fpk` ~ `FVCC_v1.4.9_fnos_x86.fpk`（10 个，约 89MB） | 确认 fnOS 安装机制不再需要历史包后归档/清理 |
| app/ui/assets 旧 hash | `FVCC/app/ui/assets/`（当前 13 文件） | 以 index.html 引用为准核对；vite 现配置 `emptyOutDir:true`，新构建后核对无多余 hash 资源 |

### 6.5 规划中未触发项（决策门保留）

| 项 | 说明 |
|---|---|
| P2-2 决策门 | 组件化收敛已达标（样板残留 0），渐进式框架（Svelte 5 试点）评估**未触发**，作为远期选项保留 |
| CI 覆盖率门槛 | 当前 ≥55%（基线 56.1%），计划后续上调 60% |

---

## 7. 剪辑页功能改进可行性方案（精简）

> 适用范围：`ui-src/src/pages/editor/`（剪辑编辑器，14 个 TS 模块）；三个功能均为前端层改动，无架构性风险。
> 排期建议：**功能 3 → 功能 2 → 功能 1**（功能 1 涉及数据结构变更，需先确认后端/渲染端是否依赖单轨结构）。

| 功能 | 难度 | 关键点 |
|---|---|---|
| 1. 视频栏改造 | 低~中 | 标题半透明浮层（纯 CSS）；删除时长信息（注意联动引用）；随时间滚动指示条（监听播放进度 + rAF/节流绘制）；改"视频/音频"双栏（唯一涉及数据模型 `{videoTracks,audioTracks}`，后端/渲染端需兼容） |
| 2. ALT+滚轮 / 双指缩放时间轴 | 高 | wheel 监听 `e.altKey` 以鼠标为锚点缩放；触屏 pointer 双触点距离比值驱动；共用 scale 状态（建议放 editorStore），`touch-action:none` |
| 3. 双击大区块顶部 5% 全窗口化 | 高 | 统一挂 dblclick 判断 `offsetY/offsetHeight<0.05`；class 切换 fixed 全屏；抽公共函数 `isDoubleClickOnHeader(e)`；Esc 退出 |

**踩坑预警（基于 v1.2.3 白屏经验）**：指示条回调订阅注意变量声明顺序避免 TDZ 白屏；组件销毁时移除监听防泄漏（复用 P2-2 dispose 模式）。

---

## 8. 下一步开发步骤

按"环境就绪 → 数据安全 → 代码清理 → 文档同步 → 质量门禁 → 端到端验收"顺序执行，每步含验收标准。

### 步骤 1：运行时环境就绪核验（约 0.5 天）

1. 在 fnOS 部署机安装 ffmpeg/ffprobe，确认 `which ffprobe` 有输出；本机构建机同验。
2. 验证 Go 工具链：在 `D:\Fnos.VideoConversion\FVCC\server` 执行：
   ```powershell
   $env:GOTOOLCHAIN = "auto"
   go version   # 应解析到 go 1.27.x 而非系统旧版
   ```
3. 前端依赖：`cd D:\Fnos.VideoConversion\FVCC\ui-src && npm ci`。
4. **验收**：`go build ./...`、`go test ./...`、`npm run build` 均通过；`ffprobe -version` 正常。

### 步骤 2：验证 ffprobe 探测真实性（约 0.5 天）

1. 启动 dev server（`cd server && go run .`），调用 `POST /app/fvcc/api/video/probe` 传入真实视频路径。
2. 对比返回 `VideoInfo`（分辨率/时长/编码）与 `ffprobe` 直接输出是否一致；若返回 stub 数据（如默认 1080p），说明运行时缺 ffprobe。
3. **验收**：探测结果与 ffprobe 一致；缺 ffprobe 时启动日志有明确告警（若无，需在 main.go 装配阶段加启动自检提示）。

### 步骤 3：清理调度器占位与过时注释（约 0.5 天）

1. `server/internal/scheduler/scheduler.go:828`：确认 `main.go` 已注入 RenderDispatcher 后，删除 TODO(B-06) 注释与 `s.dispatcher == nil` 防注入分支（或改为启动时断言）。
2. `server/internal/scheduler/scheduler.go:487/667`：核对 remote 进度推送协议是否含上传/下载阶段内进度；若含则实现 checkUploadProgress/checkDownloadProgress 消费；若不含则删除空函数并注释说明进度来源。
3. `ui-src/src/pages/editor/index.ts:24`：把 placeholder 注释由"接入前占位"改为"空态兜底"。
4. **验收**：`go vet ./...` + `go test ./...` 全绿；编辑器空态显示正常。

### 步骤 4：实现 store 备份/配置迁移/快照回滚（P1 TODO，约 2~3 天，🔴 涉及数据写入，需先评审）

1. 在 `server/internal/store/` 新增 `backup.go`：
   - 定时备份：每日/每周将 `settings.json`、`servers.json`、`profiles.json`、`tasks.json`、`projects.json` 等原子打包到授权根下 `_backup/`（时间戳后缀，保留最近 N 份）。
   - 配置迁移：`model/models.go` 增加 `schema_version` 字段；启动时执行版本化 `migrate(from, to)` 迁移链。
   - 快照回滚：新增管理接口（建议 `POST /admin/snapshot`、`POST /admin/restore`），restore 前自动先备份当前状态。
2. 路由挂载到 `server/internal/api/router.go`；破坏性恢复操作接入 `security_audit.go` 的 `auditDestructive`（新增 `destructive.restore` 类型）。
3. **验收**：模拟损坏 settings.json → 回滚快照恢复；连续 3 次备份仅保留最新 N 份；`go test ./...` 含备份/迁移/回滚单测。

### 步骤 5：文档同步（约 0.5 天）

1. 按 `WebVideoEditor_Design/07-安全校验与凭据管理细则.md` 补写 `FVCC/docs/SECURITY.md`（网关鉴权、requireAdmin、DPAPI 凭据、限流、审计、路径约束），消除 gateway.go 注释悬空引用。
2. 更新 `FVCC/README.md`：
   - 「版本号单一来源」表 1.4.4 → 1.4.9；
   - 目录树补 `ui-src/src/lib/` 与 `FVCC/docs/` 下 P2-1/P2-2 文档；
   - 治理状态表追加本次变更记录。
3. **验收**：README 三处版本与 `manifest` / `package.json` / `VERSION` 一致；`npm run check:design` 不回归。

### 步骤 6：清理低风险残留（约 0.5 天，🟡 删除类操作逐项确认）

1. `server/temp/` 调试重定向残留（build*/commit*/fmt_out.txt 等）→ 移入回收站。
2. `temp/chrome-profile-9223/9224` CDP 缓存 → 确认无调试需求后移入回收站。
3. `temp/fpk_stage*`（145/146/147/149 等）与 `temp/cdp-*.mjs` / `fvcc-win-debug.exe` / `repro-data/` / `secret.key` / `locks.json` → 确认无调试需求后清理。
4. 以 `FVCC/app/ui/index.html` 引用为准，核对 `app/ui/assets/` 是否存在未被引用的旧 hash 资源，清理多余文件（注意：**只清理未被 index.html 引用的旧 hash 文件**，防止历史 emptyOutDir:false 产物膨胀 fpk）。
5. 历史 fpk 包（v1.4.0~v1.4.9，10 个）：确认 fnOS 应用市场/安装脚本是否需要保留，不需要则归档到独立目录或删除。
6. **验收**：删除走回收站；`npm run build` 后重新核对 assets 无多余 hash 残留。

### 步骤 7：质量门禁与 CI 上调（约 1 天）

1. 后端全量：`cd server && go build ./... && go vet ./... && go test ./...`（单测约 90s+，全绿为准）。
2. 前端门禁：`cd ui-src && npm run check:design && npm run build`。
3. 覆盖率：`scripts/check-coverage.ps1` 核对当前覆盖率；若 ≥60%，把 `.github/workflows/fvcc-ci.yml` 门槛从 55% 上调 60% 并提交。
4. 全量打包回归：`D:\Fnos.VideoConversion\build.ps1 -Target FVCC` 产出新 fpk。
5. **验收**：三项全绿；CI 流水线跑通；fpk 产出成功。

### 步骤 8：端到端验收（约 1 天）

按以下闭环走查，每项需 WS 实时进度可观察：
1. **素材→转码**：扫描目录 → probe 真实数据 → 创建转码任务 → 调度下发 FVCS → RUNNING → 完成 → 历史记录落库。
2. **代理闭环**：创建 GEN_PROXY 任务 → proxy_ready 事件 → 编辑器预览使用代理文件。
3. **EDL 渲染闭环**：编辑器（#/editor）新建项目 → 添加片段 → 保存（观察 saved 状态与乐观锁）→ 渲染导出提交（限流生效）→ 进度条 → 成品路径落库。
4. **回收站闭环**：删除视频 → `_trash` 出现 → 恢复/清空 → 审计日志记录 destructive 操作。
5. **异常路径**：断网重连（WS 重连）、渲染节点宕机（熔断 + COOLDOWN）、并发保存（E_REV_CONFLICT 提示）。
6. **验收**：全部闭环通过，输出走查记录追加到 `docs/IMPROVEMENT_LOG.md`。

---

## 附：调查证据索引

- 目录树：`D:\Fnos.VideoConversion\FVCC`（深度 3 递归扫描，2026-09-20）
- 代码规模：Go 87 文件 / 19,315 行（含测试 38 文件 / 6,208 行）；TS 33 文件 / 10,035 行
- 版本核验：`manifest` / `ui-src/package.json` / `server/internal/version/VERSION` 三处一致 = 1.4.9
- TODO 扫描：`server/`、`ui-src/src/`、`cmd/` 全量关键词扫描（TODO/FIXME/未完成/占位/stub）
- 关键文件：`server/main.go`、`server/internal/api/router.go`、`server/internal/scheduler/scheduler.go`、`server/internal/store/store.go`、`ui-src/src/main.ts`、`ui-src/src/store.ts`、`ui-src/src/api.ts`、`ui-src/src/pages/editor/*.ts`
- 文档：`FVCC/README.md`、`FVCC/docs/*.md`、`D:\Fnos.VideoConversion\docs\*`
*（内容由AI生成，仅供参考）*
