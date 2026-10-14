# FVCC — 视频转码调度 Web 客户端（fnNAS 应用）

面向 fnNAS 的 Web 调度端：浏览本地素材、下发转码任务到 FVCS 渲染端、实时跟踪进度。服务端 Go（gin），前端 Vite + TypeScript（无框架原生 DOM）。

## 目录结构

```
FVCC/
├── server/              # Go 后端（单模块 fvcc）
│   ├── main.go              # 入口与装配（组件注入、优雅退出）
│   ├── internal/            # 分层内部包（P2-1 后端分层收口）
│   │   ├── api/             # HTTP 处理器组/路由/票据流/缩略图/回收站/限流/备份回滚（handlers*、router、stream、trash、backup_handlers、security_roots、hashutil、ratelimit）
│   │   ├── edl/             # EDL 载荷校验（与 FVCS 双实现）
│   │   ├── media/           # ffprobe 探测 / 本地转码 / 代理流程 / 流媒体与缩略图工具
│   │   ├── node/            # 节点选择策略
│   │   ├── pathutil/        # 根边界判定 WithinRoot 单一真源（v1.5.8 收口）
│   │   ├── protocol/        # API 错误码与 JSON 应用 helper（apierr/applyjson/errcode）
│   │   ├── remote/          # FVCS 远程连接（WSS 数据面）与渲染分发
│   │   ├── scheduler/       # 任务调度（选机、健康分、熔断）
│   │   ├── security/        # 凭据加密/审计/网关鉴权/限流核心/路径校验
│   │   ├── store/           # SQLite 主载体 + JSON 备份/迁移/快照回滚 + model 数据模型域（backup/migrate/restore/sqlite*）
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
│   ├── scripts/             # check-design.mjs 设计门禁脚本（仓库根 scripts/ 另有 check-versions.ps1 / check-coverage.ps1）
│   └── public/              # config + images（vite 构建时复制回 app/ui，禁止误删）
├── docs/                # 项目内文档
│   ├── API_CONTRACT.md      # 前后端 API 契约
│   ├── DESIGN.md            # 设计契约（Named Rules + check:design 门禁）
│   ├── PRODUCT.md           # 产品定位与反参照
│   ├── SECURITY.md          # 数据面安全策略（威胁模型/鉴权/凭据/限流/审计/路径校验）
│   ├── STORE_PERSISTENCE_ROADMAP.md # store 备份/迁移/快照回滚方案与落地记录（v1.5.6~1.5.8）
│   ├── P2-1_后端分层_阶段A实施记录.md # P2-1 分层实施留档
│   └── P2-2_细化细则.md     # P2-2 前端组件化细则留档（含 P1-2 决策门定案）
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
| 01 | [01-产品需求与交互规格.md](../../WebVideoEditor_Design/01-产品需求与交互规格.md) | 产品需求、交互规格 |
| 02 | [02-前端架构与时间线编辑器设计.md](../../WebVideoEditor_Design/02-前端架构与时间线编辑器设计.md) | 前端架构、时间线编辑器 |
| 03 | [03-EDL数据模型与接口契约.md](../../WebVideoEditor_Design/03-EDL数据模型与接口契约.md) | EDL 数据模型、接口契约、错误码 |
| 04 | [04-预览网关与代理工作流设计.md](../../WebVideoEditor_Design/04-预览网关与代理工作流设计.md) | 预览网关、代理工作流 |
| 05 | [05-RenderEDL渲染引擎与FFmpeg命令构造器设计.md](../../WebVideoEditor_Design/05-RenderEDL渲染引擎与FFmpeg命令构造器设计.md) | 渲染引擎、FFmpeg 命令构造 |
| 06 | [06-调度持久化与渲染节点管理设计.md](../../WebVideoEditor_Design/06-调度持久化与渲染节点管理设计.md) | 调度持久化、节点管理 |
| 07 | [07-安全校验与凭据管理细则.md](../../WebVideoEditor_Design/07-安全校验与凭据管理细则.md) | 安全校验、凭据管理、路径约束 |
| 08 | [08-P0实施计划与验收清单.md](../../WebVideoEditor_Design/08-P0实施计划与验收清单.md) | P0 实施计划、验收清单 |
| 09 | [09-附录-部署与迁移说明.md](../../WebVideoEditor_Design/09-附录-部署与迁移说明.md) | 部署与迁移 |
| 10 | [10-实施进度与下一步.md](../../WebVideoEditor_Design/10-实施进度与下一步.md) | 实施进度台账 |

注释引用示例：`// 规则唯一来源：WebVideoEditor_Design/07-安全校验与凭据管理细则.md §3.2` → 即上表 07 文档第 3.2 节。

## 版本号单一来源

升级版本必须**三处同步**（构建时以 `server/internal/version/VERSION` 为准，仓库根 `scripts/check-versions.ps1` 自动校验）：

| 文件 | 字段 | 当前值 |
|------|------|--------|
| `manifest` | `version` | 1.6.9 |
| `ui-src/package.json` | `version` | 1.6.9 |
| `server/internal/version/VERSION` | 文件内容 | 1.6.9 |

配套渲染节点 FVCS 版本见仓库根 `FVCS/pkg/version/VERSION`（当前 1.3.1）。

## 近期版本线摘要（1.5.4 ~ 1.6.9）

| 版本 | 内容 |
|------|------|
| 1.5.4 | 编辑器预览变速播放（J/K/L 2x~4x）+ 按钮图标化 |
| 1.5.5 ~ 1.5.7 | Windows 盘符根路径边界修复、剪辑页递归扫描与多根兜底、roots 回落 |
| 1.5.8 | P3-5 持久化收口：SQLite 主载体（JSON 一次性导入）+ 定时/写前/手动备份 + 版本化迁移链 + 快照回滚（`/api/store/*` 后端 API）；`pathutil.WithinRoot` 单一真源 |
| 1.5.9 | 代理就绪态服务端回填（`GET /api/proxy`）+ 帧率校验数值近似；非默认根代理提交带素材根；工具栏批量删除；FVCS 重连 Hello 自动收口 online；未映射绝对 sourceRoot 提交侧早失败 |
| 1.5.10 | 渲染取根跨端对齐代理（#8 返工） |
| 1.5.11 | 渲染取根「有界单根」：`EDLClip.sourceRoot` 透传 + `resolveRenderRoots`；FVCS `makeSrcResolver` 对齐 `BuildSMBPath`（配套 FVCS v1.2.5） |
| 1.5.12 | RENDER_EDL 终态轮询收口（`pollRenderTerminal`/`completeRenderTask`，修出片后卡「收尾·100%」）；`validateRootRelPath` 与 FVCS `ValidateRootPath` 严格同规则（配套 FVCS v1.2.6） |
| 1.6.0 | 视觉打磨版（方向 A 精修冷灰）：令牌层重排 / selectField·checkbox·按钮变体 / 导航分组·搜索胶囊·状态栏 / V1 趋势图修复·V5-V8 逐页 / 微交互 + WCAG 对比度验收（配套 FVCS v1.3.0：Fyne 托盘 GUI 纳入构建 `-Target FVCS-UI`，主题贴近 FVCC 蓝调） |
| 1.6.4 | 任务页视觉改进三批次（方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`，GLM5.3F / Step5P / Gemini-3.8F 三方评审整合；交付版本直接由 1.6.0 跳到 1.6.4，代码注释中的 `v1.6.1-A*` / `v1.6.2-B*/C*` / `v1.6.3 修复` 是批次开发期的本地中间 bump 编号，未入库未出包，登记见 `docs/TASK_REFERENCE.md` §3.8）：A 层级修复+状态反馈（errorMsg 独立成行、首屏骨架 `listSkeleton`、批量键禁用/pending、选中即时高亮、ERROR 红条、去硬宽）；B 热路径性能（进度条 `transform: scaleX(--p)` 走合成层、render/updateOverview 签名守卫、scrollPos 主页/抽屉独立 key）；C 速率与实验项（WS 合并 `speed` + 倍速后缀实时展示、`#/tasks?fx=stripes\|breathe` 默认关）。含两处实测抓出的修复：空任务库骨架常驻（`loaded` 改由首次成功 render 置真）、`check-design` R3 误报 `box-shadow` |
| 1.6.5 | 版本真源漂移修复 + 校验加固：`internal/version/VERSION` 补齐至 1.6.5（1.6.4 出包漏改致「设置」页显示 1.6.0）；`version_test.go` 改为向上定位仓库根比对三真源、失败一律判红不再静默 SKIP（Windows 本机自此真跑），新增禁止硬编码版本字面量用例；`scripts/check-versions.ps1` 去硬编码盘符并支持 `-RepoRoot`（用户拍板发新补丁号而非强推重指 v1.6.4 tag） |
| 1.6.6 | 依赖刷新批次（无功能/界面改动，双端 patch bump，配套 FVCS v1.3.1）：FVCC vite 8.3.0→8.3.4、postcss→8.5.30、autoprefixer→10.6.1、@types/node 22.20.1→22.20.5、间接 x/sys →v0.49.0；FVCS x/sys v0.30.0→v0.49.0、go-sqlite3 →v1.14.52、fyne v2.8.0→v2.8.2（systray 随之为 fyne 2.8.x 的 MVS 伪版本，已确认可接受）。刻意推迟：Tailwind 4、Node 26、modernc.org/sqlite v1.60.1（详见仓库根 BUILD.md「工具链与依赖版本基线」） |
| 1.6.7 | sqlite 驱动专项批次（无功能/界面改动）：`modernc.org/sqlite` v1.14.0→v1.60.1（跨 46 个 minor）。升级前审干 store 层对驱动的接触面（`sql.Open` 无 DSN 参数、三条 PRAGMA 走 `db.Exec`、无驱动私有 API）；升级后用**老驱动真实产出的 fvcc.db** 由新驱动经实际 store 路径 Open→migrate→写回→二次 Load 验证（WAL 保持、`schema_version` 不误重置、`AUTOINCREMENT` 续号、立即落盘可见）。依赖面收窄（删 cc/ccgo/opt/strutil/token/uint128/x-mod/x-sync/x-tools/shellquote 十项，go.sum 净减 109 行），代价：二进制 +565 KB、fpk +2.4%。同批 Node 运行时 v25.9.0（已 EOL 奇数 Current 线）→ v26.11.1 系统级 MSI，四处口径对齐：`engines.node >=26.0.0`、CI `setup-node` 22→26、`@types/node` ^22→^26；构建产物内容哈希逐项不变，故该轮不再 bump。依赖刷新余项仅剩 Tailwind 4 专项 |
| 1.6.8 | **Tailwind CSS 4 大版本专项**（依赖刷新主题收口，零功能改动、纯样式链迁移）：`tailwindcss` 3.4.19→4.3.3。`tailwind.config.js` 删除，token 映射迁入 `ui-src/src/style.css` 的 `@theme inline`；暗色改 `@custom-variant dark`；`--c-*` 令牌由裸 RGB 通道改为完整 `rgb()` 颜色（v4 不替换 `<alpha-value>`，`/NN` 透明度改编译期 `color-mix(in oklab, …)`）；`@tailwindcss/postcss` 取代 `tailwindcss + autoprefixer`；改名/语义漂移逐项对齐（`shadow-sm`→`shadow-xs`、`outline-none`→`outline-hidden`）；扫描面 `source(none)` + 显式 `@source` 锁定。`check:design` 新增 `TW4-token-form` 规则并修复注释折叠导致的行号错位。收益：`npm audit` 现存 **7 条告警（2 moderate / 5 high）全部清零**、`npm outdated` 归零、配置文件 −1；CSS gzip 10.00→9.78 kB。随批裁决：接受 v4 移除 `button { cursor: pointer }` 的新默认。真机视觉对照由用户实测中 |
| 1.6.9 | **低风险收尾批次**（2026-10-14，第三十七轮；主体提交 `dedd124`、注解 tag `v1.6.9` 打在后记笔，fpk 11,014,297 B）：**store 备份/恢复前端入口**——设置页新增「数据备份与恢复」面板（备份列表含 kind 中文标签/体积/时间、立即备份、整包恢复二次确认、恢复后自动 prerestore 兜底并整表重载、>20 份按需展开），接上 v1.5.8 起就在位却无人接的 `/api/store/*` 三端点；交付前用 dev 模式真实后端 + 浏览器走通"空态→备份→恢复→23 份列表"全链路。同轮 **P3-6 台账正式勾选关闭**、`FVCC/.commandcode/`（AI 工具产物）入 `.gitignore`；新登记缺口：手动备份永久保留但**无删除入口**（后端亦无 `DELETE /api/store/backups/:id`）。纯前端 + 治理，后端零改动 |

逐轮明细见仓库根 `docs/IMPROVEMENT_LOG.md`（§十一~§二十五）。

## 项目内文档体系

| 文档 | 位置 | 说明 |
|------|------|------|
| API_CONTRACT.md | `FVCC/docs/` | 前后端 API 契约（错误码、路由、载荷） |
| DESIGN.md | `FVCC/docs/` | 前端设计契约（Named Rules + 反例清单 + check:design 门禁） |
| PRODUCT.md | `FVCC/docs/` | 前端产品定位、用户与反参照 |
| SECURITY.md | `FVCC/docs/` | 数据面安全策略（威胁模型/鉴权/凭据/限流/审计/路径校验） |
| STORE_PERSISTENCE_ROADMAP.md | `FVCC/docs/` | store 备份/迁移/快照回滚方案与落地记录（v1.5.6~1.5.8） |
| 设计规格 01~10 | `../../WebVideoEditor_Design/` | 见上表"设计文档引用速查" |
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
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地 | 三处 1.6.9 已同步（2026-10-14 第三十七轮复核，`check-versions.ps1` 绿）；本表已更新，消除 README 滞后。校验闭环加固：`version_test.go` 定位仓库根比对三真源、漂移即红（旧实现路径写错 + 读取失败即 Skipf，Windows 本机长期"绿而无效"，v1.6.4 的 VERSION 漂移因此溜过），另新增"禁止硬编码版本字面量"用例；`check-versions.ps1` 不再硬编码盘符 |
| P1-5 | 依赖瘦身（前端字体 / 后端 indirect） | 🟡 部分落地 | 字体 latin 子集已瘦身（app/ui/assets 12 文件）；go.mod 已执行 `go mod tidy` 复核：29 个 indirect 均为 gin v1.12 传递依赖（mongo-driver v2 由 gin 直接 require、quic-go 由 gin http3 引入），无进一步精简空间；node_modules 全字符集不进入构建产物，建议保持不动 |

## 治理计划状态（P2-1 ~ P2-8）

依据 `FVCC_混乱度评价报告_细化版.md` §4 的 P2 组计划项，2026-09-19 复核状态：

| 计划项 | 内容 | 状态 | 说明 / 剩余动作 |
|---|---|---|---|
| P2-1 | 后端 internal 分层 | ✅ 已落地（2026-09-19） | `server/internal/` 11 包 + store/model 子包，根包仅剩 main.go（87 个 .go 含测试，非测试 49）；阶段 A/B/C 完成、8 shim 内联删除；1.4.8/1.4.9 继续细化（stream_media / roots / aliases / config / trash_handlers / edl 扩展）；1.5.8 增补 `pathutil` 包（WithinRoot 单一真源，现共 12 包）；基线 tag v1.4.3；详见 [docs/P2-1_后端分层_阶段A实施记录.md](<docs/P2-1_后端分层_阶段A实施记录.md>) |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | `src/lib/` 4 件套接入 6 页、样板残留 0；main.ts 路由切换接入 dispose 防订阅泄漏；决策门已定案关闭（P1-2，2026-10-11：维持零框架）；详见 [docs/P2-2_细化细则.md](<docs/P2-2_细化细则.md>)（Checklist 全勾） |
| P2-3 | 建立 CI 流水线 | ✅ 已落地 | `.github/workflows/fvcc-ci.yml`（paths 限定 FVCC/**）+ `scripts/check-coverage.ps1`（覆盖率门槛 ≥60%，2026-10-12 上调；演进：基线 56.1%→56.7%→59.0%→补测后实测 60.7%） |
| P2-4 | 统一 API 契约与命名 | ✅ 已收口 | 统一错误契约（apierr.go + API_CONTRACT.md + api.ts 适配层）已落地；存量 `{error}` 裸格式 grep 为 0；remote.go snake_case 结构体均为 FVCS 对等协议段（API_CONTRACT §3.2 已登记边界），无剩余动作 |
| P2-5 | 删除操作回收站化 | ✅ 已落地 | `server/internal/store/trash.go`（P2-1 分层后位置）：视频删除移入授权根下 `_trash`，同名时间戳后缀；listTrash 列表接口 |
| P2-6 | 清理 AIGC 残留与任务编号注释 | ✅ 已收口 | AIGC frontmatter 已清（README / API_CONTRACT / ui-src PRODUCT / 根目录 2 份 / 混乱报告自身 / docs/ 3 份补清；2026-10-12 补清 `FVCC/docs/SECURITY.md` frontmatter 与尾注声明）；编号映射见 docs/TASK_REFERENCE.md |
| P2-7 | 测试文件命名规范化 | ✅ 已落地 | `genproxy_share_cred_126_test.go` → `genproxy_share_cred_test.go`、`handlers_edl_list_contract_test.go` → `handlers_edl_list_test.go`（git mv 保留历史） |
| P2-8 | 破坏性操作审计闭环 | ✅ 已落地 | security_audit.go 新增 `auditDestructive`（destructive.delete / destructive.clear / destructive.empty_trash），已接入清日志/清缓存/删除视频/服务器/方案/任务/历史/清空回收站 8 处调用点；配套单测 TestP28DestructiveOpsAudited |
