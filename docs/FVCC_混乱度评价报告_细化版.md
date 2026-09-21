# FVCC 项目混乱度评价报告（细化版）

> 项目：FVCC（飞牛视频转换 NAS 调度端 Web UI）@ D:\Fnos.VideoConversion\FVCC
> 基线评价日期：2026-09-18（首版）
> **复查更新日期：2026-09-20（第五版，反映 1.4.9 落地与清理回潮后的最新现状）**
> 评价视角：Vibe Coding（AI 辅助编码）模式下的工程卫生与可维护性
> 评价方式：目录树扫描 + 关键源码阅读（后端 internal 11 包 + store/model 子包共 87 个 .go 文件；前端 TS 33 文件 10,903 行）+ 配置与文档核查 + git log/status 交叉核对 + 与根目录 README.md / BUILD.md / docs（项目现状与开发步骤 / IMPROVEMENT_LOG / TASK_REFERENCE / 项目分析与改进方向 / UI设计优化方向 / SECURITY）交叉核对
> 更新说明：第五版基于 2026-09-20 晚实测，版本迭代至 **1.4.9**（manifest / package.json / VERSION 三处一致）；后端在 1.4.8/1.4.9 继续细化拆分（stream_media / roots / aliases / config / trash_handlers / edl 扩展）；docs 布局收口（PRODUCT.md / DESIGN.md 已入 FVCC/docs/）。同时登记清理回潮：temp/ 膨胀至 549.7MB（node_modules 全量 + chrome-profile + secret.key 敏感残留）、新文档 `docs/FVCC_项目现状与开发步骤.md` 带 AIGC frontmatter、git 工作区 1.4.7~1.4.9 改动未提交、README（1.4.4）/ BUILD（1.4.6）版本表继续滞后。§4 中 P0/P1/P2 已全部落地，压缩为状态汇总表；删除 §4.1 / §4.2 落地前快照（实施记录见 FVCC/docs/ 对应文档）。

---

## 1. 项目概览

| 项 | 内容 |
|---|---|
| 定位 | fnOS NAS 上的视频转码调度端：素材扫描 → 转码方案 → 渲染节点集群 → 任务队列 → EDL 剪辑/代理预览全链路 |
| 后端 | Go 1.27.1，gin v1.12 + gorilla/websocket，**已分层**：`server/internal` 11 包 + store/model 子包（87 个 .go = 49 实现 + 38 测试，非测试 13,107 行 / 测试 6,208 行，根包仅剩 main.go 272 行） |
| 前端 | Vite + TypeScript + TailwindCSS，零框架（`el()` 手工 DOM），公共样板已收敛至 src/lib/ 4 件套；TS 33 文件 10,903 行（2026-09-20 实测） |
| 版本 | **1.4.9**（server/internal/version/VERSION、ui-src/package.json、manifest 三处一致，2026-09-20 实测；version_test.go 自动校验）；工作区含 v1.4.0~v1.4.9 共 10 个版本包（1.4.8/1.4.9 为本轮新增） |
| 版本控制 | **已启用 git（P0-3 已落地）**：由上层仓库 `D:\Fnos.VideoConversion` 统一管理，当前分支 baseline-snapshot，已推送 GitHub [hgmmm25/Fnos.VideoConversion](https://github.com/hgmmm25/Fnos.VideoConversion/tree/baseline-snapshot)；**⚠️ 工作区存在 1.4.7~1.4.9 未提交改动（manifest / VERSION / package.json / models.go / store_edl.go / 前端 editor 等），最新提交仍停留在 1.4.6 同步（adabef9），快照漂移风险实锤，见 §4 P3-1** |
| 文档 | **双 docs 布局已收口**：根仓库 `docs/` 7 份台账（混乱报告 / 项目现状与开发步骤 / IMPROVEMENT_LOG / TASK_REFERENCE / 项目分析与改进方向 / UI设计优化方向 / SECURITY）+ `FVCC/docs/` 5 份（API_CONTRACT / DESIGN / PRODUCT / P2-1 实施记录 / P2-2 细化细则）——PRODUCT.md / DESIGN.md 已从 ui-src/ 迁入 FVCC/docs/，原"两处文档根"问题解决；WebVideoEditor_Design/ 01-10 规格文档仍入库 |

目录结构（根目录，2026-09-20 晚实测）：
```
FVCC/
├── .gitignore                            ← 已建立（*.exe/*.fpk/node_modules/temp/logs/0字节残留）
├── README.md                             ← 版本表仍 1.4.4，滞后 5 个版本（待根仓库同步，见 §4 P3-4）
├── fvcc.exe / fvcc.fpk / FVCC_v1.4.0~1.4.9_fnos_x86.fpk / fvcs-service.exe  ← 构建产物物理堆积（10 个版本包；已入 .gitignore 未物理清理）
├── task.db / credentials.db / master.key / secret.key / locks.json / tasks.json / history_tasks.json / config.enc.json  ← 运行时数据混入根目录（task.db 疑似 SQLite 遗留，待确认）
├── ICON.PNG / ICON_256.png / manifest    ← fnOS 应用清单（UTF-8 正常，version=1.4.9）
├── app/          ← fnOS 应用打包目录（字体 latin 子集 + lightweight-charts 异步 chunk）
├── cmd/          ← fnOS 生命周期回调脚本（install/config/uninstall/upgrade）
├── config/       ← privilege / resource 权限声明
├── docs/         ← 5 份：API_CONTRACT.md、DESIGN.md、PRODUCT.md、P2-1_后端分层_阶段A实施记录.md、P2-2_细化细则.md
├── logs/         ← 运行时日志（已入 .gitignore）
├── server/       ← Go 后端（internal/ 11 包 + store/model 子包，87 个 .go；根包仅剩 main.go 272 行）
├── temp/         ← ⚠️ 膨胀至 549.7MB：fpk_stage / fpk_stage_145~149 打包残留 + node_modules 全量拷贝 + chrome-profile-9223/9224 + repro-data + cdp-*-test.mjs + fvcc-win-debug.exe + secret.key（敏感残留）+ package.json/lock + logs + locks.json（见 §2.7）
└── ui-src/       ← 前端源码（33 个 TS 文件 10,903 行；src/lib/ 4 件套；pages/editor/ 14 模块）
```

---

## 2. 分维度评价（含具体证据）

### 2.1 代码组织与分层 —— 2/10（已分层且持续细化）

**现状（P2-1 已落地并继续演进）**：`server/internal/` 11 包 + store/model 子包共 **87 个 .go 文件（49 实现 + 38 测试）**，根包仅剩 `main.go`（272 行，入口与装配、优雅退出）。行为零回归（实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md`，阶段 A/B/C 全部完成、8 个 shim 兼容文件已内联删除）。

**1.4.8/1.4.9 新增拆分（2026-09-20 晚实测）**：
- `internal/media/` 新增 `stream_media.go`、`roots.go`——预览流媒体与授权根从 api/ 下沉至 media 域；
- `internal/api/` 新增 `aliases.go`、`config.go`、`hashutil.go`、`security_roots.go`、`trash_handlers.go`（回收站 handler 从 store/trash.go 拆出）；
- `internal/edl/` 扩展为 4 文件（新增 `edl_support.go`、`export.go`、`audit_hook.go`）；
- `internal/store/trash.go` 收窄为纯存储（78 行），handler 侧迁至 api/trash_handlers.go；
- 新增测试 `render_progress_test.go`、`model_convert_test.go`。

**积极面**：分层后职责边界清晰（api 13 实现文件 / edl 4 / media 7 / remote 2 / scheduler 1 / security 7 / store 4 / ws 2 / protocol 3 / node 1 / version 1）；handler 按领域分文件；`RenderDispatcher` / `MediaProber` / `ProxyProber` 接口注入保持；1.4.8/1.4.9 的继续拆分证明分层模式可承载后续演进。

### 2.2 命名一致性 —— 5/10（轻度混乱，无新增）

**证据 1：JSON 字段大小写风格混杂（未变）。** 协议层既有 camelCase（`authKey`、`customFfmpeg`、`videoCache`），又有 snake_case（`server/internal/remote` 包 `progressPush` 结构体：`task_id`、`out_time_ms`、`total_ms`、`seg_total`）。API_CONTRACT.md 已定"协议字段一律 camelCase（FVCS 对等 WS 报文允许 snake_case 双风格兼容）"，存量代码未迁移。

**证据 2：注释任务编号两套语义（已登记）。** `// P2-1` 在 `handler_metrics_test.go` 指 Prometheus 指标端点，与本报告 P2-1（后端分层）语义不同；已登记于 docs/TASK_REFERENCE.md §2.1（编号漂移登记）。

**证据 3：新旧错误契约并存（过渡期）。** `ui-src/src/api.ts` 声明统一错误契约 { ok:false, code, msg, detail? }，request() 保留 `body.msg || body.error` 兜底——API_CONTRACT.md 明确"旧格式过渡期兜底，新代码只读 msg"。

**证据 4（已落地，P2-7）**：测试文件命名已统一为 `handlers_<domain>_test.go` 风格（git mv 保留历史）。

### 2.3 重复代码 —— 3/10（两大重复源已消灭，剩余中等）

- ✅ **updateProfile 59 块手写映射 → 反射白名单**（internal/protocol/applyjson.go，P1-1 落地）；实测 `if v, ok := updates[` = 0。
- ✅ **SMB 路径校验复制 3 处 → 统一 helper**（internal/security/smb_validate.go，P1-2 落地；5 处调用点单行化）。
- ✅ **前端跨页面样板**（P2-2 收口）：crudActions / scrollPos / useListPage / formBuilder 4 件套接入 6 页，CRUD/滚动/订阅/表单样板残留 0。
- ⚠️ **仍在**：VideoInfo 30+ 字段逐字段复制出现 2 处（`doScanDirectory()` cache→VideoInfo 赋值块与 `UpsertVideoCache` 调用参数块），字段新增时两处必改。

### 2.4 依赖管理 —— 5/10（前端打包侧已瘦身，后端间接依赖未变）

- **后端**：go.mod 直接依赖仅 gin + websocket；indirect 链含与业务无关的 `go.mongodb.org/mongo-driver/v2`、`quic-go/qpack`、`bytedance/sonic` 等（gin 生态连带），已执行 `go mod tidy` 复核零变化、无进一步精简空间。
- **前端**：`app/ui/assets` 保持瘦身（10 个 latin 字体 woff/woff2 + index css/js + lightweight-charts 异步 chunk）；`node_modules` 本体不进入构建产物（本轮实测 temp/ 下出现 node_modules 全量拷贝，属清理问题而非打包问题，见 §2.7）。
- **积极面**：package-lock.json 与 go.sum 存在；scripts/check-versions.ps1（版本一致性校验）与 check-coverage.ps1（覆盖率门禁）保持。

### 2.5 配置与硬编码 —— 2/10（版本单一来源推进至 1.4.9，文档表滞后加重）

- ✅ **凭据 AES-GCM 加密落盘**（internal/security/crypto.go，P0-1）：主密钥 0600 落盘 `<dataDir>/secret.key`，`enc:v1:` 前缀 + 旧明文兼容迁移 + API 脱敏；原 models.go 过时注释已清理无残留。
- ✅ **manifest 元数据正常**（P1-4）：UTF-8 无乱码，version=1.4.9。
- ✅ **版本单一来源**（P1-4）：version.go go:embed VERSION，version_test.go 自动校验三处一致（2026-09-20 实测 manifest 1.4.9 / VERSION 1.4.9 / package.json 1.4.9 一致）。
- ⚠️ **滞后加重**：FVCC/README.md 版本表仍 1.4.4（滞后 5 个版本）、根 BUILD.md 仍 1.4.6（滞后 3 个版本）——docs/ 内文档已对齐 1.4.9，根仓库文档未同步（§4 P3-4）。
- ⚠️ **仍在**：默认值散落硬编码（`SchedulerIntervalSec=1`、`ChunkSizeMB=4`、`HistoryLimit=1000`、scheduler.go `chunkSize: 4*1024*1024` 与设置项语义重复、main.go 开发模式 `127.0.0.1:8088`）。

### 2.6 文档与注释 —— 2/10（docs 布局收口，但 AIGC 残留出现新点）

**已兑现**：
- **规格文档入库 + 速查表**（P1-3）：WebVideoEditor_Design/ 01-10 + FVCC/README.md 引用速查表，注释悬空引用问题根除；
- **docs 布局收口（本轮改善）**：PRODUCT.md / DESIGN.md 已从 ui-src/ 迁入 `FVCC/docs/`，原"文档位置不统一（两处文档根）"问题解决；当前布局 = 根仓库 docs/ 7 份台账 + FVCC/docs/ 5 份技术文档；
- 根 README.md、BUILD.md、API_CONTRACT.md（统一错误契约 + code 映射表）保持；
- 注释密度与质量保持（每个 handler 有职责注释，新文件均带"Px-x 对应混乱报告条目"落地注释）。

**残留混乱点（2026-09-20 晚更新）**：
1. ⚠️ **AIGC frontmatter 新残留（P2-6 清零被打破）**：未跟踪新文档 `docs/FVCC_项目现状与开发步骤.md`（2026-09-20 21:38 创建）顶部带完整 AIGC base64 块（ContentProducer / ProduceID / ReservedCode1/2）；第四版"全库扫描 AIGC 关键词 0 命中"结论已失效，需补清（§4 P3-4）；
2. **根 README.md / BUILD.md 版本表滞后**（1.4.4 / 1.4.6 vs 实际 1.4.9，§4 P3-4）；
3. **新文档与混乱报告存在内容重叠**：`docs/FVCC_项目现状与开发步骤.md` 与混乱报告 §1 概览/目录树大量重复，建议明确两文档定位（台账 vs 现状指引）避免双轨漂移。

### 2.7 遗留/废弃代码 —— 5/10（temp 严重回潮，根目录产物未动）

**⚠️ 重大回潮（本轮主要扣分项）**：`temp/` 从第四版 36.7MB 膨胀至 **549.7MB**：
- 新增 `node_modules/`（前端依赖全量拷贝，近 500MB 级）；
- 新增调试残留：`chrome-profile-9223/9224/`、`repro-data/`、`cdp-fine-test.mjs`、`cdp-real-test.mjs`、`fvcc-win-debug.exe`、`package.json` / `package-lock.json`、`logs/`、`locks.json`；
- ⚠️ **敏感文件残留**：`temp/secret.key`（密钥落盘到 temp，与 `server/internal/security` 规范目录不符，需清理确认）；
- 打包 stage：`fpk_stage/` + `fpk_stage_145/146/147/149/`（1.4.9 新增 fpk_stage_149，无 148——与版本包跳号无关，系构建目录命名）；
- 第四版已清除的 fpk_build_test2 / fpk_inspect 未复发。

**根目录（未动）**：版本包 8→10 个（新增 v1.4.8 / v1.4.9），fvcc.exe（32MB）/ fvcc.fpk / fvcs-service.exe 仍在物理堆积；.gitignore 已覆盖 `*.exe` / `*.fpk` 但未物理清理、未移入 archive/。**新增观察**：`task.db`（12KB，疑似早期 SQLite 遗留，当前存储为 JSON 文件——需确认是否仍被引用）、`credentials.db` / `master.key` / `secret.key` / `locks.json` 等运行时数据混入根目录（P0-2 剩余，见 §4 P3-3）。

**已保持**：server/ 与 ui-src/ 根下 0 字节文件 = 0；logs 内 0 字节残留已入 .gitignore；FVCS/ 侧 build_check.txt / _cgo_full.txt 已移入根 archive/（`_build_check.txt`、`fvcs-service.exe.bak_preupgrade`），FVCS 根已无 0 字节文件。

### 2.8 测试情况 —— 2/10（优秀且持续增强）

- **规模**：**38 个 `_test.go`、约 6,208 行**（第四版 6,900 行口径略有出入，按 2026-09-20 实测 6,208）；本轮新增 `render_progress_test.go`（渲染进度状态机）、`model_convert_test.go`（模型转换回归），覆盖新拆分模块。
- **基建**：接口注入（RenderDispatcher/MediaProber/ProxyProber）专为单测替身设计；main.go 与单测共用构造点。
- **门禁**：CI 已建立（P2-3，.github/workflows/fvcc-ci.yml：go vet + test -race + 覆盖率 ≥55% + 前端 check:design/tsc/build）；`scripts/check-coverage.ps1` 强制，注释自述当前基线 56.1%。
- 测试随 P2-1 迁移至 internal 各包，平铺问题已消除。

### 2.9 安全与健壮性 —— 2/10（五硬伤保持清零，新增敏感残留观察）

**保持（首版 5 项硬伤 → 0 项未决）**：
1. ✅ 凭据 AES-GCM 加密（internal/security/crypto.go + API 脱敏）；
2. ✅ 删除回收站化（internal/store/trash.go 纯存储 + api/trash_handlers.go，移入 `<授权根>/_trash`，同名时间戳后缀）；
3. ✅ updateProfile 白名单遗漏修复（applyjson.go 反射白名单）；
4. ✅ 破坏性操作审计闭环（internal/security/audit.go + 8 处调用点，清日志/清缓存/删除视频/服务器/方案/任务/历史/清空回收站）；
5. ✅ git 版本控制（可回滚、可取证）。

**既有防线保持**：PathValidator 四层路径校验、NoRoute 兜底、requireAdmin 门禁、网关身份透传（X-Trim-* + 写操作 admin）、securityHeaders、崩溃恢复（非终态任务重置 QUEUE）、限流（ratelimit / ws_limit / stream 单文件 4 路 / 全局 64 路）。

**新增观察（1.4.8/1.4.9 期间引入，待处置）**：
- ⚠️ `temp/secret.key`：主密钥副本出现在 temp 调试目录（若为调试期生成且未使用，应删除；若被使用需评估暴露面）——与 §2.7 同源，登记为 P3-2 清理项。

---

## 3. 量化混乱度评分（第五版，2026-09-20）

| 维度 | 首版 | 二版 | 三版 | 四版 | **五版** | 一句话结论（变化） |
|---|---|---|---|---|---|---|
| 代码组织与分层 | 7 | 6 | 2 | 2 | **2** | 1.4.8/1.4.9 继续细化拆分（stream_media/roots/aliases/config/trash_handlers/edl 扩展），87 个 .go 全绿 |
| 命名一致性 | 6 | 6 | 5 | 5 | **5** | 无新增；编号歧义已登记（TASK_REFERENCE §2.1） |
| 重复代码 | 7 | 5 | 3 | 3 | **3** | 两大重复源已消灭；VideoInfo 逐字段复制 2 处仍在 |
| 依赖管理 | 5 | 5 | 5 | 5 | **5** | 间接依赖未变；前端打包侧保持瘦身 |
| 配置与硬编码 | 7 | 4 | 3 | 2 | **2** | 版本单一来源推进至 1.4.9；README（1.4.4）/BUILD（1.4.6）滞后加重 |
| 文档与注释 | 5 | 3 | 2 | 2 | **2** | docs 布局收口（PRODUCT/DESIGN 入 FVCC/docs/）；⚠️ 新文档带 AIGC frontmatter，清零被打破 |
| 遗留/废弃代码 | 8 | 6 | 6 | 6 | **5** | ⚠️ temp 回潮至 549.7MB（node_modules/secret.key/调试残留）；版本包 8→10 个 |
| 测试情况 | 3 | 2 | 2 | 2 | **2** | 38 个 *_test.go 保持，新增 render_progress / model_convert 回归 |
| 安全与健壮性 | 5 | 2 | 2 | 2 | **2** | 五硬伤清零保持；⚠️ temp/secret.key 敏感残留待处置 |
| **综合** | **6.5** | **4.3** | **2.8** | **2.4** | **2.8（轻度混乱）** | 架构与质量保障无退化，但清理项回潮（temp 549MB / AIGC 新残留 / git 未提交 / 文档滞后）致评分小幅回升 |

等级判定：**B / 轻度混乱**。工程卫生基本面保持（P0/P1/P2 全部落地、1.4.9 三处版本一致、docs 布局收口），但 1.4.8/1.4.9 迭代期积累的**物理清理回潮**（temp 549MB、根目录 10 个版本包、运行时数据混入根目录）与**文档同步滞后**（README/BUILD 版本表、新文档 AIGC frontmatter、git 未提交）成为当前主要债务，需一轮收尾（§4 P3 组）。

---

## 4. 修改建议（P0/P1/P2 全部落地，压缩为状态汇总；P3 为本轮新增遗留债务）

### 4.0 历史整改状态汇总（P0/P1/P2 全部落地或收口）

| # | 建议 | 状态 | 落地证据（简） |
|---|---|---|---|
| P0-1 | 凭据加密落库 | ✅ 已落地 | internal/security/crypto.go（AES-GCM + 0600 + enc:v1: + 兼容迁移 + API 脱敏）；过时注释已清 |
| P0-2 | 清理工程残留 + .gitignore | 🟡 基本落地（剩余见 P3-2/P3-3） | .gitignore 已建；fpk_build_test2 / fpk_inspect / 0 字节残留已清；根目录产物与 temp 回潮未物理清理 |
| P0-3 | git 建立版本控制 | ✅ 已落地（剩余见 P3-1） | 上层仓库统一管理，baseline-snapshot 已推送 GitHub；分层前 tag v1.4.3 |
| P1-1 | 消灭 updateProfile 59 块手写映射 | ✅ 已落地 | internal/protocol/applyjson.go 反射白名单，实测 `if v, ok := updates[` = 0 |
| P1-2 | 抽取 SMB 校验 helper | ✅ 已落地 | internal/security/smb_validate.go，5 处调用点统一 |
| P1-3 | 文档落地消除悬空引用 | ✅ 已落地 | WebVideoEditor_Design/ 01-10 + 速查表 + 根 README/BUILD/API_CONTRACT |
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地（文档表滞后见 P3-4） | version.go go:embed + version_test.go 三处校验 + check-versions.ps1；manifest 1.4.9 正常 |
| P1-5 | 依赖瘦身 | 🟡 部分落地 | app/ui 字体 latin 子集瘦身；go.mod tidy 复核零变化、无进一步精简空间 |
| P2-1 | 后端分层 internal | ✅ 已落地（2026-09-19） | 11 包 + store/model 子包，根包仅剩 main.go；阶段 A/B/C 完成、shim 已删；实施记录见 FVCC/docs/P2-1_后端分层_阶段A实施记录.md |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | src/lib/ 4 件套接入 6 页，样板残留 0；细则见 FVCC/docs/P2-2_细化细则.md |
| P2-3 | 建立 CI 流水线 | ✅ 已落地 | .github/workflows/fvcc-ci.yml + check-coverage.ps1（≥55%，基线 56.1%） |
| P2-4 | 统一 API 契约与命名 | ✅ 已收口 | API_CONTRACT.md + apierr.go + api.ts 适配兜底；`{error}` 裸格式 grep 0；remote snake_case 为 FVCS 对等协议段已登记边界 |
| P2-5 | 删除操作回收站化 | ✅ 已落地 | store/trash.go（纯存储）+ api/trash_handlers.go；`_trash` 保留相对路径 + 时间戳后缀 |
| P2-6 | 清理 AIGC 残留与任务编号注释 | 🟡 已收口但**出现新残留** | 存量 5+3 份文档已清；**新文档 docs/FVCC_项目现状与开发步骤.md 带 AIGC frontmatter（2026-09-20 新增），需补清（P3-4）** |
| P2-7 | 测试文件命名规范化 | ✅ 已落地 | git mv 为 handlers_<domain>_test.go 风格 |
| P2-8 | 破坏性操作审计闭环 | ✅ 已落地 | internal/security/audit.go + 8 处调用点 + 单测 |

### 4.1 / 4.2 落地前快照（已删除）

> P2-1 后端分层与 P2-2 前端组件化的**落地前快照、分阶段计划、目标架构、迁移映射、风险表**已随落地完成删除，不再保留于本报告；实施记录与验收依据分别见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` 与 `FVCC/docs/P2-2_细化细则.md`（Checklist 全勾）。保留的唯一开放决策：P2-2 §4.2.3 决策门（组合器方案去留 / Svelte 5 试点）未关闭，属远期演进选项。

### 4.3 当前遗留债务（P3 组，本轮新增，按优先级）

| # | 建议 | 优先级 | 说明 / 证据 |
|---|---|---|---|
| P3-1 | **提交 git 未跟踪改动并打 tag** | 🔴 高 | 工作区 1.4.7~1.4.9 改动未提交（manifest / VERSION / package.json / models.go / store_edl.go / 前端 editor 5 文件 / types.ts / ui.ts / 根 docs 台账 6 份），最新提交仍为 1.4.6 同步（adabef9）——"代码即真相"与快照漂移对冲机制失效，先提交再清理 |
| P3-2 | **temp/ 清理回潮（549.7MB）** | 🔴 高 | 删除 node_modules 全量拷贝、chrome-profile-9223/9224、repro-data、cdp-*-test.mjs、fvcc-win-debug.exe、logs、locks.json、package.json/lock；**确认并处置 temp/secret.key（敏感文件）**；保留 fpk_stage / fpk_stage_145~149 打包 stage（或统一收敛至 fpk_stage_latest） |
| P3-3 | **根目录产物物理清理** | 🟡 中 | 10 个版本包 + fvcc.exe / fvcc.fpk / fvcs-service.exe 移入根 archive/（保留最新 1~2 个在根即可）；确认 task.db 是否仍被引用（当前存储为 JSON），无用则移 archive/ 或删除；credentials.db / master.key / secret.key / locks.json 等运行时数据归位到 <dataDir> 或忽略清单 |
| P3-4 | **文档同步 1.4.9** | 🟡 中 | FVCC/README.md 版本表 1.4.4 → 1.4.9、BUILD.md 1.4.6 → 1.4.9；清除新文档 AIGC frontmatter；明确 docs/FVCC_项目现状与开发步骤.md 与本报告定位（现状指引 vs 治理台账）避免内容重叠双轨漂移 |
| P3-5 | **store 持久化健壮性** | 🟡 中 | 来自项目现状文档：定时备份 / 配置迁移 / 快照回滚（当前 JSON 原子写无备份轮转），列入下一迭代 |
| P3-6 | **ffprobe 运行时环境依赖评估** | 🟡 中 | 来自项目现状文档：确认 ffprobe 在 fnOS 目标环境的安装/路径依赖，避免生产缺探测能力 |
| P3-7 | **覆盖率门槛上调** | 🟢 低 | P2-3 注释自述"当前基线 56.1%，后续上调至 60%"，待 CI 稳定后执行 |
| P3-8 | **远期决策门** | 🟢 低 | P2-2 组合器方案去留 / Svelte 5 试点；VideoInfo 逐字段复制 2 处（§2.3）随下次模型重构合并 |

---

## 5. 结论（第五版，2026-09-20）

FVCC 从首版评价（2026-09-18，1.4.0）到第五版复核（2026-09-20 晚，1.4.9）：**P0/P1/P2 全部落地**，混乱度综合评分 **6.5 → 4.3 → 2.8 → 2.4 → 2.8**（轻度混乱）。架构与质量保障无退化，本轮评分小幅回升源于**清理项回潮**而非新增架构债务：

**保持的改善（高置信证据）**：
1. **安全硬伤清零并闭环**：凭据加密 / 删除回收站化 / 反射白名单 / 审计闭环（8 处调用点）/ git 版本控制——首版五硬伤全部落地且配套单测；
2. **架构演进持续**：P2-1 后端分层（1.4.8/1.4.9 继续细化：stream_media / roots / aliases / config / trash_handlers / edl 扩展，87 个 .go 全绿）+ P2-2 前端组件化（src/lib/ 4 件套，样板残留 0）；
3. **质量保障闭环**：CI 流水线（vet + test -race + 覆盖率 ≥55%）、版本单一来源推进至 1.4.9（三处一致 + version_test 自动校验）；
4. **docs 布局收口**：PRODUCT.md / DESIGN.md 入 FVCC/docs/，"两处文档根"问题解决；根 docs 7 份台账 + FVCC/docs 5 份技术文档分层清晰。

**本轮新增/加重债务（P3 组，均为物理清理与文档同步类，非阻塞项）**：
1. **git 未提交**（1.4.7~1.4.9 工作区改动未入库，快照漂移风险实锤）——最高优先（P3-1）；
2. **temp 回潮 549.7MB**（node_modules / chrome-profile / cdp 调试残留 / temp/secret.key 敏感残留）+ 根目录版本包增至 10 个、运行时数据混入根目录（P3-2/P3-3）；
3. **文档同步滞后**：README（1.4.4）/ BUILD（1.4.6）版本表滞后 3~5 个版本；新文档带 AIGC frontmatter 打破 P2-6 清零（P3-4）；
4. 真实待办转移至工程项：store 持久化健壮性（备份/迁移/回滚）、ffprobe 环境依赖评估（P3-5/P3-6，源自 docs/FVCC_项目现状与开发步骤.md）。

**当前定位**：项目处于"功能已闭环、工程质量高、局部待打磨"阶段，架构级阻塞项为零；后续工作重心应从"治理整改"转为"**保持清理纪律 + 文档实时同步 + 提交纪律**"三件事，防止 Vibe Coding 增量修补再次积累物理与文档债务。

> 维护记录：首版 2026-09-18 上午（1.4.0，6.5/10）→ 第二版 2026-09-18（1.4.2，4.3/10）→ 第三版 2026-09-19（1.4.4，2.8/10）→ 第四版 2026-09-20（1.4.7，2.4/10）→ 第五版 2026-09-20 晚（1.4.9，2.8/10）。第四版曾声明"docs/ 全库 AIGC 0 命中"，第五版实测被新文档打破，已在 §2.6/§4 P3-4 登记。；2026-09-21：P3-4 文档同步已执行（README/BUILD 版本表 → 1.5.3、docs 三份 AIGC frontmatter 清理、UI 文档随 1.5.3 更新、项目现状 §7 三项功能标注已实现），代码已推进至 1.5.3（工作区未提交，P3-1 提交待办仍有效）。
