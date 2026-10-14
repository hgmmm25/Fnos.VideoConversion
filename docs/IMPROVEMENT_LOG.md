# FVCC 改进方向落实记录（2026-09-17 起稿 · 2026-10-14 更新至 v1.6.9）

> **现状快照（2026-10-14 第三十七轮，低风险收尾批次）**：当前版本 **FVCC v1.6.9 / FVCS v1.3.1**（FVCS 零改动）。三项收尾一次做完：① **P3-5 尾部还清——store 备份/恢复前端入口**（设置页新增「数据备份与恢复」面板：备份列表 + 立即备份 + 整包恢复，接 v1.5.8 已交付但一直无人接的 `/api/store/*` 三端点）；② **P3-6 台账正式勾选关闭**（NAS `/info` `ffprobe:true` 实测已闭，本轮收口口径）；③ `FVCC/.commandcode/`（AI 工具产物）按用户裁决**入 .gitignore 不入库**。本轮为纯前端 + 治理（后端 Go 零改动），交付前用 **dev 模式真实后端 + 浏览器**把备份/恢复全链路跑通（含 `prerestore` 兜底备份与 >20 份列表展开），过程中顺手补了备份 kind 标签缺口。新登记一处真实缺口：**手动备份永久保留但无删除入口**（后端亦无 `DELETE /api/store/backups`），待裁决。按出包纪律 bump **1.6.8 → 1.6.9** 并重打 fpk（**11,014,297 B**；出包时本机 Windows PowerShell 5.1 在 AMSI 扫描环节崩于 `build.ps1` 编译，改由 `pwsh` 7.7.0-preview.5 跑同一条命令通过，脚本零改动）。已提交（主体 `dedd124` + 本后记笔，**注解 tag `v1.6.9` 打在后记笔**）并推送 `fork/baseline-snapshot`；NAS 真机安装验证由用户执行。详见 §二十八（发布后记 §28.9）。
> **现状快照（2026-10-14 第三十四轮，Tailwind CSS 4 大版本专项）**：**当时版本 FVCC v1.6.8 / FVCS v1.3.1**（现已被 v1.6.9 取代，见上段快照；FVCS 零改动）。本轮还清第三十二~三十三轮点名的**依赖刷新主题唯一余项**——`tailwindcss` **3.4.19 → 4.3.3**：`tailwind.config.js` 整文件删除，token 映射迁入 `ui-src/src/style.css` 的 `@theme inline`；38 个 `--c-*` 令牌（明暗两套共 76 条定义）由裸 RGB 通道改为完整 `rgb()` 颜色（v4 不替换 `<alpha-value>`，`/NN` 透明度改编译期 `color-mix(in oklab, …)`）；暗色改 `@custom-variant dark`；PostCSS 插件换 `@tailwindcss/postcss` 并删 `autoprefixer`（前缀由 v4 内建 Lightning CSS 负责）；改名与语义漂移逐项对齐（`shadow-sm`→`shadow-xs`、`shadow`→`shadow-sm`、`outline-none`→`outline-hidden`）；扫描面 `source(none)` + 显式 `@source` 锁定。收益：`npm audit` 现存 **7 条告警（2 moderate / 5 high）全部清零**、`npm outdated` 归零、CSS gzip 10.00 → **9.78 kB**。**依赖刷新主题至此完整收口，无余项。** 随批产品裁决（用户）：接受 v4 移除 `button { cursor: pointer }` 的新默认。按出包纪律 bump **1.6.7 → 1.6.8** 并重打 fpk（11,013,500 B），NAS 真机视觉对照由用户执行中。详见 §二十五。
> **现状快照（2026-10-14 第三十二~三十三轮，依赖/工具链专项批次）**：**当时版本 FVCC v1.6.7 / FVCS v1.3.1**（现已被 v1.6.8 取代，见上一段快照）。
> 第三十二轮还清了第三十一轮点名的唯一实质落后项——`modernc.org/sqlite` **v1.14.0 → v1.60.1**（跨 46 个 minor）：先把 store 层对驱动的接触面量清楚（`sql.Open` 无 DSN 参数、三条 PRAGMA 走 Exec、无驱动 API、SQL 为纯 SQLite 子集），再用**老驱动真实产出的 fvcc.db** 由新驱动经实际 store 路径 Open→migrate→写回→二次 Load 验证（WAL 保持、`schema_version` 不误重置、AUTOINCREMENT 续号、立即落盘可见）；副作用是依赖面收窄（删 cc/ccgo/opt/strutil/token/uint128/x-mod/x-sync/x-tools/shellquote 十项，`go.sum` 净减 109 行），代价是二进制 +565 KB、fpk +2.4%（10,752,127 → 11,011,826 B）。按出包纪律 bump **1.6.6 → 1.6.7** 并重打 fpk，真机复验由用户在 .106 执行。详见 §二十三。
> 第三十三轮把 Node 运行时从**已 EOL 的奇数 Current 线 v25.9.0** 升到 **v26.11.1**（系统级 MSI，SHA256 与官方逐字符核对，另存 v25.9.0 回滚包）；装前查进程确认全机唯一 node 是 Qoder 自身辅助进程、无项目任务在跑。升级点四处一起对齐：本机运行时、`package.json` 新增 `engines.node >=26.0.0`、CI `setup-node` `22`→`26`、`@types/node` `^22`→`^26`（解析 26.6.5）。`npm ci` + `npm run build` 绿且 **asset 内容哈希逐项不变**，故判定无需重打包、不再 bump。详见 §二十四。
> **依赖刷新主题至此收口，余项只有 `tailwindcss` 3.4.19 → 4.3.3 专项**（同时是 `npm audit` 现存 7 条告警的唯一修复路径，全部源自 tailwindcss 3 的传递链，Node 26 升级零新增告警）。**（该专项已于同日第三十四轮以 v1.6.8 落地，7 条告警清零，见上段快照与 §二十五。）**规模与覆盖率在新驱动+新运行时下**亲跑复测**：internal 12 包 / 110 个 .go（58 实现 17,197 + 52 测试 9,183）/ TS 33 文件 11,474 行 / 覆盖率 **60.7%**，与第二十九轮逐项一致。9 份叙述性长台账本轮一并回写至 1.6.7 / 1.3.1 基线（第三十一轮挂账还清）。

> 依据《项目分析与改进方向.md》逐项落实。原则：低风险直接改码并验证；结构性大改（SQLite 迁移、事件驱动调度、freecut 收敛）如实说明未落实原因，不强行破坏既有稳定链路。
> 验证基线：`go build ./...` / `go vet ./...` / `go test -short ./...` 全部通过；`tsc --noEmit` 通过；`scripts/check-versions.ps1` 退出码 0。

---

## 一、找到的文档

- 主文档：`docs/项目分析与改进方向.md`（9.1 KB，2026-09-17）——17 项改进条目（P0-1 ~ P3-3）。
- 配套：`docs/TASK_REFERENCE.md`（任务编号对照）、`docs/SECURITY.md`（安全基线）、`docs/FVCC_混乱度评价报告_细化版.md`（治理台账，第七版 2026-09-21）、`docs/FVCC_项目现状与开发步骤.md`（现状指引，2026-09-20 新增）、`docs/FVCC_UI设计优化方向.md`（UI 方向，1.5.3）。
- 落地记录：`FVCC/docs/P2-1_后端分层_阶段A实施记录.md`、`FVCC/docs/P2-2_细化细则.md`、`FVCC/docs/API_CONTRACT.md`、`FVCC/docs/DESIGN.md`、`FVCC/docs/PRODUCT.md`。

## 二、改进项总体落实情况（2026-09-21 复核）

| 编号 | 改进项 | 落实状态 | 关键落地证据 |
|---|---|---|---|
| P0-1 | 凭据加密落库 | ✅ 已落地 | `internal/security/crypto.go`（AES-GCM + 主密钥 0600 落盘 secret.key + `enc:v1:` 前缀 + 旧明文兼容迁移 + API 脱敏） |
| P0-2 | 清理工程残留 + .gitignore | ✅ 已落地（2026-09-21 P2-3 仓库治理收尾） | .gitignore 已建；fpk_build_test2 / fpk_inspect / 0 字节残留已清；temp 打包 stage 与调试残留 18 项清理（释放约 560.3MB）；6 个 fpk 归档 archive/fpk/ 完整 v1.1.0~v1.5.3（明细见 §五 第十六轮） |
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
| P1-2 | 前端收敛 freecut | ✅ **已定案关闭（2026-10-11 第二十一轮）**：方案 A——维持零框架、借 freecut 设计不借底座；框架试点绑定多轨 `{videoTracks,audioTracks}` 立项再评审（见 §五 第二十一轮） |
| P2-3 门槛 | 覆盖率 ≥60% 强制门槛 | ✅ **已落地（2026-10-12 第二十六轮，P3-7 关闭）**：补 logger/smbshare/media helper 测试后实测 59.0%→**60.7%**，check-coverage.ps1 默认阈值与 CI `-Threshold 60` 同步上调（历史：55% 落地，基线 56.7%→59.0%） |

## 四、关键验证结果（最新基线）

1. `cd FVCC\server && go build ./...` / `go vet ./...` / `go test ./...` —— ✅ 通过（**最新基线 1.6.9：本轮零后端代码改动**，仅 `internal/version/VERSION` 随 bump 变更，`go test ./internal/version` 真跑绿；历史口径：**1.6.8 全量复绿**（2026-10-14 第三十四轮 Tailwind 4 专项复跑——该轮零后端改动，`go build`/`go vet` 干净、`go test ./... -count=1` 14 包全绿）、1.6.7 sqlite 专项后于 2026-10-14 复绿、1.6.6/1.6.5 第二十八~三十轮、1.5.12 全量复绿（第二十四轮）、1.5.3 批次 go vet / go test 11 包全绿）
2. 覆盖率门禁：全包口径 56.7% → 59.0% → **60.7%**（check-coverage.ps1 口径修复 `'.'`→`'./...'` 后实测；阈值演进 55%→**60%**，2026-10-12 第二十六轮 P3-7 关闭，60.7% ≥ 60% PASS。**第二十八/二十九两轮未重跑覆盖率**，引用值均为第二十六轮实测）
3. 前端：`tsc --noEmit` / `npm run build`（tsc + check:design + vite）—— ✅ 通过（**最新基线 1.6.9**，2026-10-14 第三十七轮 store 备份/恢复前端入口后复跑：`check:design --all` 33 文件无违规 → `tsc -b` 干净 → `vite build` 48 模块 / 1.2 s，产物 `../app/ui/assets/index-ByMsNQMP.css` 49.99 kB（gzip 9.79 kB）+ `index-SH0C878Z.js` 208.43 kB（gzip 66.13 kB）；历史口径：1.6.8 为 Tailwind 4 迁移后 CSS 49.95 kB / gzip 9.78 kB，1.6.5 及以前 CSS 42.78 kB / gzip 10.00 kB。本轮依赖零变动，1.6.8 的 `npm audit` **found 0 vulnerabilities** / `npm outdated` 空输出口径继续有效）
4. 版本校验：`scripts\check-versions.ps1` —— ✅ 退出码 0（manifest / package.json / VERSION 三处一致 = **1.6.9**，2026-10-14 第三十七轮 bump 后实测，`ui-src/package-lock.json` 根 `version` 亦已 `npm install --package-lock-only` 同步至 1.6.9；历史：1.6.8 第三十四轮实测时 lock 漂留在 1.6.7——另注意 `ui-src/package-lock.json` 的根 `version` 不随手工 bump 跟进、也不在校验范围内，需 `npm install --package-lock-only` 同步，已写入 BUILD.md 版本同步清单注释。配套 FVCS v1.3.1，其两处真源（`pkg/version/VERSION` + `version_test.go` 断言字面量）已由 `go test ./pkg/version` 校绿。脚本仓库根由 `$PSScriptRoot` 推导、支持 `-RepoRoot` 覆盖；`internal/version/version_test.go` 在 Windows 本机真跑，反例改坏 manifest 即判红。历史：1.6.7 / 1.6.5 / 1.5.12 / FVCS v1.2.6 口径见第二十四~三十三轮）
5. 早期轮次专项验证（WSS 双端单测、trace ID 注入、回收站/审计单测、并发/fuzz）见 §五 对应轮次摘要

## 五、历史轮次摘要（第一~二十九轮压缩；原 §七~§二十一 已并入本表）

> 各轮完整实施细节见 git 历史（提交与 tag 均在 `fork/baseline-snapshot`）与 §一 对应落地文档，此处仅留一行摘要。

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
| 第十五轮 | 2026-09-20 / 1.4.9 | 版本迭代 1.4.8 / 1.4.9 + 后端细化拆分（stream_media / roots / aliases / config / trash_handlers / edl 扩展，87 个 .go）+ docs 布局收口（PRODUCT/DESIGN 入 FVCC/docs/）+ 前端图示化阶段 A-D（git e151d08） |
| 第十六轮 | 2026-09-21 / 1.5.3 | 代码清理（步骤 3：scheduler 防注入死分支 / checkUploadProgress/checkDownloadProgress 空占位 / TestB05SideGuards 过时用例 / editor placeholder 注释，go vet + go test 11 包 + npm run build 全绿）+ 仓库治理（步骤 6：temp 18 项清理释放约 560.3MB + 6 fpk 归档 archive/fpk/ 完整 v1.1.0~v1.5.3） |
| 第十七轮 | 2026-09-21 / 1.5.3 | 步骤 4 产出 `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md` 方案评审稿（备份/迁移/快照回滚，代码待落地）+ 步骤 5 补写 FVCC/docs/SECURITY.md + 步骤 7 质量门禁真实化（check-coverage 口径 bug `'.'`→`'./...'`，实测 56.7%；flaky 测试修复）+ P3-1 提交收尾并打 tag v1.5.3 |
| 第十八轮 | 2026-09-21 / 1.5.3 | docs 与代码现状对齐校准：确认 P0-2 SQLite 迁移（a72b8ff / 9e122d5 / 5dd373e）与 P1-1 事件驱动（eab7688）已随 v1.5.3 落地并补登；根 docs/ 7 份台账对齐（实测 96 个 .go / TS 10,315 行） |
| 第十九轮 | 2026-09-22 / 1.5.7 | 1.5.4 编辑器预览变速播放 J/K/L（2x→3x→4x）+ 播放按钮图标化（080754c）；1.5.5~1.5.7 Windows 盘符根双分隔符边界修复（handlers / edl_validate / trash / smbshare 四处）+ 剪辑页多根错位兜底 + roots 回落 + 素材递归扫描 + 配套单测（后提交 68ebdec 并打 tag v1.5.7） |
| 第二十轮 | 2026-10-11 / 1.5.8 | P3-5 store 备份/迁移/快照回滚落地（阶段 A~D：efef4bd 备份 / 16f1043 迁移框架 / 59e4004 快照回滚 / 24cbbc5 收口）；覆盖率 56.7%→**59.0%**；前端回滚入口明确延后 |
| 第二十一轮 | 2026-10-11 / 1.5.8 | P1-2 前端收敛（freecut 双轨）定案关闭：方案 A——维持零框架「借 freecut 设计不借底座」，框架试点绑定多轨立项；v1.5.8 推送 fork + 注解 tag |
| 第二十二轮 | 2026-10-11 / 1.5.10 | 步骤 8 真实 NAS+Windows e2e 走查（闭环①~⑤）+ 缺陷台账 #5~#11 + A 档修复（#5 代理提交带素材根 / #6 代理 ready 态回填 `GET /api/proxy` / #10 视频删除按钮 / #11 FVCS 重连自愈 online）+ #8 渲染根跨端返工（FVCS `srcBase=mountPath` + FVCC 移除误判闸门 4b） |
| 第二十三轮 | 2026-10-12 / 1.5.11 | #8 再复验——渲染提交侧「有界单根」修复：`EDLClip.sourceRoot` 透传 + `resolveRenderRoots`（同根/跨根拒绝/旧项目回落）；同轮第三层——FVCS `makeSrcResolver` 对齐代理 `BuildSMBPath`（修「sourceRoot 为空」误报）+ 重部署渲染机 exe |
| 第二十四轮 | 2026-10-12 / 1.5.12 | 渲染出片后 FVCC 卡「收尾·100%」——补 RENDER_EDL 终态轮询 `pollRenderTerminal`/`completeRenderTask`（Success→COMPLETED 进历史；Failed/Cancelled 分支代码就位待自然复现）；附带修复 `validateRootRelPath` 对齐 FVCS `ValidateRootPath`（共用向量 PL-ROOT-ABS 转绿）；真机 Success 收口 ✅ + 进度平滑改善 |
| 第二十五轮 | 2026-10-12 / 1.5.12 | 文档全面对齐轮：FVCC/README + FVCC/docs 7 份 + 根 docs/ 7 份 + BUILD.md 统一对齐 v1.5.12 / FVCS v1.2.6，P3-4 文档滞后清零（纯文档轮，不动代码） |
| 第二十六轮 | 2026-10-12 / 1.5.12 | v1.5.12 打注解 tag（落 ad77266）并推送 + P3-7 关闭（补 logger/smbshare/media 三份测试，实测 **60.7%**，门禁 55%→60%）+ P3-9 关闭（build_err.txt 已删）；剩余 P3 债务仅 P3-6/P3-8 |
| 第二十七轮 | 2026-10-12 / 1.6.0 | v1.6.0 视觉打磨版（方向 A 冷灰，批次①-⑥ 均经用户逐批验收）+ FVCS v1.3.0 渲染节点 GUI 复活（`FVCS/pkg/fynetheme`，构建目标 FVCS-UI）；推送 1e622f0 / 22035c6 / b1a0d93 + 注解 tag v1.6.0 |
| 第二十八轮 | 2026-10-13 / 1.6.5 | 上半段：任务页视觉改进批次 A+B+C 收口为 v1.6.4（fe06e52 + tag，用户硬件验收通过）；下半段：v1.6.5——VERSION 第三真源漂移修复 + 版本校验从「静默 SKIP」改真跑（反例验证拦得住漂移）；推送 ceeb1b2 / 5166cbe + tag v1.6.5；易错点：`v1.6.1-A*/B*/C*` 是开发期批次编号从未入库（方案稿号 ≠ 版本号，登记于 TASK_REFERENCE §3.8） |
| 第二十九轮 | 2026-10-13 / 1.6.5 | 叙述性长台账对齐轮：9 份文档基线统一至 v1.6.5 / FVCS v1.3.0（混乱报告第十一版综合 2.3 等，历史快照按惯例保留）；规模亲测 110 个 .go / 26,380 行、TS 33 文件 11,474 行、覆盖率沿用 60.7% |

## 六、Roadmap（结构性子任务，长期）

### 6.1 待办清单（持续更新）

| 编号 | 改进项 | 优先级 | 目标 | 里程碑 | 状态 |
|---|---|---|---|---|---|
| P0-2 | JSON 文件存储 → SQLite | P0 | 消除手写持久化 | M1/M2/M3 | ✅ 已落地（2026-09-21，见 §五 第十八轮） |
| P1-1 | ticker → 事件驱动调度 | P1 | 纯事件驱动 + 超时兜底 | M1/M2/M3 | ✅ 已落地（2026-09-21，见 §五 第十八轮） |
| P1-2 | 前端收敛 freecut | P1 | 单前端单入口 | M1/M2/M3 | ✅ 已定案关闭（2026-10-11 第二十一轮，方案 A 维持零框架，见 §五） |
| P3-5 | store 持久化健壮性（定时备份/配置迁移/快照回滚） | P3 | 原子写 + 备份轮转 | 下一迭代 | ✅ 已落地（2026-10-11 v1.5.8，阶段 A~D：efef4bd / 16f1043 / 59e4004 / 24cbbc5，见 §五 第二十轮）；**前端入口已补（2026-10-14 v1.6.9 第三十七轮）**——设置页「数据备份与恢复」面板接上 `/api/store/*`；残余缺口：手动备份无删除入口（见 §二十八.6） |
| P3-6 | ffprobe 运行时环境依赖评估 | P3 | 生产缺探测能力兜底 | 下一迭代 | ✅ **已关闭**（2026-10-14 第三十七轮台账勾选：2026-10-11 真机 `/info` `ffprobe:true`，fnOS mediasrv 提供、无 stub 降级路径触发；启动自检 `main.go` 已打印 `ffprobe=%v`。遗留的"probe-vs-ffprobe 差异核对"为可选项，不再占用 P3 编号） |
| P3-7 | 覆盖率门槛 55% → 60% | P3 | CI 稳定后上调 | 后续 | ✅ 已关闭（2026-10-12 第二十六轮：补 logger/smbshare/media helper 测试，实测 60.7%，check-coverage.ps1 默认阈值与 CI `-Threshold 60` 同步上调） |

### 6.2 推进约定

1. 每轮改动后重新执行 §四 验证基线；版本升级时三处（manifest / package.json / VERSION）同步并跑 check-versions.ps1。
2. 破坏性操作（删除/清空/审计）必须过 `internal/security/audit.go` 审计闭环。
3. 结构性大改先出设计文档再动码，落地后回写实施记录（P2-1/P2-2 模式）。

---
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

### 25.10 入库与发布后记（2026-10-14）

用户判定"功能没问题"，批准整批一次性入库。本批次按两个逻辑提交推送 `fork/baseline-snapshot`：

- **`e1ed35c` `chore(deps): Tailwind CSS 4.3.3 大版本专项（CSS-first 令牌迁移），版本 bump v1.6.8`**——23 文件 `+1,219 / −1,418`，含 `FVCC/ui-src/tailwind.config.js` 删除（`delete mode 100644`）。
- 本笔 `docs(ledger)` 后记提交（提交状态随入库更新），**注解 tag `v1.6.8` 打在此笔**并推送，与 v1.6.7 的发布口径一致。

提交前复跑门禁全绿：`check:design --all` 33 文件无违规、`npm run build`（CSS `index-By6Fdeqj.css` 内容哈希未变）、`go build ./...` 与 `go vet ./...` 干净、`go test ./... -count=1` 14 包全绿、`scripts/check-versions.ps1` 三方 1.6.8 一致；`FVCC/fvcc.fpk`（11,013,500 B）为提交前已核验产物。

**未入库项如实登记**：`FVCC/.commandcode/` 仍是 `??`（既未跟踪也未 ignore），本轮不代为处置。构建产物 `FVCC/app/`、`FVCC/temp/`、`fvcc.fpk` 按 `.gitignore` 规则不入库；本轮自建的临时解包校验脚本 `FVCC/temp/verify_fpk.py` 已删除。

**发布后仍待回收**：NAS 真机视觉对照清单（淡色 `color-mix` 色差、焦点环、按钮无 `cursor:pointer` 的操作感、触屏悬停门控、forced-colors）与既有的渲染 Failed/Cancelled 分支验证。CI 侧限制不变：`baseline-snapshot` 推送不触发工作流，v4 构建路径首次 CI 真跑要到向 `main` 提 PR 时。

## 二十六、第三十五轮（2026-10-14）：CI Actions 大版本升级（checkout/setup-go/setup-node → v7）

### 26.1 背景

第三十五轮的前置是一次全量工具版本复核（本机安装态 vs 各上游最新版）。复核结论：两个 Go 模块的直接依赖与 FVCC 前端 9 个包**全部已是最新版**（含跨大版本核对，不只是 `npm outdated` 的范围内核对），工具链里 Node 26.11.1 与 MSYS2 gcc 16.2.0 亦为最新，落后项都是补丁级（Go 1.27.1→1.27.2、FFmpeg 9.0.1→9.0.2）。**唯一实质工具债是 CI 的三个 action pin**：`checkout@v4`（最新 v7.0.1）、`setup-go@v5`（v7.0.0）、`setup-node@v4`（v7.1.0）。

### 26.2 三个大版本各自的破坏性变更（逐条判定是否影响本仓库）

| action | 跨越 | 破坏性变更 | 对本仓库的影响 |
|---|---|---|---|
| `actions/checkout` | v4→v7 | v5：运行时换 Node 24，要求 runner ≥ v2.327.1；v6：凭据落盘改为独立文件（`persist-credentials` 语义）；v6.1/v5.1：新增 `allow-unsafe-pr-checkout`；v7：**拦截 `pull_request_target` / `workflow_run` 里的 fork PR 检出**，并迁移到 ESM | checkout 之后没有任何 git 写操作，凭据落盘方式无关；本工作流触发事件是 `push` / `pull_request`，**不碰 v7 的拦截面**；ubuntu-latest runner 远高于 v2.327.1 |
| `actions/setup-go` | v5→v7 | **v6：显式设 `GOTOOLCHAIN=local`**（不再自动下载 go.mod 声明的更高工具链）、优先用 `toolchain` 指令、运行时换 Node 24；v7：ESM 迁移 + `@actions/cache` 6.2.0 | 与仓库既有 `GOTOOLCHAIN=auto` 口径正面冲突，需判定（见 26.3）；`cache: true` 默认 + `cache-dependency-path` 用法不变 |
| `actions/setup-node` | v4→v7 | v5：`package.json` 有 `packageManager` 字段时**自动开缓存**（可用 `package-manager-cache: false` 关）+ 运行时换 Node 24；v6：自动缓存限定为 npm；v7：**移除 dummy `NODE_AUTH_TOKEN` 导出**、新增 `cache-primary-key` / `cache-matched-key` 输出、缓存文档改为防投毒口径 | `ui-src/package.json` 没有 `packageManager` 字段，自动缓存路径根本不触发，显式 `cache: npm` 行为不变；本工作流不发布 npm 包，`NODE_AUTH_TOKEN` 移除无影响 |

### 26.3 `GOTOOLCHAIN=local` 的判定（本轮唯一的实质风险）

`FVCC/server/go.mod` 声明 `go 1.27.1`（**下限**），CI 的 `go-version: '1.27.x'` 在 v6+ 的 local 语义下会解析出 runner 上最新的 1.27 补丁（当前 1.27.2）并**禁止再下载别的工具链**。因为 1.27.2 ≥ 1.27.1，两个 job 的所有 `go` 命令都正常执行，**升级无影响**。

需要留一道后手：若将来把 go.mod 的 `go` 指令抬到比 runner 可用补丁还高，CI 会直接报 `go.mod requires go >= X (running go Y; GOTOOLCHAIN=local)`。这是"抬 go 指令必须同步改 `go-version`"的信号，已写进工作流头注释。

顺带澄清一个口径差异：本机 Windows 仍按 `build-env.ps1` 的 `GOTOOLCHAIN=auto`（启动器 `sdk/go1.25.3` 与 go.mod 下限不同源，必须靠 auto 选取），CI 则是 local 锁定。**两端不一致是有意为之**，不是漂移。

### 26.4 改动与本地验证

改动仅 `.github/workflows/fvcc-ci.yml` 一个文件：三处 `uses:` 升到 v7（4 个 step 实例），头部新增 action 版本基线注释。**未 bump 应用版本、未出包**（纯 CI 配置，产物零变化）。

本地按 CI 的两个 job 逐项复现（Go 侧统一 `env -u GOOS -u GOARCH` + `CGO_ENABLED=1` + mingw gcc）：

- **frontend job 全绿**：`npm ci`（exit 0）→ `npm run build` 绿，产物内容哈希与本轮复核前那次构建逐项一致（`index-By6Fdeqj.css` 49.95 kB / `index-CIDB_L2E.js` 205.82 kB）。
- backend job：`go vet ./...` 干净；`scripts/check-coverage.ps1 -Threshold 60` **PASS，总覆盖率 60.7%**（与第二十六轮口径一致）。
- **backend 的 `go test -race ./...` 判红**——见 26.5。

### 26.5 首次真跑 `-race` 暴露的两个 scheduler 失败（与 action 版本无关）

第三十三轮出包时登记的是"`-race` 本机跑不了（需 CGO），留给 Linux CI"。本轮本机装了 MSYS2 mingw-w64 gcc，`CGO_ENABLED=1 go test -race` **实际可跑**，于是这个从未被执行过的门禁第一次真跑就红了。两处：

1. **`TestP1EventDrivenQueueDispatch`：`WARNING: DATA RACE`（确定性、跨平台）**。`fakeDispatcher` 的 `edlCalls` / `lastServer` / `lastTask` 是裸字段，写方是调度器协程（`scheduler.go:183 handleEvent → :274 processTask → :939 handleRenderEDL → :979 dispatchRenderLike → scheduler_edl_test.go:43` 自增），读方是测试协程在 `scheduler_p1_test.go:47` 轮询 `d.edlCalls == 0`。桩件无同步 → **Linux CI 同样会红**。涉及面：`fakeDispatcher` 被 4 个测试文件使用（`scheduler_edl_test.go`、`scheduler_p1_test.go`、`scheduler_concurrency_test.go`、`node_select_integration_test.go`），裸读点约 12 处，修法为桩内加 mutex + 提供带锁读取器。
2. **`TestSchedulerTickConcurrentSafe`：`仍有 11 个任务滞留 QUEUE 未收敛`**（`scheduler_concurrency_test.go:123`）。160 个并发提交任务在 `-race` 降速下超出 10 s 收敛窗口；不带 `-race` 时该包 9.091 s 全绿（本轮两次实测一致）。属时序余量问题，需判定是放宽窗口还是确有收敛缺陷。

**不带 `-race` 时两个失败都不出现**——这解释了为什么历轮"`go test ./... -count=1` 14 包全绿"的记录与本轮结论不矛盾。

### 26.6 CI 现状事实（本轮首次查清，后续批次的基础）

- **CI 从来没有绿过**。仓库只有 2 次运行记录，均 2026-09-21，均为 `go test` 步骤失败、覆盖率门禁随之 skipped：`35596178916`（push/main，a7070c0）、`35596150083`（pull_request，080754c）。当时门禁还是 `>= 55%`。
- **`fork/main` 落后 `baseline-snapshot` 45 个提交**（main 头 = a7070c0，2026-09-21 的 "Merge pull request #1"），其工作流仍是旧版（`node-version: '22'`、无 `-Threshold`）。`origin`（zdongno01）侧 0 次运行。
- 工作流只在 `main` 的 push / PR 触发，日常分支 `baseline-snapshot` 的推送不触发——所以**本轮的 pin 升级无法靠推日常分支验证**，要么向 `main` 提 PR，要么临时把 `baseline-snapshot` 加进 `on.push.branches`。
- 本机 `gh` 未登录（`gh run list` 报需 `gh auth login`），运行历史是经公开 API 取到（两个仓库均 public）；action 破坏性变更取自各仓库 release notes + `setup-go` PR #460 原文。

### 26.7 本批次未做与待定（等用户裁决）

- **是否在本批次一并修 `-race`**：修则目标是拿到史上第一次绿 CI，代价是批次范围从"CI 配置"扩到"产品测试代码"；不修则本批次推送后 CI 仍会在 `go test -race` 判红（Setup 步骤会成功，足以证明 pin 升级本身有效）。
- **验证跑 CI 的路径**：开 PR `baseline-snapshot → fork/main`（不动 main，PR head 用新工作流）／临时把 `baseline-snapshot` 加进 push 触发（多两个提交，全程命令行）／先不推送只留本地验证。
- 复核中另记下两项非本批次欠账：`actions` 之外，CI 的 `go-version` 仍是 `1.27.x` 范围写法（与本机 1.27.1 存在补丁漂移，是否钉死待定）；`setup-go` 也支持 `go-version-file: FVCC/server/go.mod` 直接读下限。
- 与本批次无关的既有挂账不变：`FVCC/.commandcode/` 处置、NAS 真机视觉清单回收、渲染 Failed/Cancelled 分支、P3-6/P3-8、e2e B 档 #7/#9。

## 二十七、第三十六轮（2026-10-14）：本机 Go 工具链升级 1.27.1 → 1.27.2 与「auto 选取语义」查清

### 27.1 批次裁决（用户口径）

- 第三十五轮的 26.7 两个待定项由用户裁决：**"本批次只推 pin，race 另立批次"**、**"验证先不做"**（不推送、不开 PR 跑 CI）。
- 本轮范围：升级本机工具链里唯二落后项中的 Go（选 **官网 MSI 覆盖安装**），npm **暂不升**，FFmpeg **"不升 ffmpeg，这是另个体系"**。
- **纯工具链与文档口径批次：不 bump 应用版本、不出 fpk。**

### 27.2 升级动作与包完整性

- 路径：`C:\Program Files\Go`（`go env GOROOT`）由官网 MSI 覆盖安装，PATH Go 从 1.27.1 → **1.27.2**；`sdk\go1.25.3` 启动器保留未动（这点是关键，见 27.3）。
- `winget upgrade --id GoLang.Go` 报"找不到可用的升级"——winget 源当时仍只有 1.27.1，**补丁级升级不可依赖 winget**，走 go.dev/dl 才对。
- 安装包 `go1.27.2.windows-amd64.msi`：本地 66,154,496 B，sha256 `dc673c9207a83f95031549fafe72cc0b4c4554a1c16b373d875e90cf8d995ed9`，与 `go.dev/dl?mode=json&include=all` 公布值**逐项一致**（本轮复算确认）。

### 27.3 关键发现：交付产物并不随本机 PATH Go 的补丁升级而变（并纠正上一轮的误判）

`GOTOOLCHAIN=auto` 的选取语义是——**启动器版本 ≥ go.mod 下限 → 用启动器；启动器 < 下限 → 严格取 go.mod 声明的那个版本**（命中模块缓存或下载）。本仓库两个 go.mod 下限是 `go 1.27.1`，而 `build.ps1` 的 `$SDK_PATHS` 首选 `sdk\go1.25.3`（低于下限），所以：

| 构建路径 | 实际编译用的 Go | linux/amd64 产物尺寸 |
|---|---|---|
| `build.ps1`（1.25.3 启动器 + auto） | **go1.27.1**（模块缓存 `golang.org/toolchain@v0.0.1-go1.27.1.windows-amd64`） | 27,414,688 B |
| 直接在 PATH 上 `go build`（1.27.2 启动器 + auto） | go1.27.2 | 27,431,072 B（+16,384 B） |
| 已入库的 v1.6.8 交付 `FVCC/app/fvcc` | `go version -m` = **go1.27.1** | 27,414,688 B |

本轮实测：升级后用 1.25.3 启动器按 `build.ps1` 的同参（`-trimpath -ldflags "-s -w"`、`GOOS=linux GOARCH=amd64 CGO_ENABLED=0`）重建，产物 **27,414,688 B，与交付二进制同尺寸**、`go version -m` 同为 go1.27.1、嵌入的 build settings 逐项相同。

- **纠正**：本轮初判"Go 补丁升级会让二进制 +16 KB，从而『同尺寸即后端零改动』这条打包校验跨补丁级失效"——那是**直接在 PATH 上 `go build`（1.27.2）**得到的，不是交付路径。`build.ps1` 恒用 1.27.1，所以该口径在 fpk 层面**未被破坏**，v1.6.8 的"二进制逐字节同尺寸"结论仍然成立。
- 顺带登记：同参数两次构建的 **sha256 不同**（每次构建的 buildid 随机，`-s -w -trimpath` 也不消除），所以打包核验只能比**尺寸与版本串**，不能比哈希——历史上比的就是尺寸，口径一致。
- 反面含义（后续批次的决策依据）：想让交付产物真正吃到 1.27.2 的补丁修复，必须**同步抬两个 go.mod 的 `go` 指令 + CI 的 `go-version`**（setup-go v6+ 是 local 语义，见 26.3），单升本机 PATH Go 没有效果。本轮按"`go` 指令是下限不是 pin"的既有口径**未抬 go.mod**。

### 27.4 验证（本轮亲跑，非沿用）

- 双端构建：FVCC `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" .` exit 0（产物头 4 字节 `\177ELF`）；FVCS `CGO_ENABLED=1 + mingw64 gcc` 的 `go build ./pkg/... ./cmd/service` exit 0（该路径直用 PATH Go，`go env GOVERSION` = go1.27.2）。
- `cd FVCC/server`：`go build ./...` 与 `go vet ./...` 干净，`go mod tidy -diff` 空输出（依赖图无需变动）。
- `go test ./... -count=1`：**14 包全绿**（api 19.4s / edl 7.3s / media / node / pathutil / protocol / remote / scheduler 15.4s / security / store 27.1s / store/model / version / logger / smbshare）。
  - 升级后第一次全量跑时 `TestSchedulerTickConcurrentSafe` 判红（"仍有 2 个任务滞留 QUEUE 未收敛" + TempDir RemoveAll 非空告警）；单独复跑 scheduler ×3 全绿、复跑全量再次全绿 → 判定为**已登记的主机负载时序 flake**（不带 `-race` 时该用例的 10 s 收敛窗口本就在余量边缘），**不是 Go 1.27.2 的回归**。
- `scripts/check-versions.ps1` 绿：三方 1.6.8 一致 + go.mod `go 1.27.1`。
- 本轮改的两个 `.ps1` 只动注释，均已 `Parser::ParseFile` 语法核验 PARSE-OK。

### 27.5 npm 停在 11.20.0 的原因（用户请求升，实测升不动，如实登记）

`npm -v` 仍是 11.20.0（Node 26.11.1 MSI 内置）。上游 12.2.0，但本机**无法原地升**：`npm config get prefix` = `C:\Users\xx318\AppData\Roaming\npm`（可写、无需管理员），而 PATH 里 `C:\Program Files\nodejs`（内置 npm 的落点）**排在它之前**，`npm i -g npm@12` 装到前者也会被后者的 11.20.0 遮蔽，`npm -v` 照旧；`C:\Program Files\nodejs` 需管理员权限才能改写。两条可行路径留给用户决策：① 管理员调整 PATH 顺序（把 `%APPDATA%\npm` 提前）后 `npm i -g npm@12`；② 等下一次 Node MSI 升级自带新 npm。**不影响交付**：CI 用 `setup-node` 自带的 npm，本仓库只跑 `npm ci` / `npm run build`。本轮未擅自改系统 PATH（会波及其他项目）。

### 27.6 FFmpeg：裁决"另个体系"，同时登记实测真相

- 用户裁决：**不升 ffmpeg**，它不属于本仓库工具链口径。
- 复核时发现旧文档口径是错的：`where`/`which` 实测 PATH 上唯一命中是 **`D:\YTSage\ffmpeg.exe`**（9.0.1-essentials，gyan.dev 构建），**`D:\FFmpegTools\bin` 根本不存在**；仓库代码里也没有对该目录的引用。
- FVCS 侧 ffmpeg 是**配置项** `ffmpeg_path`（`FVCS/pkg/config/config.go:41`，默认 `.\ffmpeg.exe`），由安装者按部署实际填写；ffprobe 路径由它同目录推导（`FVCS/pkg/ffmpeg/edl.go:143`）。即 ffmpeg 不随 `build.ps1` 构建、不进 fpk——与"另一体系"的裁决一致。
- 已按此口径改写 `docs/FVCC_项目现状与开发步骤.md` §6.2 的 ffprobe/ffmpeg 行（旧文写"本机构建机已装 ffprobe 9.0.1"但没写解析路径）。**fnOS 侧 ffmpeg PATH 是否可达仍未解**，属真机核验项，由用户实测。

### 27.7 本批次改动清单与入库状态

- 文档/注释口径（本轮）：`BUILD.md`（FVCC 前置条件 Go 行、FVCS 环境要求 Go 行、注意事项「Go 工具链」新增"auto 的选取语义"条、基线表 Go 工具链行改写 + 新增 npm 行 + 新增 FFmpeg 行）、`build-env.ps1`（工具链选择注释）、`build.ps1`（auto 选取语义注释）、`docs/FVCC_项目现状与开发步骤.md` §6.2 两行、本节台账。**后记**：上述三处与本轮台账同随 `b0ff3d1` 入库。
- 第三十五轮的 CI pin 批次 **`4365e31`**：已提交（该批次不 bump 版本、不出包），随 v1.6.9 发布批次一并推送 `fork/baseline-snapshot`。
- 未入库项照旧：`FVCC/.commandcode/` 已随第三十七轮（v1.6.9）按用户裁决入 `.gitignore`（不入库、不删除）；`temp/` 下本轮的核验产物（MSI、两份对比二进制、go.dev 元数据 JSON）按 `.gitignore` 不入库。

### 27.8 未做与待定

- **`go test -race` 修复**仍是独立批次（26.5 的 `fakeDispatcher` 裸字段数据竞争 + `TestSchedulerTickConcurrentSafe` 收敛窗口判定）。
- **是否抬 go.mod 的 `go` 指令到 1.27.2**：本轮按"下限不是 pin"未抬；若希望交付产物吃到补丁，需与 CI `go-version` 同批处理（27.3）。
- **npm 12 的两条升级路径选哪条**：等用户裁决（27.5）。
- **CI 首跑路径**（PR 到 `fork/main` vs 临时加 push 分支）仍未定，`4365e31` 也因此停在本地。
- 与本批次无关的既有挂账不变：`FVCC/.commandcode/` 处置、NAS 真机视觉清单回收、渲染 Failed/Cancelled 分支、P3-6/P3-8、e2e B 档 #7/#9。**（后记：同日第三十七轮还清其中两项——`.commandcode/` 入 .gitignore、P3-6 正式勾选关闭；store 备份/恢复前端入口亦随 v1.6.9 交付，见 §二十八。）**

---

## 二十八、第三十七轮（2026-10-14）：低风险收尾批次——store 备份/恢复前端入口 + P3-6 勾选 + `.commandcode` 收编（v1.6.9）

### 28.1 批次构成

用户在「下一步改进方向」三档里选了**①低风险收尾**，一次做完三项：① P3-5 尾部——store 备份/恢复**前端入口**；② **P3-6 台账正式勾选关闭**；③ `FVCC/.commandcode/` 按裁决**入 .gitignore**。纯前端 + 治理，后端 Go **零代码改动**。

### 28.2 动手前的后端事实核对（不猜契约）

- 三端点在位：`server/internal/api/router.go:42-44` → `POST /api/store/backup`、`GET /api/store/backups`、`POST /api/store/restore`，**全部挂 `security.RequireAdmin()`**；处理函数 `internal/api/backup_handlers.go`。
- 响应形状：`store.BackupResult`（`backup.go:59-65`，5 字段）与 `store.RestoreResult`（`restore.go:52-60`，6 字段含 `preRestoreId` 与 `counts`）；Go `time.Time` 序列化为 RFC3339，前端按字符串收。
- **备份 kind 实际有 5 种**（`backup.go:35-41`：auto / manual / prewrite / premigration / prerestore），不是头注释说的"auto+prewrite"两种。轮转面也需按代码读：`rotateBackupPool`（`backup.go:277-309`）只跳过 `manual`，其余**全池按时间保留最近 12 份**——面板说明文字据此写，未沿用注释的窄口径。

### 28.3 前端实现（三文件）

| 文件 | 内容 |
|---|---|
| `ui-src/src/types.ts` | `StoreBackupKind`（5 kind 全列）+ `StoreBackup` + `StoreRestoreResult` |
| `ui-src/src/api.ts` | `createStoreBackup` / `listStoreBackups` / `restoreStoreBackup`（沿用统一 `request` 错误契约） |
| `ui-src/src/pages/settings.ts` | 「数据备份与恢复」面板（插在视频缓存之后、播放之前）：备份列表（本地化时间 / kind 中文标签 / 体积 / 恢复按钮）+ 立即备份 + 刷新 + 空态文案 + 超过 20 份时「显示其余 N 份」按需展开 |

恢复流程：`confirmDialog(danger)` → `POST /store/restore` → toast 带回滚兜底备份 ID → `store.loadAll()` + `load()` **整表重载**。重载是必须的而非礼貌性刷新：备份内容包含 `settings` 本身，恢复后表单若仍持旧内存态就是脏数据。破坏性把关依赖后端已有的三重（RequireAdmin + 回滚前自动 prerestore + `snapshot.restore` 审计），前端只做一次二次确认，不自造"输入备份 ID"这类摩擦。

### 28.4 交付前实测：真实后端 + 浏览器走全链路（不是只看构建绿）

用 `FVCC_DEV=1` 起本机 `fvcc-dev.exe`（临时 `FVCC_DATADIR=temp/devdata`）+ vite dev（5180，代理到 8088），逐项走查：

1. 空态「暂无备份（数据写入时会自动产生）」；
2. 立即备份 → 后端落 `backup-20261014062441-manual.zip`（3,072 B），列表刷新出该行；
3. 行渲染正确（`2026/10/14 14:24:41｜手动｜3.0 KB｜恢复`）；
4. 点恢复 → 弹出危险确认（文案含"当前全部业务数据会被覆盖；恢复前系统自动生成一份兜底备份"）；
5. 确认 → toast「恢复完成，兜底备份：20261014062503-prerestore」，列表变 2 行且新 kind 出现在数据里；
6. 灌到 **23 份**验证分页器：首屏 20 行 + 「显示其余 3 份」，点开后 23 行、按钮隐藏。

**实测抓到一个漏项**：`prerestore` 最初不在标签映射里，列表直接把英文原文显示给用户。补齐 5 个 kind 的中文标签（定时/手动/写前/迁移前/回滚兜底），并把标签列宽 `w-12`→`w-16`（4 字标签会挤）。验证产物（`FVCC/temp/dev/`、`FVCC/temp/devdata/`）本轮结束即删，**未触碰 NAS**。

### 28.5 治理两项

- `FVCC/.commandcode/`（AI 工具遗留，内含 `taste/**/taste.md`）：按用户裁决**忽略不入库、不删除**，`.gitignore` 追加 `FVCC/.commandcode/`；`git check-ignore -v` 核验命中，工作树除本轮改动外归零。
- **P3-6 正式勾选关闭**：真机证据 2026-10-11 NAS `/info` 返回 `ffprobe:true`（fnOS mediasrv 提供，FVCC 无 stub 降级路径被触发，启动自检 `main.go` 打印 `ffprobe=%v`）。遗留的"probe-vs-ffprobe 差异核对"是可选项，不再占 P3 编号。

### 28.6 新登记缺口：手动备份没有删除入口

`rotateBackupPool` 明确跳过 `manual`（永久保留），而后端只有 backup/backups/restore 三端点，**不存在 `DELETE /api/store/backups/:id`**。结论：面板上的手动备份**只增不减**，目前回收磁盘只能进 shell 手删。本轮按"不扩范围"原则**未擅自加删除端点**（那是新的破坏性 API + 审计 + 单测一整套），登记为待裁决项。

### 28.7 版本 bump 与入库状态

- bump **1.6.8 → 1.6.9**：`FVCC/manifest`、`FVCC/ui-src/package.json`、`FVCC/server/internal/version/VERSION` 三处 + `ui-src/package-lock.json` 根 `version`（`npm install --package-lock-only` 同步，1.6.8 那轮它漂留在 1.6.7）。
- 校验：`scripts/check-versions.ps1` 退出码 0（三方 1.6.9 一致 + go.mod `go 1.27.1`）；`go test ./internal/version` 本机真跑绿；前端 `check:design --all` 33 文件无违规 → `tsc -b` → `vite build` 48 模块，CSS 49.99 kB（gzip 9.79 kB）/ JS 208.43 kB（gzip 66.13 kB）。
- **已出 fpk**：`./build.ps1 -Target FVCC` → `FVCC/fvcc.fpk` **11,014,297 B**（+797 B vs v1.6.8 的 11,013,500 B，纯前端增量）。解包核验：包内 `manifest` `version = 1.6.9`、`app.tgz` 内 `ui/assets/index-ByMsNQMP.css` 与 `index-SH0C878Z.js` 与本轮构建产物逐名一致、`app/fvcc` ELF 内嵌 VERSION = 1.6.9。提交/推送/tag 见 §28.9。
- 本轮改动 8 个代码/配置文件（`.gitignore`、manifest、package.json、package-lock.json、VERSION、types.ts、api.ts、settings.ts）+ 4 份根台账回写（本文件、`FVCC_混乱度评价报告_细化版.md`、`FVCC_项目现状与开发步骤.md`、`项目分析与改进方向.md`）+ `README.md` / `BUILD.md` / `FVCC/README.md` 版本表基线。

### 28.8 挂账变化：本轮还清 2 项

- **还清**：store 备份/恢复前端入口（v1.6.9）、`FVCC/.commandcode/` 处置（入 .gitignore）。
- **新增**：手动备份删除入口（§28.6）。
- **不变**：NAS 真机视觉清单回收、Tailwind 4 真机视觉对照（用户实测中）、渲染 Failed/Cancelled 分支、P3-8、e2e B 档 #7/#9、概览 Filter Chips、`?fx=` 动画裁决、其余页面 `listSkeleton` 迁移、`go test -race` 修复批次、go.mod `go` 指令是否抬 1.27.2、npm 12 升级路径、CI 首跑路径。
- **本轮文档覆盖面（如实登记）**：回写 4 份根台账（本文件 / 混乱报告第十四版 / 项目现状与开发步骤 / 项目分析与改进方向第十一次回写）+ `README.md` / `BUILD.md` / `FVCC/README.md` 版本表 + `FVCC/docs/API_CONTRACT.md`（补 `/api/store/*` 响应字段与 kind 五值口径）。**未回写**：`FVCC/docs/DESIGN.md`、`FVCC/docs/SECURITY.md`（内外两份）、`docs/TASK_REFERENCE.md`、`docs/FVCC_UI设计优化方向.md` 的"复核基线 v1.6.8"行——它们是**带日期的复核声明**而非当前版本声明，按惯例留给专门的文档对齐轮统一回写（与第三十一~三十三轮 `246e905` 的做法一致）。

### 28.9 入库与发布后记（2026-10-14）

用户裁决"1 2 3 逐个执行"，本批三步收口：本地提交 → 出包 → 推送 + 打 tag。

- **`dedd124` `feat(store): store 备份/恢复前端入口接上 /api/store/*，版本 bump v1.6.9`**——16 文件 `+281 / −62`（8 个代码/配置 + 4 份根台账 + README/BUILD/FVCC README + API_CONTRACT）。
- 本笔 `docs(ledger)` 后记提交（提交状态随入库更新），**注解 tag `v1.6.9` 打在此笔**并推送，与 v1.6.8（`65c79e4`）的发布口径一致。
- 本次推送共携带 **5 笔**提交到 `fork/baseline-snapshot`：`4365e31`（CI pin v7）、`b0ff3d1`（第三十六轮工具链收口）、`a5768d3`（九份长台账压缩）、`dedd124`（本轮主体）、本后记笔。

**出包环境波折（登记为工具链事实）**：Windows PowerShell 5.1 编译 `build.ps1` 时在 AMSI 扫描环节崩溃——`System.AccessViolationException` at `System.Management.Automation.AmsiUtils.AmsiScanBuffer`，连续两次复现（相对路径与绝对路径入参各一次），崩在脚本编译阶段，一行构建逻辑都没执行。用最小 `.ps1` 探针验证 5.1 本机仍可正常编译运行，故判定为**本机反病毒/AMSI 提供器对这个 13 KB 脚本内容的崩溃**，属环境故障、与仓库和脚本无关。改由 PowerShell 7（`pwsh` 7.7.0-preview.5）跑同一条 `build.ps1 -Target FVCC` 一次通过（17.1s）。构建三步（`npm run build` 里的 vite / `go build` Linux 交叉编译 / `fnpack build`）都是外部进程，产物与 PS 宿主版本无关——本次产出 `index-ByMsNQMP.css` 49.99 kB（gzip 9.79 kB）、`index-SH0C878Z.js` 208.43 kB（gzip 66.13 kB）与 §28.7 提交前 `npm run build` 的哈希逐项一致。**`build.ps1` 本轮未作任何改动**；5.1 侧 AMSI 崩溃留待重启/病毒库更新后由用户复现核查，若持续即改走 `pwsh` 口径（届时同步 BUILD.md）。

**发布产物核验**：`FVCC/fvcc.fpk` = **11,014,297 B**（v1.6.8 为 11,013,500 B，+797 B）；解包只读校验包内 `manifest` `version = 1.6.9`、`app.tgz` 内两个 asset 名与构建一致、`app/fvcc` ELF 内嵌 VERSION = 1.6.9；临时解包目录已删，**未触碰 NAS**。

**未入库项如实登记**：构建产物 `FVCC/app/`、`FVCC/temp/`、`FVCC/fvcc.fpk` 按 `.gitignore` 不入库；`FVCC/.commandcode/` 本轮已按裁决 ignore（不删除，本地仍在）。

**发布后仍待回收**：v1.6.9 fpk 的 NAS 真机安装（重点看设置页「数据备份与恢复」面板在真实数据量下的备份/恢复，以及第三十四轮起未回收的 Tailwind 4 视觉对照清单）、手动备份删除入口裁决（§28.6）、渲染 Failed/Cancelled 分支验证。CI 侧限制不变：推送 `baseline-snapshot` 不触发工作流。
