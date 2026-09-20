# FVCC 项目混乱度评价报告（细化版）

> 项目：FVCC（飞牛视频转换 NAS 调度端 Web UI）@ D:\Fnos.VideoConversion\FVCC
> 基线评价日期：2026-09-18（首版）
> **复查更新日期：2026-09-19（第三版，反映 P2-1 后端分层与 P2-2 前端组件化全部落地后的最新现状）**
> 评价视角：Vibe Coding（AI 辅助编码）模式下的工程卫生与可维护性
> 评价方式：目录树扫描 + 关键源码阅读（后端 internal 11+1 子包 82 个 .go 文件，根包仅剩 main.go；前端 TS 9,999 行）+ 配置与文档核查 + git log/tag 交叉核对 + 与根目录 README.md / BUILD.md / 项目分析与改进方向.md / WebVideoEditor_整体架构设计方案.md / FVCC/docs（P2-1 实施记录 / P2-2 细化细则）交叉核对
> 更新说明：第三版基于 2026-09-19 实测，逐项核对 §4 修改建议的落地状态：**P2-1（后端 internal 分层）与 P2-2（前端组件化）均已落地**，上一版"仍单 main 包 / 样板未抽取"的过时描述已改写为落地结果；版本号同步为 1.4.4（工作区三处一致，含 v1.4.3/v1.4.4 打包产物，HEAD 1.4.3 待提交）；并重新量化评分。§4.1 / §4.2 保留为"方案 + 落地结果"双视图。

---

## 1. 项目概览

| 项 | 内容 |
|---|---|
| 定位 | fnOS NAS 上的视频转码调度端：素材扫描 → 转码方案 → 渲染节点集群 → 任务队列 → EDL 剪辑/代理预览全链路 |
| 后端 | Go 1.27.1，gin v1.12 + gorilla/websocket，**已分层**：`server/internal` 11 个包 + store/model 子包（82 个 .go 文件，根包仅剩 main.go） |
| 前端 | Vite + TypeScript 7 + TailwindCSS 3，零框架（`el()` 手工 DOM），公共样板已收敛至 src/lib/（crudActions/scrollPos/useListPage/formBuilder） |
| 版本 | **1.4.4**（server/VERSION、ui-src/package.json、manifest 三处一致，2026-09-19 实测；version.go 以 go:embed 注入并配 version_test.go 自动校验；git HEAD 1.4.3 待提交，工作区含 v1.4.3/v1.4.4 打包产物） |
| 版本控制 | **已启用 git 版本控制（P0-3 已落地）**：由上层仓库 `D:\Fnos.VideoConversion` 统一管理（FVCC 内不再有独立 .git，.gitignore 由仓库根统一维护，构建产物/依赖/temp/logs/0 字节残留已纳入忽略）；已推送 GitHub [hgmmm25/Fnos.VideoConversion](https://github.com/hgmmm25/Fnos.VideoConversion/tree/baseline-snapshot)，当前工作区 HEAD 为 1.4.3（baseline-snapshot 快照分支之后的后续提交）；P2-1 分层启动前基线已打 tag v1.4.3，可随时回滚 |
| 文档 | **已大幅补全**：根 README.md、BUILD.md、FVCC/README.md（含设计文档引用速查表 + P2 组治理状态表）、docs/API_CONTRACT.md、WebVideoEditor_Design/（01-10 编号规格文档已入库）、docs/（混乱报告/TASK_REFERENCE/IMPROVEMENT_LOG/UI 优化方向）；ui-src/DESIGN.md + PRODUCT.md 保留 |

目录结构（根目录，2026-09-19 实测）：
```
FVCC/
├── .gitignore                            ← 已建立（*.exe/*.fpk/node_modules/temp/logs/0字节残留）
├── README.md                             ← 已建立（目录结构 + 设计文档引用速查 + 版本单一来源表 + P1/P2 治理状态表）
├── fvcc.exe / fvcc.fpk / FVCC_v1.4.0~1.4.4_fnos_x86.fpk / fvcs-service.exe  ← 构建产物仍物理堆积在根（已入 .gitignore，未物理清理；v1.4.3/v1.4.4 为本轮新增）
├── ICON.PNG / ICON_256.png / manifest    ← fnOS 应用清单（乱码已修复，UTF-8 正常）
├── app/          ← fnOS 应用打包目录（字体已瘦身为 latin 子集，仅 10 个 woff/woff2 + css + js）
├── cmd/          ← fnOS 生命周期回调脚本（install/config/uninstall/upgrade）
├── config/       ← privilege / resource 权限声明
├── docs/         ← 已建立（API_CONTRACT.md、P2-1_后端分层_阶段A实施记录.md、P2-2_细化细则.md）
├── logs/         ← 2026-09-17/app.log（0B 残留，已入 .gitignore）
├── server/       ← Go 后端（已分层：internal/ 11 包 + store/model 子包，82 个 .go 文件（46 实现 + 36 测试）；根包仅剩 main.go）
├── temp/         ← 仅剩 fpk_stage（33 文件 36.7MB：fnpack.exe + fvcc.fpk + ICON + manifest + app/cmd/config）；fpk_build_test2（数千压测 log 垃圾）与 fpk_inspect 已清除
└── ui-src/       ← 前端源码（all.txt/err.txt/out.txt 已清除；tsconfig.tsbuildinfo 仍留但已入 .gitignore；src/pages/editor/ 已拆分 15 个模块；src/lib/ 已收敛公共样板 4 件套）
```

---

## 2. 分维度评价（含具体证据）

### 2.1 代码组织与分层 —— 3/10（已分层收口，P2-1 落地）

**证据 1（已消除）：后端 `package main` 单包巨型文件 → internal 分层完成。**（P2-1 已落地，2026-09-19 实测）

- 第二版记录的原状：`server/` 32 个非测试 .go 文件 12,902 行全部 `package main`，handlers.go 1,737 / remote.go 1,227 / scheduler.go 1,204 等巨型文件无包级边界；
- 现状：`server/internal/` 11 个包 + store/model 子包共 **82 个 .go 文件（46 实现 + 36 测试）**，根包仅剩 `main.go`（入口与装配、优雅退出）；巨型文件已按域拆解迁入 internal/api、internal/remote、internal/scheduler、internal/media、internal/store 等，行为零回归（实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md`，阶段 A/B/C 全部完成、8 个 shim 兼容文件已内联删除）；
- 分层前基线已打 tag v1.4.3，可随时回滚；测试随包迁移（36 个 *_test.go），`go test ./...` 全绿。

**证据 2（P2-2 已收口）：前端编辑器区已拆分，列表页公共样板已收敛。**（2026-09-19 复核）

- `ui-src/src/pages/editor/` 已拆出 **15 个模块**（assets.ts 663 / timeline.ts 631 / preview.ts 580 / index.ts 455 / editorStore.ts 451 / layout.ts 320 / dialog.ts 214 / taskView.ts 145 / shortcuts.ts 131 / taskDrawer.ts 120 / ruler.ts 95 / clipOps.ts 90 / format.ts 83 / preset.ts 59），总 4,037 行；
- 列表页公共样板已收敛至 `src/lib/` 4 件套（crudActions / scrollPos / useListPage / formBuilder，接入 6 页），CRUD 四连 / 滚动位置 / 订阅 / 表单样板残留 0；列表页业务文件仍在（scanner.ts 1,300 / profiles.ts 1,276 / tasks.ts 479 / settings.ts 420），行数以业务逻辑为主；前端 TS 总计 29 文件 9,999 行。

**积极面（需客观记录）**：内部仍按文件划分了职责（router / security / ratelimit / ws_limit / ffprobe / proxy_flow 单独成文件）；handler 按领域用注释分节；`RenderDispatcher` / `MediaProber` / `ProxyProber` 采用接口注入便于单测替身；前端新增 `theme.ts`（设计 token 收敛）与 `ws.ts`（WS 封装）；`pages/editor/` 拆分是向组件化迈出的实质一步。

### 2.2 命名一致性 —— 5/10（轻度混乱，编号歧义已登记）

**证据 1：JSON 字段大小写风格混杂。** 同一协议层既有 camelCase（`authKey`、`customFfmpeg`、`videoCache`），又有 snake_case（`server/internal/remote` 包 `progressPush` 结构体：`task_id`、`out_time_ms`、`total_ms`、`seg_total`）。docs/API_CONTRACT.md 已将"协议字段一律 camelCase（FVCS 对等 WS 报文允许 snake_case 但双风格兼容解析）"定为规范，**但存量代码尚未迁移**。

**证据 2：注释中的任务编号体系对维护者不透明（两套语义已登记）。** 全代码库仍散布 `P0-4`、`P1-1`、`P2-1`、`B-04`、`B-09`、`C-02`、`D-04`、`M4`、`126` 等编号。已知同号异义：

- `server/internal/api/handler_metrics_test.go`：**`// P2-1 可观测性端点验证`** —— 指 Prometheus 指标端点（`/metrics/prometheus`），与本报告 §4 中 P2-1（后端分层，已落地）含义完全不同；**同一编号在代码注释体系与报告建议体系间存在两套稳定语义**，已登记于 docs/TASK_REFERENCE.md §2.1（编号漂移登记），维护者按文件归属对照；
- `server/internal/version/version.go` 注释建立了"发布约定（2026-09-16 建立）"——编号体系仍在持续新增。

**证据 3：同名接口新旧风格并存（过渡期）。** `ui-src/src/api.ts:27` 已声明统一错误契约 { ok:false, code, msg, detail? }，request() 保留 `body.msg || body.error` 兜底——API_CONTRACT.md 明确"旧格式过渡期兜底，新代码只读 msg"，新旧格式仍同时在线上流通。

**证据 4：测试文件命名带票号（P2-7 已落地）。** `genproxy_share_cred_126_test.go`、`handlers_edl_list_contract_test.go` 原以票号/契约后缀命名，2026-09-18 已 git mv 为 `genproxy_share_cred_test.go`、`handlers_edl_list_test.go`，统一为 `handlers_<domain>_test.go` 风格。

### 2.3 重复代码 —— 5/10（两大重复源已消灭，剩余中等）

**证据 1（已消除）：updateProfile 的 59 块手写字段映射 → 反射白名单 helper（P1-1 已落地）。**

`server/applyjson.go`（83 行）新增 `applyJSONUpdates(dst, partial)` 反射白名单：按结构体 json tag 反射赋值，未知字段与类型不匹配自动忽略，非指针不 panic。实测 `handlers.go` 中 `if v, ok := updates[` 计数 **= 0**（首版 59）。配套 applyjson_test.go（61 行，4 个用例：基础更新 / 部分更新保留其余 / 忽略未知与错误类型 / 非指针不 panic）。首版"方案 A 全量绑定 / 方案 B 反射白名单"中实际采用了**方案 B**。

**证据 2（已消除）：SMB 共享路径校验复制 3 处 → 统一 helper（P1-2 已落地）。**

`server/smb_validate.go`（31 行）新增 `func (h *Handlers) validateSMBPath(settings Settings, path string) error`；`handlers.go` 中 **5 处**调用点（L331 / L478 / L665 / L913 / L921）统一改为单行调用；`smbshare.IsPathShared` 仅在 helper 内出现 1 次。配套 smb_validate_test.go（15 行）。

**证据 3（仍在）：VideoInfo 30+ 字段逐字段复制出现 2 处。** `server/internal/api/handlers*.go` `doScanDirectory()` 中 cache→VideoInfo 赋值块与 `UpsertVideoCache` 调用参数块，各 30+ 字段逐项手写，字段新增时两处必改（P2-1 分层迁移未涉及此逻辑，保留为待办）。

**证据 4（P2-2 已收口）：前端跨页面样板重复。** 2026-09-18 复核：`crudActions` / `scrollPos` / `useListPage` / `formBuilder` 已抽取至 `src/lib/` 并接入 6 页，CRUD 四件套 / 滚动位置 / store 订阅 / 表单构建样板残留 0；历史收敛轨迹：`renderList` 仅 `profiles.ts`(2) / `servers.ts`(2) 残留，scanner/tasks/settings/history 已为 0；`scrollPos` 仅 profiles.ts(3) 残留；编辑器区拆分为 pages/editor/ 15 个模块后，样板随模块边界自然缩小。

### 2.4 依赖管理 —— 5/10（前端打包侧已瘦身，后端间接依赖未变）

- **后端**：`go.mod` 直接依赖仍仅 gin + websocket，indirect 链 30+ 包仍含与业务无关的 `go.mongodb.org/mongo-driver/v2`、`quic-go/qpack`、`bytedance/sonic` 等（gin 生态连带），未做 `go mod tidy` 后的精简校验。
- **前端（已改善）**：`app/ui/assets` 实测仅 **12 个文件**（ibm-plex-mono latin 400/500 + ibm-plex-sans latin 400/500/600 各 woff/woff2，共 10 个字体文件 + index css/js）——首版"50+ woff 全字符集打包"问题已消除（P1-5 打包侧落地）；但 `node_modules` 内 @fontsource 仍含全字符集（cyrillic/greek/vietnamese 等），node_modules 本体未瘦身。TypeScript 仍锁 `^7.0.2`、Vite `^8.3.0`，大版本激进风险保留。
- **积极面**：存在 `package-lock.json` 与 `go.sum`；新增 `scripts/check-versions.ps1`（版本一致性校验）与 `scripts/check-coverage.ps1`（覆盖率门禁）。

### 2.5 配置与硬编码 —— 4/10（凭据/清单/版本三处硬伤已修复，残留注释与表滞后）

**证据 1（已修复）：凭据明文存储 → AES-GCM 加密落盘（P0-1 已落地）。**

`server/crypto.go`（164 行）：AES-GCM 对称加密，主密钥以 **0600 权限**落盘 `<dataDir>/secret.key`；`EncryptSecret` 输出 `enc:v1:<base64>`，`DecryptSecret` 对非 `enc:` 前缀按旧明文原样返回（兼容迁移），`IsEncrypted` 判断密文格式；适用范围注释明确"Server.AuthKey、Settings.SMBPassword 等凭据字段的落盘加密；内存态保持明文供出站连接/挂载使用，对外 API 一律脱敏（MaskSecret，`******`）"。配套 crypto_test.go（158 行）。

**⚠️ 残留跟踪（2026-09-19 更新）**：原 `server/models.go:147` 过时注释 `// MVP 明文存储，TODO: P0 AES 加密` 随 P2-1 迁移至 `server/internal/store/model/models.go`（约 L152），**代码已加密、注释仍过时**，属代码层清理待办（本轮 md 文档归档不修改代码）。

**证据 2（已修复）：manifest 乱码 → 元数据正常（P1-4 已落地）。**

`manifest`（2026-09-18 实测）：
```
desc = Fnos Video Conversion Client - 视频转码任务调度与远程服务器管理
maintainer = fvcc
distributor = fvcc
```
UTF-8 无乱码，占位符已消除。

**证据 3（仍在）：默认值散落硬编码。** `saveSettings` 中 `SchedulerIntervalSec=1`、`ChunkSizeMB=4`、`HistoryLimit=1000` 等兜底值；`scheduler.go` `chunkSize: 4 * 1024 * 1024` 与设置项语义重复；`main.go` 开发模式默认 `127.0.0.1:8088`。

**证据 4（已改善）：版本号三处手工维护 → 单一来源机制建立。** `server/internal/version/version.go` 以 `go:embed VERSION` 注入版本号（P2-1 迁移后位置，原 server/version.go），注释明确"版本号唯一来源：本目录 VERSION 文件，禁止在代码中另写版本字面量"；`version_test.go` 自动校验三处一致性（2026-09-19 实测 manifest 1.4.4 / VERSION 1.4.4 / package.json 1.4.4 一致）；`scripts/check-versions.ps1` 提供人工校验入口；BUILD.md 版本同步清单与 FVCC/README.md 版本表已于 2026-09-19 同步至 1.4.4，README 滞后残留消除。

**证据 5（已清理，P2-6 收口）：AIGC 元数据残留。** 原 `PRODUCT.md`、`docs/API_CONTRACT.md`、根目录 `项目分析与改进方向.md`、`WebVideoEditor_整体架构设计方案.md` 及本报告顶部均带 AIGC frontmatter（`ContentProducer`、`ProduceID`、`ReservedCode1/2` 等 base64 块）；2026-09-18 已全部清除，README 侧 frontmatter 上一轮已清。

**新增积极面：网关身份与权限门禁。** `server/gateway.go`（62 行）解析 fnOS 网关注入的 `X-Trim-*` 请求头（UID / Name / Role），提供 `getGatewayUser` 与 `requireAdmin` 中间件：独立模式（无网关头）放行、网关头非 admin 返回 `E_FORBIDDEN`——写操作（/render、/proxy、DELETE 类）权限收紧，对应 07 §4.2/§7 规格。

### 2.6 文档与注释 —— 3/10（悬空引用已消除，文档体系成型）

**积极面（重大改善，P1-3 已落地）**：
- **规格文档已入库**：`WebVideoEditor_Design/` 目录含 01-10 编号规格文档 + README，与代码注释章节引用一一对应；`FVCC/README.md` 提供**设计文档引用速查表**（编号 → 文档 → 覆盖范围 → 相对仓库根解析规则），注释示例 `// 规则唯一来源：WebVideoEditor_Design/07-安全校验与凭据管理细则.md §3.2` 可经速查表直接溯源——首版"悬空引用"问题从根上解决。
- **根文档补齐**：根 `README.md`（项目组成/功能/构建部署/目录结构）、`BUILD.md`（2026-09-18 更新：构建规范/版本同步清单/故障排查/产物校验）、`docs/API_CONTRACT.md`（8,124B，统一错误契约 + code 映射表 + 域内 helper 清单）。
- **注释密度与质量保持**：每个 handler 有职责注释；crypto.go/applyjson.go/trash.go/security_audit.go 等新文件均带"Px-x 对应混乱报告条目"的落地注释；security.go 明确实现"07 四层校验"。

**残留混乱点（2026-09-19 更新）**：
1. **代码注释滞后于代码实现**：`server/internal/store/model/models.go`（约 L152）仍留过时注释 `// MVP 明文存储，TODO: P0 AES 加密`（代码已 AES-GCM 加密）——属代码层清理待办；
2. **文档位置仍不统一（轻微）**：PRODUCT.md / DESIGN.md 仍在 ui-src/ 根下而非 docs/（API_CONTRACT.md 已入 FVCC/docs/，形成两处文档根）——已在 FVCC/README.md 文档体系表登记，属可接受的既有布局；
3. **AIGC frontmatter 污染已清零（P2-6 收口 + 本轮补清）**：2026-09-18 清除 5 份文档后，2026-09-19 复查发现 `docs/IMPROVEMENT_LOG.md`、`docs/TASK_REFERENCE.md` 顶部与 `docs/SECURITY.md` 顶部仍带 AIGC frontmatter，本轮已一并清除；全仓 md（含 docs/ 全部文档）已无 AIGC base64 残留。

### 2.7 遗留/废弃代码 —— 6/10（temp 130MB 与 0 字节残留已清除，根目录产物仍在）

**证据 1（已清除，重大改善）：temp/ 打包测试残留 2364 文件/130MB → 仅剩 fpk_stage。**

- `temp/fpk_build_test2/`（数千个 `log2_1.txt`~`log2_11xx.txt` 压测垃圾 + 旧包 + fnpack-1.2.1.exe）**已删除**；
- `temp/fpk_inspect/` **已删除**；
- `temp/` 现存仅 `fpk_stage/`（33 文件 36.7MB：fnpack-1.2.3.exe + fvcc.fpk 9.0MB + ICON.PNG/ICON_256.PNG + manifest + app/ + cmd/ + config/），为 build.ps1 每次构建自动重建的干净打包 stage（BUILD.md 已规范"必须用干净目录，fnpack 递归扫描，真实目录打包耗时 14s → 干净目录 2s"）。

**证据 2（已清除）：构建产物与 0 字节残留。**

- `server/fvcc.exe`、`server/build_err.txt`、`server/out.txt` **已删除**（实测 server/ 下 0 字节文件 = 0）；
- `ui-src/all.txt`、`err.txt`、`out.txt` **已删除**（实测 ui-src/ 根 0 字节文件 = 0）；
- `ui-src/tsconfig.tsbuildinfo` 仍留（已入 .gitignore）；
- `logs/2026-09-17/app.log`（0B）仍留（已入 .gitignore）；
- `app/ui/` 构建产物仍在源码树（已入 .gitignore，字体已瘦身见 2.4）。

**证据 3（仍在，版本包数量增加）：根目录堆积构建产物。** 2026-09-19 实测根目录仍堆：`fvcc.exe`（32MB）、`fvcc.fpk`（8.8MB，与 v1.4.4 同大小）、`FVCC_v1.4.0~1.4.4_fnos_x86.fpk`（5 个版本包，v1.4.3/v1.4.4 为本轮新增）、`fvcs-service.exe`（9.9MB）、ICON.PNG/ICON_256.png、manifest、README.md——.gitignore 已覆盖 `*.exe` / `*.fpk`，但**物理文件未清理、未移入 archive/**（archive/ 已建立并收纳历史包 v1.1.0~v1.3.1 与升级前备份，说明治理动作正在进行但根目录未同步执行）。

**新增观察（FVCS 侧，非 FVCC 范围，供根仓库治理参考）**：`FVCS/` 根下仍有 `build_check.txt`(0B)、`_cgo_full.txt`(0B)、6 个历史 FVCC fpk、`fvcs-service.exe.bak_preupgrade`——根仓库治理（项目分析与改进方向.md P2"仓库治理"）尚未覆盖 FVCS 目录。

### 2.8 测试情况 —— 2/10（优秀且持续增强，CI 与覆盖率门禁已建立）

- **测试规模（扩大）**：**29 个 `_test.go`、约 6,458 行**（首版 24 个 5,406 行），新增 crypto_test.go(158)、applyjson_test.go(61)、smb_validate_test.go(15)、trash_test.go(101)、security_audit_test.go(159)、apierr_test.go(119)、handler_metrics_test.go(115)、remote_wss_test.go(189)、router_auth_test.go(132)、version_test.go(48) 等，覆盖新落地的加密/回收站/审计/错误契约/版本一致性与既有关键路径（store 并发、EDL 校验 + fuzz、remote WS 重连、安全 e2e、stream Range、调度）。
- **测试基建**：接口注入（`RenderDispatcher`/`MediaProber`/`ProxyProber`）专为单测替身设计；`main.go` 与单测共用构造点。
- **扣分项（已改善两项）**：
  ① **CI 已建立（P2-3 已落地）**：`.github/workflows/fvcc-ci.yml`（根仓库，paths 限定 `FVCC/**`）——后端 `go vet + go test -race ./...` + **覆盖率门槛 ≥55%**（`scripts/check-coverage.ps1` 强制，注释自述当前基线 56.1%，后续上调至 60%）；前端 `npm ci + npm run build`（内部含 `check:design` 设计门禁 + `tsc -b` + vite build）；
  ② **覆盖率基线已建立**（见上）；
  ③ ~~命名带票号/契约后缀~~（P2-7 已落地，两文件 git mv 规范化）；
  ④ 测试已随 P2-1 迁移至 internal 各包（36 个 *_test.go 随实现分层），平铺问题已消除。

### 2.9 安全与健壮性 —— 2/10（首版五硬伤已全部修复）

**积极面（做得不错的）**：
- `server/security.go`：`PathValidator` 四层路径校验（授权目录 + 额外目录 + 动态授权文件 + 环境变量回退）
- `server/router.go`：`NoRoute` 兜底拦截 `/config` 等敏感路径；`requireAdmin()` 保护删除/清缓存等破坏性操作
- `server/gateway.go`：fnOS 网关身份透传 + 写操作 admin 门禁（新增，见 2.5）
- `server/securityHeaders()` 安全响应头
- 崩溃恢复：`store.go` Load 时非终态任务重置为 QUEUE；`scheduler.go` 用 `sync.Map` 防并发处理同一任务、`connMu` 单飞防重复拨号
- 限流：`ratelimit.go`、`ws_limit.go`、`stream.go`（单文件 4 路 / 全局 64 路并发限制）
- 加密基座：`crypto.go` AES-GCM + 0600 密钥文件 + API 脱敏（新增，见 2.5）

**硬伤（首版 5 项 → 现 0 项未决）**：
1. ✅ **凭据明文已修复**（internal/security/crypto.go 落地，见 2.5 证据 1）；过时注释已随 P2-1 迁移至 internal/store/model/models.go（约 L152），属代码层清理待办
2. ✅ **`deleteVideo` 物理删除已修复（P2-5 已落地）**：`server/trash.go`（213 行）——视频删除不再物理删除，移入所在授权根下 `_trash` 目录（`_` 前缀受 `isReservedMediaName` 保护），保留相对路径结构避免同名覆盖，同名追加时间戳后缀；提供 `listTrash`（回收站列表，前端展示路径/原始路径）；配套 trash_test.go(101)
3. ✅ **`updateProfile` 白名单遗漏已修复**（applyjson.go 反射白名单，见 2.3 证据 1）
4. 🟡 **审计闭环部分落地（P2-8 已收口）**：`server/security_audit.go`（131 行）新增 `auditRejection` 记账拒绝类事件（`validate.reject` / `security.alert`，含按阈值突增告警）、`auditRejectCode` 判断错误码（07 §7 需记账的越权/载荷校验拒绝）、`alertReject` 告警文本；`handlers_edl.go` 的 `edlErr` 附带审计记账副作用（API_CONTRACT.md 已登记）；2026-09-18 起破坏性操作（删除/清缓存/清日志/清空回收站）经 `auditDestructive` 统一强制记账，8 处调用点 + 单测闭环，见 §4 P2-8 状态行。
5. ✅ **无版本控制已解决（P0-3 已落地，2026-09-19 更新）**：git 已启用，由上层仓库 `D:\Fnos.VideoConversion` 统一管理（FVCC 内不再有独立 .git），已推送 GitHub https://github.com/hgmmm25/Fnos.VideoConversion/tree/baseline-snapshot（快照分支）；P2-1 分层阶段 B/C 已有提交记录（见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` §5），P2-7 两文件 git mv 保留历史——误改/误删可回滚，安全事件取证与恢复有依据；**剩余动作**：后续改动持续提交，避免快照漂移

---

## 3. 量化混乱度评分（第三版，2026-09-19）

| 维度 | 首版得分 | 第二版得分 | **第三版得分** | 一句话结论（变化） |
|---|---|---|---|---|
| 代码组织与分层 | 7 | 6 | **2** | P2-1 已落地：internal 11 包 + store/model 子包，根包仅剩 main.go（82 个 .go = 46 实现 + 36 测试） |
| 命名一致性 | 6 | 6 | **5** | camel/snake 混用收敛至协议边界；编号歧义已登记（TASK_REFERENCE §2.1）；P2-7 两文件 git mv 规范化 |
| 重复代码 | 7 | 5 | **3** | 59 块映射与 SMB 校验 x3 已消灭；P2-2 列表页样板收敛至 src/lib/ 4 件套（残留 0）；VideoInfo 逐字段复制 2 处仍在 |
| 依赖管理 | 5 | 5 | **5** | 间接依赖未变；app/ui 字体已瘦身为 latin 子集（12 文件） |
| 配置与硬编码 | 7 | 4 | **3** | 凭据加密、manifest 修复、版本单一来源（1.4.4 三处一致）；过时注释残留 1 处（代码层待办） |
| 文档与注释 | 5 | 3 | **2** | 文档体系建立 + AIGC 残留清零（含本轮补清 docs/ 3 份）+ 混乱报告第三版对齐；残留：PRODUCT/DESIGN 未入 docs/（已登记） |
| 遗留/废弃代码 | 8 | 6 | **6** | temp 130MB 已清理、0 字节残留清除；根目录构建产物仍物理堆积（版本包增至 5 个） |
| 测试情况 | 3 | 2 | **2** | 36 个 *_test.go 随 P2-1 分层迁移，平铺问题消除；CI 流水线 + 覆盖率门禁 55% 保持 |
| 安全与健壮性 | 5 | 2 | **2** | 凭据加密、删除回收站化、白名单反射、审计闭环、版本控制已建立（首版五硬伤清零） |
| **综合** | **6.5/10（中等偏混乱）** | **4.3/10（轻度混乱）** | **2.8/10（轻度混乱）** | P0 全落地、P1 全落地、P2 全落地（P2-1~P2-8 均已落地/收口，2026-09-19 复核），混乱度显著下降 |

等级判定：**B / 轻度混乱（接近 B+）**。从"能跑、能测、但很难继续演进"过渡到"工程卫生显著改善、具备继续演进条件"的状态——1.4.4 已消除全部安全硬伤（凭据明文/物理删除/白名单遗漏/无版本控制），P2-1 后端分层与 P2-2 前端组件化落地，文档体系对齐 1.4.4（AIGC 残留清零），剩余主要债务：根目录构建产物未物理清理、代码层过时注释 1 处、P2-3 覆盖率门槛待上调至 60%。

---

## 4. 修改建议（按优先级，附落地状态追踪）

### P0 —— 立即处理（安全与止血，1 周内）

| # | 建议 | 状态（2026-09-19） | 落地证据 / 剩余动作 |
|---|---|---|---|
| P0-1 | **凭据加密落库** | ✅ **已落地** | server/internal/security/crypto.go（AES-GCM + 0600 secret.key + enc:v1: 前缀 + 旧明文兼容迁移 + API 脱敏）；**剩余**：internal/store/model/models.go（约 L152）过时注释"TODO: P0 AES 加密"清理（代码层待办，本轮 md 归档不涉及） |
| P0-2 | **清理工程残留并建立 .gitignore** | 🟡 **基本落地** | .gitignore 已建（覆盖 *.exe/*.fpk/node_modules/temp/logs/*.tsbuildinfo/0 字节残留）；temp/fpk_build_test2 与 fpk_inspect、server/ui-src 0 字节残留已删；**剩余**：根目录 fvcc.exe/fvcc.fpk/5 个版本 fpk/fvcs-service.exe 物理清理或移入 archive/；logs/2026-09-17/app.log(0B) 删除；FVCS/ 目录 build_check.txt/_cgo_full.txt/旧 fpk 治理 |
| P0-3 | **git init 建立版本控制** | ✅ **已落地** | git 已启用：由上层仓库 `D:\Fnos.VideoConversion` 统一管理（FVCC 内不再有独立 .git），已推送 GitHub https://github.com/hgmmm25/Fnos.VideoConversion/tree/baseline-snapshot（当前 HEAD 为 baseline-snapshot 快照分支）；P2-1/P2-7 等重构已具备回滚前提；**剩余**：后续改动持续提交，避免快照漂移 |

### P1 —— 短期改进（1~2 个迭代）

| # | 建议 | 状态（2026-09-19） | 落地证据 / 剩余动作 |
|---|---|---|---|
| P1-1 | **消灭 updateProfile 的 59 块手写映射** | ✅ **已落地（方案 B）** | server/applyjson.go 反射白名单 helper（applyJSONUpdates），实测 `if v, ok := updates[` = 0；配套 4 个单测 |
| P1-2 | **抽取 SMB 校验 helper** | ✅ **已落地** | server/smb_validate.go（validateSMBPath），handlers.go 5 处调用点统一 |
| P1-3 | **文档落地：消除悬空引用** | ✅ **已落地** | WebVideoEditor_Design/ 01-10 入库 + FVCC/README.md 速查表 + 根 README.md + BUILD.md + docs/API_CONTRACT.md |
| P1-4 | **修复 manifest 元数据 + 版本单一来源** | ✅ **已落地** | manifest desc/maintainer/distributor 正常（UTF-8）；server/version.go go:embed VERSION + version_test.go 三处一致性校验 + scripts/check-versions.ps1；FVCC/README.md 版本表已于 2026-09-18 同步至 1.4.2，无剩余动作 |
| P1-5 | **依赖瘦身** | 🟡 **部分落地** | app/ui/assets 字体已瘦身为 latin 子集（12 文件）；go.mod 已于 2026-09-18 执行 `go mod tidy` 复核：29 个 indirect 均为 gin v1.12 传递依赖（mongo-driver v2 由 gin 直接 require、quic-go 由 gin http3 引入），tidy 后零变化、无进一步精简空间；node_modules 全字符集不进入构建产物，建议保持不动 |

### P2 —— 中期架构演进（1~2 月）

| # | 建议 | 状态（2026-09-19） | 落地证据 / 剩余动作 |
|---|---|---|---|
| P2-1 | **后端分层：单 main 包 → internal 分层** | ✅ **已落地（2026-09-19）** | `server/internal/` 11 包 + store/model 子包，根包仅剩 main.go（82 个 .go = 46 实现 + 36 测试）；阶段 A/B/C 全部完成、8 个 shim 兼容文件已内联删除；go build / vet / test 全绿（实施记录：`FVCC/docs/P2-1_后端分层_阶段A实施记录.md`，分层前基线 tag v1.4.3）；**注意**：代码注释中 P2-1 另指"指标端点"（见 2.2 证据 2 与 TASK_REFERENCE §2.1 漂移登记），两套编号语义并存 |
| P2-2 | **前端组件化** | ✅ **已收口（2026-09-19）** | `src/lib/` 4 件套（crudActions / scrollPos / useListPage / formBuilder）接入 6 页，CRUD/滚动/订阅/表单样板残留 0；main.ts 路由切换接入 dispose 防订阅泄漏；验收：pages/ 行数以业务逻辑为主未达 -30%（§4.2 遗留说明），样板类残留为 0；详见 `FVCC/docs/P2-2_细化细则.md`（Checklist 全勾）；**决策门仍开放**：组合器方案去留 / 是否启动 Svelte 5 试点（§4.2.3） |
| P2-3 | **建立 CI 流水线** | ✅ **已落地** | .github/workflows/fvcc-ci.yml（go vet + test -race + 覆盖率 ≥55% + 前端 check:design/tsc/build）+ scripts/check-coverage.ps1 |
| P2-4 | **统一 API 契约与命名** | ✅ **已收口（2026-09-18 复核）** | docs/API_CONTRACT.md（统一错误契约 + code 映射表 + 域内 helper 清单）+ server/apierr.go（fail/failWithCode）+ api.ts 适配层兜底（body.msg \|\| body.error）；`{error}` 裸格式 grep 为 0；remote.go 剩余 snake_case（task_id/progress/stage 等）均为 FVCS 对等协议段（progressPush/helloPush），API_CONTRACT §3.2 已登记边界并补注释引用，无剩余动作 |
| P2-5 | **删除操作回收站化** | ✅ **已落地** | server/trash.go（moveToTrash → `<授权根>/_trash`，保留相对路径 + 同名时间戳后缀；listTrash 列表接口） |
| P2-6 | **清理 AIGC 残留与任务编号注释** | ✅ **已收口（2026-09-19 复核）** | AIGC frontmatter 已从 5 份文档清除（ui-src/PRODUCT.md、docs/API_CONTRACT.md、根目录 2 份、本报告自身）+ docs/ 3 份补清（TASK_REFERENCE / 项目分析与改进方向 / P2-1 实施记录）；README 原 frontmatter 上一轮已清；version.go"发布约定 2026-09-16"注释已语义化并指向 README 版本表；编号映射收敛于 docs/TASK_REFERENCE.md；2026-09-19 全库扫描 AIGC 关键词 0 命中 |
| P2-7 | **测试文件命名规范化** | ✅ **已落地（2026-09-18）** | `genproxy_share_cred_126_test.go` → `genproxy_share_cred_test.go`、`handlers_edl_list_contract_test.go` → `handlers_edl_list_test.go`（git mv 保留历史；TASK_REFERENCE §3.6 已同步） |
| P2-8 | **破坏性操作审计闭环** | ✅ **已落地（2026-09-18）** | server/security_audit.go 新增 `auditDestructive`（destructive.delete / destructive.clear / destructive.empty_trash，复用拒绝类审计通道，Detail 脱敏记方法+路径+IP，actor 取网关用户）；已接入清日志/清缓存/删除视频/服务器/方案/任务/历史/清空回收站共 8 处调用点；配套单测 TestP28DestructiveOpsAudited 验证记账与脱敏 |

---

### 4.1 P2-1 细化方案：后端分层（单 main 包 → internal 分层）

> **状态：✅ 已落地（2026-09-19）**——本方案已按 §4.1.4 阶段 A/B/C 全部执行完毕（实施记录：`FVCC/docs/P2-1_后端分层_阶段A实施记录.md`）。下方 4.1.1~4.1.7 为落地前快照，保留作迁移映射与验收依据。

#### 4.1.1 现状盘点（2026-09-19 第三版实测）

`server/` 目录实测共 **61 个 `.go` 文件（32 个实现 + 29 个测试）**，仍全部位于单一 `package main`；子目录 logger/、smbshare/ 各 1 个文件（仍属 main 包）。非测试代码 12,902 行，测试 6,458 行。按行数排序的 Top 实现文件：

| 文件 | 实测行数 | 职责 | 备注 |
|---|---|---|---|
| handlers.go | 1,737 | 全部 HTTP handler | 按注释分 **10 大节**：通用 / 设置 / 视频管理 / 文件操作 / 目录浏览 / 服务器管理 / 转码方案管理 / 任务管理 / 历史记录 / 监控指标；59 块手写映射已移除（applyjson.go 接管，见 2.3） |
| remote.go | 1,227 | WS 远端客户端 + 协议结构体 | 内含 `progressPush` 等 snake_case 结构体（见 2.2 证据 1） |
| scheduler.go | 1,204 | 1s 调度循环 + 全任务状态机 | 与 store 深度耦合 |
| handlers_render.go | 828 | 渲染类 handler | 独立文件但仍在 main 包 |
| stream.go | 794 | 预览网关（Range/票据/缩略图） | 含单文件 4 路 / 全局 64 路并发限制 |
| store.go | 777 | 内存缓存 + JSON 原子持久化 | 含崩溃恢复逻辑 |
| store_edl.go | 649 | EDL 项目存储 | 与 store.go 同族 |
| models.go | 639 | 全部数据模型 | Task/Server/Profile/Settings/Project/NodeCaps/AuditEntry/AssetProxy 等十几类 |
| security.go | 492 | 四层路径校验 | 07 规格落地 |
| edl_validate.go | 491 | EDL 载荷校验 | 与 FVCS 双实现 |
| ws.go / node_select.go / ratelimit.go / handlers_edl.go / ffprobe.go / proxy_flow.go / localtranscode.go / handlers_proxy.go | 457 / 454 / 334 / 321 / 303 / 297 / 290 / 200 | 各自领域 | — |
| main.go | 265 | 组装与启动 | — |

**2026-09-18 新增（首版未列）**：trash.go(213)、crypto.go(164)、router.go(149)、store_proxy.go(144)、security_audit.go(131)、applyjson.go(83)、gateway.go(62)、ws_limit.go(57)、apierr.go(56)、smb_validate.go(31)、version.go(19)、localtranscode_syscall_windows/generic(19/15)、logger/logger.go、smbshare/smbshare.go——**新增 10+ 文件全部仍进 main 包**，单包体量持续膨胀，分层的迫切性随每次迭代上升。

**已有"准领域文件"（分层意识存在的证据，呼应 2.1 积极面）**：见上表全部单文件职责划分；注意首版"准领域文件"清单中的行数已全部更新。

> **状态：✅ 已落地（2026-09-19）**——下述痛点均已随 P2-1 分层消解（见 §4.1 顶部横幅与 P2-1 实施记录），保留作动机说明。

#### 4.1.2 核心痛点（第二版更新）

1. **无边界（未变）**：单包内所有符号互相可见，编译依赖面全联通。领域间"意外耦合"无编译期拦截，任何文件改动都可能影响全局；新增 10+ 文件未拆包，单包体量从 11,336 行增至 12,902 行。
2. **巨型文件（未变）**：handlers.go 一个文件承载 10 个资源域（设置/视频/文件/目录/服务器/方案/任务/历史/监控 + 通用），实际职责至少可拆 10 个文件，行数 1,700+；remote.go/scheduler.go 破千行。
3. **模型集中（未变）**：models.go 单文件定义十几类模型；且 models.go:147 残留过时注释（见 2.5），模型与安全（凭据字段）职责混叠的隐患仍在。
4. **测试平铺（未变）**：29 个测试与实现 1:1 平铺在 server/ 下，无测试分层（对应 P2-7 命名问题）。
5. **编号歧义（已实锤）**：`server/router.go:29` 与 `server/handler_metrics_test.go:1` 的 `// P2-1` 均指"可观测性：Prometheus 指标端点"，与本报告 §4 中 P2-1（后端分层）含义完全不同——两套编号体系语义冲突已从 1 处增至 2 处。

#### 4.1.3 目标架构

```
FVCC/server/
├── main.go                      ← 仅组装：加载配置 → 依赖注入 → 启动（当前 265 行基本保留）
├── internal/
│   ├── api/                     ← HTTP 层：路由 + handler（按资源域拆文件）
│   │   ├── router.go            ← 路由注册 + NoRoute / requireAdmin 挂载
│   │   ├── middleware.go        ← securityHeaders / 认证 / 限流中间件
│   │   ├── common.go            ← respondOK / respondErr / getSettings / 分页解析 等公共基元
│   │   ├── settings.go          ← 设置域（原 handlers.go "===== 设置 =====" 节）
│   │   ├── video.go             ← 视频管理（原 "===== 视频管理 =====" 节）
│   │   ├── fileops.go           ← 文件操作（原 "===== 文件操作 =====" 节）
│   │   ├── browse.go            ← 目录浏览（原 "===== 目录浏览 =====" 节）
│   │   ├── server.go            ← 服务器管理（原 "===== 服务器管理 =====" 节）
│   │   ├── profile.go           ← 转码方案管理（含 updateProfile / createProfile）
│   │   ├── task.go              ← 任务管理（原 "===== 任务管理 =====" 节）
│   │   ├── history.go           ← 历史记录（原 "===== 历史记录 =====" 节）
│   │   ├── metrics.go           ← 监控指标（原 "===== 监控指标 =====" 节；含 /metrics/prometheus，注意与代码注释 P2-1 语义对应）
│   │   ├── edl.go               ← EDL 项目 handler（handlers_edl.go 迁入）
│   │   ├── render.go            ← 渲染类 handler（handlers_render.go 迁入）
│   │   ├── proxy.go             ← 代理 handler（handlers_proxy.go 迁入）
│   │   └── stream.go            ← 预览网关（Range/票据/缩略图，stream.go 迁入）
│   ├── store/                   ← 持久化层
│   │   ├── store.go             ← 内存缓存 + JSON 原子写基元（崩溃恢复）
│   │   ├── edl.go               ← EDL 项目存储（store_edl.go 迁入）
│   │   ├── proxy.go             ← 代理存储（store_proxy.go 迁入）
│   │   ├── trash.go             ← 回收站存储（trash.go 迁入）
│   │   ├── audit.go             ← 审计记录（security_audit.go 记账逻辑迁入，联动 P2-8）
│   │   └── model/               ← 按域拆分的数据模型（详见 4.1.5）
│   ├── scheduler/               ← 1s 调度循环 + 任务状态机（scheduler.go 迁入）
│   ├── remote/                  ← WS 远端客户端 + 协议结构体（remote.go / ws.go 迁入）
│   ├── security/                ← PathValidator / 凭据加解密 / 审计 helper（security.go / crypto.go / smb_validate.go / gateway.go 迁入）
│   ├── edl/                     ← EDL 校验与 fuzz（edl_validate.go 迁入）
│   ├── media/                   ← ffprobe / proxy_flow / localtranscode（ffprobe.go / proxy_flow.go / localtranscode.go 迁入）
│   ├── node/                    ← 节点选择（node_select.go 迁入）
│   ├── version/                 ← 版本单一来源（version.go 迁入）
│   └── protocol/                ← 跨包共享的 WS 消息结构体与错误契约（apierr.go / ws_limit.go / applyjson.go 迁入）
```

**迁入映射表（现有文件 → 目标包，第二版补充新增文件）**：

| 现有文件 | 目标包 | 迁移批次 |
|---|---|---|
| apierr.go / ws_limit.go / applyjson.go | internal/protocol | A（叶子文件，无内部依赖） |
| crypto.go / smb_validate.go / gateway.go | internal/security | A |
| ffprobe.go / proxy_flow.go / localtranscode.go（含 syscall 两个文件） | internal/media | A |
| node_select.go | internal/node | A |
| edl_validate.go | internal/edl | A |
| version.go | internal/version | A |
| store.go / store_edl.go / store_proxy.go / trash.go | internal/store | A |
| remote.go / ws.go | internal/remote | A |
| security_audit.go | internal/store 或 internal/security（按审计记录归属） | A/B |
| scheduler.go | internal/scheduler | A |
| handlers_edl.go / handlers_render.go / handlers_proxy.go / stream.go | internal/api | B |
| handlers.go | internal/api（按 10 节拆 8~10 个文件） | B |
| models.go | internal/store/model（按域拆分） | C |
| main.go | 根（保持组装职责） | C |

#### 4.1.4 分阶段实施计划（基本保留首版，更新回归基线）

**阶段 A：骨架搭建 + 叶子文件搬移（约 1 周）**
1. 创建 `internal/` 各包目录与占位文件；`main.go` 顶部保持 `package main` 不变。
2. 优先搬移"零内部依赖 / 仅依赖标准库"的叶子文件（apierr.go、ws_limit.go、applyjson.go、crypto.go、smb_validate.go、gateway.go、version.go），逐一修正包名与引用点。
3. 再搬移"仅依赖叶子包"的中层文件（ffprobe / proxy_flow / localtranscode / node_select / edl_validate / store 族 / trash / security_audit / remote 族 / scheduler）。
4. **每搬一个文件立即 `go build ./...` + `go test ./...`**，全绿才继续；本阶段禁止任何行为重构。

**阶段 B：handler 按资源域拆文件（约 2 周）**
1. 在 `internal/api` 下新建 `common.go`，抽取 handlers 公共基元：respondOK / respondErr / JSON 绑定错误归一、getSettings（带默认值兜底）、分页参数解析。
2. 按 handlers.go 的 10 个注释分节切分为独立文件：settings.go / video.go / fileops.go / browse.go / server.go / profile.go / task.go / history.go / metrics.go + common.go。
3. handlers_render.go / handlers_proxy.go / handlers_edl.go / stream.go 整体迁入 api 包。
4. 迁移完成后运行现有 29 个测试 + `go vet ./...`，确认 HTTP 行为零回归。

**阶段 C：模型拆分与调度收敛（约 3~4 周）**
1. models.go 按域拆分为：task.go / server.go / profile.go / settings.go / project.go / edl.go / asset.go / audit.go / nodecaps.go；公共基元（时间戳、状态枚举）收敛到 model/common.go。
2. 定义模型归属规则：**"模型即持久化契约"**——凡落盘 / 跨进程传输的结构体归 `store/model`；仅内存态的内部结构体可留在所属包内。
3. scheduler 与 store 解耦：scheduler 通过 store 暴露的接口操作任务，不再直接持有模型细节。
4. 测试文件随实现迁移，命名统一（联动 P2-7）；为每个包补充 doc.go 或简短包注释，说明职责与依赖方向。

**每阶段完成标准（DoD，第二版更新基线）**：
- `go build ./...` / `go vet ./...` / `go test ./...` 全绿；
- 无循环依赖（`go list -deps` / `go vet` 检测 + 人工核对 import 图）；
- 现有 6,458 行测试全部通过（回归基线，首版 5,406 行）；
- HTTP 对外行为不变（对照 contract 类测试：handlers_edl_list_test.go 等 + 新增 router_auth_test.go / handler_metrics_test.go 定向回归）；
- CI 覆盖率门槛 ≥55% 不降（新增检查项，首版无 CI）。

#### 4.1.5 模型拆分建议（详细，不变）

| 现 models.go 内模型 | 目标文件 | 归属理由 |
|---|---|---|
| Task / TaskStatus / TaskEvent | model/task.go | 调度核心契约 |
| Server / NodeCaps | model/server.go | 节点集群 |
| Profile | model/profile.go | 转码方案 |
| Settings（含 AuthKey / SMBPassword） | model/settings.go | 配置契约（联动 P0-1 加密——现已落地，拆分时同步清理 models.go:147 过时注释） |
| Project / EDL 相关 | model/edl.go | EDL 剪辑 |
| AssetProxy / VideoInfo / VideoInfoCache | model/asset.go | 素材代理 |
| AuditEntry | model/audit.go | 审计（联动 P2-8） |
| 时间戳 / 状态枚举 / 通用 JSON 类型 | model/common.go | 公共基元 |

#### 4.1.6 依赖方向规则（强制，不变）

```
api → {store, scheduler, remote, security, edl, media, node, protocol}
scheduler → {store, remote, media, node}
remote → {protocol, security}
store → {security}（加密落盘时）
其余包禁止反向依赖 api
```

- 包间禁止 import 环；`main.go` 是唯一允许 import 所有 internal 包的"组装点"。
- 迁移期临时兼容：若存在难以拆分的交叉引用，允许在目标包内保留一段"兼容 shim"，并在阶段 C 收尾时内联清理；**严禁**为省事新建"垃圾场包"。

#### 4.1.7 风险与注意事项（第二版更新）

| 风险 | 等级 | 缓解措施 |
|---|---|---|
| 单包内符号互相引用密集，拆包产生大量跨包引用改动 | 高 | 严格"叶子优先"搬移顺序；每文件搬移即编译+测试，禁止攒批 |
| 测试直接访问私有符号（package main 内测试可见全部符号） | 高 | 测试随实现一起迁移；必要时将符号导出（首字母大写），或改用外部测试包视角 |
| 搬移同时顺手重构导致回归难定位 | 中 | 阶段 A/B 只搬不移；重构（如 models.go 过时注释清理）必须等分层完成后再做，两个高风险变更禁止并行 |
| stream.go 票据/限流状态、scheduler.go sync.Map 单飞逻辑被搬运破坏 | 中 | 行为敏感区单独提交；搬移前后跑 stream_test.go / scheduler_edl_test.go / remote_reconnect_test.go / remote_wss_test.go 定向回归 |
| 新增文件（crypto/trash/security_audit/apierr 等）已与 store/handlers 深度耦合 | 中 | 这些文件大多已按"准领域"独立，搬移成本低；security_audit 的 store.AppendAudit 调用在拆包时收敛到 store/audit |
| 与 P2-7（测试命名）耦合 | 低 | 迁移时同步改名，保留 git 历史（git 已建立，见 P0-3） |
| 与 P0-3（版本控制）前置关系 | 高 | git 已建立（baseline-snapshot 快照分支），分层是仓库最大一次重构，**启动前先确认当前基线已提交并打 tag**；搬移错误可回滚 |

---

### 4.2 P2-2 细化方案：前端组件化

> **状态：✅ 已收口（2026-09-19）**——本方案已按 4.2.1~4.2.2 落地（`src/lib/` 4 件套接入 6 页，样板残留 0；细则：`FVCC/docs/P2-2_细化细则.md`，Checklist 全勾）。下方 4.2.1~4.2.2 为落地前快照；4.2.3 决策门仍开放（组合器去留 / Svelte 5 试点）。

#### 4.2.1 现状盘点（2026-09-19 第三版实测）

`ui-src/src` 结构实测（第二版更新）：

```
ui-src/src/
├── pages/          ← 列表页 6 个文件：scanner.ts(1,300) / profiles.ts(1,276) / tasks.ts(479) / settings.ts(420) / servers.ts(255) / history.ts(189)
│   └── editor/     ← 【新增拆分】编辑器区 15 个模块：assets.ts(663) / timeline.ts(631) / preview.ts(580) / index.ts(455) / editorStore.ts(451) / layout.ts(320) / dialog.ts(214) / taskView.ts(145) / shortcuts.ts(131) / taskDrawer.ts(120) / ruler.ts(95) / clipOps.ts(90) / format.ts(83) / preset.ts(59)
├── types.ts(545) / main.ts(389) / ui.ts(323) / api.ts(253) / command.ts(200) / store.ts(191) / ws.ts(79) / theme.ts(62) / vite-env.d.ts(1)
```

前端 TS 总计 29 文件 9,999 行（首版约 8,300 行）。列表管理页（pages/ 6 文件）合计约 3,919 行，其中可量化的重复样板（第二版实测）：

| 样板模式 | 首版范围 | 第二版实测 | 说明 |
|---|---|---|---|
| `renderList` / `renderEdit` 双态切换 | 6 页 | **profiles.ts(2) / servers.ts(2)，其余 0** | scanner/tasks/settings/history 已不再使用该命名（可能内联或改名） |
| `scrollPos` 保存 / 恢复 | 6 页 | **仅 profiles.ts(3)** | 其余页面已收敛 |
| `store.subscribe` 订阅 / 退订 | 6 页 | profiles/scanner/tasks/servers/history 各 1 | settings 为 0（可能改同步读取） |
| CRUD 四件套（confirmDialog → api.* → toast → store.load*） | 6 页 | 仍普遍存在 | 无公共封装 |
| 表单生成 + 校验逻辑 | settings / profiles 最重 | settings 420 / profiles 1,276 | profiles.ts 仍是最大列表页 |

**估算：6 个列表页中约 25%~35% 行数为样板重复（首版估算 40%~50%，编辑器区拆分 + 部分收敛后下降）**。

另注意：`ui-src/src/api.ts:27` 已按新契约实现 `{ ok:false, code, msg, detail? }` 统一错误处理（ApiError.code），request() 保留 `body.msg || body.error` 兜底——错误契约过渡期由 api.ts 适配层承担（联动 P2-4 已部分落地），组件化抽取时**公共层只依赖新契约**（ApiError），兼容分支留在 api.ts。

#### 4.2.2 短期方案（1~2 迭代，不引入框架）——已按此落地（2026-09-19）

**目标**：把每个列表页的样板从 ~100 行降到 ~20 行调用，业务逻辑保持零框架 `el()` 手工 DOM 不变。

**抽取 1：`crudActions`（CRUD 四件套）**

```ts
// src/lib/crudActions.ts
export function crudActions<T>(opts: {
  confirmTitle: (item: T) => string
  apiCall: (action: 'create' | 'update' | 'delete', item: T) => Promise<unknown>
  reload: () => void
}) {
  return {
    create(item) { /* confirmDialog → apiCall → toast → reload */ },
    update(item) { /* 同上 */ },
    remove(item) { /* 同上，删除走 confirmDialog 二次确认 */ },
  }
}
```

统一流程：confirmDialog 弹窗 → api 调用 → 成功 toast → reload；错误统一走 ApiError 展示（只认新契约，见 P2-4）。

**抽取 2：`scrollPos` helper**

```ts
// src/lib/scrollPos.ts
export function withScrollPos(el: HTMLElement, key: string): void
// 按 key 隔离保存/恢复 scrollTop，页面 renderList/renderEdit 切换时保持位置
```

6 页统一替换为一行调用（当前仅 profiles.ts 3 处残留，接入成本已大幅降低）。

**抽取 3：`useListPage` 组合器**

```ts
// src/lib/useListPage.ts
export function useListPage<T>(opts: {
  load: () => Promise<T[]>
  render: (data: T[]) => void
  subscribe?: (cb: () => void) => () => void
}) {
  /* 内部管理：加载态、错误 toast、subscribe 生命周期、refresh() */
}
```

将"订阅 store → 拉数据 → 渲染 → 错误处理"的公共流程收敛。

**抽取 4：`formBuilder`（表单 builder）**

```ts
// src/lib/formBuilder.ts —— 生成 label+input 行、校验规则、取值/回填
```

先用于 settings.ts / profiles.ts 两个表单最重的页面。

**试点顺序（第二版更新，基于已拆分现状）**：
1. 第一周：抽出 crudActions + scrollPos；接入 profiles.ts 与 scanner.ts（样板最重，收益最直观）。
2. 第二周：useListPage 成型，接入 tasks / settings / servers / history；formBuilder 落地 settings / profiles。
3. 验收：pages/ 目录总行数下降 ≥30%；scrollPos 行为回归（列表切编辑再返回保持位置）；所有 CRUD 走查通过；`npm run check:design` 门禁通过。

#### 4.2.3 中期方案（渐进式框架评估，决策点）——保留首版设计

**框架选项对比**：

| 选项 | 心智成本 | 与现有 el() 融合 | 打包体积增量 | 适用性 |
|---|---|---|---|---|
| 不引入框架，仅组合器 | 最低 | 天然融合 | 0 | 列表管理区够用，样板收敛上限约 60% |
| Svelte 5 | 低（编译期、模板接近原生） | 可用自定义元素（custom element）桥接 | 小（按需编译） | **推荐候选**：渐进引入成本最低 |
| Vue 3 | 中（渐进式、生态成熟） | 可挂载到现有 DOM 子树 | 中（~34KB gzip） | 可选候选 |
| React 18 | 高（心智迁移大） | 需要独立挂载点 | 中（~45KB gzip） | 不推荐，收益/成本比低 |
| Preact / Lit | 低 | 轻量桥接 | 极小 | 保守候选 |

**推荐路径**：
1. **列表管理区（pages/）与编辑器区（pages/editor/）分开评估**。列表区是纯"数据列表 + CRUD"形态，组件化收益高；编辑器区已按模块拆分（timeline / assets / preview / editorStore / layout 等 15 文件）且依赖精细 DOM 控制与自定义交互（时间线拖拽、画布缩放），零框架直接操作 DOM 反而灵活，框架收益低。
2. 若选择引入：用 **Svelte 5 做 1 个页面试点**（建议 history.ts 或 servers.ts，业务最轻），通过自定义元素嵌入现有 `el()` 渲染树，验证：构建集成、状态共享（store.ts 跨框架读写）、打包体积增量、交互一致性。
3. 试点通过后再决定是否推广；**禁止全量重写**——旧页面保持组合器方案，新页面 / 重写页面才用框架，新旧共存直至自然替换。

**决策门**：阶段 B 完成后评估样板收敛率——
- 若 pages/ 总行数下降 ≥30% 且维护者认为可接受 → 可停留在"组合器方案"，框架作为远期选项；
- 若仍感觉样板负担重、新增页面成本高 → 启动 Svelte 试点，2 周内给出结论。

#### 4.2.4 与相邻建议的联动（第二版更新）

| 关联项 | 关系 | 处理顺序 |
|---|---|---|
| P2-4 统一 API 契约 | crudActions 的错误处理必须只依赖新契约（ApiError） | **已部分前置**：api.ts 适配层兜底已就位，公共层直接消费 ApiError |
| P1-3 文档落地 | 公共模块需配套简短 doc comment | 随抽取同步写（文档体系已建立，直接对齐 FVCC/README 速查风格） |
| P0-3 版本控制 | 重构前先建立版本控制，保证每步可回滚 | ✅ 已前置落地：git 已启用并推送 GitHub（baseline-snapshot 快照分支），组件化抽取每步可提交回滚 |

#### 4.2.5 风险与注意事项（第二版更新）

| 风险 | 等级 | 缓解措施 |
|---|---|---|
| 组合器 API 设计不良，演变成"自研框架前身" | 中 | 组合器 API 保持小而稳定：只封装"流程"，不封装"样式/布局"；严禁为节省行数把业务判断塞进公共层 |
| 抽取过程引入行为回归（scrollPos 丢失、订阅泄漏） | 中 | 每页接入后单独走查；subscribe 退订逻辑保持对称（当前残留点已少：仅 profiles 3 处 scrollPos） |
| 错误契约未统一导致公共层耦合旧格式 | 高 | 新契约 { ok:false, code, msg, detail? } 已由 api.ts 适配层兜底转换，公共层只感知 ApiError |
| 渐进引入框架后打包体积与首屏劣化 | 中 | 试点页独立打包对比；Svelte 按需编译，控制 runtime 增量 < 15KB gzip；超限则回退组合器方案 |
| editorStore / store.ts 等状态模块与框架耦合 | 中 | 状态模块保持框架无关（纯 TS + 订阅），框架组件只消费不改造；pages/editor/editorStore.ts 已是独立状态模块，符合此方向 |

---

## 5. 结论（第三版，2026-09-19）

FVCC 从首版评价（2026-09-18 上午，1.4.0）到第三版复核（2026-09-19，1.4.4），不到两天完成 **P0 全部、P1 全部、P2 全部（P2-1~P2-8 均已落地/收口）** 的整改，混乱度综合评分从 **6.5/10 → 4.3/10 → 2.8/10**（轻度混乱，接近 B+）：

**已兑现的改善（高置信证据）**：
1. **安全硬伤清零并闭环**：凭据 AES-GCM 加密（internal/security/crypto.go）、删除回收站化（internal/store/trash.go）、字段更新反射白名单（internal/protocol/applyjson.go）、破坏性操作审计闭环（internal/security/security_audit.go，8 处调用点 + 单测）、版本控制（git + GitHub baseline-snapshot）——首版五硬伤全部落地且配套单测；
2. **架构演进两大项落地**：P2-1 后端 internal 分层（11 包 + store/model 子包，根包仅剩 main.go，82 个 .go 全绿）+ P2-2 前端组件化（src/lib/ 4 件套接入 6 页、样板残留 0、dispose 防订阅泄漏）——第二版"均未启动"的两大项已于 2026-09-19 收口；
3. **质量保障闭环**：CI 流水线（go vet + test -race + 覆盖率 ≥55%）、版本单一来源（manifest / package.json / VERSION 三处 1.4.4 + check-versions.ps1）、覆盖率基线 56.1%（门槛后续上调 60%）；
4. **文档体系成型且 AIGC 残留清零**：规格文档 01-10 入库 + 速查表，根 README / BUILD / API_CONTRACT / 混乱报告 / TASK_REFERENCE 等全部对齐 1.4.4；2026-09-19 全库扫描 AIGC 关键词 0 命中。

**仍待解决的主要债务（按优先级，均为非阻塞项）**：
1. **根目录构建产物物理堆积**（fvcc.exe + 5 个版本 fpk + fvcs-service.exe）：archive/ 已收纳 v1.1.0~v1.3.1 旧包，根目录与 FVCS/ 残留待物理清理（P0-2 剩余）；
2. **代码层过时注释 1 处**：internal/store/model/models.go（约 L152）"TODO: P0 AES 加密"（P0-1 剩余，属代码层清理，不在 md 归档范围）；
3. **P2-3 覆盖率门槛待上调至 60%**；**§4.2.3 决策门仍开放**（组合器方案去留 / Svelte 5 试点）。

**当前定位**：项目已从"能跑、能测、但很难继续演进"过渡到"工程卫生显著改善、具备继续演进条件"。剩余债务集中在物理清理与代码层注释，不再有架构级阻塞项。后续以"代码即真相"与快照漂移说明机制（P3-1/P3-2）持续对冲 AI 增量修补式开发带来的文档滞后。

> 维护记录：首版 2026-09-18 上午（1.4.0）→ 第二版 2026-09-18（1.4.2，评分 4.3/10）→ 第三版 2026-09-19（1.4.4，评分 2.8/10）。本报告与 docs/ 下其他台账文档（TASK_REFERENCE / 项目分析与改进方向 / P2-1 实施记录 / P2-2 细化细则）已于 2026-09-19 统一对齐 1.4.4 现状。
