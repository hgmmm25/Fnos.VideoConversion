# FVCC 改进方向落实记录（2026-09-17 起稿 · 2026-10-11 更新至 v1.5.10）

> **现状快照（2026-10-11 第二十轮，本次落地）**：当前版本 **v1.5.8**（manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，check-versions.ps1 实测通过）。本轮完成 **P3-5 store 持久化健壮性（备份/迁移/快照回滚）**，按阶段 A~D 依次本地提交：**A 备份**（efef4bd：backup.go 定时 30min×12 轮转池 + 写前备份 + manual 永久）、**B 迁移框架**（16f1043：migrate.go data_version 锚点与 schema_version 正交，SQLite 走 import_meta、JSON 走 data_version.json，1→2→3 迁移链 + 全新安装打锚 + 高版本拒启）、**C 快照回滚**（59e4004：restore.go 整包验签换入 + API POST /store/restore 仅后端，zip 白名单防 zip-slip + 64MB 单条上限 + 回滚前自动 prerestore 备份 + 失败零副作用）、**D 收口**（store.go 头部 TODO(P1) 移除 + 版本 1.5.8 + 文档台账回写）。评审口径已锁：secret.key 不入备份、恢复沿用现密钥；恢复后运行中任务重置 QUEUE（同崩溃恢复语义）；本轮不做前端入口。上一轮（1.5.5~1.5.7 Windows 路径边界批次）**已提交 68ebdec 并打 tag v1.5.7**（校正第十九轮"工作区未提交"的过时记载，P3-1 对该批次关闭）。验证：`go build` / `go vet` / `go test -count=1 ./...`（12 包）全绿，coverage 56.7%→**59.0%**（门禁 55%）；-race 需 cgo，留 Linux CI。详见 §十二。

> **现状快照（2026-09-22 第十九轮，历史文档校准）**：当前版本 **v1.5.7**（manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，工作区实测）。自第十八轮（v1.5.3 基线）以来推进 **1.5.4~1.5.7** 四个小版本：**1.5.4 已提交（080754c）**——editor 预览变速播放（PR 惯例 J/K/L，同向 2x→3x→4x、最大 4x、反向重置）+ 播放控制按钮图标化（preview.ts +100 / shortcuts.ts +16 / ui.ts / layout.ts）；**1.5.5~1.5.7 工作区未提交**——一批 Windows 盘符根路径边界修复（`D:\` Clean 后再拼分隔符产生 `D:\\` 双分隔符，导致子路径前缀永不命中、报"无权限/不存在"，统一修 handlers.go 目录浏览 / edl_validate.go / trash.go / smbshare.go 四处）+ 剪辑页多根错位兜底（stream.go resolveMediaFile 指定根失败时对全部授权素材根重试，越权 403 立即拒绝）+ roots.go 素材根本机不可见时回落授权目录首个 + 剪辑页素材递归扫描（assets.ts recursive=true，修复 v1.5.6 子文件夹视频不显示）+ 配套单测（edl_validate/stream/trash/roots 各 *_test.go）。store.go:19 `TODO(P1)`（定时备份/快照回滚）延续，方案评审稿 `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md` 已出、代码未落地；其余未落实项见 §十/§十一。前序结构债（第十八轮基线）：**P0-2 SQLite 迁移已落地**（三个提交 a72b8ff / 9e122d5 / 5dd373e，modernc.org/sqlite v1.14.0 纯 Go 驱动 + 12 张实体表 + schema v1 迁移链 + 存量 JSON 导入 + SQLite 读回主路径与 persist 载体切换，store 内存索引 + 脏标记 Flush 批量落盘）；**P1-1 事件驱动调度已落地**（提交 eab7688：事件循环 + 内存索引 + 定时落盘 + WS 广播全覆盖）。

> 依据《项目分析与改进方向.md》逐项落实。原则：低风险直接改码并验证；结构性大改（SQLite 迁移、事件驱动调度、freecut 收敛）如实说明未落实原因，不强行破坏既有稳定链路。
> 验证基线：`go build ./...` / `go vet ./...` / `go test -short ./...` 全部通过；`tsc --noEmit` 通过；`scripts/check-versions.ps1` 退出码 0。
>
> **现状快照（2026-09-21 第十八轮复核）**：当前版本 **v1.5.3**（manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，version_test.go 自动校验），**已提交并打 tag v1.5.3**（含 SQLite 迁移三提交 a72b8ff / 9e122d5 / 5dd373e 与事件驱动 eab7688）。P0-2 SQLite 迁移已落地（server/internal/store/ 实现 4→8 文件，modernc.org/sqlite v1.14.0 + 12 张实体表）；步骤 3 代码清理与 P2-3 仓库治理（temp 18 项清理释放约 560.3MB；6 个 fpk 归档 archive/fpk/ 完整收纳 v1.1.0~v1.5.3）已落地，详见 §八/§十。当前工作区 5 文件未提交（docs 校准：FVCC/docs/SECURITY.md、STORE_PERSISTENCE_ROADMAP.md + 前端若干），提交纪律延续（P3-1）。

---

## 一、找到的文档

- 主文档：`docs/项目分析与改进方向.md`（9.1 KB，2026-09-17）——17 项改进条目（P0-1 ~ P3-3）。
- 配套：`docs/TASK_REFERENCE.md`（任务编号对照）、`docs/SECURITY.md`（安全基线）、`docs/FVCC_混乱度评价报告_细化版.md`（治理台账，第七版 2026-09-21）、`docs/FVCC_项目现状与开发步骤.md`（现状指引，2026-09-20 新增）、`docs/FVCC_UI设计优化方向.md`（UI 方向，1.5.3）。
- 落地记录：`FVCC/docs/P2-1_后端分层_阶段A实施记录.md`、`FVCC/docs/P2-2_细化细则.md`、`FVCC/docs/API_CONTRACT.md`、`FVCC/docs/DESIGN.md`、`FVCC/docs/PRODUCT.md`。

## 二、改进项总体落实情况（2026-09-21 复核）

| 编号 | 改进项 | 落实状态 | 关键落地证据 |
|---|---|---|---|
| P0-1 | 凭据加密落库 | ✅ 已落地 | `internal/security/crypto.go`（AES-GCM + 主密钥 0600 落盘 secret.key + `enc:v1:` 前缀 + 旧明文兼容迁移 + API 脱敏） |
| P0-2 | 清理工程残留 + .gitignore | ✅ 已落地（2026-09-21 P2-3 仓库治理收尾） | .gitignore 已建；fpk_build_test2 / fpk_inspect / 0 字节残留已清；temp 打包 stage 与调试残留 18 项清理（释放约 560.3MB）；6 个 fpk 归档 archive/fpk/ 完整 v1.1.0~v1.5.3（明细见 §八） |
| P0-3 | git 建立版本控制 | ✅ 已落地 | 上层仓库 `D:\Fnos.VideoConversion` 统一管理，baseline-snapshot 分支已推送 GitHub；分层前基线 tag v1.4.3 |
| P1-1 | updateProfile 反射重构 + 调度 tick 优化 | ✅ 已落地 | `internal/protocol/applyjson.go` 反射白名单（实测 `if v, ok := updates[` = 0）；scheduler tick 合并快照 |
| P1-2 | 抽取 SMB 校验 helper | ✅ 已落地 | `internal/security/smb_validate.go`，5 处调用点统一 |
| P1-3 | 文档落地消除悬空引用 + 数据面 WSS 加密 | ✅ 已落地 | WebVideoEditor_Design/ 01-10 入库 + 速查表；FVCS TLS 证书 + FVCC wss 拨号（第二轮，双端单测全绿） |
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地（文档表滞后见 P3-4） | `version.go` go:embed VERSION + version_test.go 三处一致校验 + `scripts/check-versions.ps1` |
| P1-5 | 依赖瘦身 | 🟡 部分落地 | app/ui 字体 latin 子集瘦身（56→10，-70%）；go.mod tidy 复核零变化、无进一步精简空间 |
| P2-1 | 后端 internal 分层 | ✅ 已落地（2026-09-19） | 11 包 + store/model 子包，根包仅剩 main.go；阶段 A/B/C 完成、shim 已删；实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | `src/lib/` 4 件套（crudActions / scrollPos / useListPage / formBuilder）接入 6 页，样板残留 0；细则见 `FVCC/docs/P2-2_细化细则.md` |
| P2-3 | 建立 CI 流水线 + 覆盖率门禁 | ✅ 已落地 | `.github/workflows/fvcc-ci.yml`（go vet + test -race + 覆盖率 ≥55% + 前端 check:design/tsc/build）+ `scripts/check-coverage.ps1` |
| P2-4 | 统一 API 契约与命名 | ✅ 已收口 | `API_CONTRACT.md` + apierr.go + api.ts 适配兜底；`{error}` 裸格式 grep 0；remote snake_case 为 FVCS 对等协议段已登记边界 |
| P2-5 | 删除操作回收站化 | ✅ 已落地 | `store/trash.go`（纯存储）+ `api/trash_handlers.go`；移入 `<授权根>/_trash`，同名时间戳后缀 |
| P2-6 | 清理 AIGC 残留与任务编号注释 | ✅ 已收口（2026-09-21 补清） | 存量文档已清 + TASK_REFERENCE.md 编号对照表；新文档 AIGC frontmatter 已于 2026-09-21 补清（P3-4） |
| P2-7 | 测试文件命名规范化 | ✅ 已落地 | git mv 为 `handlers_<domain>_test.go` 风格 |
| P2-8 | 破坏性操作审计闭环 | ✅ 已落地 | `internal/security/audit.go` + 8 处调用点（清日志/清缓存/删除视频/服务器/方案/任务/历史/清空回收站）+ 单测 |

## 三、结构性未落实项及原因（2026-09-21 第十八轮校准：P0-2 / P1-1 已落地，剩余 P1-2 / P2-3 门槛延续）

| 编号 | 未落实内容 | 原因 |
|---|---|---|
| P0-2 | ~~JSON 文件存储迁 SQLite~~ | ✅ **已落地（2026-09-21 第十八轮确认）**：a72b8ff 引入 modernc.org/sqlite v1.14.0 纯 Go 驱动、9e122d5 存量 JSON 导入、5dd373e SQLite 读回主路径 + persist 载体切换；12 张实体表 + schema v1 迁移链；store 内存索引 + 脏标记 Flush 批量落盘。注：此前根目录 `task.db` / `credentials.db` 为 FVCS 渲染端运行产物（fvcs-service.exe 常驻根目录所致），现已与 FVCC 自身迁移区分开 |
| P1-1 完整版 | ~~1s ticker 改纯事件驱动调度~~ | ✅ **已落地（2026-09-21 第十八轮确认）**：提交 eab7688 事件循环 + 内存索引 + 定时落盘 + WS 广播全覆盖（第十六轮漏登，第十八轮补登记） |
| P1-2 | 前端收敛 freecut | 跨前端代码库 UI 合并，需产品决策 |
| P2-3 门槛 | 覆盖率 ≥60% 强制门槛 | 已以 ≥55% 落地（口径修复后实测基线 56.7%），上调 60% 列为低优先（P3-7） |

## 四、关键验证结果（最新基线）

1. `cd FVCC\server && go build ./...` / `go vet ./...` / `go test -short ./...` —— ✅ 通过（1.5.3，go 1.27.1；本轮代码清理后 go vet / go test 11 包全绿）
2. 覆盖率门禁：全包口径 **56.7%**（check-coverage.ps1 口径修复 `'.'`→`'./...'` 后实测，阈值 55%，第十七轮）
3. 前端：`tsc --noEmit` / `npm run build`（tsc + check:design + vite）—— ✅ 通过（本轮 npm run build 含 check:design:diff + tsc -b + vite build 通过）
4. 版本校验：`scripts\check-versions.ps1` —— ✅ 退出码 0（manifest / package.json / VERSION 三处一致 = 1.5.3）
5. 早期轮次专项验证（WSS 双端单测、trace ID 注入、回收站/审计单测、并发/fuzz）见 §五 对应轮次摘要

## 五、历史轮次摘要（原 §六~§十四 压缩）

> 各轮完整实施细节见 git 历史与 §一 对应落地文档，此处仅留一行摘要。

| 轮次 | 日期 / 版本 | 核心内容 |
|---|---|---|
| 第一轮 | 2026-09-17 / 1.4.0 | P0-1 GOTOOLCHAIN 修复 + check-versions.ps1；P2-1 调度 tick 优化 + 并发测试；P2-2 并发/fuzz 测试；P2-3 仓库治理（历史 fpk / 工具 / 备份移入 archive/）；P3-1/P3-2 文档漂移说明 |
| 第二轮 | 2026-09-18 / 1.4.0 | P1-3 WSS/TLS 数据面加密：FVCS ws_tls_cert/key 自签证书 + FVCC useWSS/tlsCACert/tlsSkipVerify，双端单测全绿 |
| 第三轮 | 2026-09-18 / 1.4.0 | P2-1 trace ID 全链路注入（请求字段透传 + 包级 T 系列日志函数）+ 耗时直方图 / 失败率指标 |
| 第四轮 | 2026-09-18 / 1.4.0 | P2-5 删除回收站化：trash.go 三路由 + requireAdmin 门禁 + 前端回收站 UI |
| 第五轮 | 2026-09-18 / 1.4.2 | P1-5 前端字体 latin 子集瘦身（56→10，-70%）+ P2-3 CI 流水线（fvcc-ci.yml）+ 磁盘残留复核 |
| 第六轮 | 2026-09-18 / 1.4.2 | P2-6 任务编号注释清理（docs/TASK_REFERENCE.md 对照表）+ P2-3 覆盖率基线 55% 收口 |
| 第七轮 | 2026-09-18 / 1.4.2 | P2-4 API 契约收口复核 + P2-6 AIGC frontmatter 存量清除 + P2-7 测试文件改名 + P2-8 破坏性操作审计闭环 |
| 第十三轮 | 2026-09-19 / 1.4.4 | P2-1 后端 internal 分层（11 包 84 个 .go，根包仅剩 main.go）+ P2-2 前端组件化 4 件套 + 文档体系对齐 1.4.4（Roadmap 见 §六） |
| 第十四轮 | 2026-09-20 / 1.4.7 | 版本迭代 1.4.5~1.4.7（版本包 5→8）+ 前端进度图示化启动（lightweight-charts 异步 chunk 入 app/ui/assets）+ 代码注释清理 + 文档对齐 1.4.7 |
| 第十五轮 | 2026-09-20 / 1.4.9 | 版本迭代 1.4.8 / 1.4.9 + 后端细化拆分（stream_media / roots / aliases / config / trash_handlers / edl 扩展，87 个 .go）+ docs 布局收口（PRODUCT/DESIGN 入 FVCC/docs/）+ 前端图示化阶段 A-D（详见 §七） |
| 第十六轮 | 2026-09-21 / 1.5.3 | 代码清理（步骤 3：scheduler 防注入死分支 / checkUploadProgress/checkDownloadProgress 空占位 / TestB05SideGuards 过时用例 / editor placeholder 注释，go vet + go test 11 包 + npm run build 全绿）+ 仓库治理（步骤 6：temp 18 项清理释放约 560.3MB + 6 fpk 归档 archive/fpk/ 完整 v1.1.0~v1.5.3，详见 §八） |

## 六、Roadmap（结构性子任务，长期）

### 6.1 待办清单（持续更新）

| 编号 | 改进项 | 优先级 | 目标 | 里程碑 | 状态 |
|---|---|---|---|---|---|
| P0-2 | JSON 文件存储 → SQLite | P0 | 消除手写持久化 | M1/M2/M3 | ✅ 已落地（2026-09-21，详见 §十） |
| P1-1 | ticker → 事件驱动调度 | P1 | 纯事件驱动 + 超时兜底 | M1/M2/M3 | ✅ 已落地（2026-09-21，详见 §十） |
| P1-2 | 前端收敛 freecut | P1 | 单前端单入口 | M1/M2/M3 | 🔲 待启动（需产品决策） |
| P3-5 | store 持久化健壮性（定时备份/配置迁移/快照回滚） | P3 | 原子写 + 备份轮转 | 下一迭代 | ✅ 已落地（2026-10-11 v1.5.8，阶段 A~D：efef4bd 备份 / 16f1043 迁移框架 / 59e4004 快照回滚 / 本轮收口，详见 §十二） |
| P3-6 | ffprobe 运行时环境依赖评估 | P3 | 生产缺探测能力兜底 | 下一迭代 | 🔲 待启动（源自现状文档） |
| P3-7 | 覆盖率门槛 55% → 60% | P3 | CI 稳定后上调 | 后续 | 🟡 口径已修复（实测 56.7%），上调延续 |

### 6.2 推进约定

1. 每轮改动后重新执行 §四 验证基线；版本升级时三处（manifest / package.json / VERSION）同步并跑 check-versions.ps1。
2. 破坏性操作（删除/清空/审计）必须过 `internal/security/audit.go` 审计闭环。
3. 结构性大改先出设计文档再动码，落地后回写实施记录（P2-1/P2-2 模式）。

## 七、第十五轮（2026-09-20）：1.4.8 / 1.4.9 版本迭代 + 后端细化拆分 + docs 布局收口

### 7.1 背景

承接第十四轮，版本由 1.4.7 迭代至 **1.4.9**；后端在 P2-1 分层基础上继续细化拆分；docs 双根布局收口；前端图示化阶段 A-D 全部完成。

### 7.2 本轮改动

1. **版本迭代 1.4.8 / 1.4.9**：新增构建产物 `FVCC_v1.4.8_fnos_x86.fpk`、`FVCC_v1.4.9_fnos_x86.fpk` 及最近打包 `fvcc.fpk`，根目录版本包 8→10 个；manifest / package.json / VERSION 三处一致（2026-09-20 实测）。
2. **后端细化拆分（server/internal/）**：
   - `internal/media/` 新增 `stream_media.go`、`roots.go`——预览流媒体与授权根从 api/ 下沉至 media 域；
   - `internal/api/` 新增 `aliases.go`、`config.go`、`hashutil.go`、`security_roots.go`、`trash_handlers.go`（回收站 handler 从 store/trash.go 拆出）；
   - `internal/edl/` 扩展为 4 文件（新增 `edl_support.go`、`export.go`、`audit_hook.go`）；
   - `internal/store/trash.go` 收窄为纯存储（78 行）；
   - 新增测试 `render_progress_test.go`（渲染进度状态机）、`model_convert_test.go`（模型转换回归）。
   - 现状：server/internal 11 包 + store/model 子包共 **87 个 .go**（49 实现 + 38 测试；非测试 13,107 行 / 测试 6,208 行），根包仅剩 main.go（272 行）。
3. **docs 布局收口**：PRODUCT.md / DESIGN.md 从 ui-src/ 迁入 `FVCC/docs/`（原"两处文档根"问题解决）；当前布局 = 根 docs/ 7 份台账 + FVCC/docs/ 5 份技术文档。
4. **前端图示化阶段 A-D 完成**（git e151d08）：tasks 队列可视化（迷你进度条 + 状态占比）、history 趋势图（lightweight-charts 异步 chunk，仅 history 页签加载）、servers 负载圆环、scanner 分布条 + 大列表分页；`app/ui/assets` 收敛为 13 个文件（10 字体 + index css/js + charts chunk）。

### 7.3 验证

- 三处版本一致（version_test.go 自动校验）；`go build` / `go vet` / `go test -short` 全绿；
- `tsc --noEmit` / vite build 通过；check-coverage.ps1 基线 56.1%。

### 7.4 清理回潮登记（P3 组新债务，非本轮改进，供治理台账）

| 编号 | 债务 | 说明 |
|---|---|---|
| P3-1 | git 未提交改动 | 工作区 1.4.7~1.4.9 改动未入库（manifest / VERSION / package.json / models.go / store_edl.go / 前端 editor 等），最新提交 adabef9 仍停留在 1.4.6 同步，快照漂移风险 |
| P3-2 | temp/ 清理回潮 | 膨胀至 549.7MB：node_modules 全量拷贝、chrome-profile-9223/9224、repro-data、cdp-*-test.mjs、fvcc-win-debug.exe、**temp/secret.key 敏感残留** |
| P3-3 | 根目录产物物理堆积 | 10 个版本包 + fvcc.exe / fvcc.fpk / fvcs-service.exe 未移入 archive/（已入 .gitignore 但未物理清理）；FVCS/ 根亦残留旧 fpk（v1.2.0~1.2.6）与 build_check.txt / _cgo_full.txt |
| P3-4 | 文档同步滞后 1.4.9 | FVCC/README.md 版本表仍 1.4.4、根 BUILD.md 仍 1.4.6；新文档 docs/FVCC_项目现状与开发步骤.md 带 AIGC frontmatter，P2-6 清零被打破 |

### 7.5 未落实项延续

P0-2（SQLite 迁移）、P1-1 完整版（事件驱动）、P1-2（freecut 收敛）保持 §三 状态不变；新增 P3-5（store 持久化健壮性）、P3-6（ffprobe 依赖）、P3-7（覆盖率门槛上调）见 §6.1。

---

## 八、第十六轮（2026-09-21）：1.5.3 基线回写 + 步骤 3 代码清理 + 步骤 6 仓库治理

### 8.1 背景

承接第十五轮（1.4.9），版本已推进至 **1.5.3**（manifest / package.json / VERSION 三处一致，2026-09-21 实测）；本轮为文档状态回写 + 两项物理改进落地：步骤 3 代码清理、步骤 6 仓库治理（对应《FVCC_项目现状与开发步骤.md》步骤 3 / 6），版本号未变仍 1.5.3。

### 8.2 步骤 3：清理调度器占位与过时注释（✅ 已完成）

改动文件 3 个：
- `server/internal/scheduler/scheduler.go`：删除 TODO(B-06) 防注入死分支（`main.go:170` 已注入 `scheduler.SetRenderDispatcher(remote)`，原分支为死代码）；删除 `checkUploadProgress` / `checkDownloadProgress` 空占位并更新阶段注释（remote 进度协议 Stage 不含上传/下载阶段内进度，进度仅依赖远端推送推进阶段）。
- `server/internal/scheduler/scheduler_edl_test.go`：移除 TestB05SideGuards 过时用例（守卫已删，用例失去对象）。
- `ui-src/src/pages/editor/index.ts`：placeholder 注释由"接入前占位"更新为"空态兜底"。

验证：`go vet ./...` 全绿；`go test ./...`（server）11 包全绿；`npm run build`（ui-src）通过。

### 8.3 步骤 6：清理低风险残留（✅ 已完成）

- **清理 18 项（全走回收站，释放约 560.3MB）**：`server/temp/` 下 15 个打包 stage 目录（`fpk_stage*`）+ `fvcc-win-debug.exe`（34.19MB）+ 误构建嵌套目录 `FVCC/FVCC/`（22.16MB）+ `server/vet_err.txt`（0 字节空文件）。
- **归档 6 个 fpk 至根仓库 `archive/fpk/`**：`FVCC_v1.4.9~v1.5.3_fnos_x86.fpk` + `fvcc.fpk` 副本（与 v1.5.3 哈希一致 696ED423）；`archive/fpk/` 现完整收纳 v1.1.0~v1.5.3。
- **实测修正（原文档多项不存在）**：`chrome-profile-9223/9224`、`cdp-*.mjs`、`repro-data/`、`temp/secret.key`、`temp/locks.json`、`build*/commit*/fmt_out.txt`、`c2~c6.txt` 均不存在；历史 fpk 非 10 个（v1.4.0~v1.4.8 早已归档）。
- **保留项**：根目录运行时数据（credentials.db / master.key / secret.key / locks.json / tasks.json 等）按禁删原则保留原位；`temp/` 仅剩 `logs/`（9KB）。

### 8.4 git 状态

1.4.9 已提交（cd71f08）；当前工作区为 1.5.3 基线 + 本轮代码清理改动（3 文件）未提交（P3-1 提交待办延续）。

### 8.5 未落实项延续

P0-2（SQLite 迁移）、P1-1 完整版（事件驱动）、P1-2（freecut 收敛）、P3-5 / P3-6 / P3-7 保持 §三 / §6.1 状态不变；P3-2 / P3-3 / P3-4 本轮已处置（详见治理台账第六版）。

---

## 九、第十七轮（2026-09-21）：步骤 4 方案评审稿 + 步骤 5 文档同步 + 步骤 7 质量门禁修复 + P3-1 提交收尾

### 9.1 背景

承接第十六轮（1.5.3），本轮依据《FVCC_项目现状与开发步骤.md》§8 推进剩余步骤：步骤 4（store 备份/迁移/快照回滚，方案评审先行）、步骤 5（文档同步）、步骤 7（质量门禁与 CI 上调前置核查）、P3-1（git 提交收尾）。版本号未变仍 1.5.3。

### 9.2 步骤 4：store 持久化健壮性方案评审稿（✅ 产出方案，代码待评审后落地）

新增 `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md`，对齐 `internal/store/store.go:20 TODO(P1)`：

- **备份**：定时（默认 30min）+ 关键写前（节点/凭据/设置变更）+ 手动 `POST /api/store/backup`；zip 含 manifest（SHA-256 校验），定时备份保留 12 份；
- **迁移**：新增 `migrate.go` 版本化迁移链（From→To + Apply），幂等、迁移前自动备份、失败回滚；projects `SchemaVer=0→1` 收口为显式迁移；
- **快照回滚**：`POST /api/store/restore` 整包恢复（禁止部分回滚），恢复后运行时态重置 QUEUE（同崩溃恢复语义），新增审计类型 `snapshot.restore`；
- **实施拆分** A（备份）→ B（迁移）→ C（回滚）→ D（收口），本轮未动代码。

### 9.3 步骤 5：文档同步（✅ 完成）

- 补写 `FVCC/docs/SECURITY.md`（依据 07 规格 + 源码实测），覆盖网关鉴权（GatewayUserMiddleware / RequireAdmin / 权限分级）、凭据加密（AES-256-GCM + secret.key 0600）、限流四类阈值、审计类型表、路径四层校验 + EDL 结构化白名单、传输链路引用——消除 `server/main.go:205/207`、`models.go:158` 对 docs/SECURITY.md 的悬空引用；
- 核对 FVCC/docs 现存 5 份文档与 TASK_REFERENCE §4 映射一致，无其他缺口；
- 新增文档均 grep 复核无 AIGC frontmatter 残留。

### 9.4 步骤 7：质量门禁真实化（✅ 完成）

- **check-coverage.ps1 口径 bug 修复**：测试目标原为 `'.'`（仅主包 fvcc/main），主包无测试文件导致恒 0%，CI 覆盖率门禁形同虚设；改为 `'./...'` 后实测总覆盖率 **56.7%**（与历史基线 56.1% 吻合），55% 门槛 PASS（EXIT=0）；
- **flaky 测试修复**：`TestP1EventDrivenQueueDispatch` 在覆盖率插桩/并行下偶发 FAIL（根因：handleEvent QUEUE 分支为 `go s.processTask` 异步协程，派发调用先于 store 状态更新可见）；改为 2s 超时等待事件 + 状态轮询断言，覆盖率模式连跑 5 次全绿；
- `fvcc-ci.yml` 基线注释更新 56.1% → 56.7%；实测 56.7% < 60%，门槛不上调（P3-7 延续）；
- 验证：`go vet ./...` 全绿、check-coverage.ps1 -Short PASS、scheduler 覆盖率模式稳定。

### 9.5 P3-1：git 提交收尾（✅ 本轮执行）

提交工作区全部改动并打 tag v1.5.3，快照漂移风险清除；详见 git log / git tag。

### 9.6 未落实项延续

P0-2（SQLite 迁移）、P1-1 完整版（事件驱动）、P1-2（freecut 收敛）、P3-5（store 持久化代码落地，方案已出待评审）、P3-6（ffprobe 依赖）、P3-7（覆盖率上调 60%）、步骤 8（端到端验收）保持 §三 / §6.1 状态不变。

> **第十八轮校准注记（2026-09-21）**：本行所记"P0-2 / P1-1 未落实"已过时——git log 证实 SQLite 迁移（a72b8ff / 9e122d5 / 5dd373e）与事件驱动调度（eab7688）均在 1.5.3 tag 内，第十七轮提交收尾时一并入库，当时文档未同步；第十八轮已确认落地并补登（见 §十）。

---

*维护记录：2026-09-17 起稿（第一轮）→ 2026-09-18（第二~七轮）→ 2026-09-19（第十三轮，1.4.4）→ 2026-09-20（第十四轮，1.4.7）→ 2026-09-20 复核更新（v1.4.9，压缩历史轮次为 §五 摘要，新增 §七 第十五轮）→ 2026-09-21（第十六轮，1.5.3，新增 §八 本轮落实日志）→ 2026-09-21（第十七轮，1.5.3，新增 §九 步骤4方案稿/步骤5文档同步/步骤7门禁修复/P3-1提交收尾）→ 2026-09-21（第十八轮，1.5.3，新增 §十 docs 校准：P0-2 SQLite 迁移 / P1-1 事件驱动补登为已落地，7 份台账全面对齐代码现状）→ 2026-09-22（第十九轮，1.5.7，新增 §十一：1.5.4 编辑器变速播放 J/K/L + 按钮图标化（已提交 080754c）、1.5.5~1.5.7 Windows 盘符根路径边界修复 + 剪辑页多根兜底 + roots 回落 + 素材递归扫描 + 配套单测（工作区未提交），全套台账对齐 1.5.7）。*


## 十、第十八轮（2026-09-21）：docs 与代码现状对齐校准

### 10.1 背景

全局任务"FVCC 文档与实际代码现状对齐"。本轮对根 docs/ 7 份台账全文通读，并实测 FVCC 代码/产物（目录递归扫描、go.mod、git log/tag、manifest / package.json / VERSION、前端 src 规模、server .go 统计），系统性修正文档与代码的漂移项。

### 10.2 代码实测结论（2026-09-21）

- **版本**：manifest / package.json / VERSION 三处一致 = **1.5.3**，已打 tag v1.5.3（旧文档多处记"工作区未提交"过时）；
- **P0-2 SQLite 迁移已落地**：git log 三个提交 a72b8ff（引入 modernc.org/sqlite v1.14.0）/ 9e122d5（存量 JSON 导入）/ 5dd373e（SQLite 读回主路径 + persist 载体切换）；`server/internal/store/` 实现文件 4→8（新增 sqlite.go / sqlite_import.go / sqlite_load.go / sqlite_persist.go）；12 张实体表；store.go 头部新增"脏标记 + Flush 批量落盘"说明，:19 TODO(P1) 定时备份/快照回滚仍在；
- **P1-1 事件驱动调度已落地**：提交 eab7688（第十六轮漏登，本轮补登）；
- **server 规模**：实际 **96 个 .go**（实现 53 / 测试 43；非测试 14,218 行 / 测试 6,853 行），旧文档记 87（49+38）/ 13,107 / 6,208；
- **前端规模**：ui-src/src 33 个 TS / **10,315 行**（旧记 10,035）；
- **覆盖率基线**：**56.7%**（第十七轮口径修复后实测），旧记 56.1%；
- **FVCC/docs**：实际 7 份（含 SECURITY.md、STORE_PERSISTENCE_ROADMAP.md），旧记 5 份；
- **temp/ 残留**：FVCC/temp 仅 logs/（9KB）；server/ 根现存 `build_err.txt` 0 字节空文件 1 个（旧文档误记 vet_err.txt）；
- **git 工作区**：未提交 5 文件（FVCC/docs/SECURITY.md、STORE_PERSISTENCE_ROADMAP.md + 前端若干），为第十七轮 P3-1 收尾后新积累，提交纪律延续。

### 10.3 本轮文档改动（docs/ 7 份）

| 文档 | 主要改动 |
|---|---|
| `FVCC_混乱度评价报告_细化版.md` | 第七版：§2.6 docs 布局 5→7 份、§2.7 补 build_err.txt 实测、§2.8 测试 38→43 / 6,208→6,853、§3 评分表加第七列并更新结论、§4.0 P1-1/P2-3、§4.3 P3-1/P3-5/P3-7 状态更新 + 新增 P3-9、§5 结论与维护记录更新 |
| `FVCC_项目现状与开发步骤.md` | §3.1 存储改 SQLite 主载体 + JSON 回退、§3.3 CI 基线 56.7%、§4.1 store 模块描述更新、§6.1 store.go:19 状态、§6.3 SECURITY.md 已补写、§6.4 补 build_err.txt、§6.5/§7/§8 步骤 4/5/7 状态更新、附索引代码规模 96/21,071 |
| `FVCC_UI设计优化方向.md` | 规模 10,315 行、版本 1.5.3 已提交打 tag、复核日期统一 2026-09-21 |
| `TASK_REFERENCE.md` | 第六版：§2.1 补 P0-2 注释批次（SQLite 迁移落地后已无歧义）登记 |
| `项目分析与改进方向.md` | §3 / §4.2 / §5 中 P0-2 三处"未落地"改"已完成"（SQLite 迁移落地） |
| `SECURITY.md` | 补 P0-2 SQLite 载体与凭据/数据存储现状陈述，与 `FVCC/docs/SECURITY.md` 交叉引用 |
| `IMPROVEMENT_LOG.md` | 本文件：§三 / §四 / §6.1 状态更新 + 本 §十 记录 |

### 10.4 待办延续

- **P3-1**：工作区 5 文件未提交，随下轮一并提交；
- **P3-5**：store 备份/回滚代码落地（方案稿已出待评审）；
- **P3-6**：ffprobe 运行时依赖评估；
- **P3-7**：覆盖率实测 56.7% < 60%，门槛不上调；
- **P3-9**：server/ 根 build_err.txt 待清（本次新登记）；
- **步骤 8**：端到端验收未执行（IMPROVEMENT_LOG 无 v1.5.3 全链路走查记录）。

---

## 十一、第十九轮（2026-09-22）：1.5.4~1.5.7 迭代——编辑器变速播放 + Windows 路径边界修复

### 11.1 背景

承接第十八轮（v1.5.3 基线），版本迭代至 **1.5.7**（manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，工作区实测）。本轮跨 1.5.4~1.5.7 四个小版本：1.5.4 已提交（tag 未打，commit 080754c，2026-09-21 19:46），1.5.5~1.5.7 为 2026-09-22 一批 Windows 端边界修复，工作区未提交。

### 11.2 1.5.4（已提交 080754c）：编辑器预览变速播放 + 按钮图标化

- **变速播放（PR 惯例 J/K/L）**：`ui-src/src/pages/editor/preview.ts`（+100 行）新增 `setSpeed(dir)`——dir=-1 快退 / 1 快进 / 0 停止；同向按 `2x→3x→4x` 递增，最大 4x；反向或 K 键重置回 1 档并暂停。驱动 `video.playbackRate`，快进/快退按钮标题标注档位。`shortcuts.ts`（+16 行）接入 J/K/L 键位。
- **播放控制按钮图标化**：`ui.ts`（ICON_PATHS +10 行，新增 rewind/play/ff 等内联 SVG）+ `preview.ts`/`layout.ts`（+8 行）——预览控制条文字按钮替换为图标按钮。
- **验证**：三处版本一致（version_test.go 自动校验）；`go build` / `go vet` / `go test` 全绿；`npm run build`（check:design:diff + tsc -b + vite build）通过。

### 11.3 1.5.5~1.5.7（工作区未提交；2026-10-11 校正：后已提交 68ebdec 并打 tag v1.5.7）：Windows 端路径边界与素材定位修复

| # | 修复 | 位置 | 说明 |
|---|---|---|---|
| 1 | **盘符根双分隔符边界** | `api/handlers.go` browseDirs、`edl/edl_validate.go` edlWithinRoot、`store/trash.go` withinRoot、`smbshare/smbshare.go` withinOrEqual | Windows 盘符根（`D:\`）经 `filepath.Clean` 后仍以分隔符结尾，再拼 `+Separator` 产生 `D:\\` 双分隔符，任何子路径（`D:\xxx`）都无法命中前缀——表现为"授权根为盘符根时读文件/回收站/共享判定全部报无权限或不存在"。四处统一加边界特判：根以分隔符结尾时直接以该根为前缀。 |
| 2 | **剪辑页多根错位兜底** | `api/stream.go` resolveMediaFile | 前端 root 仅传 `src`/`proxy` 枚举、不含根信息，而素材列表可见于任意授权根，指定根（videoRoot/缺省 src）解析失败即报 E_ASSET_MISSING。改为：指定根报"不存在/不可访问"时，对全部授权素材根（proxy 场景取 `<根>/_proxy`）逐个兜底重试，命中即成功；越权（软链逃逸 403）立即拒绝，绝不跨根。 |
| 3 | **素材根不可见回落** | `media/roots.go` ResolveMediaRoots | 新增 `localRootVisible`：配置根本机不可访问（dev 下默认 POSIX 根 `/media/videos` 在 Windows 不存在、或 SMB/UNC 未挂载）或未配置时，回落授权目录首个，与素材扫描/浏览（`pv.AccessPaths`）口径一致，消除"列表可见、票据申请 E_ASSET_MISSING"错位。 |
| 4 | **素材递归扫描** | `ui-src/src/pages/editor/assets.ts` | `recursive` 由 `false` 改 `true`：修复 v1.5.6 剪辑页子文件夹内视频不显示；递归模式下子文件夹视频直接汇入列表，`ensureSubDir` 目录行不再触发。 |

- **配套单测**：`api/stream_test.go`（+53，多根兜底 + 越权拒绝）、`edl/edl_validate_test.go`（+52，盘符根边界）、`store/trash_test.go`（+19，withinRoot 边界）、`media/roots_test.go`（新增，localRootVisible/回落）。
- **验证**：`go build ./...` / `go vet ./...` / `go test ./...`（11 包）全绿；`npm run build` 通过。

### 11.4 待办延续

- **P3-1**：1.5.5~1.5.7 工作区改动（含版本号三处 + 后端 6 文件 + 前端 assets.ts + 单测 + docs 校准）未提交，1.5.4 已提交但 1.5.5~1.5.7 未打 tag，提交纪律延续；（2026-10-11 校正：该批次已提交 68ebdec 并打 tag v1.5.7，P3-1 对该批次关闭）
- **P3-5 / P3-6 / P3-7 / P3-9 / 步骤 8**：状态同第十八轮 §十，未变化。（2026-10-11 校正：P3-5 已于第二十轮落地关闭，见 §十二；其余延续）

## 十二、第二十轮（2026-10-11）：P3-5 store 备份/迁移/快照回滚落地（v1.5.8，本次）

### 12.1 背景

承接第十九轮（v1.5.7 已提交 68ebdec + tag），按 `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md` 评审稿分四阶段落地 **P3-5**，每阶段独立本地提交：A 备份（efef4bd）→ B 迁移框架（16f1043）→ C 快照回滚（59e4004）→ D 收口（本轮）。评审口径（2026-10-11 确认）：备份默认 **30min 定时 × 12 份保留**（auto/prewrite/premigration/prerestore 共用轮转池），manual 永久；**secret.key 不入备份**、回滚沿用现密钥；恢复后**运行中任务重置 QUEUE**（同崩溃恢复语义）；本轮**仅后端 API**，不做前端入口。

### 12.2 阶段 A（efef4bd）：备份

- `store/backup.go`：`Backup(kind)` 对内存快照整体打包 zip（12 个实体 JSON + manifest.json），manifest 携带 **SHA-256 + size 逐文件校验**与 SQLite `schema_version`；servers/settings 沿用 `enc:v1:` 密文入包。kind 常量 auto/manual/prewrite（本轮加 premigration/prerestore），非 manual 全部进 **12 份轮转池**，manual 永久保留。
- **定时备份**：30min ticker（store 内 goroutine）；**关键写前备份**（preCriticalBackup）在破坏性批量写前触发。
- API：`GET /api/store/backups`（列表）、`POST /api/store/backup`（手动，RequireAdmin）。

### 12.3 阶段 B（16f1043）：版本化数据迁移框架

- `store/migrate.go`：新增与 SQLite DDL `schema_version` **正交**的数据面锚点 **`data_version`**（目标 3）。锚点双载体：SQLite 复用 `import_meta` 表（零 DDL 变更）+ JSON 镜像 `data_version.json`。
- 迁移链 `dataMigrations`：1→2 `task-columns-default`（复用 migrateTaskColumns）、2→3 `project-schema-ver`（SchemaVer 0→1）；每步幂等（只补零值）+ **锚点渐进打戳**（崩溃重放安全）。
- Bootstrap 语义：无锚点且业务集合全空 = 全新安装，直接打目标锚不跑链不备份；无锚点有数据 = 视为 1 跑全链；**锚点高于目标 = 拒绝启动**（提示升级）。
- `Load()` 拆分为 `loadAll()`（持锁）+ `applyDataMigrations()`（解锁后跑，因 Backup/AppendAudit 内部自锁）；原 store.go / store_edl.go / sqlite_load.go 三处就地补丁式迁移收口进链，`postLoadLocked` 的 SchemaVer 兜底移除。迁移前自动 `Backup(premigration)`（失败仅告警不阻断），审计 `schema.migrate`。

### 12.4 阶段 C（59e4004）：快照整包回滚

- `store/restore.go`：`Restore(backupID)`——**先验签解析、后换入**（失败零副作用）。zip 条目名必须**精确匹配白名单**（12 实体 JSON + manifest）= zip-slip 防御（不落地解包，只在内存解析）；单条 64MB 上限；manifest 逐文件 size + SHA-256 验证；`manifest.SchemaVersion` 高于本机拒绝。备份 ID 合法性：`filepath.Base` + `pathutil.WithinRoot` + `parseBackupName` 三重校验。
- 换入流程：Flush → **prerestore 备份（失败即中止回滚**，破坏性操作必须先有回滚点）→ 持锁整包换入 12 集合 + `postLoadLocked()`（运行中任务重置 QUEUE）→ 全量 persist 回载体 → 幂等重跑迁移链（兜住阶段 B 之前的旧备份）→ 审计 `snapshot.restore`(ok)。
- API：`POST /api/store/restore`（RequireAdmin，`{backupId}`），`security.AuditDestructive` 以真实操作者身份并记（成功双写：数据侧 + 身份侧）；失败审计 `security.alert`(error)。
- 与 §4 评审稿的有意偏差：JSON 时代"Rename 换入 + 重新 Load"改为 **SQLite 主载体下的内存验签换入**，理由已写入 restore.go 头注释。

### 12.5 阶段 D（本轮）：收口 + 验证

- `store.go` 头部 `TODO(P1)`（原 :19，定时备份/快照回滚）移除，注释标注 P3-5 已补齐。
- 版本 **1.5.7 → 1.5.8**：manifest / ui-src/package.json / server/internal/version/VERSION 三处同步，check-versions.ps1 通过。
- 单测：`migrate_test.go`（遗留数据跑全链 / 全新安装只打锚 / 锚点 2 只跑后半链 / 未来版本拒启）、`restore_test.go`（往返 + QUEUE 重置 + 重启存活 / 篡改 zip 拒绝且内存不动 / zip-slip 拒绝 / 未来 schema 拒绝 / 非法 ID 拒绝）；`sqlite_import_test.go` 两处断言按阶段 B 副作用校准（audit_log +1 schema.migrate；import_meta 允许 data_version 行）。
- 验证：`go build ./...` / `go vet ./...` / `go test -count=1 ./...`（12 包）全绿；覆盖率 **56.7% → 59.0%**（门禁 55% 通过）；`-race` 需 cgo（本机无 gcc），留 Linux CI 跑。观察项：`scheduler.TestSchedulerTickConcurrentSafe` 在全量并行首轮因主机负载偶发收敛超时（10s deadline 内 100 任务未派发完），单跑与全量复跑均通过（10.5s，贴线），非本单元改动引入的功能回归，登记待观察（如需上调 deadline 另行处理）。
- 文档回写：本台账 §十二 + §6.1 P3-5 行；`docs/FVCC_项目现状与开发步骤.md`（步骤 4 关闭 / §4 store 模块 / 版本线）；`docs/FVCC_混乱度评价报告_细化版.md` P3-5 行；`FVCC/docs/STORE_PERSISTENCE_ROADMAP.md` 变更记录与评审要点收口。

### 12.6 待办延续

- **P3-5**：✅ 本单元关闭（阶段 A~D 四提交均在本地 baseline-snapshot，未 push 未打 tag，待评审）。
- **P3-6**（ffprobe 运行时依赖评估）/ **P3-7**（覆盖率 55%→60% 上调，现实测 59.0% 已接近阈值）/ **P3-9** / **步骤 8**（真实 NAS+Windows e2e）：延续，未变化。
- **前端回滚入口**：本轮明确不做，待单元评审通过后另行排期。

## 十三、第二十一轮（2026-10-11）：P1-2 前端收敛（freecut 双轨）定案关闭 + v1.5.8 推送/tag 收口

### 13.1 背景

P3-5 单元评审通过：v1.5.8 阶段 A~D 提交（efef4bd / 16f1043 / 59e4004 / 24cbbc5）与后续构建修复（6043fbd）均已推送 `fork/baseline-snapshot`，附注 tag **v1.5.8** 已建并指向 24cbbc5（与 v1.5.7 先例一致：tag 打在版本收口提交，不含其后修复）。P3-1 对该单元关闭。本轮无代码改动，为**产品决策定案 + 文档回写**：关闭项目分析清单 P1-2"前端收敛"。

### 13.2 定案：方案 A——维持零框架，"借 freecut 设计不借底座"

对四个候选方案对比后定案（决策轴：架构兼容 / 首屏体积守则 / 迁移成本 / 双轨维护消除）：

| 方案 | 结论 |
|---|---|
| A. 维持零框架，仅借 freecut 设计语言与契约范式 | ✅ **采纳**——已成事实路线（UI 优化方向 P0/P1/P2 + 阶段 A-D 全按此落地，DESIGN.md R1-R10 门禁挂 build）；框架引入三触发器（跨页签状态联动频繁出错 / el() 细节 bug 占比上升 / 团队扩大）均未触发（§7.2） |
| B. 引入轻量框架（Svelte 5/Vue3/Preact）重构 editor 区 | ⏸ **改绑多轨立项**——不再独立待决；`{videoTracks,audioTracks}` 双栏时间线是手写 DOM 复杂度陡增点，该立项启动时一并评审 |
| C. 复用 freecut 底座（React 19 + WebGPU） | ❌ **排除**——根本架构冲突（freecut 浏览器本地解码剪辑 vs FVCC 素材不出 NAS + `/stream` 票据流预览 + FVCS 侧渲染）；294,057 行体量击穿首屏硬约束（主 chunk 192.6KB 基线不劣化）；引入即维护 fork，双轨反升三轨 |
| D. freecut 独立部署 + FVCC 外链/嵌入 | ⏸ **搁置**——仅当产品决定同时提供全功能 Web 剪辑时再评估（跨源凭据/票据鉴权成本、fnOS 内嵌体验割裂） |

双轨维护消除方式由原建议"收敛后删除冗余 editor 代码"调整为**边界固化**：FVCC editor 定位调度端轻剪辑，freecut 保持独立上游参照，不再互为迁移目标。

### 13.3 文档回写

| 文档 | 改动 |
|---|---|
| `项目分析与改进方向.md` | 头部快照漂移说明第五次回写（基线 1.5.8）；§4.2 问题 4 改"已定案"；§5 P1"前端收敛"勾选关闭并记录定案要点 |
| `FVCC_UI设计优化方向.md` | §7.2 增"定案转正（2026-10-11）"段；头部状态行补定案说明 |
| `FVCC_项目现状与开发步骤.md` | §6.5 P1-2 定案关闭入表；P2-2 Svelte 5 试点决策门改绑多轨立项 |
| `FVCC_混乱度评价报告_细化版.md` | §4.1/4.2 决策门表述收口；§4.3 P3-8 收窄（仅剩 VideoInfo 复制项） |
| `TASK_REFERENCE.md` | §2.1 编号漂移登记补 P1-2 双语义（项目分析=前端收敛，已定案关闭；混乱报告/§2.1=SMB 校验 helper，早已落地） |

### 13.4 待办延续

- **P3-5 后续**：备份/回滚前端入口仍未做（后端 API only），待排期；
- **P3-6**（ffprobe 运行时依赖评估）/ **P3-7**（覆盖率 59.0%→门槛上调 60%）/ **P3-9**（server/ 根 build_err.txt 待清）/ **步骤 8**（真实 NAS+Windows e2e）：延续；
- **新绑定项**：时间线双栏/多轨立项 → 连带评审框架试点（B）+ 后端/FVCS EDL 契约兼容 + EDL 转场（models.go:463）同属跨端特性，建议合并为一个跨端立项单元评估。

## 十四、第二十二轮（2026-10-11）：步骤 8 真实 NAS+Windows e2e 走查 + 缺陷批量修复（v1.5.9 起点）

### 14.1 背景与走查环境

P3-5 收口后启动步骤 8（端到端验收），环境为**真实 NAS（Linux/amd64，v1.5.8）+ 本机 Windows 渲染机（FVCS）**。执行方式：用户在真机逐项走查并回传结果，代理据此在代码侧定位缺陷、按批修复并回写本台账（决策：A 档逐项独立提交 + 全绿 + 收尾 STOP-for-review；随批回写）。`GET /info` 实测：`ffprobe:true`、`accessPaths` 4 根（`/vol3/1000/T`、`/vol2/1000/Test`、`/vol2/1000/RP-1`、`/vol4/1000/RP-2`）、`runtime:linux/amd64`——P3-6 探测真实可用，无降级。

### 14.2 走查结果（闭环①~⑤）

| 闭环 | 内容 | 结果 |
|---|---|---|
| ① | 扫描 → 转码提交 | ✅ 通过（部分目录文件转码失败，归入 #5 排查） |
| ② | 代理生成 → 预览切换 | ❌ 缺陷：生成成功后不显示 / 重启页面后 ready 态丢失（提示重新生成）/ 判定"很迷"（部分 4K 不出现在代理候选） |
| ③ | EDL → 渲染派发 | ❌ 缺陷：FVCS 拒 `E_EDL_INVALID 禁止绝对路径（sourceRoot）`；非默认根渲染在 FVCC 直接报"素材不存在" |
| ④ | 回收站 | ✅ 删除（入回收站）/清空均正常；⚠️ 视频删除入口仅右键菜单，可发现性差 |
| ⑤ | 断联恢复 | ⚠️ 浏览器切后台再切回秒重连；FVCS 后端断开后有时需手点"测试连接"才恢复在线 |

### 14.3 缺陷台账与分级

| # | 缺陷 | 端 | 档 | 状态 |
|---|---|---|---|---|
| 5 | 代理提交不带素材根 → 非默认根文件报"素材不存在"（前端 `createProxy` 未发 root） | 前端为主 | A | ✅ 本轮修复 |
| 6 | 代理 ready 态无法从服务端回填（仅瞬时 WS `proxy_ready`）；`VerifyProxyMeta` 帧率严格串相等易误判 invalid | 前端+后端 | A | ✅ 本轮修复 |
| 7 | `judgeVisibility` 未纳入分辨率 → 4K 素材代理判定"很迷" | 前端 | B | 待产品决策（阈值） |
| 8 | 渲染根与代理取根不一致（`mountPath+sourceRoot` 双拼 + 拒绝对根）；旧闸门 4b 误挡未配 smbSharePath 的渲染 | 跨端(FVCS 落地取根 + FVCC 回退闸门) | A | ⚠️ 复验判原修法错误 → 返工跨端对齐（见 §14.4 #8），待真机复验 |
| 9 | 渲染/EDL 单根载荷模型（`WireClip` 无 per-clip root） | 跨端(FVCC+FVCS) | B | 待立项 |
| 10 | 视频删除入口仅右键，无行内/批量按钮 | 前端 | A | ✅ 本轮修复 |
| 11 | FVCS 重连后节点状态需手点"测试连接"才广播 online | 后端/调度 | A | ✅ 本轮修复 |

### 14.4 A 档逐项修复（随批回写）

**#5 代理提交带素材根（本轮提交）**：

- 根因：`assets.ts:requestProxy` 调 `api.createProxy({assetId, file})` 未带 `root`，后端据此回退默认素材根解析 `file`，非默认根（如 `/vol3/1000/T` 之外）下的文件 `filepath.Join(srcLocal,file)` 不存在 → `E_ASSET_MISSING "素材不存在或不可访问"`。后端本已支持授权根绝对路径 + 同素材按根去重（`handlers_proxy.go` 修复①/`sameProxyTaskSrcRoot`），缺的只是前端把根传下去。
- 修复：`AssetRef` 增 `sourceRoot`（`toAssetRef` 于扫描时写入所属根绝对路径）；`requestProxy` 提交带 `root: a.sourceRoot`。
- 遗留（归 #9/多根收口）：`proxyByFile`/`GET /proxy` 仍以相对 `file` 为键，跨根同名相对路径存在态串扰——单视图扫描已限定单根，影响面小，待渲染多根立项一并处理键复合化。

**#6 代理就绪态回填（本轮提交）**：

- 后端：新增 `GET /api/proxy`（`handlers_proxy.go:handleProxyList`，router 只读注册）——返回 `asset_proxies` 中 `state=ready` 的映射（`file`/`proxyFile`/`durationMs`），令 ready 态可持久回填，不再依赖瞬时 WS。
- 前端：`api.ts` 增 `listProxies()`；`assets.ts` 增 `hydrateProxies()`，剪辑页挂载即拉取并回填 `proxyByFile`（跨扫描保留），`toRef` 自动标注 ready。
- `VerifyProxyMeta`（`media/proxy.go`）帧率校验由严格串相等改为**数值近似（相对容差 1%）**，无法解析时回退严格比较——修复重编码/VFR 下分数串不同（如 `"25"` vs `"25/1"`）误判 valid 代理为 invalid 的问题；单测补 `"帧率同值异串(30 vs 30000/1001)"` 通过、`"25/1"` 仍拒。
- 验证：`go build/vet/test` 全绿（含新增 `TestM4ProxyListHydration`）、`npm run build` 通过。

**#8 渲染根跨端对齐（返工，v1.5.9 复验后）**：

- 原判（闸门 4b）经真机复验证伪：闸门 4b 以 `rootMapped`（仅当配置 `smbSharePath` 才为 true）作早失败，但**代理/转码恰恰不依赖 `smbSharePath`**——它们靠 `SMBUser`（NAS 管理员账号）经 `BuildSMBURL→FindShareForPath` 把本地绝对根映射为可挂载 UNC。用户配的是 `SMBUser`，闸门 4b 遂把一个"本应像代理一样成功"的渲染挡成了 400。
- 真因（FVCS 渲染落地取根错误）：`BuildSMBURL` 返回的 UNC **已下沉到素材根**（如 `\\NAS\share\videos`），挂载点 `mountPath` 即素材根本身；`GEN_PROXY` 正是 `srcBase=mountPath + SrcFile` 解析（proxy.go:144），故代理正常。而 `RENDER_EDL` 旧实现 `srcBase = mountPath + payload.SourceRoot`（render_edl.go:271）把素材根**重复拼接**，且 `ValidateRenderEDLPayload→ValidateRootRelPath` 直接拒绝对 `sourceRoot`（"禁止绝对路径"）——两条都让渲染从未真正跑通。
- 修复（跨端对齐，用户选定）：
  - **FVCS** `render_edl.go`：`srcBase := mountPath`（与代理同机制；素材根由挂载点给出，片段相对根）。`pkg/protocol/edl.go`：渲染根 `sourceRoot/destRoot` 改为"下发专用"——新增 `ValidateRootPath`（仅拒 `..` 穿越，允许绝对/相对写法），不再阻断 FVCC 未映射时的本地绝对根（该字段仅供调度侧 `toSMBUNC→BuildSMBURL` 解析共享，不参与落地）；共享测试向量 `PL-ROOT-ABS`（destRoot 绝对）期望由 `E_EDL_INVALID` 改为 `OK`。
  - **FVCC** `handlers_render.go`：移除误加的**闸门 4b** 与 `isAbsoluteEDLRoot`；未映射绝对根照常入队，交调度器按与代理一致的 `SMBUser` 映射下发（闸门 8 的 warn-skip 过渡分支保留）。测试 `TestB04RenderRootUnmappedAbsoluteFailsEarly` 改写为 `TestB04RenderRootUnmappedDispatchesLikeProxy`（断言入队成功）；回退 `newRenderTestEnv`/`newD02RenderEnv` 为通过闸门 4b 而临时加的 `SMBSharePath=root`。
- 验证：FVCS `go build ./...` + `pkg/protocol`、`pkg/task` 测试绿、`cmd/service` 编译通过；FVCC 全套 `go build/vet/test` 绿。**待真机复验**：默认根与非默认根渲染均应像代理一样跑通（需**重建并重新部署渲染机 `fvcs-service.exe`** 与 FVCC fpk）。
- 关联：本返工使渲染取根与 `GEN_PROXY` 对齐，非默认根渲染随之可下发；根写法/多根路径的完整契约仍归 #9。

**#10 视频删除入口可发现性（本轮提交）**：

- 现状：scanner 视频删除仅存在于右键上下文菜单（`createContextMenu`），新用户难发现；`回收站`按钮可见但"删除到回收站"的入口不可见。
- 修复：`scanner.ts` 工具栏补「删除」按钮（`btn-sm text-danger`，随选中态启用/禁用，与 `创建转码任务` 一致由 `updateStats` 驱动）。点击复用既有 `handleDelete`——`selectedVideos` 非空即按选中集批量删除（弹确认框、移入回收站），不新增删除逻辑。右键菜单保留。
- 验证：`npm run build` 通过。

**#11 FVCS 重连自动恢复 online（本轮提交）**：

- 根因：`main.go` OnDisconnect 把 `store` 服务器状态置 `offline`；FVCS 重连并上报 Hello 时 `ApplyNodeHello`（`node.go`）却**沿用该 stale 的 store 状态**广播（`if sv.Status!="" { status = sv.Status }`），既不落库 online 也不广播 online → 节点长期显示 offline，直到用户手点"测试连接"（`testServer` 才写 online）。
- 修复：收到 Hello = 该节点 WS 会话在线，`ApplyNodeHello` 改为 `store.UpdateServerStatus(id,"online")` 并恒广播 `online`（低版本 Agent 仍在线，仅经 `nodeVersionOK` 单独把关选机，不受影响）。`node.Store` 接口补 `UpdateServerStatus`。
- 测试：新增 `TestB08ApplyNodeHelloSelfHealsOffline`（初始 offline → Hello 后 store online + 广播 online）；既有 `TestB08ApplyNodeHello` 不回归。全套 `go build/vet/test` 绿。

### 14.5 待办延续

- **A 档**：#6 代理回填、#5 代理带根、#10 删除按钮、#11 节点自动恢复复验通过（真机）。**#8 复验判原闸门 4b 为误修 → 已返工为跨端对齐**（FVCS 渲染 `srcBase=mountPath` 与代理一致 + FVCC 回退闸门 4b），两侧代码全绿，待真机复验：默认根与非默认根渲染均应像代理跑通——**需重建并重新部署渲染机 `fvcs-service.exe`（.104）+ FVCC fpk**。
- **版本收口**：应用版本三方对齐 **1.5.9 → 1.5.10**（`server/internal/version/VERSION` + `manifest` + `ui-src/package.json`，`check-versions.ps1` 口径一致；注：fnOS 侧只能按更高版本号覆盖安装，故每次重下发 fpk 须递增版本）。文档版本线（本台账头部 / `FVCC_项目现状与开发步骤.md` §步骤8 与"当前版本"）同步对齐 1.5.10。构建部署后即可供真机复验。
- **B 档（延后）**：#7 分辨率阈值（待用户定阈值）、#9 渲染多根（跨端立项，与 §十三 多轨/EDL 契约立项合并评估）。
- **P3-6**：e2e 实测 `ffprobe:true`，运行时依赖评估可据此关闭。
- **收尾**：A 档复验通过后走版本收口（v1.5.10）+ 文档对齐，随后 STOP-for-review——**未经确认不 push/不打 tag**。
