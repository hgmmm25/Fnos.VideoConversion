# FVCC 改进方向落实记录（2026-09-17 起稿 · 2026-10-14 更新至 v1.6.8）

> **现状快照（2026-10-14 第三十四轮，Tailwind CSS 4 大版本专项）**：当前版本 **FVCC v1.6.8 / FVCS v1.3.1**（FVCS 零改动）。本轮还清第三十二~三十三轮点名的**依赖刷新主题唯一余项**——`tailwindcss` **3.4.19 → 4.3.3**：`tailwind.config.js` 整文件删除，token 映射迁入 `ui-src/src/style.css` 的 `@theme inline`；38 个 `--c-*` 令牌（明暗两套共 76 条定义）由裸 RGB 通道改为完整 `rgb()` 颜色（v4 不替换 `<alpha-value>`，`/NN` 透明度改编译期 `color-mix(in oklab, …)`）；暗色改 `@custom-variant dark`；PostCSS 插件换 `@tailwindcss/postcss` 并删 `autoprefixer`（前缀由 v4 内建 Lightning CSS 负责）；改名与语义漂移逐项对齐（`shadow-sm`→`shadow-xs`、`shadow`→`shadow-sm`、`outline-none`→`outline-hidden`）；扫描面 `source(none)` + 显式 `@source` 锁定。收益：`npm audit` 现存 **7 条告警（2 moderate / 5 high）全部清零**、`npm outdated` 归零、CSS gzip 10.00 → **9.78 kB**。**依赖刷新主题至此完整收口，无余项。** 随批产品裁决（用户）：接受 v4 移除 `button { cursor: pointer }` 的新默认。按出包纪律 bump **1.6.7 → 1.6.8** 并重打 fpk（11,013,500 B），NAS 真机视觉对照由用户执行中。详见 §二十五。
> **现状快照（2026-10-14 第三十二~三十三轮，依赖/工具链专项批次）**：**当时版本 FVCC v1.6.7 / FVCS v1.3.1**（现已被 v1.6.8 取代，见上一段快照）。
> 第三十二轮还清了第三十一轮点名的唯一实质落后项——`modernc.org/sqlite` **v1.14.0 → v1.60.1**（跨 46 个 minor）：先把 store 层对驱动的接触面量清楚（`sql.Open` 无 DSN 参数、三条 PRAGMA 走 Exec、无驱动 API、SQL 为纯 SQLite 子集），再用**老驱动真实产出的 fvcc.db** 由新驱动经实际 store 路径 Open→migrate→写回→二次 Load 验证（WAL 保持、`schema_version` 不误重置、AUTOINCREMENT 续号、立即落盘可见）；副作用是依赖面收窄（删 cc/ccgo/opt/strutil/token/uint128/x-mod/x-sync/x-tools/shellquote 十项，`go.sum` 净减 109 行），代价是二进制 +565 KB、fpk +2.4%（10,752,127 → 11,011,826 B）。按出包纪律 bump **1.6.6 → 1.6.7** 并重打 fpk，真机复验由用户在 .106 执行。详见 §二十三。
> 第三十三轮把 Node 运行时从**已 EOL 的奇数 Current 线 v25.9.0** 升到 **v26.11.1**（系统级 MSI，SHA256 与官方逐字符核对，另存 v25.9.0 回滚包）；装前查进程确认全机唯一 node 是 Qoder 自身辅助进程、无项目任务在跑。升级点四处一起对齐：本机运行时、`package.json` 新增 `engines.node >=26.0.0`、CI `setup-node` `22`→`26`、`@types/node` `^22`→`^26`（解析 26.6.5）。`npm ci` + `npm run build` 绿且 **asset 内容哈希逐项不变**，故判定无需重打包、不再 bump。详见 §二十四。
> **依赖刷新主题至此收口，余项只有 `tailwindcss` 3.4.19 → 4.3.3 专项**（同时是 `npm audit` 现存 7 条告警的唯一修复路径，全部源自 tailwindcss 3 的传递链，Node 26 升级零新增告警）。**（该专项已于同日第三十四轮以 v1.6.8 落地，7 条告警清零，见上段快照与 §二十五。）**规模与覆盖率在新驱动+新运行时下**亲跑复测**：internal 12 包 / 110 个 .go（58 实现 17,197 + 52 测试 9,183）/ TS 33 文件 11,474 行 / 覆盖率 **60.7%**，与第二十九轮逐项一致。9 份叙述性长台账本轮一并回写至 1.6.7 / 1.3.1 基线（第三十一轮挂账还清）。
> **现状快照（2026-10-13 第三十一轮，工具链口径收口）**：**当时版本 FVCC v1.6.6 / FVCS v1.3.1**（现已被 v1.6.7 取代，见上一段快照；第三十轮依赖刷新批次已随 `1dcb774` 推送并打 tag **v1.6.6**，本轮补记其台账）。本轮零代码改动，收的是**工具链与依赖版本的叙述口径**：`BUILD.md` / `build-env.ps1` / `build.ps1` 里"FVCS 用 go1.24.6 + `GOTOOLCHAIN=local`"的遗留写法全部作废，双端统一 **go.mod `go 1.27.1` 下限 + `GOTOOLCHAIN=auto`**（本机 `sdk\go1.24.6` 目录实际已不存在）；新增 BUILD.md「工具链与依赖版本基线」表登记 2026-10-13 审计结论——npm 侧除刻意推迟的 `@types/node`(^22→26.6.5) 与 `tailwindcss`(3.4.19→4.3.3) 外无落后项，Go 侧除 `modernc.org/sqlite`(v1.14.0→v1.60.1，待专项) 外全部为当前版本，上游 Go 已到 go1.27.2 但 `go` 指令是下限而非 pin 故不抬。文档版本引用（根 README / FVCC README / BUILD / API_CONTRACT）同步至 1.6.6 / 1.3.1。详见 §二十二。
>
> **现状快照（2026-10-13 第二十九轮，文档对齐）**：**无代码/版本变更**，当时当前版本 **v1.6.5**（现已被 v1.6.6 取代）。第二十八轮交付后 1.6.0/1.6.4/1.6.5 三版未回写叙述性长台账，本轮把 9 份文档基线统一至 **FVCC v1.6.5 / FVCS v1.3.0**：`项目现状与开发步骤`、`混乱度评价报告`（**第十一版，2.4→2.3**，九维度维持、仅两条硬证据下调）、`项目分析与改进方向`（**第八次回写**）、`UI设计优化方向`（新增 §7.7 记 1.6.x 视觉线）、`TASK_REFERENCE`（**第十版**，登记批次编号族与"1.6.1~1.6.3 未入库"最高易错点）、根/FVCC 两份 `SECURITY`（口径无漂移，仅追加复核并区分"版本治理 ≠ 数据面安全"）、`DESIGN.md`（新增热路径与状态反馈惯例）、`STORE_PERSISTENCE_ROADMAP`（前端入口仍未完成）。规模实测：internal 12 包 / Go 110 个 .go（58 实现 17,197 + 52 测试 9,183）/ TS 33 文件 11,474 行；覆盖率本轮未重跑（沿用第二十六轮 60.7%）。附带纠正：`FVCC/README.md` 曾把该批次写成"1.6.1 ~ 1.6.4 版本线"，实为交付 **1.6.4**（代码注释里的 `v1.6.1-A*`/`v1.6.2-B*/C*`/`v1.6.3 修复` 是开发期本地中间 bump 编号，从未入库）。详见 §二十一。
>
> **现状快照（2026-10-13 第二十八轮，本次落地）**：当前版本 **v1.6.5**（manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，`check-versions.ps1` 实跑通过）。本轮两段：上半段收口**任务页视觉改进批次 A+B+C**（方案 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md` + 三方外部模型评审，已随 `fe06e52` 以 **v1.6.4** 推送并打 tag）；下半段修 **VERSION 第三真源漂移**——v1.6.4 出包只 bump 了 manifest + package.json，漏了 `internal/version/VERSION`（仍 1.6.0），已推送的包「设置」页经 `/api/info` 显示 1.6.0；并把 **版本一致性校验从"Windows 静默 SKIP"改为真跑**（旧 `version_test.go` 相对路径写成 `../../manifest`、解析到不存在的 `FVCC/server/manifest`，读取失败又走 `t.Skipf`，故本机 `go test ./...` 长期"绿而无效"，漂移只在 Linux CI 才暴露）。用户拍板走 **发 v1.6.5** 而非强推重指 v1.6.4 tag（tag 历史干净）。详见 §二十。

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
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地（文档表滞后已随 P3-4 于 2026-10-12 对齐 1.5.12 清零） | `version.go` go:embed VERSION + version_test.go 三处一致校验 + `scripts/check-versions.ps1` |
| P1-5 | 依赖瘦身 | 🟡 部分落地 | app/ui 字体 latin 子集瘦身（56→10，-70%）；go.mod tidy 复核零变化、无进一步精简空间 |
| P2-1 | 后端 internal 分层 | ✅ 已落地（2026-09-19） | 11 包 + store/model 子包（v1.5.8 增 pathutil 后 12 包），根包仅剩 main.go；阶段 A/B/C 完成、shim 已删；实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | `src/lib/` 4 件套（crudActions / scrollPos / useListPage / formBuilder）接入 6 页，样板残留 0；细则见 `FVCC/docs/P2-2_细化细则.md` |
| P2-3 | 建立 CI 流水线 + 覆盖率门禁 | ✅ 已落地 | `.github/workflows/fvcc-ci.yml`（go vet + test -race + 覆盖率 ≥60% + 前端 check:design/tsc/build）+ `scripts/check-coverage.ps1`（门槛 55%→60%，2026-10-12 P3-7 关闭，实测 60.7%） |
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
| P1-2 | 前端收敛 freecut | ✅ **已定案关闭（2026-10-11 第二十一轮，§十三）**：方案 A——维持零框架、借 freecut 设计不借底座；框架试点绑定多轨 `{videoTracks,audioTracks}` 立项再评审 |
| P2-3 门槛 | 覆盖率 ≥60% 强制门槛 | ✅ **已落地（2026-10-12 第二十六轮，P3-7 关闭）**：补 logger/smbshare/media helper 测试后实测 59.0%→**60.7%**，check-coverage.ps1 默认阈值与 CI `-Threshold 60` 同步上调（历史：55% 落地，基线 56.7%→59.0%） |

## 四、关键验证结果（最新基线）

1. `cd FVCC\server && go build ./...` / `go vet ./...` / `go test ./...` —— ✅ 通过（**最新基线 1.6.8 全量复绿**，2026-10-14 第三十四轮 Tailwind 4 专项复跑——该轮零后端改动，`go build`/`go vet` 干净、`go test ./... -count=1` 14 包全绿；历史口径：1.6.7 sqlite 专项后于 2026-10-14 复绿、1.6.6/1.6.5 第二十八~三十轮、1.5.12 全量复绿（第二十四轮）、1.5.3 批次 go vet / go test 11 包全绿）
2. 覆盖率门禁：全包口径 56.7% → 59.0% → **60.7%**（check-coverage.ps1 口径修复 `'.'`→`'./...'` 后实测；阈值演进 55%→**60%**，2026-10-12 第二十六轮 P3-7 关闭，60.7% ≥ 60% PASS。**第二十八/二十九两轮未重跑覆盖率**，引用值均为第二十六轮实测）
3. 前端：`tsc --noEmit` / `npm run build`（tsc + check:design + vite）—— ✅ 通过（**最新基线 1.6.8**，2026-10-14 第三十四轮 Tailwind 4 迁移后复跑：`check:design` 33 文件无违规 → `tsc -b` → `vite build` 48 模块 / 908 ms，产物 `../app/ui/assets/index-By6Fdeqj.css` 49.95 kB（gzip 9.78 kB）；配套 `npm audit` **found 0 vulnerabilities**、`npm outdated` 空输出。历史：1.6.5 及以前 CSS 为 42.78 kB / gzip 10.00 kB）
4. 版本校验：`scripts\check-versions.ps1` —— ✅ 退出码 0（manifest / package.json / VERSION 三处一致 = **1.6.8**，2026-10-14 第三十四轮 bump 后实测；另注意 `ui-src/package-lock.json` 的根 `version` 不随手工 bump 跟进、也不在校验范围内，需 `npm install --package-lock-only` 同步，已写入 BUILD.md 版本同步清单注释。配套 FVCS v1.3.1，其两处真源（`pkg/version/VERSION` + `version_test.go` 断言字面量）已由 `go test ./pkg/version` 校绿。脚本仓库根由 `$PSScriptRoot` 推导、支持 `-RepoRoot` 覆盖；`internal/version/version_test.go` 在 Windows 本机真跑，反例改坏 manifest 即判红。历史：1.6.7 / 1.6.5 / 1.5.12 / FVCS v1.2.6 口径见第二十四~三十三轮）
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
| P1-2 | 前端收敛 freecut | P1 | 单前端单入口 | M1/M2/M3 | ✅ 已定案关闭（2026-10-11 第二十一轮，方案 A 维持零框架，详见 §十三） |
| P3-5 | store 持久化健壮性（定时备份/配置迁移/快照回滚） | P3 | 原子写 + 备份轮转 | 下一迭代 | ✅ 已落地（2026-10-11 v1.5.8，阶段 A~D：efef4bd 备份 / 16f1043 迁移框架 / 59e4004 快照回滚 / 本轮收口，详见 §十二） |
| P3-6 | ffprobe 运行时环境依赖评估 | P3 | 生产缺探测能力兜底 | 下一迭代 | 🟡 实际已满足（2026-10-11 真机 `/info` ffprobe:true，fnOS mediasrv 提供，无 stub；仅剩可选 probe-vs-ffprobe 差异核对） |
| P3-7 | 覆盖率门槛 55% → 60% | P3 | CI 稳定后上调 | 后续 | ✅ 已关闭（2026-10-12 第二十六轮：补 logger/smbshare/media helper 测试，实测 60.7%，check-coverage.ps1 默认阈值与 CI `-Threshold 60` 同步上调） |

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

*维护记录：2026-09-17 起稿（第一轮）→ 2026-09-18（第二~七轮）→ 2026-09-19（第十三轮，1.4.4）→ 2026-09-20（第十四轮，1.4.7）→ 2026-09-20 复核更新（v1.4.9，压缩历史轮次为 §五 摘要，新增 §七 第十五轮）→ 2026-09-21（第十六轮，1.5.3，新增 §八 本轮落实日志）→ 2026-09-21（第十七轮，1.5.3，新增 §九 步骤4方案稿/步骤5文档同步/步骤7门禁修复/P3-1提交收尾）→ 2026-09-21（第十八轮，1.5.3，新增 §十 docs 校准：P0-2 SQLite 迁移 / P1-1 事件驱动补登为已落地，7 份台账全面对齐代码现状）→ 2026-09-22（第十九轮，1.5.7，新增 §十一：1.5.4 编辑器变速播放 J/K/L + 按钮图标化（已提交 080754c）、1.5.5~1.5.7 Windows 盘符根路径边界修复 + 剪辑页多根兜底 + roots 回落 + 素材递归扫描 + 配套单测（工作区未提交），全套台账对齐 1.5.7）。* → 2026-10-11（第二十轮 P3-5 v1.5.8 §十二 / 第二十一轮 P1-2 定案 §十三 / 第二十二轮 步骤8 e2e 缺陷批量修复 + #8 跨端返工 v1.5.10 §十四）→ 2026-10-12（第二十三轮，1.5.11，新增 §十五：#8 再复验——渲染取根「有界单根」提交侧修复，片段所属授权根透传 + `resolveRenderRoots`，不动 checksum；含旧项目回落边界；同轮追加 §15.6 第三层——FVCS `makeSrcResolver` 对齐代理（`BuildSMBPath`，修 `sourceRoot 为空` 误报）+ 重建部署 `fvcs-service.exe`）→ 2026-10-12（第二十四轮，1.5.12，新增 §十六：渲染出片后 FVCC 卡在「收尾·100%」——补 RENDER_EDL 终态轮询收口 `pollRenderTerminal`/`completeRenderTask`（FVCC-only，镜像 `proxyWf.Poll`；Success→COMPLETED+归档 / Failed→ERROR / Cancelled→CANCELLED））→ 2026-10-12（第二十五轮，基线 1.5.12，新增 §十七：文档全面对齐轮——FVCC/README、FVCC/docs 7 份、根 docs/ 7 份台账与 BUILD.md 统一对齐 v1.5.12 / FVCS v1.2.6，P3-4 文档滞后清零，纯文档不动代码）→ 2026-10-12（第二十六轮，基线 1.5.12，新增 §十八：v1.5.12 打注解 tag 并推送（P3-1 收尾）+ P3-7 关闭（补 logger/smbshare/media 三份测试，实测 60.7%，门禁 55%→60%）+ P3-9 关闭（server/ 根 build_err.txt 已删），剩余 P3 债务仅 P3-6/P3-8）。*


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

## 十五、第二十三轮（2026-10-12）：#8 再复验——渲染取根「有界单根」提交侧修复（v1.5.11）

### 15.1 背景（v1.5.10 复验结果）

用户部署 v1.5.10（含 §14.4 跨端返工）到真实 NAS（.106）+ 渲染机（.104）复验：
- **代理侧**（默认根 / 非默认根 / 重建）全部正常。
- **渲染侧仍报 `提交失败：素材不存在（E_ASSET_MISSING）`**——但报错发生在**提交阶段**（`precheckAssets` 本地预检），并非节点落地阶段。说明 §14.4 的 FVCS 跨端返工方向正确、已生效，残留缺陷在 FVCC 提交侧取根口径。

### 15.2 真因：渲染提交一律回落 `videoRoot`，丢弃片段所属根

- **代理**逐片段以「素材自身根」解析：`handlers_proxy.go` 用 `h.ResolveRoot(in.Root)`，前端提交带 `root`（§14.4 #5）。
- **渲染**提交 `precheckAssets(localSource, clips)` 的 `localSource` 来自 `resolveEDLRoots`，一律取 `Settings.videoRoot`（缺省 `/media/videos`）。而 `videoRoot/exportRoot` **并非设置表单字段**（`handlers.go` 保留默认值），用户实际素材在 `/vol*/1000/*` 等非默认授权根下 → `filepath.Join(videoRoot, clip.file)` 本地不存在 → 提交侧 404 `E_ASSET_MISSING`。
- 数据模型缺口：项目持久化的 `EDLClip` 只有相对 `file`，**未携带片段所属授权根**；代理根来自 `AssetRef.sourceRoot`，渲染路径未透传，故提交时无根可依。

### 15.3 修复（有界单根，FVCC-only，不动 checksum/FVCS）

- **前端**：`EDLClip` 增可选 `sourceRoot`（`types.ts`）；`buildClip` 从素材 `ClipSource.sourceRoot` 复制该根（`clipOps.ts`）；预览打点入片路径（`editor/index.ts onAddClip`）按 `assetId` 回填素材根。
- **后端**：`model.EDLClip` 增 `SourceRoot`（`sourceRoot,omitempty`，仅项目落盘 + 提交消费；**渲染线协议 `WireClip` 不变** → 幂等 `checksum`（clips+profile）与 FVCS 载荷零影响）。
- **取根逻辑**（`handlers_render.go` 闸门 4）：新增 `resolveRenderRoots(clips)`——
  - 片段全部同根 → `matchAuthorizedSrcRoot` 校验其为授权根，命中则**以该根为素材根、其下 `_exports` 为成品根**（与代理「素材/产物同根」同机制），彻底修复非默认根误报；未授权 → `E_ROOT_UNKNOWN`(400)。
  - 片段跨多根 → 明确 `E_EDL_INVALID`(400)「暂不支持跨多个授权目录混合素材」（真正多根混合归 #9 跨端立项）。
  - 片段均无根（旧项目）→ 回落 `resolveEDLRoots` 既有 `videoRoot/exportRoot` 口径（向后兼容）。
  - 抽公共 `mapEDLRoots`（本地根↔协议根/共享相对根/本地可见性），默认口径与片段根口径共用。

### 15.4 已知边界（须告知用户）

- **旧项目**（本次修复前保存、片段无 `sourceRoot`）仍回落 `videoRoot`，非默认根素材**需重新添加片段**（重建时带上所属根）方能渲染；新建/重加项目不受影响。
- 真正「跨多个授权目录混合素材」的渲染仍未支持（`E_EDL_INVALID` 明确拒绝），归 #9。

### 15.5 验证与收口

- 新增后端单测（`handlers_render_test.go` §5）：`TestB04RenderResolvesClipSourceRoot`（非默认根片段按所属根取根、`payload.sourceRoot/destRoot` 校验）、`TestB04RenderLegacyNoClipRootFallsBackToVideoRoot`（旧无根片段回落 videoRoot）、`TestB04RenderMixedClipRootsRejected`（跨根 400）、`TestB04RenderUnknownClipRootRejected`（未授权根 400 `E_ROOT_UNKNOWN`）。
- 全套 `go build/vet/test ./...` 绿、`npm run build` 通过（design check 通过）。
- **版本收口**：三方对齐 **1.5.10 → 1.5.11**（`VERSION` + `manifest` + `ui-src/package.json`；fnOS 需更高版本覆盖安装）。
- **待真机复验**（用户自测，未 push/未 tag）：默认根与非默认根渲染提交均应通过预检并像代理跑通；FVCC fpk 需重装至 .106。**注**：复验又暴露渲染落地第三层（见 §15.6），需一并重新部署加固后的 `fvcs-service.exe`。

### 15.6 第三层（同轮追加）：渲染落地 `makeSrcResolver` 与陈旧 exe

v1.5.11 部署后真机复验：FVCC 提交侧不再 `E_ASSET_MISSING`，任务成功入队、SMB 共享挂载成功（`\\192.168.1.106\Test` / `\\...\RP-1`），但 **FVCS 渲染落地报 `E_PAYLOAD_INVALID: sourceRoot 为空`**（前端卡在「准备 · 0%」）。定位：

- 报错点 `render_edl.go` `makeSrcResolver` 的 `!HasSuffix(base, sep) → else 返回"sourceRoot 为空"` 守卫：把「带尾分隔符的合法挂载根」误判为空。`MountSMBShare` 返回 `smbPath + "\"`（smb.go:47），故 `mountPath = \\host\share\`；盘符根 `X:\` 经 `filepath.Clean` 亦保留尾分隔符——两者都会落入该错误分支。§14.4 返工把 `srcBase` 由 `mountPath+payload.SourceRoot` 改为 `srcBase=mountPath`（对齐代理），却**未同步加固 `makeSrcResolver`**，代理因直接 `BuildSMBPath(mountPath, file)` 而免疫，渲染因此仍跑不通。
- **同时判定运行节点为陈旧构建**：对纯 UNC，当前源码 `filepath.Clean(\\…\Test\)` 实测会去掉尾分隔符 → 走解析器分支、并不报此错（已用一次性 Go 程序在 Windows 复核）。既然真机报出该错，说明 `.104` 上执行的 `fvcs-service.exe` 并非 §14.4 之后的构建。
- 修复（FVCS-only，对齐 GEN_PROXY）：`makeSrcResolver` 重写为——仅当根 trim 后为空才判 `E_PAYLOAD_INVALID "素材根为空"`；相对素材用 **`smb.BuildSMBPath(srcBase, rel)`** 拼接（与代理同一函数），越权判定用 `filepath.Clean` 归一根前缀 + `isSafeRelPath`（已拦 `..`/绝对/盘符）。彻底移除"带尾分隔符即空"的错误守卫。
- 测试：新增 `pkg/task/render_src_resolver_test.go`——①UNC 带/不带尾分隔符、POSIX 根、本地目录根均构造成功，仅空/纯空白报 `E_PAYLOAD_INVALID`；②真实临时文件命中返回落地路径，缺失 `E_ASSET_MISSING`，穿越/绝对 `E_ASSET_NOT_IN_ROOT`。`go vet ./pkg/task` + `pkg/task` 测试绿；`build.ps1 -Target FVCS` 重建 `fvcs-service.exe`（CGO，00:42，FVCS_DIR + FVCC_DIR 各一份）。
- **收口修正**：本轮 §15.3 所述「不动 FVCS」仅对提交侧成立；渲染落地取根经此第三层仍需一处 FVCS 加固 + **重新部署渲染机 exe**。故 #8 全链路 = ①FVCS 落地 `srcBase=mountPath`（§14.4）②FVCC 提交侧有界单根（§15.3）③FVCS `makeSrcResolver` 对齐代理（本节）——三层齐备方等价代理。

## 十六、第二十四轮（2026-10-12）：渲染出片后 FVCC 卡在「收尾 · 100%」——RENDER_EDL 终态收口缺失（v1.5.12）

### 16.1 现象（v1.5.11 三层落地后再复验）

§15.6 加固的 `fvcs-service.exe` 部署后真机复验：FVCS 侧渲染**已跑通**——finalize ffmpeg 完成，产物落 `\\192.168.1.106\RP-1\_exports\1_20261012_2.mp4`（可直接打开播放），节点日志 `Task completed` / `渲染完成`。但 **FVCC 任务面板永远停在「转码中 / 收尾 · 100%，已用时 01:38」**，进度不再变化、也不移入历史。

### 16.2 真因：渲染只有进度回路，没有终态回路（FVCC 调度侧缺口）

与代理链路对比即可定位——三类任务的"完成落地"路径不对称：

- **转码**：`checkTranscodeProgress`（scheduler.go）按窗口 `QueryTaskDetail`，`Success → UpdateTaskStatus(COMPLETED,100)` + 广播 + `MoveToHistory`（scheduler.go:662-670）。
- **GEN_PROXY**：`proxyWf.Poll`（media/proxy.go:117）按窗口 `QueryTaskDetail` 拿终态，`SUCCESS → Finish` / `FAILED → FailPermanent`。
- **RENDER_EDL**：两处都不沾。`processTask` 的 `case StatusTranscoding: if isRenderTask(t)` 分支仅对 GEN_PROXY 调 `proxyWf.Poll`，RENDER_EDL **直接 return**（无终态轮询）；而 WS 进度入口 `HandleRemoteProgress → store.ApplyRenderProgress`（store_edl.go:135）对渲染任务**只更新 stage/percent，永不改 Status**——即便收到 `finalize·100%` 帧，任务仍是 `Transcoding`。

根因确认：FVCS 的 WS 只回推 `Progress` 帧（protocol.go `buildProgressFrame`），**不发完成/失败终态事件**（`remote.readPump` 仅路由 `Progress`/`Hello`）；终态 `Success/Failed/Cancelled` 只在 FVCS 任务存储里（task.go:1179），只能经 `QueryTaskDetail` 轮询获取。RENDER_EDL 缺这条轮询 → 节点已出片而本端永不落地。

### 16.3 修复（FVCC-only，镜像 proxyWf.Poll）

- `scheduler.go`：新增窗口字段 `renderPollAt sync.Map` + 常量 `renderPollInterval=5s`；`processTask` 的渲染分支改为 `switch TaskType{ GenProxy→proxyWf.Poll；RenderEDL→pollRenderTerminal }`。
- 新增 `pollRenderTerminal(t)`：按窗口 `QueryTaskDetail`，二次读取防覆盖已收口/暂停任务后——`Success→completeRenderTask` / `Failed→failRenderTaskPermanent`（沿用节点 `E_*` 码与 `RemoteFailureMessage` 明细）/ `Cancelled→置 CANCELLED`；查询失败保持 RUNNING 等下个窗口（抖动不误杀，与代理同口径）。
- 新增 `completeRenderTask(t)`：渲染产物已由节点写入共享导出目录，**无需像代理那样本地 ffprobe 校验/登记**——置 `COMPLETED·100` → `BroadcastTaskUpdate(...string(StatusCompleted)...)`（口径对齐 SMB 转码完成，前端 `store.applyTaskUpdate` 识别 `COMPLETED` 终态并移入历史）→ 5s 后 `MoveToHistory`。
- **注（WireName vs Go 串）**：渲染进度全量帧用 `Status.WireName()`（`RUNNING/SUCCESS/FAILED`），而转码/本次完成收口用 `string(TaskStatus)`（`TRANSCODING/COMPLETED/ERROR`）；前端终态判定按后者（`store.ts:163`）。故完成广播刻意采用 `string(model.StatusCompleted)="COMPLETED"`，命中前端终态分支。

### 16.4 测试

- 新增 `internal/scheduler/render_poll_test.go`（4 例，均不依赖真实节点）：`completeRenderTask` 成功落 `COMPLETED·100` 且清窗口；缺 `RemoteTaskID` 空转保持 RUNNING；节点记录消失 → `E_NODE_NOT_FOUND` 不可重试失败；窗口内二次轮询被节流（不清计时，保窗口语义）。
- `Success/Failed/Cancelled` 的 `QueryTaskDetail` 结果分支需真实 WS/httptest 端，与既有 `checkTranscodeProgress`/`proxyWf.Poll` 一致交由真机复验收口。
- **真机复验结论（2026-10-12，v1.5.12）**：Success 收口通过——渲染出片后任务由「收尾·100%」正常翻 `COMPLETED` 并进历史；且本轮观察到进度推进明显平滑（此前长期是 1%→99% 的跳变，因 WS 阶段帧与轮询窗口共同落地）。
- **失败/取消分支说明（暂无复现场景，遇到再收）**：`pollRenderTerminal` 的 `Failed→failRenderTaskPermanent`（带节点 `E_*` 码/阶段/明细）与 `Cancelled→CANCELLED` 两条分支代码已就位并有单测兜底（节点记录消失/窗口节流等不依赖真实结果的分支已覆盖），但真机上**尚未遇到自然的渲染失败/取消场景**去触发这两条 `QueryTaskDetail` 终态返回；按用户决定，**待实际出现失败任务时顺带核验落 ERROR/带码 + 取消落 CANCELLED 即可，无需专门构造**。
- 全套 `go build ./...` + `go vet ./internal/scheduler/` + `go test ./internal/scheduler/` 绿。

### 16.5 收口

- **版本收口**：三方对齐 **1.5.11 → 1.5.12**（`VERSION` + `manifest` + `ui-src/package.json`；fnOS 需更高版本覆盖安装）。
- **待真机复验**（用户自测，未 push/未 tag）：渲染出片后 FVCC 任务应在轮询窗口内（≤5s）由「收尾·100%」翻为 `COMPLETED` 并移入历史；渲染失败应落 `ERROR` 并带节点错误码；主动取消应落 `CANCELLED`。**仅需重装 FVCC fpk 至 .106**（本轮无 FVCS 改动，`.104` 的 `fvcs-service.exe` 沿用 §15.6 加固版）。
  - **复验进度（2026-10-12）**：Success 收口 ✅ 通过（含进度平滑显示改善）；Failed→ERROR / Cancelled→CANCELLED ⏸ **暂无自然场景，按用户决定遇到失败任务时顺带核验，不专门构造**。
- **#8 全链路最终态**：①②③（取根三层，§14.4/§15.3/§15.6）+ ④完成收口（本轮）= 渲染端到端等价代理。遗留仍为 B 档 #7（分辨率阈值，产品决策）、#9（真·多根混合渲染，跨端）。

### 16.6 附带修复：FVCC 渲染根校验器与 FVCS 对齐（共用向量 `PL-ROOT-ABS` 长期红）

全量 `go test ./...` 复核时暴露一处早于本轮即存在的红测试：`internal/edl.TestD02EDLVectorsPayload`。共用向量 `FVCS/pkg/protocol/testdata/edl_vectors.json` 的 `PL-ROOT-ABS`（destRoot=`/media/videos/_exports`）期望 `OK`，注记「渲染根为下发专用写法…跨端对齐 #8」——该向量由 §14.4（71403e1）随 FVCS `ValidateRootPath`（允许空/相对/绝对、仅拒 `..`）一并引入，但 **FVCC 侧 `validateRootRelPath` 从未同步**，仍走严格的 `validateRelPath`（L1 直接拒绝对 `/` 开头），导致两端对同一载荷裁决不一致、FVCC 测试长期红（`build.ps1` 的 FVCC 目标不跑 `go test`，故未被 CI 拦截）。

- 修复（FVCC-only）：`edl_validate.go` `validateRootRelPath` 重写为与 FVCS `ValidateRootPath` 同规则——`TrimSpace` 后空放行，`ReplaceAll(\→/)` 分段仅拒 `..` 穿越段（错误码 `E_ASSET_NOT_IN_ROOT`，对齐 `PL-ROOT-TRAVERSAL`）。渲染根的**授权边界不在此字符级校验**，而由提交侧 `resolveRenderRoots`→`matchAuthorizedSrcRoot`（精确匹配授权根）与落地侧 §15.6 `makeSrcResolver`（`BuildSMBPath`+根前缀+`os.Stat`）双重把关，故放宽绝对/反斜杠写法是安全的跨端对齐，不削弱安全。
- 验证：`internal/edl` + `internal/api` + 全量 `go test ./...` 复绿；`go vet` 干净。本轮 fpk 因此改动一并重打。

---

## 十七、第二十五轮（2026-10-12）：文档全面对齐轮——全部台账统一对齐 v1.5.12 / FVCS v1.2.6（P3-4 清零）

### 17.1 背景与口径

全局任务"按最新版本更新文档"。范围 = `FVCC/README.md` + `FVCC/docs/` 7 份 + 根 `docs/` 7 份台账 + 根 `BUILD.md`（根 `README.md` 的对齐已先行提交 4dcdb26）。口径：对齐的是**现状陈述**（版本号、代码规模、包结构、helper 真源路径、落实状态），各轮**带日期的历史快照按台账惯例保留原文**，只追加复核注记；纯文档改动，不动代码与版本号（基线仍 1.5.12）。本轮实测口径：internal 12 包 / 107 个 .go（58 实现 + 49 测试）/ main.go 274 行；前端 TS 33 文件 11,257 行；coverage 59.0%。

### 17.2 逐文件落实

| 文件 | 改动 |
|---|---|
| `FVCC/README.md` | 版本表与三处一致推进至 **1.5.12**（配套 FVCS v1.2.6）；目录树重写（新增 `pathutil/` 行、`store/` 改"SQLite 主载体 + JSON 备份/迁移/快照回滚"、`api/` 补 backup_handlers、docs/ 列全 7 份、脚本归属修正 check-design.mjs∈ui-src/scripts 与 check-versions/coverage∈根 scripts/）；**新增「近期版本线摘要（1.5.4~1.5.12）」表**；设计规格链接修复为 `../../WebVideoEditor_Design/` 可点路径；P1-4/P2-1/P2-2/P2-6 行状态刷新 |
| `FVCC/docs/API_CONTRACT.md` | 版本头 1.5.12；§2.3/§2.4 错误 helper 真源改实路径（`internal/protocol/apierr.go` Fail/FailWithCode/FailCode + edlErr/streamErr/网关内联）；§3.2 SQLite 载体口径 + `pollRenderTerminal` 上游状态透传注记；§4 示例改 `protocol.Fail` 写法；**新增 §6 接口段登记表**（`/api/store/backup|backups|restore` `{backupId}` camelCase、`GET /api/proxy`、render 提交 `EDLClip.sourceRoot`）；§7 验收两项勾 [x]（`gin.H{"error"` 实测 0） |
| `FVCC/docs/SECURITY.md` | 清 AIGC frontmatter 与尾注残留；§5 审计表补 `snapshot.restore`；§6.1 补渲染根字段 v1.5.12 写法（`validateRootRelPath` ≡ FVCS `ValidateRootPath`，共用向量）与 `pathutil.WithinRoot` 单一真源；changelog 记 2026-10-12 对齐 |
| `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` | 头部加 2026-10-12 复核（第 12 包 pathutil 增补注记）；阶段 C "待提交"回填实际提交 `009e2f8` |
| `FVCC/docs/P2-2_细化细则.md` | 决策门定案关闭回写（P1-2，2026-10-11，`be43119`：维持零框架，框架试点绑定多轨立项）；Checklist 末项勾 [x] |
| `FVCC/docs/DESIGN.md` | H1 后加复核横幅（令牌真源 / check:design 门禁 / Named Rules 与代码一致；P1-2 定案后契约维持零框架不变） |
| `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md` | 现状注记：SQLite 自主载体（v1.5.6~1.5.7），正文 JSON 文件表述为 2026-09-21 方案稿快照；锚点目标值改"迁移链最高 To"非退化写法；changelog 记 2026-10-12 复核 |
| 根 `BUILD.md` | 版本同步表与正文版本行 1.5.3→**1.5.12**；archive/fpk 归档范围注记 |
| `docs/FVCC_项目现状与开发步骤.md` | #8 全链路真机复验通过 + 已推送 fork/baseline-snapshot 收口陈述；manifest/版本核验附录 1.5.12；步骤 5/步骤 8 状态翻转；§5 同步表更新；尾注 AIGC 声明移除；治理进展登记本轮文档对齐 |
| `docs/FVCC_混乱度评价报告_细化版.md` | **第十版**：更新说明段 + §1 概览行（12 包/107 .go/1.5.12/版本控制状态）+ 目录树注记 + §2.1/§2.5/§2.6 现状刷新 + §3 评分表结论列 + §4 P1-4/P3-1/P3-4 状态（**P3-4 文档同步清零**）+ §5 结论与维护记录 |
| `docs/项目分析与改进方向.md` | **第六次回写**（文首基线 1.5.12 + #8 收口 + 推送状态）；不足表 #1（P3-5 已关闭）/#5（文档滞后清零）刷新；§5 P0 store 备份子项翻 ✅、P2 测试补强刷新（49 test / 59.0%）、P2-3/P3-1 git 状态注记、P3 文档同步项收口（根 README 4dcdb26） |
| `docs/FVCC_UI设计优化方向.md` | 状态行/§1.1 版本与规模/§5 版本同步/§7.6 落地状态：1.5.7→**1.5.12** 复核（含 1.5.5~1.5.7 已提交 68ebdec + tag 校正、UI 结论不变声明） |
| `docs/TASK_REFERENCE.md` | **第九版**：登记新编号族「闭环①-⑤ / A档 #N」（§3.7，含 #8 四层语义与代表位置、#7/#9 B档沿用同族注记）；§1 总览表补行；维护日期刷新 |
| `docs/SECURITY.md`（根） | 头部加 2026-10-12 复核记录：传输策略口径无漂移，代码面新增校验（snapshot.restore / validateRootRelPath 跨端 / WithinRoot 单一真源）登记于 FVCC/docs/SECURITY.md |

### 17.3 验证与状态

- 全部为 markdown 文档改动；未触碰代码、版本号与构建产物（1.5.12 三方一致不受影响，`check-versions.ps1` 口径不变）。
- 事实核验方式：逐条对照当前源码/文件系统/git log（包清单、helper 符号与调用点、路由与载荷、审计枚举、共用向量、脚本位置、archive/fpk 内容、提交哈希 `009e2f8`/`be43119`/`68ebdec`/`24cbbc5`/`6043fbd` 均经实查）；代码规模与 TS 行数为 2026-10-12 实测。
- **状态：本轮改动全部为工作区未提交，待用户评审后决定是否提交/推送（checkpoint 惯例，不自动 push/tag）。**

---

## 十八、第二十六轮（2026-10-12）：v1.5.12 打 tag 收口 + P3-7 覆盖率门槛上调 60% + P3-9 build_err.txt 清理

### 18.1 背景

1.5.12 版本线（#8 渲染全链路收口 + 文档全面对齐轮 ad77266）后，P3 尾巴仅剩三项小项：P3-1 收尾（1.5.12 未打 tag）、P3-7（覆盖率门槛 55%→60%）、P3-9（server/ 根 0 字节 build_err.txt）。本轮逐一关闭，不改业务代码、不升版本（测试与门禁改动，交付产物不变）。

### 18.2 本轮改动

- **P3-1 收尾**：`v1.5.12` 打**注解 tag**（落在文档对齐收口提交 `ad77266`）并推送 `fork`，tag 注记 #8 四层收口 + A 档修复 + 文档对齐 + 配套 FVCS v1.2.6。
- **P3-7 关闭**（55%→60%）：缺口分析仅需约 18 条语句，选取纯函数/可单测目标补三份测试文件——
  - `FVCC/server/logger/logger_api_test.go`：日志级别与配置 roundtrip、RecentLogs 缓冲/TraceID/导出过滤、traceSuffix；
  - `FVCC/server/smbshare/smbshare_parse_test.go`：samba 配置解析（注释/引号/tab 边界）、WithinOrEqual（`filepath.FromSlash` 保证 Windows 主机 + Linux CI 双端可移植）、空用户名各入口错误路径、未知用户 resolveUID；
  - `FVCC/server/internal/media/transcode_failure_test.go`：parseTime 表驱动（含非法输入）、RemoteFailureCode 修剪与兜底 `E_RENDER_FAILED`、RemoteFailureMessage 全量/最小文案；
  - 门禁同步上调：`scripts/check-coverage.ps1` 默认 `$Threshold` 55→**60**，`.github/workflows/fvcc-ci.yml` 覆盖率步骤改 `-Race -Threshold 60` 并更新基线注释；
  - 实测：59.0%→**60.7%**（+约 65 语句余量），`check-coverage.ps1` 实跑 PASS；*_test.go 49→52。
- **P3-9 关闭**：`FVCC/server/build_err.txt`（0 字节、未被 git 跟踪）已删除，server/ 根 0 字节文件归零。

### 18.3 验证与状态

- `go build ./...` / `go vet ./...` / `go test -count=1 ./...` 全绿；覆盖率门禁脚本端到端实跑输出 60.7% ≥ 60% PASS。
- 台账回写：混乱报告（§2.7/§2.8/§4 P3-1/P2-3/P3-7/P3-9/§5 结论/维护记录）、项目现状（总体判断/CI 行/治理表/步骤 7/版本核验附录）、TASK_REFERENCE P2-3 行、项目分析（第七次回写 + #6/测试补强/git 状态）、UI 文档 tag 注记、FVCC/README P2-3 行。
- **剩余 P3 债务仅 P3-6（ffprobe 生产依赖评估）/ P3-8（远期决策门）**。

## 十九、第二十七轮（2026-10-12）：v1.6.0 视觉打磨版收口 + FVCS v1.3.0 渲染节点 GUI 复活

### 19.1 背景

按 `docs/FVCC_视觉更新方案_v1.6.0.md`（V1–V9 问题清单 + 六批次）执行 v1.6.0 视觉打磨版：方向 A「精修冷灰」令牌、控件层自绘化、外壳导航/搜索/状态栏重排、逐页修复（趋势图/任务分区/编辑器左栏/服务器栅格/内联样式清零）、微交互与 WCAG 对比度验收；并按既定决策将 FVCS Fyne GUI（托盘 + 设置窗口）纳入同批发布（FVCS 1.2.6→1.3.0，GUI 从"源码存在但无构建通道"复活为一等交付产物）。

### 19.2 批次落实（①–⑤ 均经用户逐批验收）

- **批次①令牌层**：style.css 明暗两套 `--c-*` 按方向 A 重排（暗色卡片提亮一档/边框降一档拉开层次；浅色主按钮降一档明度 #2563eb 去刺眼）；`color-scheme` 声明使原生弹层随主题。
- **批次②控件层**：ui.ts 新增 `selectField`（原生 select + chevron 覆盖层的 DOM 兼容门面，35 处调用点零逻辑改动迁移）、`.checkbox`（自绘勾选/半选 SVG data-URI + ring focus，规避 R3）、`.btn-ghost`/`.btn-danger-ghost` 变体；V2 原生下拉/V4 实心红删除按钮全部替换。
- **批次③外壳**：导航分组标签（`.nav-group-label`）+ 分隔线（`.nav-divider`）、V3 Unicode 图标全部换 svgIcon、搜索胶囊重排（rounded-full + kbd 徽标）、状态栏徽标化（`.status-chip`）。
- **批次④逐页**：
  - **V1 趋势图**：右轴 `minimumWidth:44`、成功率线迁隐藏 `rate` 自定义轴（v5 需系列绑定后再 applyOptions 隐藏）、`lastValueVisible/priceLineVisible` 关悬空标签、空日视窗压缩（数据首尾 ±3 天、最少 7 天）、TradingView 角标（曾被误读为"17"）CSS 淡显移至右上；
  - **V5**：任务页 overview 容器补 flex（此前占比条换行成伪小节线）、空态垂直居中、统计文字 tabular-nums；
  - **V6**：授权根下拉显示末两段路径 + 完整路径 tooltip、↑↓ 文本箭头换 svgIcon、浏览按钮防截断；
  - **V7**：服务器空态居中、负载环零任务时「—」替代歧义「0」；
  - **V8**：profiles.ts 53 处 `.style.display` 收编 `setDisplay()`（hidden 工具类）；deleteSourceHint/ffmpegPreview/预览深底/素材栏折叠收编工具类（tailwind 新增 `log-bg`/`preview.bg` 映射）；timeline.ts 18 处为动态几何值，维持内联（评估结论：收编无收益）。
- **批次⑤微交互 + V9**：页面切换 0.2s 淡入（重排重放）、任务卡首入场淡入（animationend 摘类防重挂闪烁）、toast 规范（滑入淡入/错误 4s 其余 2.5s/同屏 ≤4 条/z-[60] 高于对话框遮罩）；V9 对比度实测修复——ink-subtle 浅 2.73→4.76、暗 4.04→4.80，新增 `.chip-active`（暗色选中态深蓝底 3.68→5.17），PROXY 徽标 warning 底改恒定深色字 2.15→8.80，不一致徽标换 danger-hover 底 3.76→4.83，success/danger toast 改 soft 底+强色字（≥4.6）。

### 19.3 FVCS v1.3.0 GUI 复活（批次⑥）

- `build.ps1` 新增 `-Target FVCS-UI`：CGO/mingw 通道编译 `cmd/ui`→`fvcs-ui.exe`、`cmd/settings`→`fvcs-settings.exe`（`-H windowsgui` 免控制台窗），15.3s 双产物成功；ALL 模式不含 GUI（保持原流水线速度）。
- 新增 `FVCS/pkg/fynetheme`：Fyne v2.8 `fyne.Theme` 接口包装，Primary/Focus/Hyperlink 色贴近 FVCC 方向 A 蓝调（浅 #2563eb / 暗 #3b82f6）；cmd/ui 与 cmd/settings 明暗分支统一接入（此前暗色分支外一律默认主题）。
- 版本：`pkg/version/VERSION` 1.2.6→**1.3.0**，version_test 同步；`go test ./pkg/... ./cmd/service ./cmd/ui ./cmd/settings -count=1` 全绿。
- 图标沿用现有内嵌资源（FVCS.png/FVCS.ico + cmd/ui syso），未新增素材。

### 19.4 版本与治理

- FVCC 三方版本对齐 1.5.12→**1.6.0**（manifest / ui-src/package.json / server/internal/version/VERSION），`scripts/check-versions.ps1` 实跑通过；
- 门禁：每批次 `tsc --noEmit` + `check-design --all`（33 文件）+ `npm run build` 全绿；
- 临时产物清理：`ui-src/public/seed-theme.html`（走查主题播种页）与 `server/_walkthrough/`（走查素材）随本轮删除；`.gitignore` 补 `FVCC/server/_wve_cache/`、`backup/`、`data_version.json` 运行库；
- README（根）版本行、可视化托盘管理章节（fynetheme 注记）、部署行（fvcs-ui/fvcs-settings + FVCS-UI 构建命令）同步。

**状态（本轮收口，2026-10-12）**：批次①-⑥ + 版本收口全部经用户逐批验收通过后，拆三笔提交推送 `fork/baseline-snapshot`：`1e622f0` feat(ui) 批次①-⑤（16 文件）、`22035c6` feat(fvcs) v1.3.0 渲染节点 GUI 复活（含新增 `FVCS/pkg/fynetheme`）、`b1a0d93` chore(release) 版本收口 1.6.0/1.3.0 + 本台账 + gitignore 治理；注解 tag **v1.6.0** 落在 `b1a0d93` 并推送（配套 FVCS v1.3.0）。工作树干净，临时走查产物（seed-theme.html/_walkthrough）未混入提交。

## 二十、第二十八轮（2026-10-13）：任务页视觉改进批次 A+B+C 收口（v1.6.4 已推送）+ VERSION 第三真源漂移修复与校验加固（v1.6.5）

### 20.1 背景

按 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`（GLM5.3F / Step5P / Gemini-3.8F 三方外部模型评审整合稿）对任务页做三批次改造，并在收口时发现 **v1.6.4 的第三个版本真源漏 bump**：包内 `internal/version/VERSION` 仍为 `1.6.0`，导致已推送的 1.6.4 包「设置」页经 `/api/info` 显示 1.6.0。本台账一并补记上半段（v1.6.4，已于 2026-10-13 02:40 随 `fe06e52` + `b007a3d` 推送并打 tag **v1.6.4**）与下半段（v1.6.5 修复轮，本轮）。

### 20.2 上半段：v1.6.4 任务页视觉改进批次 A+B+C（`fe06e52`，7 文件 +197/−44）

- **批次 A（层级修复 + 状态反馈）**：`errorMsg` 独立成行并带 `title`；拆除 `sub` 上 `truncate` 与 `flex-wrap` 的互相抵消；首屏骨架接入 `listSkeleton`；批量三键零选中时禁用、刷新期 pending 态；`.task-selected` 选中即时高亮；活动卡左侧 `signal` 静态指示线；`getPhaseStyle` 补全状态文案且 ERROR 走红条；操作区去掉 `w-[200px]` 硬宽；`serverName` 改中性标签去撞色；hover ring 与 servers 页对齐。
- **批次 B（热路径性能）**：进度条由 `width` 改 `transform: scaleX(--p)` 走合成层；`render` 增加 id/顺序签名重排守卫；`updateOverview` 增加计数签名守卫；`scrollPos` 接线（主页与抽屉独立 key，不互串）。硬约束沿用 v1.6.1 方案：**视觉改进不得增加 WS tick 重绘**。
- **批次 C（速率 + 实验项）**：WS `applyTaskUpdate` 合并 `speed` 字段（经 `models.go` `speed,omitempty` + `TaskUpdateFullMsg` 核实链路后转正，不再等"产品确认"），进度文案实时带倍速后缀；`#/tasks?fx=stripes` / `?fx=breathe` URL 开关（默认关）；`ui.ts` 新增 `listSkeleton` 泛化组件。
- **两处过程修复（用户实测抓出）**：
  - 空任务库下骨架常驻"出不来"——A2 首屏骨架初版把退出条件写成 `tasks.length>0`，改为**首次成功 render 即置 `loaded`**，空列表正常落到 `emptyState`；
  - `check-design` R3 正则误报 CSS 属性 `box-shadow`（规则本意查内联样式），前缀排除 `-`/`:`；选中态 inset 阴影按 v1.6.1 方案 §2.3 裁决为合法。

### 20.3 下半段：v1.6.5——VERSION 漂移修复 + 版本校验从"静默 SKIP"改为真跑

**版本收口 1.6.0/1.6.4 → 1.6.5**（三真源同步：`FVCC/server/internal/version/VERSION`、`FVCC/manifest`、`FVCC/ui-src/package.json`）。路线经用户拍板选 **发新补丁号 v1.6.5**，不强推重指 `v1.6.4` tag：一是 tag 历史保持干净，二是 fnOS 只装严格更高版本，同版本号重指也无法覆盖已装到 NAS 的包。

- **`FVCC/server/internal/version/version_test.go` 重写校验口径**（+130/−12）：
  - 新增 `findRepoRoot`：以 **`runtime.Caller` 的测试源文件目录 + `os.Getwd()` 双起点**向上逐级搜索，判定条件是目录内同时存在 `FVCC/manifest` 与 `FVCC/ui-src/package.json`（用文件存在性而非 `.git`，因 tarball/CI 检出未必带 `.git`）；
  - **失败一律 `t.Fatal`，彻底移除 `t.Skipf`**——定位不到仓库根、读不到文件、版本字段缺失、版本不一致，四种情况全部判红。旧实现写死 `../../manifest`（从包目录 `internal/version` 上溯 2 层实为 `FVCC/server/manifest`，不存在；正确层数是 4 层到 `FVCC/`），读不到就 Skip，于是 Windows 本机 `go test ./...` "全绿但零校验"；
  - 新增 `TestAppVerIsNotPinnedLiteral`：扫描本包**非测试** `.go` 文件，出现 `"x.y.z"` 形式硬编码版本字面量即判红（守住"VERSION 是唯一真源"，历史同类缺陷见 FVCS `hello.go` 硬编码 `1.2.0`）；测试文件不进扫描，避免用例夹具（如 9.9.9）误伤。
- **`version.go` 注释修正**：原"同步修改：本目录 VERSION、`../manifest`、`../ui-src/package.json`"的相对路径本身就是错的（正是它误导了上一轮只改两处），改为按仓库根写清三处真源 + 层数，并注明校验已由"会 SKIP"变为"必真跑"。
- **`scripts/check-versions.ps1` 去硬编码**：`$BASE_DIR = "D:\Fnos.VideoConversion"` 改为由 `$PSScriptRoot` 推导仓库根，并新增 `-RepoRoot` 显式覆盖 + 定位失败明确判红。
  - **本机环境事实（值得记）**：这台主机的 PowerShell 5.1 实测下**变量名区分大小写**——`$BaseDir` 有值时 `$BASE_DIR` 仍为空、`Get-Variable` 只登记一个。初版把参数命名为 `$BaseDir` 而下游沿用 `$BASE_DIR`，运行即 `Join-Path: Cannot bind argument ... null`。修复方式为只保留一种大小写写法（参数改名 `-RepoRoot`，与 `$BASE_DIR` 无大小写歧义），不依赖语言的大小写不敏感语义。

### 20.4 验证

- `go vet ./...` 通过；`go test ./... -count=1`（FVCC/server 全量）全绿；`internal/version` 三条用例 `-v` 实跑均为 **PASS 且无 SKIP**（`版本三方对齐通过：AppVer=1.6.5`）。
- **反例验证（拦得住漂移才算修好）**：受控把 `FVCC/manifest` 改回 `1.6.4` → `TestAppVerMatchesPackagingFiles` 立刻 `FAIL`，报错文案指向"三处一起 bump"；还原后恢复一致。
- `npm run build`（`check:design` + `tsc -b` + `vite build`）全绿，48 模块，产物输出 `FVCC/app/ui/`。
- `scripts/check-versions.ps1`：仓库根实跑输出 `manifest=1.6.5 package.json=1.6.5 VERSION=1.6.5 => 一致`；从 `C:\` 以绝对路径调用同样通过（不再依赖 CWD 与盘符）；`-RepoRoot C:\Windows` 正常判红退出 1。
- **出包核验**：`build.ps1 -Target FVCC -NoTest` 产出 `FVCC/fvcc.fpk`（10,752,127 字节）。解包核对——包内 `manifest` 为 `version = 1.6.5`；`app.tgz` 内 `app/fvcc` 二进制中 `1.6.x` 版本串**只出现 `1.6.5` 一次**，`1.6.4`/`1.6.0` 命中数均为 0（即漂移值已彻底不在包内）。
- 1.6.x 线未再做 `FVCC_v*_fnos_x86.fpk` 归档副本（该惯例实际停在 1.5.11），本轮沿用现状未新增。

**状态（本轮，2026-10-13 收口）**：v1.6.4 批次 A+B+C 已推送并打 tag（`fe06e52` / `b007a3d` + tag `v1.6.4`，用户硬件验收"蛮正常"）。v1.6.5 修复轮改动 6 文件（三真源 + `version.go` + `version_test.go` + `check-versions.ps1`）+ 本台账，**已按用户授权收口**：拆两笔提交推送 `fork/baseline-snapshot` —— `ceeb1b2` fix(version) 版本收口 1.6.5 + 校验加固、`5166cbe` docs(fvcc) 第二十八轮台账 + 文档版本引用对齐；注解 tag **v1.6.5** 落在 `5166cbe`（tag 对象 `81ef63a`）并已推送，远端 `git ls-remote --tags fork` 核验在位，`v1.6.0` / `v1.6.4` / `v1.6.5` 三 tag 并列、未改写历史。**用户 2026-10-13 真机确认 NAS「设置」页显示 1.6.5**，漂移线彻底闭环。

**补充事实（本轮文档对齐时查得，影响后续溯源）**：任务页批次的代码注释里确实存在 `v1.6.1-A*` / `v1.6.2-B*` / `v1.6.2-C*` / `v1.6.3 修复` 这类编号（实 grep 命中 `FVCC/ui-src/src/pages/tasks.ts`、`store.ts`），但 **git 历史中 manifest 的版本序列是 `1.6.0 → 1.6.4 → 1.6.5`** —— 即这些前缀是批次开发期的本地中间 bump 与子项编号，从未作为交付版本号入库/出包/打 tag；方案文件名里的 `v1.6.1` 是**方案稿号非版本号**。故"1.6.1~1.6.4 版本线"的写法作废（本轮此前在 `FVCC/README.md` 版本表里就这么写过，已纠正为 `1.6.4` 单行 + 括注说明），该易错点登记于 `docs/TASK_REFERENCE.md` §3.8。

仍开放项：概览 Filter Chips（待产品拍板）、`?fx=stripes|breathe` 实验动画转正/弃用（待真机并发裁决）、其余页面骨架迁移 `listSkeleton`、store 备份/恢复前端入口、e2e B 档 #7/#9、渲染 Failed/Cancelled 分支（无自然复现，顺带验证）。

## 二十一、第二十九轮（2026-10-13）：叙述性长台账对齐轮——9 份文档基线统一至 FVCC v1.6.5 / FVCS v1.3.0

### 21.1 背景与范围

第二十五轮曾把全部台账对齐到 v1.5.12，但 1.6.0 / 1.6.4 / 1.6.5 三个版本（视觉打磨版 + FVCS GUI 复活、任务页三批次、版本真源收口与校验加固）落地后没有跟进，导致除 IMPROVEMENT_LOG / README / BUILD / FVCC README / API_CONTRACT 之外的**叙述性长台账集体冻结在 v1.5.12 基线**——规模数字、待办状态、评分与复核记录全部滞后。本轮补齐这 9 份，不改任何代码与构建产物。

回写前先在 `temp/alignment-facts-20261013.md`（temp 已 gitignore，用完即删）固化一份**唯一事实底稿**：权威版本号、git 提交/tag 链、2026-10-13 实测规模、三版内容、待办与开放项、以及上节的"1.6.1~1.6.3 未入库"易错点。9 份文档分三组并行回写，均要求：按各文档自身的格式与编号链续写、**历史叙述与旧实测值一律保留为当时快照**、底稿没给的数字标注"本轮未实测"而非编造、未完成任务不得写成已完成。

### 21.2 各文档回写要点

- `docs/FVCC_项目现状与开发步骤.md`：§1 现状概述保留 v1.5.12 长段并加"2026-10-12 时点口径、已被 v1.6.5 取代"括注，其后另起【2026-10-13 更新·当前版本基线】登记 1.6.x 三版与 tag/提交链；§1.1 规模表与证据索引更新为实测值（旧值留作历史括注）；§3.3 / 步骤 7 覆盖率统一标注"第二十六轮实测 60.7%，本轮未重跑"；§6 / §7 / 步骤 4·5·8 待办收敛为未完成/待裁决。
- `docs/FVCC_混乱度评价报告_细化版.md`：**第十一版（1.6.5，2.3/10）**。规模、§2.1/2.2/2.8、目录树、P 项表按实测与 git 核验更新；§2.5 登记 v1.6.4 漂移事实与 v1.6.5 六点加固；新增 §2.10（1.6.4 三批次 + 真机抓出的两处缺陷）与 §4.4（十项开放/未验证清单）。**评分纪律**：九维度分值全部维持，综合 2.4→2.3 只依据两条硬证据（校验"漂移即红"根治一类长期潜伏的一致性缺陷；任务页信息层级/反馈与每 tick DOM churn 收敛），并显式列出"明确不涨分的事项"（覆盖率未重跑、依赖未重跑 tidy、temp/archive 未重扫）。
- `docs/项目分析与改进方向.md`：**第八次回写**至 1.6.5 基线；§3 规模表、§4.1 优点、§4.2、§5 各项状态更新，新增 §5.1（十项未完成清单）与 §6 版本线摘要（含强制表述的易错点）。
- `docs/FVCC_UI设计优化方向.md`：规模行 33 文件 / **11,474 行**（注明 +217 来自 1.6.4，旧 10,315 / 11,257 留作历史）；§1.1 版本行、§3.3、§5.2、§7.5（并入"视觉改进不得增加 WS tick 重绘"硬约束）更新；新增 §7.7 分四小节记 1.6.0 方向 A、1.6.4 批次 A+B+C（含骨架退出条件 `tasks.length>0` 的教训与 `box-shadow` 误报裁决）、1.6.5 无设计改动、四项未完成遗留。
- `docs/TASK_REFERENCE.md`：**第十版**；§1 登记 `v1.6.1-A*` / `v1.6.2-B*` / `v1.6.2-C*` 与 `方案稿 §x.y` 两族，新增 §3.8（批次子项 → 交付版本 1.6.4 的还原规则 + "1.6.1/1.6.2/1.6.3 从未入库"标为**本表最高易错点**），§5 增第 4 条约定。
- `docs/SECURITY.md` 与 `FVCC/docs/SECURITY.md`：逐项比对后判定**数据面口径无漂移**，仅追加 2026-10-13 复核记录；并按范畴区分写明 v1.6.5 的版本一致性属**发布/打包治理**、不是数据面安全机制，未新增任何安全条款。
- `FVCC/docs/DESIGN.md`：复核行推进至 2026-10-13（10-12 v1.6.0 记录留作历史，注明 R1-R10 与 frontmatter 取值本次未逐项复跑）；R7 补 `listSkeleton` 与"首次成功 render 即 loaded"写法，R10 补 `box-shadow` 误报修复与 inset 阴影裁决；新增「热路径与状态反馈惯例」节（`scaleX(--p)`、签名守卫、`scrollPos` key、`speed` 合并、fx 默认关）。
- `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md`：基线更新 v1.6.5，§6 第 4 项**前端入口保持未完成**，文末追加 2026-10-13 复核（含未复跑声明）。

### 21.3 核验与实测

- 规模实测复核（本轮亲自复跑，非采信 agent 自述）：`FVCC/server/internal` 目录数 = **12 包**（api/edl/media/node/pathutil/protocol/remote/scheduler/security/store/version/ws，另 store/model 子包）；Go 58 实现 17,197 行 + 52 测试 9,183 行 = **110 个 .go / 26,380 行**；前端 TS 33 文件 11,474 行。
- 编号族真实性核验：`grep` 确认 `v1.6.1-A2/A3`、`v1.6.2-B2/B3/B4/C1/C2`、`v1.6.3 修复` 存在于 `tasks.ts` / `store.ts` 注释中，与"交付版本 1.6.4"并存——据此上节补充事实并修正 `FVCC/README.md` 的错误版本线表述。
- 删除行审查：逐文件核对 diff 的 `-` 行，确认头部/规模/评分行的旧数值均以"历史/上一记载"括注形式保留在新行内，未抹掉当时快照。
- 交叉一致性：9 份文档 + 上轮 4 份（README / BUILD / FVCC README / API_CONTRACT）现均指向 v1.6.5 / FVCS v1.3.0；覆盖率引用处一律带"第二十六轮实测 60.7%、本轮未重跑"限定；"1.6.1~1.6.4 版本线"写法全域清零。
- 本轮为纯文档回写：未改代码、未跑构建、未重新出包（`fvcc.fpk` 仍为上轮核验过的 1.6.5 产物）；版本三真源与 `check-versions.ps1` 不受影响。

**状态（本轮，2026-10-13）**：9 份文档 + `FVCC/README.md` 版本线纠正一并收口。仍未对齐到底稿口径的只剩历史方案稿（`FVCC_视觉更新方案_v1.6.0.md` / `FVCC_任务页视觉改进整合方案_v1.6.1.md`，属当时点方案文件，按纪律不回写）。

## 二十二、第三十一轮（2026-10-13）：工具链与依赖版本口径收口 + 第三十轮依赖批次台账补记

### 22.1 背景

第三十轮（依赖审计 + 低风险刷新批次）在提交说明里明确挂了两笔欠账：**台账未写**、**`BUILD.md` 的 go1.24.6 工具链文档漂移留下一轮统一处理**。本轮就是还这两笔：零代码改动，只把"工具链与依赖版本"的**声明面（脚本/CI）与叙述面（文档）**对齐到事实，并顺带把文档里的版本引用从 1.6.5 推到 1.6.6。

### 22.2 补记第三十轮（已随 `1dcb774` 推送，注解 tag **v1.6.6** → tag 对象 `a321308`，落 `fork/baseline-snapshot`）

基线 FVCC v1.6.5 / FVCS v1.3.0，批次口径"只升落后但可顺手升的补丁级项，大版本项另立专项"，无功能/界面改动故双端 patch bump：

- **FVCC 1.6.5 → 1.6.6**：vite 8.3.0→8.3.4、postcss 8.5.28→8.5.30、autoprefixer 10.5.2→10.6.1、@types/node 22.20.1→22.20.5（仍在 `^22` 区间内，`package.json` 依赖区间未动）、间接 golang.org/x/sys v0.41.0→v0.49.0。
- **FVCS 1.3.0 → 1.3.1**：golang.org/x/sys v0.30.0→v0.49.0（直接依赖，仅 `pkg/winapi` 的 DPAPI 调用使用）、mattn/go-sqlite3 v1.14.48→v1.14.52、fyne v2.8.0→v2.8.2（连带 go-text/typesetting v0.3.5、go-gl/glfw v0.1.0-pre.2、fyne-io/oksvg 伪版本）。
- **易漏点**：FVCS 的 `pkg/version/version_test.go` 把期望值写成硬编码字面量，只 bump `pkg/version/VERSION` 会让 `go test ./pkg/version` 判红——两处必须同改（现已登记进 `BUILD.md`「FVCS 版本号同步清单」）。
- **用户拍板（2026-10-13）**：fyne v2.8.2 的 go.mod 使 `fyne.io/systray` 由发布版 v1.12.2 变为伪版本 `v1.12.3-0.20260810170012-af4e8e793ec4`（MVS 覆盖不掉，属 fyne 2.8.x 既有做法）→ **接受**，随该批提交。
- **刻意推迟**：Tailwind 3.4.19→4.3.3（v4 CSS-first 重写，专项工程）；Node 运行时 25.9.0→26.x + @types/node 26（与下次大版本批次同做）；modernc.org/sqlite v1.14.0→v1.60.1（该批审计发现的最实质落后项）；FVCS 的 x/net v0.35.0、x/text v0.22.0、x/image v0.24.0 偏旧但代码无直接引用（纯 fyne 间接，fyne 2.8.2 也未抬高）→ 忽略。
- 真机复验：用户在 .104 渲染机复验通过后该批才 bump、复跑全绿、提交出 tag。

### 22.3 工具链口径收口（本轮改动，4 文件）

审计前的事实核对（本轮实测，非采信历史记载）：双端 go.mod 均为 `go 1.27.1` 且无 `toolchain` 行；本机 PATH 上的 Go 是 **1.27.1**（`C:\Program Files\Go`），`sdk\` 下只剩 `go1.25.3`（**`go1.24.6` 目录已不存在**）；模块缓存 `D:\go\gomodcache` 已含 `golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64`；上游最新补丁版 **go1.27.2**（1.26 线为 1.26.9）；CI `actions/setup-go@v5 → 1.27.x`、`actions/setup-node@v4 → 22`。

- `build-env.ps1`：`GOTOOLCHAIN` 由 `local` 改 **`auto`**，并把它从"FVCS 专属"提为**双端共用**段落（旧注释"使用模块级 go1.24.6"是错的：`local` 会把版本锁死在启动器自身，任何机器上启动器低于 1.27.1 时 FVCS 立刻构建失败）；`GOEXE_FVCC` 注释改为说明"启动器版本 ≠ 语言版本，实际由 go.mod + auto 决定"；`[ENV]` 输出行标签同步。
- `build.ps1`：SDK 探测表删去失效的 `C:\Users\xx318\sdk\go1.24.6\go\bin\go.exe`（顺序保持 `sdk\go1.25.3` → `Program Files\Go` → PATH `go`，命中任一由 auto 升到 go.mod 要求版本）。
- `BUILD.md`：FVCC 前置条件表（Go / Node 两行，写明 Node 本机 v25.9.0 与 CI 22 的差异及 `@types/node` 钉 `^22` 的因果）、FVCS 环境要求表与环境变量块（`local`→`auto`）、「注意事项 · Go 工具链」重写（双端统一 auto + `go` 指令是**下限不是 pin**，故不抬到 1.27.2）、故障排查两行（`internal/stringslite` 缺失改为"换完整安装的 Go 并用 auto"；`go.mod requires go >= 1.27.1` 的原因/解法去掉"FVCS 才是 local"的过时指引）；**新增两节**：「FVCS 版本号同步清单」（两处真源）与「工具链与依赖版本基线（2026-10-13 审计复核）」表——逐项列本仓库声明值 / 上游最新 / 处置（保持、刻意推迟、待专项），并写明刷新纪律。
- 文档版本引用对齐至 **FVCC v1.6.6 / FVCS v1.3.1**：根 `README.md`（首段当前版本、版本单一来源段）、`FVCC/README.md`（三真源表、配套 FVCS 行、版本线摘要新增 1.6.6 行、P1-4 复核值）、`BUILD.md`（三真源表 + 部署段当前版本）、`FVCC/docs/API_CONTRACT.md`（头版本，接口/路由表无变更）。

### 22.4 依赖新鲜度复跑（本轮亲自复跑，非沿用上批自述）

- **npm**（`FVCC/ui-src`，registry.npmjs.org）：`npm outdated` 仅两项落后——`@types/node` 22.20.5 → 26.6.5、`tailwindcss` 3.4.19 → 4.3.3，两者均为上批**刻意推迟项**；vite 8.3.4、TypeScript 7.0.2、postcss 8.5.30、autoprefixer 10.6.1、lightweight-charts 5.2.1、@fontsource 5.3.0 逐项比对 latest **全部为当前版本**，本批无可刷项。
- **Go**（GOPROXY=goproxy.cn）：FVCC 侧 gin v1.12.0、gorilla/websocket v1.5.3 均为最新；FVCS 侧 fyne v2.8.2、go-sqlite3 v1.14.52、x/sys v0.49.0 均为最新；唯一实质落后仍是 `modernc.org/sqlite` **v1.14.0 → v1.60.1**（跨 46 个 minor，FVCC 唯一 SQLite 依赖、纯 Go 驱动、`sql.Open("sqlite", path)` + WAL/busy_timeout PRAGMA）。
- 结论：**当前没有"低风险可顺手升"的余项**，下一批依赖工作即 sqlite 专项（需单独验证交叉编译 linux/amd64、WAL 与 busy_timeout 行为、以及 P3-5 备份/迁移链在新驱动上的回归）。

### 22.5 验证

- `PSParser` 语法检查 `build-env.ps1` / `build.ps1`：均无解析错误。
- 源入新 `build-env.ps1` 后：`go env GOTOOLCHAIN` = **auto**、`GOVERSION` = **go1.27.1**；FVCS `go build ./pkg/... ./cmd/service`（CGO=1 + mingw64 在 PATH）**exit 0**；FVCC `GOOS=linux CGO_ENABLED=0 go build`**exit 0** 且产物头 4 字节为 `\177ELF`（auto + 1.25.3 启动器的交叉编译链路未受影响）。
- `scripts/check-versions.ps1`：exit 0，`manifest=1.6.6 package.json=1.6.6 VERSION=1.6.6 => 一致`、`go.mod 工具链声明 go 1.27.1`。
- 版本用例真跑：FVCC `go test ./internal/version -count=1 -v` 三条 **PASS 无 SKIP**（`AppVer=1.6.6`）；FVCS `go test ./pkg/version -count=1` **ok**（1.3.1 断言一致）。
- 本轮未改代码与依赖清单，故 **不出包、不 bump 版本**：最新交付版本仍是 FVCC v1.6.6 / FVCS v1.3.1（`FVCC/fvcc.fpk` 为该版本产物）。

**状态（本轮，2026-10-13）**：工具链口径与依赖叙述收口，改动 7 文件（`build-env.ps1`、`build.ps1`、`BUILD.md`、根 `README.md`、`FVCC/README.md`、`FVCC/docs/API_CONTRACT.md`、本台账）。

**本轮已知未做（如实登记）**：第二十九轮对齐过的 9 份**叙述性长台账**（`项目现状与开发步骤`、`混乱度评价报告`、`项目分析与改进方向`、`UI设计优化方向`、`TASK_REFERENCE`、两份 `SECURITY`、`DESIGN.md`、`STORE_PERSISTENCE_ROADMAP`）仍写 **v1.6.5 / FVCS v1.3.0** 基线——1.6.6 是纯依赖批次，未重跑规模实测与覆盖率，若按第二十九轮纪律做整批回写需先补测；本轮只动"当前版本指针"类文档。另：`FVCC/.commandcode/` 仍未处置（未入库也未 ignore），依赖刷新遗留项 sqlite/Tailwind4/Node26 待专项。

## 二十三、第三十二轮（2026-10-13）：modernc.org/sqlite 专项批次（v1.14.0 → v1.60.1，跨 46 个 minor）+ v1.6.7 出包

### 23.1 背景

第三十一轮把依赖新鲜度复跑收口为一句结论：**当前没有"低风险可顺手升"的余项**，唯一实质落后项就是 `FVCC/server` 的 `modernc.org/sqlite` v1.14.0 → v1.60.1。本轮即该专项批次。选它做纯 Go 驱动的硬约束在 `store/sqlite.go` 头部注释里写死：FVCC 交叉编译 linux/amd64 且 `CGO_ENABLED=0`，`mattn/go-sqlite3` 在 CGO=0 下 stub 报错不可用。

### 23.2 用法面审计（先量风险再动手）

升级前把 store 层对驱动的**接触面**逐项摸清（`internal/store/sqlite.go` 为唯一入口）：

- `sql.Open("sqlite", path)` —— **无 DSN 参数串**（journal/synchronous/busy_timeout 全部走 `db.Exec` 的 PRAGMA 而非 DSN 查询参数），故 v1.14→v1.60 之间驱动 DSN 解析的语义变化打不到我们。
- `db.SetMaxOpenConns(1)` 单连接串行写，与内存 `RWMutex` 的串行写语义一致；不依赖驱动的连接池行为差异。
- 三条 PRAGMA：`journal_mode=WAL` / `synchronous=NORMAL` / `busy_timeout=5000`，均为标准 SQLite 语句，非驱动私有扩展。
- 业务 SQL 只有 `CREATE TABLE` / `INSERT` / `UPDATE` / `DELETE` / 简单 `SELECT`（单列 JSON 存储，无列级查询、无 FTS、无虚拟表、无扩展函数注册、无 `sql.Named`/驱动 API 类型断言）。
- 迁移链 `schema_version`（结构）与 `data_version`（数据）双链均在应用层事务内实现，不依赖驱动版本。

结论：驱动 API 面为零、SQL 面为纯 SQLite 子集，风险集中在**文件格式兼容**与**行为漂移**两点，需用真数据验证而非只跑单测。

### 23.3 升级与老库兼容性实测

- `go get modernc.org/sqlite@v1.60.1` + `go mod tidy`。依赖面**收窄**为副作用收益：dropped `modernc.org/cc/v3`、`ccgo/v3`、`opt`、`strutil`、`token`、`lukechampine.com/uint128`、`golang.org/x/mod`、`x/sync`、`x/tools`、`kballard/go-shellquote`（老驱动内置 cgo 转译工具链，新驱动已不依赖）；added `github.com/ncruces/go-strftime v1.0.0`、`github.com/dustin/go-humanize v1.0.1`；bumped `google/uuid` 1.3.0→1.6.0、`mattn/go-isatty` →v0.0.24、`golang.org/x/crypto` →v0.57.0、`x/net` →v0.59.0、`x/text` →v0.42.0、`modernc.org/libc` v1.11.70→v1.77.1、`mathutil` →v1.7.1、`memory` →v1.12.1。`go.sum` 66 增 175 删（净减 109 行）。
- 驱动自身 `go.mod` 要求 `go 1.26.0`，**仍低于**本仓库 `go 1.27.1` 下限，故无需抬 go.mod（第三十一轮"`go` 指令是下限不是 pin"的口径继续成立）。
- **老库实测**（单测覆盖不到的部分）：用 v1.14.0 驱动在 `temp/olddata/fvcc.db` 生成一份 102,400 B 的真实老版本库（含 12 张表数据 + `schema_version`），再由升级后的新驱动经**真实 store 代码路径**执行 Open→migrate→读→写回→二次 Load。验证结果：WAL 模式保持、`schema_version` 未被误重置、`node_health_samples`/`audit_log` 的 `AUTOINCREMENT` 在老 seq 基础上续号、`UpsertServer` 的立即落盘在第二次 Load 可见。一处自我纠正记入纪律：断言 `audit_log` 行数时先写 6 后改 7 —— `Store.Load()` 的崩溃恢复路径（`recover task tk-1 DONE -> QUEUE`）本身会追加一条审计，**这是应用语义而非驱动行为**，第一次断言错是因为没把恢复路径算进去。
- 临时探针 `internal/store/zz_tmp_olddriver_compat_test.go` 与老驱动生成器 `temp/olddrv/`、备份 `temp/sqlite_upgrade_gomod.bak`/`_gosum.bak` 均在 `temp/`（已 ignore），探针**未提交、已删除**。

### 23.4 验证与出包

- `go vet ./...` 通过；`go test ./...`（宿主，已 `unset GOOS`）**14 个包全绿**；`-race` 在宿主不可用（"requires cgo"），按既有分工留给 Linux CI。
- 覆盖率不变：**60.7%**（`scripts/check-coverage.ps1 -Threshold 60` PASS），与第二十六轮 P3-7 上调门禁后的基线一致。
- 交叉编译 `GOOS=linux CGO_ENABLED=0 go build` 通过；产物体积 **+565 KB**，`fvcc.fpk` 由 10,752,127 B → **11,011,826 B（+2.4%）**，属纯 Go 驱动转译代码体量增长的自然代价。
- 版本按"出包前必 bump"纪律 **1.6.6 → 1.6.7**（manifest / `ui-src/package.json` / `internal/version/VERSION` 三处同改，`check-versions.ps1` 绿）；新包完整性核验：包内 manifest=1.6.7、`manifest.checksum == md5(app.tgz)`、二进制内版本串 `1.6.7`×1 且 `1.6.4/1.6.5/1.6.6`×0。
- 真机复验由用户在 NAS .106 执行（交付 6 点核验清单：设置页版本号、任务列表/历史读取、服务端与模板持久化、重启后数据不丢、日志无 SQLite 报错、快照备份/恢复）。

## 二十四、第三十三轮（2026-10-14）：Node.js 运行时升级到 v26.11.1（系统级 MSI）+ 四处口径对齐

### 24.1 背景与决策

Node 26 升级是第三十轮挂账的"刻意推迟项"。本轮做它是因为**升级点有三处必须一起对齐**，单改任何一处都会留下叙述面漂移：本机运行时、CI `setup-node`、`@types/node` 类型区间。此前本机是 **v25.9.0**——v25 是**已 EOL 的奇数 Current 线**，属实际的坏基线。

用户拍板两项：**直接装系统级 MSI**（不引版本管理器）、**追 26 Current**（不回落 24 Krypton LTS）。26.11.1 确认为当前 **Current、非 LTS**（active LTS = 24.21.0 Krypton，maintenance LTS = 22.23.3 Jod）。

### 24.2 安装过程

1. 下载 `node-v26.11.1-x64.msi`（36,880,384 B），`Get-FileHash -Algorithm SHA256` = `5001c8e78d39593864339fb1a8c94c1ae19809bffde71e64b473d56f1f9726d3`，与官方 `SHASUMS256.txt` 逐字符一致。
2. **装前查进程**（用户要求的步骤）：全机唯一 node 进程是 PID 17244 = Qoder 自身的 computer-use MCP 启动器（`D:/Qoder CN/resources/extensions/qoder.computer-control/.../computerUseClientLauncher.cjs mcp`），**没有任何项目 dev server / vite / 构建任务在跑**；它占用的正是 `C:\Program Files\nodejs\node.exe`。PATH 首位为系统目录，`AppData\Local\hermes\node\node.exe` 是排在其后的残留副本。
3. 先取回滚包：`node-v25.9.0-x64.msi`（33,411,072 B），SHA256 `e469104e5d0c99f9185fbf17179fbb1d732261e6f9d281b6b2308a0126fb6d`，与官方校验一致，存 `temp/`。
4. `msiexec /i ... /qb REBOOT=ReallySuppress /l*v` 提权安装，**退出码 0**；Restart Manager 按设计关闭了那台 Qoder 辅助进程（会话内 computer-use 工具如需使用会自动重启）。装后 `node -v` = **v26.11.1**，`npm -v` 11.12.1 → **11.20.0**。

### 24.3 仓库侧改动（四处对齐）

- `FVCC/ui-src/package.json`：`@types/node` `^22.10.0` → **`^26.0.0`**（实际解析 26.6.5）；新增 **`engines.node: ">=26.0.0"`**，把"本机运行时 = CI 运行时"的契约写进包声明，避免再出现"CI 跑 22、本机跑 25 奇数线"的三不管状态。
- `.github/workflows/fvcc-ci.yml`：`actions/setup-node@v4` 的 `node-version: '22'` → **`'26'`**。
- `package-lock.json`：随 `npm install` 更新（`node_modules/@types/node` resolved 22.20.5 → 26.6.5）。CI 用 `npm ci`，本轮专门复跑过 `npm ci` 确认 lockfile 一致。
- `BUILD.md`：FVCC 前置条件表 Node 行改为 v26.11.1 事实（含 hermes 残留副本的 PATH 说明）；「工具链与依赖版本基线」表拆出 **Node 运行时** 行（并写明 26 是 Current 不是 LTS、active LTS 为 24 Krypton，下次抬 LTS 时 CI 与 `engines` 需同步），CI Node 行改 '26'，`@types/node` 行由"刻意推迟"改为"已随 Node 26 同批刷新"。

### 24.4 验证

- `npm ci` 干净安装 99 包；`npm run build`（`check:design` + `tsc -b` + `vite build`）绿，48 模块转换、1.72 s。
- **构建产物逐字节未变**：asset 内容哈希与升级前一致（`index-T2MOet9Q.js` 205.83 kB、`index-BrgkBbXh.css` 42.78 kB、lightweight-charts `DRrAN9MZ`）。据此判定本轮**不需要重打包、不需要再 bump**——交付版本仍是 FVCC v1.6.7，`engines`/CI/类型区间属开发者与 CI 侧契约。
- `scripts/check-versions.ps1` 绿：三方 1.6.7 一致 + go.mod `go 1.27.1`。
- **`npm audit` 复跑**：7 条告警（2 moderate / 5 high）**全部来自 tailwindcss 3 的传递依赖链**（braces ← micromatch/fast-glob ← tailwindcss；chokidar ← tailwindcss；postcss-selector-parser ← postcss-nested ← tailwindcss），唯一修复路径是 `npm audit fix --force → tailwindcss@4.3.3`，即刻意推迟的 Tailwind 4 专项。**Node 26 升级本身零新增告警**。
- `npm outdated` 复跑：当前**只剩 `tailwindcss` 3.4.19 → 4.3.3 一项**。

**状态（两轮合计，2026-10-14）**：第三十二轮改动 `FVCC/server/go.mod`、`go.sum` + 版本三真源（1.6.7）；第三十三轮改动 `FVCC/ui-src/package.json`、`package-lock.json`、`.github/workflows/fvcc-ci.yml`、`BUILD.md`。两轮均为**依赖/工具链批次，零功能与界面改动**；FVCS 侧本轮无变更（仍 v1.3.1）。规模与覆盖率复测（2026-10-14，新驱动 + 新运行时下亲跑）：后端 internal **12 包** / **110 个 .go**（58 实现 17,197 行 + 52 测试 9,183 行）、前端 TS **33 文件 11,474 行**、覆盖率 **60.7%** —— 与第二十九轮实测逐项一致，无漂移。

**本轮已知未做（如实登记）**：依赖刷新主题的**唯一余项是 Tailwind 4 专项**（3.4.19 → 4.3.3，CSS-first 配置重写 + 全套 `tailwind.config.js` 令牌迁移 + 暗色模式与 `@apply` 语义变更，且它同时是 7 条 npm 告警的唯一修复路径，属大工程需另立批次）。其余挂账与本主题无关：`FVCC/.commandcode/` 处置、store 备份/恢复前端入口、任务页 Filter Chips 与 fx 动画、其余页面骨架屏、e2e B 档 #7/#9、渲染 Failed/Cancelled 分支。另：`FVCS` 的 x/net v0.35.0、x/text v0.22.0、x/image v0.24.0 偏旧但无直接引用（纯 fyne 间接），维持"忽略"口径。

**入库与发布后记（2026-10-14）**：第三十一~三十三轮的改动按四个逻辑批次提交并推送 `fork/baseline-snapshot`——`923a8b3` build(toolchain)（GOTOOLCHAIN 统一 auto + 删失效探测项）/ `3bb1772` chore(deps)（sqlite v1.60.1 + Node 26 四处口径 + 三真源 bump 1.6.7）/ `246e905` docs(alignment)（§二十三/§二十四 + 13 份文档基线）/ `4154d31` docs(ledger)（三处长台账"未提交"表述随入库修正）。提交前复跑门禁：`go vet` 干净、`go test ./...` 14 包全绿、覆盖率 60.7%（阈值 60）PASS、`GOOS=linux CGO_ENABLED=0` 出 ELF、`npm ci` + `npm run build` 绿、`check-versions.ps1` 四方一致。经用户批准在 `4154d31` 打**注解 tag `v1.6.7`**（tag 对象 `fd5b04e`）并推送。

两笔如实记下的未验证面：① `baseline-snapshot` 的推送**不触发 CI**（`.github/workflows/fvcc-ci.yml` 仅对 `main` 的 push/PR 生效），所以本轮改的 `node-version: '26'` 与 `-race` 覆盖率门禁要到向 `main` 提 PR 时才第一次真跑；② `-race` 本机跑不了（需 CGO），故新 SQLite 驱动的并发行为（`database is locked`、WAL 下读写并发）目前只有单测与非 race 全量绿作为依据。另：`FVCC/.commandcode/` 仍未处置（未入库也未 ignore），v1.6.7 真机 6 点清单的逐项结果待回收。

---

## 二十五、第三十四轮（2026-10-14）：Tailwind CSS 4 大版本专项（3.4.19 → 4.3.3）+ v1.6.8 出包

### 25.1 背景与决策

这是 2026-10 依赖刷新主题的**最后一个余项**，也是 `npm audit` 那 7 条告警（2 moderate / 5 high，全部挂在 tailwindcss 3 的传递链上）的唯一修复路径。第三十二~三十三轮把它单列出来是因为它不是"改个版本号"：v4 是 **CSS-first 配置**，`tailwind.config.js` 的 token 映射、暗色策略、扫描面、`@apply` 语义、透明度写法全都变，直接触碰 `FVCC/docs/DESIGN.md` 契约的令牌层，属专项工程。

执行前与用户对齐了**五道门禁**（①依赖+postcss 接线 ②config token 迁 CSS-first + 暗色变体 + 透明度修饰符 ③改名/语义漂移工具类对齐 + check-design 同步 + @source 边界 ④构建全绿 + 真机视觉对照 ⑤文档令牌映射回写 + bump + 出包），并约定按"同主题批次最后统一入库"的习惯走：**逐门禁不提交**，全部做完再一次性收口台账、bump、打包。用户随后判定"直接走 5 打包新版 FPK，我在 NAS 上实测"，故门禁④的清单起草并入交付环节，真机对照由用户执行。

### 25.2 门禁①：依赖与 PostCSS 接线

- `package.json`：`tailwindcss` `^3.4.17` → **`^4.3.3`**；新增 **`@tailwindcss/postcss: ^4.3.3`**（v4 把 PostCSS 插件拆成独立包）；**删除 `autoprefixer`**（v4 内建 Lightning CSS 负责加前缀）。
- `postcss.config.js` 重写为只挂 `@tailwindcss/postcss` 一个插件。
- 探路时踩到 v4 的第一处结构变化：`npx tailwindcss` 报"could not determine executable"——v4 的 CLI 是另一个包 `@tailwindcss/cli`，本项目走 Vite/PostCSS 路径不需要它；改用小范围 PostCSS API 探针直接跑编译来验证接线。

### 25.3 门禁②：CSS-first token 迁移（`<alpha-value>` 是本轮最大的坑）

初版迁移保留 v3 的映射形状 `--color-x: rgb(var(--c-x) / <alpha-value>)`，编译产物里出现 **`rgb(var(--c-preview-bg) / <alpha-value>)` 字面量泄漏**——v4 不做 `<alpha-value>` 占位替换，它原样进 CSS。根因是 v4 的机制变了：注册色必须是**完整颜色**，`/NN` 透明度由编译期展开成 `color-mix(in oklab, X N%, transparent)`（并包一层 `@supports (color: color-mix(…))`，不支持时回落满不透明）。

因此：

- `tailwind.config.js` **整文件删除**（`git rm`），token 映射迁到 `src/style.css` 头部的 `@theme inline` 块，共 **38 条 `--color-*`** 注册（`inline` 让工具类直接引用 `--c-*`，运行时主题覆写照常穿透）。
- `:root` 与 `[data-theme="dark"]` 两套共 **76 条 `--c-*` 定义**（38 个令牌名）：其中 74 条由裸 RGB 通道（`37 99 235`）改为完整颜色（`rgb(37 99 235)`），另 `--c-log-bg` 明暗各一条本就是 rgba 完整色、直接透传。
- 手写 CSS 侧同步 `rgb(var(--c-x))` → `var(--c-x)`；需要透明度的用 `color-mix()`，例如 `.task-selected` 的选中内描边改为 `color-mix(in oklab, var(--c-primary) 55%, transparent)`。`src/ui.ts` 里一处内联样式标题色同样去 `rgb(var(...))` 包装。
- 暗色机制：`@custom-variant dark (&:where([data-theme="dark"], [data-theme="dark"] *))`，`theme.ts` 的 `data-theme` 切换逻辑零改动。
- 曾试过 `@config` 桥接 v3 配置文件，被否——它只是把债务往后推，且 v4 的 `@apply`/变体语义仍会变；一次性迁到 CSS-first 才是契约里认的形态。

### 25.4 门禁③：改名与语义漂移对齐、门禁脚本同步、扫描面锁定

逐项核查 v4 的破坏性变更清单，判定"暴露 / 不暴露"后再动手：

| v4 变更 | 本项目暴露情况 | 处置 |
|---|---|---|
| `shadow-sm` → `shadow-xs`、`shadow` → `shadow-sm` | 暴露（存在 `shadow-sm` 用法） | 类名改 `shadow-xs`；`check-design` 的 R3 正则同步扩到 `xs\|sm\|md` |
| `outline-none` 语义变为真去描边，v3 行为改名 `outline-hidden` | 暴露（**4 处**焦点环：`.btn` / `.input` / `.checkbox` / 编辑器页签） | 4 处改 `focus:-outline-hidden`，保留 forced-colors 下的 2px 透明描边，无障碍行为不变 |
| 裸 `ring` 由 3px 变、默认色变 currentColor | 不暴露（全仓 `ring-*` 均带显式宽度与颜色） | 不动 |
| `space-y-*` / `divide-y-*` 移入 `:where()` 零特异性 + `margin-block` | 不暴露（无依赖特异性的覆写） | 不动 |
| `transition-transform` 展开为 `transform,translate,scale,rotate` | 无行为差异 | 不动 |
| preflight 移除 `button,[role=button]{cursor:pointer}` | **暴露**（94 处按钮） | 见 25.5 产品裁决 |
| `hover:` 变体包进 `@media (hover:hover)` | 暴露（触屏不再残留悬停态） | 接受 |
| 默认边框色 / placeholder 色改 currentColor 系 | 不暴露（用法均带显式 token 类） | 不动 |
| `--spacing: .25rem` 基准，间距数值与 v3 一致 | 无差异 | 不动（实测对齐） |

门禁脚本 `scripts/check-design.mjs` 三处改动：

1. **修复一个真实缺陷**：`stripComments` 原来把多行块注释整体压成一行，导致行号错位——`--ansi-*` 豁免是按行索引判断的，v1.6.8 里 `style.css` 头部注释变多后暴露（v3 期注释是单行所以潜伏未发）。改为把注释字符替换成同数量空格并保留 `\n`。
2. R1 的修复提示文案改为"style.css `@theme` 注册的 token 类"。
3. **新增 `TW4-token-form` 规则**：命中 `<alpha-value>` 或 `rgb(var(--c-` 即失败，防止 v3 令牌写法回潮。命名刻意不叫 `R6-…`——契约的 Named Rule R6 是 Dialog Semantics，避免两个 R6 撞名。

扫描面：v4 默认自动源探测把 `scripts/check-design.mjs` 注释里的 `shadow-sm` 字样当真用法编进了产物（多出一条 `.shadow-sm` 规则）。改为 `@import "tailwindcss" source(none)` + 显式 `@source "../index.html"` 与 `@source "./**/*.{ts,js,html}"`，等价于 v3 的 `content` 域。

### 25.5 一处产品裁决：按钮的 `cursor: pointer`

v4 preflight 不再给 `button` / `[role=button]` 加 `cursor: pointer`，意味着 FVCC 94 处按钮悬停不再变手型。这是"改一行 `@layer base` 就能恢复"的事，但属产品观感决策，故提请用户裁决——**用户判定"接受 v4 新默认"**，不做覆写。该裁决同时写进 `DESIGN.md` 的 v1.6.8 复核条目与本轮真机核对清单，避免后续被当成迁移漏洞"顺手修回来"。

### 25.6 门禁④：迁移保真校验（构建产物层面）

方法是**拿 v3 基线 CSS 产物与 v4 产物做规则级比对**，而不是只看构建是否绿：

- 抽出两边全部类 token 共 **388** 个逐项对齐：**v3 有 / v4 无 = 0**（收紧 `@source` 之前曾出现真实缺失，定位即扫描面 scope creep）。
- **22** 个不同的 `/NN` 透明度修饰符类在 v4 产物中全部存在（含带前缀变体形式），`color-mix` 出现 **52** 处，`<alpha-value>` 泄漏 **0** 处，`forced-colors` 规则 **4** 处。
- 体积：CSS 原始 **42,782 → 49,956 B**（v4 展开 `@supports` 兜底与 `color-mix` 包裹），**gzip 10.00 → 9.78 kB**（Lightning CSS 压缩更狠，净变小）。
- 过程中若干次"疑似回归"逐一证伪：`@types/node` 探针的"22 个淡色类缺失"是 JSON 双反斜杠与变体选择器（`.active\:brightness-90`、`:where(.group)`）显示造成的误读；`.select` 的 `@apply appearance-none` 被内联；两次比对用 `matchAll` 取代 `t.match`。

### 25.7 门禁⑤：文档回写、版本 bump 与出包

- 三真源 bump **1.6.7 → 1.6.8**（`FVCC/manifest` / `ui-src/package.json` / `server/internal/version/VERSION`），`scripts/check-versions.ps1` 绿。
- **新发现的一处漂移**：`ui-src/package-lock.json` 的根 `version` 字段手工 bump `package.json` 时不会跟进，实测漂留在 1.6.7，且 `check-versions.ps1` 不覆盖它。用 `npm install --package-lock-only`（只刷元数据、不动 `node_modules`）同步，并把这条写进 BUILD.md 的版本同步清单注释，作为第四处提醒。
- 文档回写 **9 份**：`FVCC/docs/DESIGN.md`（frontmatter `colors:` 块改 v4 形态 + 新增 v1.6.8 复核条目 + R1/R10 文案 + `TW4` 门禁口径）、根 `BUILD.md`（依赖基线表两行 + 新增「Tailwind 4 样式链接线」小节 + 版本同步清单）、根 `README.md` 与 `FVCC/README.md`（版本指针 + 1.6.8 版本线行）、`docs/IMPROVEMENT_LOG.md`（本节 + 现状快照）、`docs/FVCC_UI设计优化方向.md`（五复核 + §1.1/§1.2 令牌载体口径）、`docs/FVCC_项目现状与开发步骤.md`（v1.6.8 基线段 + 目录树删 `tailwind.config.js` + 技术栈样式行 + 证据索引复核记录）、`docs/FVCC_混乱度评价报告_细化版.md`（**第十三版**，评分维持 2.3）、`docs/项目分析与改进方向.md`（**第十次回写**）、`docs/SECURITY.md` / `docs/TASK_REFERENCE.md` / `FVCC/docs/API_CONTRACT.md` / `WebVideoEditor_Design/02-前端架构与时间线编辑器设计.md`（版本指针与"未引入新任务编号 / 无新依赖来源"口径）。
- `build.ps1 -Target FVCC` 输出 **Packing successfully**，`FVCC/fvcc.fpk` = **11,013,500 B**（v1.6.7 为 11,011,826 B，+1,674 B）。解包核验：包内 manifest `1.6.8`、`manifest.checksum == md5(app.tgz)`、包内 `ui/assets/index-By6Fdeqj.css` 49,956 B 且 banner `tailwindcss v4.3.3`、无 `<alpha-value>` 残留、无 `.shadow-sm`；二进制 `app/fvcc` 27,414,688 B 与 v1.6.7 **逐字节同尺寸**（零后端改动），内含版本串 `1.6.8`×1、`1.6.4/1.6.5/1.6.6/1.6.7`×0。

### 25.8 验证（本轮亲跑，非沿用）

- `npm run build` 绿：`check:design`（33 文件，无违规）→ `tsc -b` → `vite build`（48 模块，908 ms），产物 `index-By6Fdeqj.css` 49.95 kB / gzip 9.78 kB、`index-CIDB_L2E.js` 205.82 kB。
- **`npm audit`：`found 0 vulnerabilities`**——第三十三轮登记的 7 条告警（2 moderate / 5 high）随 tailwindcss 3 传递链整体移除而清零。
- **`npm outdated`：空输出**——依赖刷新主题无落后项。
- `cd FVCC/server`：`go build ./...` 与 `go vet ./...` 干净；`go test ./... -count=1` **14 包全绿**（api / edl / media / node / pathutil / protocol / remote / scheduler / security / store / store/model / version / logger / smbshare，`fvcc` 与 `internal/ws` 无测试文件），store 包 21.955 s 最长。
- `scripts/check-versions.ps1` 绿：三方 1.6.8 一致 + go.mod `go 1.27.1`。

### 25.9 本轮已知未做与未验证面（如实登记）

- **未验证面（关键）**：NAS 真机视觉对照。构建产物层面的保真已逐项核验，但**渲染观感必须真机**——① 低透明度 `color-mix(in oklab)` 淡色（`bg-primary/5`、`/15`、进度条纹）与 v3 sRGB 插值的色差；② 94 处按钮失去 `cursor: pointer` 的可点感（已裁决接受，但需确认不影响操作直觉）；③ `hover:` 受 `@media (hover:hover)` 门控后触屏不再有悬停态；④ forced-colors 模式下焦点环可见性（`outline-hidden` 的透明描边）；⑤ 明/暗两套主题整体走查、时间线 clip 数据色、拖拽落点线、滚动条、ANSI 日志面板。核对清单随批交付，**用户实测中**。
- 零后端改动，故代码规模与覆盖率沿用 2026-10-14 复测口径（Go 110 个 .go / TS 33 文件 11,474 行 / 60.7%）不变，本轮未重跑覆盖率。
- 渲染 Failed→ERROR / Cancelled→CANCELLED 终态分支仍无自然复现，沿用"遇真实失败顺带验证"口径。
- 与本轮无关的既有挂账不变：`FVCC/.commandcode/` 处置（仍未入库也未 ignore）、store 备份/恢复前端入口、任务页 Filter Chips 与 `?fx=` 实验动画、其余页面骨架迁 `listSkeleton`、e2e B 档 #7/#9、P3-6/P3-8。
- `baseline-snapshot` 推送不触发 CI 这条限制依旧成立：v4 的构建路径要到向 `main` 提 PR 时才第一次在 CI 上真跑（本机 `npm ci` + `npm run build` 已绿作为替代证据）。

### 25.10 入库与发布后记

（待用户批准提交/push/tag 后回填。）
