---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 839b5d1d4fff15220193598838e6072d_9ccc93ccb2fe11f19369525400de85a5
    ReservedCode1: guczdTABk4ms3FZ+ZdLQeN3n2er0ckEd5VEkqQMVWAq4yWtpojCmCYkdsoFkvFBqafMsablvd82hnL9z5AR8yRv+YghJgfy1c+N4kTxkkqkgCJj2IS4ZXG7lnV1ucL+YGGOeDesWxMJ8WZEEtI5gZybmjp/znNIiJLuIqOYrmjfV7IjokFHGp4IXTsM=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 839b5d1d4fff15220193598838e6072d_9ccc93ccb2fe11f19369525400de85a5
    ReservedCode2: guczdTABk4ms3FZ+ZdLQeN3n2er0ckEd5VEkqQMVWAq4yWtpojCmCYkdsoFkvFBqafMsablvd82hnL9z5AR8yRv+YghJgfy1c+N4kTxkkqkgCJj2IS4ZXG7lnV1ucL+YGGOeDesWxMJ8WZEEtI5gZybmjp/znNIiJLuIqOYrmjfV7IjokFHGp4IXTsM=
---



# FVCC 项目混乱度评价报告

> 项目：FVCC（飞牛视频转换 NAS 调度端 Web UI）@ D:\Fnos.VideoConversion\FVCC
> 评价日期：2026-09-18
> 评价视角：Vibe Coding（AI 辅助编码）模式下的工程卫生与可维护性
> 评价方式：目录树扫描 + 关键源码阅读（后端 Go 11,336 行非测试代码 / 5,406 行测试，前端 TS 约 8,300 行）+ 配置与文档核查

---

## 1. 项目概览

| 项 | 内容 |
|---|---|
| 定位 | fnOS NAS 上的视频转码调度端：素材扫描 → 转码方案 → 渲染节点集群 → 任务队列 → EDL 剪辑/代理预览全链路 |
| 后端 | Go 1.27.1，gin v1.12 + gorilla/websocket，单 `package main` |
| 前端 | Vite + TypeScript 7 + TailwindCSS 3，零框架（`el()` 手工 DOM） |
| 版本 | 1.4.0（VERSION / package.json / manifest 三处各自维护） |
| 版本控制 | **无 .git，无任何版本控制** |
| 文档 | ui-src/DESIGN.md（设计契约 R1-R10 + check:design 门禁）、ui-src/PRODUCT.md；**无根 README、无 docs/ 目录** |

目录结构（根目录）：
```
FVCC/
├── fvcc.exe / fvcc.fpk / FVCC_v1.4.0_fnos_x86.fpk / fvcs-service.exe   ← 构建产物混入源码根
├── ICON.PNG / ICON_256.png / manifest                                  ← fnOS 应用清单（含乱码）
├── app/          ← fnOS 应用打包目录（含 50+ woff 字体与构建产物）
├── cmd/          ← fnOS 生命周期回调脚本（install/config/uninstall/upgrade）
├── config/       ← privilege / resource 权限声明
├── logs/
├── server/       ← Go 后端（混入 33MB fvcc.exe 与 0 字节重定向残留）
├── temp/         ← 打包测试残留：2364 个文件，约 130MB
└── ui-src/       ← 前端源码（混入 all.txt/err.txt/out.txt 0 字节残留）
```

---

## 2. 分维度评价（含具体证据）

### 2.1 代码组织与分层 —— 7/10（混乱）

**证据 1：后端全部代码堆在 `package main` 单包内，巨型文件林立。**

| 文件 | 行数 | 职责 |
|---|---|---|
| server/handlers.go | 1,765（read_text 实测 1,924） | 全部 HTTP handler，含 59 块手写字段映射 |
| server/remote.go | 1,110 | WS 远端客户端 + 协议结构体 |
| server/scheduler.go | 1,064 | 1s 调度循环 + 全任务状态机 |
| server/handlers_render.go | 768 | 渲染类 handler |
| server/stream.go | 716 | 预览网关（Range 流/票据/缩略图） |
| server/store.go | 684 | 内存缓存 + JSON 原子持久化 |
| server/store_edl.go | 589 | EDL 项目存储 |
| server/models.go | 574 | **全部数据模型（Task/Server/Profile/Settings/Project/NodeCaps/AuditEntry/AssetProxy 等十几类）** |

无 `internal/` 分层、无按领域拆包，全部 `package main` 直接互相引用。models.go 单文件定义十几类数据模型，任何模型变更都会触碰该文件。

**证据 2：前端无框架手工 DOM 导致页面级大函数。**

- `ui-src/src/pages/profiles.ts`（1,162 行）、`scanner.ts`（1,152 行）、`editor/assets.ts`（612 行）、`timeline.ts`（579 行）、`preview.ts`（533 行）
- 每个页面重复手写：`renderList/renderEdit` 切换、`scrollPos` 保存恢复、`store.subscribe` 订阅、`confirmDialog + toast + api.* + store.load*` 的 CRUD 样板

**积极面（需客观记录）**：内部仍按文件划分了职责（router / security / ratelimit / ws_limit / ffprobe / proxy_flow 单独成文件）；handler 按领域用注释分节；`RenderDispatcher` / `MediaProber` / `ProxyProber` 采用接口注入便于单测替身——说明分层意识存在，只是粒度停留在"文件级"而非"包级"。

### 2.2 命名一致性 —— 6/10（中度混乱）

**证据 1：JSON 字段大小写风格混杂。** 同一协议层既有 camelCase（`authKey`、`customFfmpeg`、`videoCache`），又有 snake_case（`server/remote.go` 中 `progressPush` 结构体：`task_id`、`out_time_ms`、`total_ms`、`seg_total`），甚至同一文件注释自述"兼容两种风格：camelCase 与 snake_case"。

**证据 2：注释中的任务编号体系对维护者不透明。** 全代码库散布 `P0-4`、`P1-1`、`P1-2`、`P2-1`、`B-04`、`B-05`、`B-06`、`B-07`、`B-08`、`C-02`、`D-04`、`M4`、`修复①-⑤` 等数十种编号，例如：

- `server/router.go`：`// P2-1`、`// B-09`、`// D-04`、`// M4`
- `server/main.go`：`// 07 §4.5`、`// 03 §2.3`、`// B-06`
- `ui-src/src/main.ts`：`// P1-4`、`// P0-4`、`// C-02`

这些是 Vibe Coding 多轮任务/多 Agent 并行开发的直接痕迹。对后续维护者，编号含义无法从仓库内推断。

**证据 3：同名接口新旧风格并存。** `ui-src/src/api.ts` 注释自述："统一错误契约 { ok:false, code, msg, detail? }；**旧接口沿用 { error }**"——新旧两种错误响应格式同时在线上流通。

**证据 4：测试文件命名带票号。** `genproxy_share_cred_126_test.go`、`handlers_edl_list_contract_test.go` 等以票号/契约后缀命名，而非按被测对象统一命名。

### 2.3 重复代码 —— 7/10（高重复）

**证据 1：updateProfile 的 59 块手写字段映射（最大单一重复源）。**

`server/handlers.go` 中 `updateProfile` 用 `map[string]interface{}` + 逐字段 `if v, ok := updates["xxx"]; ok {...}` 手写白名单，实测 **59 个** `if v, ok := updates[` 块，约 250+ 行。每个块 4~5 行模式完全一致：

```go
if v, ok := updates["width"]; ok {
    if f, ok := v.(float64); ok {
        p.Width = int(f)
    }
}
if v, ok := updates["height"]; ok {
    if f, ok := v.(float64); ok {
        p.Height = int(f)
    }
}
// ... 共 59 个字段
```

且与 `createProfile`（全量 `ShouldBindJSON`）风格不一致，两条路径对同一 Profile 的字段处理规则天然分叉，新增字段时极易只改一处。

**证据 2：SMB 共享路径校验逻辑复制 3 处。** 几乎相同的"TransferMode == smb && SMBUser != '' → smbshare.IsPathShared"校验代码出现在：

- `server/handlers.go` `doScanDirectory()`
- `server/handlers.go` `probeVideo()`
- `server/handlers.go` `browseDirs()`（且内部出现两次）

**证据 3：VideoInfo 30+ 字段逐字段复制出现 2 处。** `handlers.go` `doScanDirectory()` 中 cache→VideoInfo 赋值块与 cache→VideoInfoCache 的 `UpsertVideoCache` 调用参数块，各 30+ 字段逐项手写，字段新增时两处必改。

**证据 4：前端跨页面样板重复。** `scrollPos` 保存、`store.subscribe`、`renderList/renderEdit`、CRUD 四件套（confirmDialog→api→toast→render）在 profiles/scanner/tasks/servers/settings 等页面反复出现，无公共封装。

### 2.4 依赖管理 —— 5/10（中度混乱）

- **后端**：`go.mod` 直接依赖仅 gin + websocket，但 indirect 链 30+ 包，含与业务无关的 `go.mongodb.org/mongo-driver/v2`、`quic-go/qpack`、`bytedance/sonic` 等（gin 生态连带）。对"gin+ws"两个直接依赖的应用而言偏重，且未做 `go mod tidy` 后的精简校验（91 行 go.sum 属正常）。
- **前端**：`@fontsource/ibm-plex-*` 全字符集（cyrillic/greek/vietnamese/latin-ext）被打包进 `app/ui/assets`，实测 **50+ woff/woff2 文件**；`node_modules` 81.5MB 未做依赖瘦身。TypeScript 锁 `^7.0.2`、Vite `^8.3.0`——大版本激进，存在上游不兼容风险。
- **积极面**：存在 `package-lock.json`（81KB）与 `go.sum`，依赖版本可复现。

### 2.5 配置与硬编码 —— 7/10（高混乱）

**证据 1：凭据明文存储（已知安全债务，最严重）。**

`server/models.go:147`：
```go
AuthKey string `json:"authKey"` // MVP 明文存储，TODO: P0 AES 加密
```
Settings 含 `SMBPassword` 明文字段，经 `/settings` 接口传输与 `settings.json` 落盘。

**证据 2：manifest 占位符与乱码未完成。**

`manifest`：
```
desc = Fnos Video Conversion Client - ????????????????
maintainer = ??
distributor = ??
```
发布清单的元数据仍为编码错误的占位符。

**证据 3：默认值散落硬编码。** `saveSettings` 中 `SchedulerIntervalSec=1`、`ChunkSizeMB=4`、`HistoryLimit=1000` 等兜底值；`scheduler.go` `chunkSize: 4 * 1024 * 1024` 与设置项语义重复；`main.go` 开发模式默认 `127.0.0.1:8088`。

**证据 4：版本号三处手工维护**（VERSION / package.json / manifest），无单一真源。

**证据 5：PRODUCT.md 顶部混入 AIGC 元数据残留**（`ContentProducer`、`ProduceID`、`ReservedCode1/2` 等 base64 块），系 AI 生成内容标记系统写入产物，污染了产品文档的 frontmatter。

### 2.6 文档与注释 —— 5/10（中度混乱）

**积极面：注释密度与质量其实不错。** 每个 handler 有职责注释；修复类改动带"修复①-⑤"上下文；`scheduler.go` P1-1 注释说明了"合并两次 GetTasks 为一次"的性能优化理由；`security.go` 明确实现"FS.md 8.2 四层校验"。

**混乱点 1：注释深度耦合外部文档章节号，而文档不在仓库。** 全库引用 `03 §4.1`、`06 §2.2`、`07 §4.5`、`04 §2.4`、`02 §8.1`、`FS.md 8.2` 等章节号，但项目根 **不存在 docs/ 目录**（实测 NO docs dir）。这些引用全部成为悬空引用，新维护者无法追溯设计依据——这是 Vibe Coding 依赖外部 spec 文档生成代码、但 spec 未入库的典型后果。

**混乱点 2：文档覆盖不平衡。** 前端有设计契约（DESIGN.md）与产品定位（PRODUCT.md），但后端无架构文档、根目录无 README、无 CHANGELOG。架构知识全靠代码注释中的章节号"指路"。

**混乱点 3：文档文件位置不当。** DESIGN.md/PRODUCT.md 位于 `ui-src/` 根下而非 `docs/`，且 PRODUCT.md 被 AIGC 残留污染。

### 2.7 遗留/废弃代码 —— 8/10（严重，本次评价最差维度）

**证据 1：`temp/` 打包测试残留 2364 个文件、约 130MB。**

- `temp/fpk_build_test2/`：含 `fnpack-1.2.1.exe`、`fvcc.exe`（33MB）、`fvcc.fpk`、**旧版本包 `FVCC_v1.3.1_fnos_x86.fpk`**（16MB）、`manifest`、`app/`、`cmd/`、`config/` 完整复制
- `temp/fpk_build_test2/junk_dir/sub1/sub2/`：**数千个 `log2_1.txt` ~ `log2_11xx.txt` 压测垃圾文件**（每个 18~21 字节），明显是打包递归压测脚本的产物
- `temp/fpk_inspect/`、`temp/fpk_stage/`：打包中间目录

**证据 2：构建产物混入源码目录。**

- `server/fvcc.exe`（33MB）+ `server/build_err.txt`（0B）+ `server/out.txt`（0B）——编译输出与命令重定向残留
- `ui-src/all.txt`、`err.txt`、`out.txt`（均 0B）——构建脚本重定向残留
- `ui-src/tsconfig.tsbuildinfo`——增量编译缓存未清理
- `app/ui/` 全部构建产物（50+ 字体 + index-*.js/css）保留在源码树

**证据 3：根目录堆积 3 个可执行文件 + 2 个安装包 + 2 个图标**（fvcc.exe 32MB、fvcs-service.exe 9.5MB、fvcc.fpk 9.5MB、FVCC_v1.4.0_fnos_x86.fpk 9.3MB）。

### 2.8 测试情况 —— 3/10（优秀，全项目最大亮点）

- **测试规模**：24 个 `_test.go`、约 5,406 行，覆盖 store 并发（`store_concurrency_test.go`）、EDL 校验 + fuzz（`edl_fuzz_test.go`）、remote WS 重连（`remote_reconnect_test.go`）、安全 e2e（`security_e2e_test.go`）、stream Range（`stream_test.go`）、调度（`scheduler_edl_test.go`）等关键路径。
- **测试基建**：存在接口注入（`RenderDispatcher`/`MediaProber`/`ProxyProber`）专为单测替身设计，`main.go` 与单测共用构造点。
- **扣分项**：① 无覆盖率统计与基线；② 无 CI（无 .git 更无流水线）；③ 命名带票号/契约后缀，与实现文件对应关系不直观；④ 测试文件分布与实现文件 1:1 平铺在 server/ 下，无测试分层。

### 2.9 安全与健壮性 —— 5/10（有亮点，有硬伤）

**积极面（做得不错的）**：
- `server/security.go`：`PathValidator` 四层路径校验（授权目录 + 额外目录 + 动态授权文件 + 环境变量回退）
- `server/router.go`：`NoRoute` 兜底拦截 `/config` 等敏感路径；`requireAdmin()` 保护删除/清缓存等破坏性操作
- `server/securityHeaders()` 安全响应头
- 崩溃恢复：`store.go` Load 时非终态任务重置为 QUEUE；`scheduler.go` 用 `sync.Map` 防并发处理同一任务、`connMu` 单飞防重复拨号
- 限流：`ratelimit.go`、`ws_limit.go`、`stream.go`（单文件 4 路 / 全局 64 路并发限制）

**硬伤**：
1. **凭据明文**（AuthKey / SMBPassword），自述"TODO: P0 AES 加密"却仍在 1.4.0 发布版中
2. **`deleteVideo` 直接 `os.Remove`**：`server/handlers.go` 中视频删除为物理删除，无回收站/软删，NAS 用户误操作不可恢复
3. **`updateProfile` 手写白名单虽是安全设计，但 59 块重复极易遗漏新增字段**，遗漏字段会静默"丢更新"
4. **无审计闭环**：虽有 audit_log 模型，但删除/清缓存等破坏性操作的审计记录覆盖情况未在代码中形成统一强制
5. **无版本控制**意味着任何误改都无法回滚，安全事件后的取证与恢复均无依据

---

## 3. 量化混乱度评分

| 维度 | 得分（1=很整洁，10=很混乱） | 一句话结论 |
|---|---|---|
| 代码组织与分层 | 7 | 单包巨型文件 + 前端大函数，但文件级职责划分尚可 |
| 命名一致性 | 6 | camel/snake 混用、新旧接口并存、任务编号注释难追溯 |
| 重复代码 | 7 | 59 块字段映射、SMB 校验 x3、VideoInfo 复制 x2、前端样板重复 |
| 依赖管理 | 5 | 间接依赖偏重、字体全子集打包，但有 lockfile |
| 配置与硬编码 | 7 | 凭据明文、manifest 乱码、默认值散落、版本号三处维护 |
| 文档与注释 | 5 | 注释密度高但悬空引用外部文档、无根 README |
| 遗留/废弃代码 | **8** | temp 130MB 打包残留 + 构建产物/0 字节文件混入源码 |
| 测试情况 | **3** | 5,406 行测试认真覆盖关键路径，缺 CI 与覆盖率基线 |
| 安全与健壮性 | 5 | 路径校验/崩溃恢复/限流出色，凭据明文 + 物理删除是硬伤 |
| **综合** | **6.5/10（中等偏混乱）** | 功能完整、测试扎实，但工程卫生与可维护性被 Vibe Coding 迭代痕迹严重拖累 |

等级判定：**C+ / 中等偏混乱**。属于"能跑、能测、但很难继续演进"的状态——1.4.0 的功能完成度与测试诚意值得肯定，但继续在同一仓库上叠加新功能，维护成本将加速上升。

---

## 4. 修改建议（按优先级）

### P0 —— 立即处理（安全与止血，1 周内）

| # | 建议 | 理由 | 大致改法 |
|---|---|---|---|
| P0-1 | **凭据加密落库** | `models.go:147` 自述"TODO: P0 AES 加密"却仍在发布版；SMBPassword 明文经接口与落盘传输 | 引入 AES-GCM（密钥存 fnOS 系统 keychain 或文件权限 0600 保护）；`settings.json`/`server.json` 升级 schema 版本，启动时迁移旧明文；前端不回显密码 |
| P0-2 | **清理工程残留并建立 .gitignore** | temp 130MB 打包垃圾、server/ui-src 0 字节重定向残留、构建产物混入源码，是 Vibe Coding 不加清理的直接后果 | 删除 `temp/fpk_build_test2`、`temp/fpk_inspect`、`temp/fpk_stage`、`server/fvcc.exe`、`server/build_err.txt`、`server/out.txt`、`ui-src/all.txt/err.txt/out.txt`；建立 .gitignore（`temp/`、`*.exe`、`*.fpk`、`node_modules/`、`dist/`、`*.tsbuildinfo`、`app/ui/assets/`） |
| P0-3 | **git init 建立版本控制** | 全项目无 .git，任何误改/误删不可回滚 | `git init` + 清理后首次提交；后续所有改动入版本库；有条件则推私有远端做异地备份 |

### P1 —— 短期改进（1~2 个迭代）

| # | 建议 | 理由 | 大致改法 |
|---|---|---|---|
| P1-1 | **消灭 updateProfile 的 59 块手写映射** | 最大单一重复源；新增字段两处必改，遗漏即静默丢更新 | 方案 A：改全量 `ShouldBindJSON(&p)` + 显式字段校验（与 createProfile 对齐）；方案 B：反射白名单 helper `applyFields(dst, src, allowed...)`。推荐 A，语义最直白 |
| P1-2 | **抽取 SMB 校验 helper** | `doScanDirectory`/`probeVideo`/`browseDirs` 三处复制同一段逻辑 | 新增 `func (h *Handlers) validateSMBPath(settings Settings, path string) error`，三处改为单行调用 |
| P1-3 | **文档落地：消除悬空引用** | 全库注释引用 `03/04/06/07/FS.md` 章节，但 docs/ 不存在 | 将规格文档（至少章节级摘要）纳入仓库 `docs/`；或将注释中的章节引用改写为自包含说明；补根 README（架构图 + 构建 + 部署 + 目录说明） |
| P1-4 | **修复 manifest 元数据** | desc 乱码、maintainer/distributor 为 `??`，属发布级缺陷 | 补全 manifest 字段并校验编码（UTF-8）；将版本号收敛为单一来源（构建时注入 VERSION/package.json/manifest） |
| P1-5 | **依赖瘦身** | mongo-driver/quic-go 等间接依赖与业务无关；字体全子集打包 50+ woff | `go mod tidy` + 评估是否可替换 gin 为更轻路由（如 stdlib net/http + chi）；`@fontsource` 只引 latin 子集或改用系统字体回退 |

### P2 —— 中期架构演进（1~2 月）

| # | 建议 | 理由 | 大致改法 |
|---|---|---|---|
| P2-1 | **后端分层：单 main 包 → internal 分层** | 11,336 行堆在 package main，互相可见无边界 | 按领域拆 `internal/{api,store,scheduler,remote,security,edl}`；handlers.go 按资源域（settings/video/server/profile/task/edl/stream）拆文件；models 按域拆分并收敛公共类型 |
| P2-2 | **前端组件化** | 无框架手工 DOM 导致页面 1,000+ 行样板重复 | 短期：抽取公共 `useListPage` 风格组合器与表单 builder；中期：评估引入 Svelte/Vue（渐进式，无需全量重写）；至少统一 CRUD 四件套与 scrollPos 逻辑 |
| P2-3 | **建立 CI 流水线** | 测试扎实但无自动化保障 | GitHub Actions / fnOS 本地脚本：`go vet + go test ./...`、`tsc -b`、`npm run check:design`、fpk 构建；加入覆盖率基线（目标 ≥60%） |
| P2-4 | **统一 API 契约与命名** | 新旧错误格式并存、JSON tag 大小写混用 | 全面迁移 `{error}` → `{code,msg,detail}`（前端适配层过渡）；协议层统一 snake_case 或 camelCase 并加 `json` tag 规范文档 |
| P2-5 | **删除操作回收站化** | `deleteVideo` 直接 `os.Remove`，NAS 误删不可恢复 | 改为移入 `<素材根>/_trash`（受 `_` 前缀保留规则保护）或系统回收站；提供清空/恢复入口 |
| P2-6 | **清理 AIGC 残留与任务编号注释** | PRODUCT.md 混入 AI 元数据；`B-04/M4/修复⑤/126` 等编号无解释 | 移除 PRODUCT.md frontmatter 残留；注释中的任务编号统一替换为可读语义（或附任务名映射表） |

---

## 5. 结论

FVCC 是一个典型的 **Vibe Coding 产物**：功能覆盖面广（转码调度、节点集群、EDL 剪辑、代理预览网关全链路）、注释密度高、测试意外地认真（5,406 行），但工程卫生明显落后于功能推进速度。

其混乱的根因有三：
1. **无版本控制 + 无构建隔离**：所有中间产物（打包测试、编译输出、0 字节重定向）就地堆在源码树，temp/ 一处即 130MB 垃圾；
2. **外部规格文档未入库**：代码注释引用 02/03/04/06/07 章节号，仓库内却无 docs/，知识链断裂；
3. **AI 增量修补式开发**：任务编号（B-xx/M4/修复①-⑤）、新旧接口并存、59 块手写字段映射，都是"每轮只改最小差异"的累积。

综合评价 **6.5/10（中等偏混乱）**。但该项目的底子并不差——路径安全、崩溃恢复、限流、接口注入、设计门禁（check:design）这些"正规工程素养"都在。只要先做 P0 的止血（凭据加密、残留清理、版本控制），再按 P1→P2 推进，混乱度可在一个月内降到 4/10 以下，具备长期演进能力。
*（内容由AI生成，仅供参考）*
*（内容由AI生成，仅供参考）*
