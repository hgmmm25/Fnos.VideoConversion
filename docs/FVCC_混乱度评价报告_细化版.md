# FVCC 项目混乱度评价报告（细化版）

> 项目：FVCC（飞牛视频转换 NAS 调度端 Web UI）@ D:\Fnos.VideoConversion\FVCC
> 基线评价日期：2026-09-18（首版）
> **复查更新日期：2026-10-13（第十一版，反映 v1.6.0 视觉打磨 + v1.6.4 任务页三批次 + v1.6.5 版本真源漂移修复与校验加固 + 第二十八轮长台账对齐后的最新现状）**
> 评价视角：Vibe Coding（AI 辅助编码）模式下的工程卫生与可维护性
> 评价方式：目录树扫描 + 关键源码阅读（后端 internal 11 包 + store/model 子包共 97 个 .go 文件；前端 TS 33 文件 10,315 行——2026-10-12 第十版实测已推进至 internal 12 包 / 107 个 .go（58 实现 + 49 测试）/ TS 33 文件 11,257 行——**2026-10-13 第十一版实测再推进至 internal 12 包（api / edl / media / node / pathutil / protocol / remote / scheduler / security / store / version / ws）+ store/model 子包 / 110 个 .go（58 实现 17,197 行 + 52 测试 9,183 行，合计 26,380 行）/ TS 33 文件 11,474 行**；覆盖率本轮未重跑，沿用第二十六轮实测 60.7%）+ 配置与文档核查 + git log/status 交叉核对 + 与根目录 README.md / BUILD.md / docs（项目现状与开发步骤 / IMPROVEMENT_LOG / TASK_REFERENCE / 项目分析与改进方向 / UI设计优化方向 / SECURITY）交叉核对
> 更新说明：第五版基于 2026-09-20 晚实测，版本迭代至 **1.4.9**（manifest / package.json / VERSION 三处一致）；后端在 1.4.8/1.4.9 继续细化拆分（stream_media / roots / aliases / config / trash_handlers / edl 扩展）；docs 布局收口（PRODUCT.md / DESIGN.md 已入 FVCC/docs/）。同时登记清理回潮：temp/ 膨胀至 549.7MB（node_modules 全量 + chrome-profile + secret.key 敏感残留）、新文档 `docs/FVCC_项目现状与开发步骤.md` 带 AIGC frontmatter、git 工作区 1.4.7~1.4.9 改动未提交、README（1.4.4）/ BUILD（1.4.6）版本表继续滞后。§4 中 P0/P1/P2 已全部落地，压缩为状态汇总表；删除 §4.1 / §4.2 落地前快照（实施记录见 FVCC/docs/ 对应文档）。
> 第六版基于 2026-09-21 实测：版本推进至 **1.5.3**；步骤 3 代码清理（scheduler 防注入死分支 / checkUploadProgress / checkDownloadProgress 空占位 / TestB05SideGuards 过时用例 / editor placeholder 注释）与步骤 6 仓库治理（temp 18 项清理释放约 560.3MB、6 个 fpk 归档 archive/fpk/ 完整 v1.1.0~v1.5.3、实测修正多项不存在项）已落地；P3 组物理清理与文档同步债务清零，详见 §2.7 / §3 / §4。
> 第七版基于 2026-09-21 晚实测（文档校准）：**P0-2 SQLite 迁移已落地**（2026-09-21 三个提交 a72b8ff / 9e122d5 / 5dd373e，modernc.org/sqlite 纯 Go 驱动 + 12 张实体表 + 存量 JSON 导入 + 读回主路径/persist 切换），最大结构债消除；**P1-1 事件驱动调度亦已落地**（提交 eab7688，事件循环 + 内存索引 + 定时落盘 + WS 广播全覆盖，早于第六版登记遗漏，本次补登）；代码规模实测 96 个 .go（53 实现 + 43 测试）/ 21,071 行；FVCC/docs 技术文档 5 份 → 7 份（+SECURITY.md 已补写 + STORE_PERSISTENCE_ROADMAP.md 方案稿）；代码级 TODO 收敛为 store 定时备份/快照回滚（P1 TODO，方案稿已出）、ffprobe 运行时依赖、覆盖率上调三项。
> 第八版基于 2026-09-22 工作区实测：版本推进至 **1.5.7**（manifest / package.json / VERSION 三处一致，工作区）。**1.5.4 已提交（080754c）**——editor 预览变速播放（J/K/L，PR 惯例同向 2x→3x→4x、最大 4x、反向重置）+ 播放控制按钮图标化；**1.5.5~1.5.7 工作区未提交**——Windows 盘符根（`D:\`）双分隔符边界 bug 统一修复（handlers.go 目录浏览 / edl_validate.go / trash.go / smbshare.go 四处前缀判定，根以分隔符结尾时直接以其为前缀）、stream.go 剪辑页多根错位兜底（指定根失败时对全部授权素材根重试，越权 403 立即拒绝）、roots.go 素材根本机不可见时回落授权目录首个、assets.ts 素材递归子文件夹扫描（修复 v1.5.6 子文件夹视频不显示）；配套新增/扩充 stream/edl_validate/trash/roots 各 *_test.go，server .go 96→**97**（53 实现 + 44 测试，新增 media/roots_test.go）；TS 仍 33 文件。落实细节见 `docs/IMPROVEMENT_LOG.md` §十一。（2026-10-11 第九版校正：该批次后已提交 68ebdec 并打 tag v1.5.7）
> 第九版基于 2026-10-11 实测：版本推进至 **1.5.8**。**P3-5 落地关闭**（IMPROVEMENT_LOG §十二）：阶段 A 定时 30min×12 备份 + 写前备份（efef4bd）、阶段 B data_version 版本化迁移链（16f1043，与 SQLite schema_version 正交，锚点载体 import_meta + data_version.json）、阶段 C 快照整包回滚 + POST /api/store/restore（59e4004，验签后内存换入、zip 白名单防 zip-slip、prerestore 兜底、QUEUE 重置）、阶段 D 收口（store.go 头部 TODO(P1) 移除、文档回写）。代码级 TODO 收敛为 ffprobe 运行时依赖（P3-6）、覆盖率上调（P3-7）两项；coverage 实测 56.7%→**59.0%**（门禁 55%）。
> 第十版基于 2026-10-12 实测：版本推进至 **1.5.12**（配套 FVCS v1.2.6）。**步骤 8 e2e A 档五项缺陷修复收口**（IMPROVEMENT_LOG §十四~§十六）：#6/#5/#10/#11（代理就绪回填 `GET /api/proxy` + 帧率数值近似 / 代理提交带素材根 / scanner 批量删除按钮 / FVCS 重连 Hello 自动 online）真机复验通过；**#8 渲染四层全链路修复端到等价代理**——①FVCS 落地 `srcBase=mountPath`（v1.5.10）②FVCC 提交侧「有界单根」`EDLClip.sourceRoot` 透传 + `resolveRenderRoots`（v1.5.11）③FVCS `makeSrcResolver` 对齐代理 `BuildSMBPath` 修 `sourceRoot 为空` 误报（v1.5.11）④FVCC RENDER_EDL 终态轮询 `pollRenderTerminal`/`completeRenderTask` 修出片后卡「收尾·100%」（v1.5.12），附带修复 FVCC `validateRootRelPath` 与 FVCS `ValidateRootPath` 长期不一致（共享向量 PL-ROOT-ABS 转绿）。真机验证：Success 收口 + 进度平滑 ✅（2026-10-12 用户执行）；Failed/Cancelled 分支无自然复现、顺带验证。**推送状态**：#8 链路 + 版本收口 + .gitignore + 根 README 已提交推送 `fork/baseline-snapshot`（800eb4c / de20d03 / d63d68e / f74ec54 / a20cc6b / 4dcdb26），**v1.5.12 已打注解 tag 并推送（2026-10-12 第二十六轮，P3-1 收尾）**。**文档全面对齐轮（第十版登记）**：FVCC/README.md、FVCC/docs/ 7 份、根 BUILD.md 与根 docs/ 台账统一对齐 v1.5.12（P3-4 剩余项清零）。代码规模实测 107 个 .go（58 实现 + 49 测试，internal 12 包 + store/model 子包，根包仅剩 main.go 274 行；v1.5.8 增 internal/pathutil）；TS 仍 33 文件 11,257 行（2026-10-12 实测）。
> 第十一版基于 2026-10-13 实测：版本推进至 **1.6.5**（配套 FVCS **v1.3.0**）。**1.6.x 版本线登记**（git 核验，见 §2.10）：**v1.6.0**（tag b1a0d93，含 1e622f0 批次①-⑤ / 22035c6 FVCS GUI 复活）视觉打磨——方向 A「精修冷灰」令牌层重排 + 控件层自绘 + 外壳导航分组·搜索胶囊·状态栏 + 逐页修复 V1/V5-V8 + 微交互与 WCAG 对比度（V9）验收，同批 FVCS 1.2.6→1.3.0（Fyne 托盘 + 设置窗口入构建、新增 pkg/fynetheme）；**v1.6.4**（tag b007a3d，含 fe06e52 代码，7 文件 +197/−44；方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`，三方外部模型评审整合）任务页三批次——A 层级修复与状态反馈、B 热路径性能（进度条 `transform: scaleX(--p)` 走合成层 + render/updateOverview 签名守卫，硬约束"视觉改进不得增加 WS tick 重绘"）、C 速率合并 + 实验项；**v1.6.5**（tag 5166cbe，含 ceeb1b2 代码）VERSION 第三真源漂移修复 + 版本一致性校验加固（漂移即红、移除 `t.Skipf`、新增反硬编码字面量用例、check-versions.ps1 去硬编码路径）。**易错点**：1.6.1/1.6.2/1.6.3 仅为批次开发过程中的本地中间 bump，从未进入 git 历史、未出包、未打 tag（manifest 实测序列 1.6.0 → 1.6.4 → 1.6.5），方案文件名中的 "v1.6.1" 是方案稿编号而非交付版本号。代码规模实测 110 个 .go（58 实现 17,197 行 + 52 测试 9,183 行，internal 12 包 + store/model 子包）；TS 33 文件 **11,474** 行（增量来自 1.6.4 任务页批次）。覆盖率**本轮未重跑**，沿用第二十六轮实测 **60.7%**（门禁 60%）。本轮验证：go vet 通过、`go test ./internal/version -count=1` 与全量 `go test ./...` 绿、`npm run build` 绿、check-versions.ps1 绿；交付物 `FVCC/fvcc.fpk` 已重打并解包核验（包内 manifest=1.6.5，二进制内 1.6.x 版本串仅 1.6.5）；真机验收：用户 2026-10-13 确认 NAS「设置」页显示 1.6.5。**评分**：综合 2.4 → **2.3**（依据见 §3，各维度维持原分）。**第二十八轮长台账对齐（第十一版登记）**：本混乱报告与 `docs/项目分析与改进方向.md` 对齐至 1.6.5 基线；其余文档（根 README / BUILD / FVCC/docs / 项目现状与开发步骤）的 1.6.x 对齐本轮**未实测**，见 §2.6。

---

## 1. 项目概览

| 项 | 内容 |
|---|---|
| 定位 | fnOS NAS 上的视频转码调度端：素材扫描 → 转码方案 → 渲染节点集群 → 任务队列 → EDL 剪辑/代理预览全链路 |
| 后端 | Go 1.27.1，gin v1.12 + gorilla/websocket，**已分层**：`server/internal` 12 包 + store/model 子包（第十一版实测 110 个 .go = 58 实现 17,197 行 + 52 测试 9,183 行；第十版记载 107 个 = 58 实现 + 49 测试，根包仅剩 main.go 274 行；store 已迁 SQLite：sqlite.go / sqlite_import.go / sqlite_load.go / sqlite_persist.go + P3-5 backup/migrate/restore；v1.5.8 增 internal/pathutil，2026-10-13 实测） |
| 前端 | Vite + TypeScript + TailwindCSS，零框架（`el()` 手工 DOM；P1-2 决策门 2026-10-11 定案维持），公共样板已收敛至 src/lib/ 4 件套；TS 33 文件 11,474 行（2026-10-13 实测；上一记载 11,257 行 / 2026-10-12，增量来自 1.6.4 任务页批次，含 ui.ts 新增 listSkeleton 泛化组件） |
| 版本 | **1.6.5**（server/internal/version/VERSION、ui-src/package.json、manifest 三处一致，2026-10-13 实测；version_test.go 自动校验已在 v1.6.5 加固为"漂移即红"，不再静默 Skip；配套 FVCS v1.3.0，FVCS/pkg/version/VERSION，本轮未触碰 FVCS 代码）；历史版本包已全部归档 `archive/fpk/`（v1.1.0~v1.5.3，根目录无 fpk / exe 堆积；1.5.4~1.6.5 构建产物为 gitignore 本地件） |
| 版本控制 | **已启用 git（P0-3 已落地）**：由上层仓库 `D:\Fnos.VideoConversion` 统一管理，当前分支 baseline-snapshot，已推送 GitHub [hgmmm25/Fnos.VideoConversion](https://github.com/hgmmm25/Fnos.VideoConversion/tree/baseline-snapshot)；**状态（2026-09-22 第八版校准）：v1.5.3 已提交并打 tag（含 SQLite 迁移三提交 a72b8ff / 9e122d5 / 5dd373e 与事件驱动 eab7688）；1.5.4 已提交（080754c，编辑器变速播放 + 按钮图标化）未打 tag；1.5.5~1.5.7（Windows 路径边界修复 + 多根兜底 + roots 回落 + 素材递归扫描 + 单测 + docs 校准）**已提交 68ebdec 并打 tag v1.5.7**；**（第十版状态 2026-10-12）1.5.8 已推送并打 tag v1.5.8（24cbbc5，后续 6043fbd 构建修复）；1.5.9~1.5.12（步骤 8 e2e A 档 + #8 渲染全链路）已提交推送 fork/baseline-snapshot（50052a3/890a648/bcc413d/1935be0/3d0ca67/c532adc/71403e1/333ed77/800eb4c/de20d03/d63d68e/f74ec54 + a20cc6b/4dcdb26），**v1.5.12 已于 2026-10-12 第二十六轮打注解 tag 并推送（落于文档对齐收口提交 ad77266）**；当前工作区未提交项为第二十六轮 P3-7/P3-9 收口批次（3 份新测试 + 门禁上调 + 台账回写，**已于 2026-10-12 提交推送 `a821f4d`，工作区干净**）；**（第十一版状态 2026-10-13）1.6.x 全部批次已提交并推送 `fork/baseline-snapshot`（`baseline-snapshot` 分支；远端 fork = hgmmm25/Fnos.VideoConversion，另有 origin = zdongno01/... 非推送目标），推送链 `1e622f0` → `22035c6` → `b1a0d93`(tag v1.6.0) → `6f80233` → `2f5c4e7` → `649d079` → `fe06e52` → `b007a3d`(tag v1.6.4) → `ceeb1b2` → `5166cbe`(tag v1.6.5)——三个版本均已打注解 tag；台账轮次对应 IMPROVEMENT_LOG §十九=第二十七轮（v1.6.0 + FVCS v1.3.0）、§二十=第二十八轮（v1.6.4 批次 A+B+C 收口 + v1.6.5 漂移修复与校验加固，含本轮文档对齐补记）；本轮为文档回写，未执行 git add/commit/push/tag，也未运行构建** |
| 文档 | **双 docs 布局已收口**：根仓库 `docs/` 7 份台账（混乱报告 / 项目现状与开发步骤 / IMPROVEMENT_LOG / TASK_REFERENCE / 项目分析与改进方向 / UI设计优化方向 / SECURITY；**第十一版实测目录共 9 份 = 原 7 份台账 + 1.6.x 方案稿 2 份**：`FVCC_视觉更新方案_v1.6.0.md`、`FVCC_任务页视觉改进整合方案_v1.6.1.md`——后者文件名 v1.6.1 为**方案稿编号**非交付版本号，见 §2.10）+ `FVCC/docs/` 7 份（第十一版实测数量不变；API_CONTRACT / DESIGN / PRODUCT / P2-1 实施记录 / P2-2 细化细则 / SECURITY（2026-09-21 补写）/ STORE_PERSISTENCE_ROADMAP（方案稿））——PRODUCT.md / DESIGN.md 已从 ui-src/ 迁入 FVCC/docs/，原"两处文档根"问题解决；WebVideoEditor_Design/ 01-10 规格文档仍入库。**第十一版对齐口径**：仅本混乱报告 + 项目分析两份长台账回写至 1.6.5，其余文档 1.6.x 对齐**本轮未实测**（§4.4） |

目录结构（根目录，2026-09-20 晚实测；2026-10-12 第十版行内更新；2026-10-13 第十一版行内更新）：
```
FVCC/
├── .gitignore                            ← 已建立（*.exe/*.fpk/node_modules/temp/logs/0字节残留；a20cc6b 补 FVCS 凭据/密钥/运行库忽略）
├── README.md                             ← 版本表已更新至 1.5.12 + 版本线摘要（2026-10-12 文档全面对齐轮；设计规格速查链接修复 ../../）；1.6.0~1.6.5 是否已补入版本表：本轮未实测
├── archive/fpk/                          ← 历史版本包完整归档 v1.1.0~v1.5.3（2026-09-21；根目录已无 fpk/exe 堆积）
├── credentials.db / master.key / secret.key / locks.json / tasks.json / history_tasks.json / config.enc.json  ← 运行时数据（按禁删原则保留原位；task.db 仅 archive/ 下 12KB 备份，为 FVCS 侧运行产物）
├── ICON.PNG / ICON_256.png / manifest    ← fnOS 应用清单（UTF-8 正常，version=1.6.5，2026-10-13 实测）
├── app/          ← fnOS 应用打包目录（字体 latin 子集 + lightweight-charts 异步 chunk）
├── cmd/          ← fnOS 生命周期回调脚本（install/config/uninstall/upgrade）
├── config/       ← privilege / resource 权限声明
├── docs/         ← 7 份：API_CONTRACT.md（1.5.12 复核，helper 真源路径 + /api/store/* 接口段登记）、DESIGN.md（+复核头）、PRODUCT.md、P2-1_后端分层_阶段A实施记录.md（阶段 C 哈希回填 009e2f8）、P2-2_细化细则.md（决策门定案关闭）、SECURITY.md（2026-09-21 补写，2026-10-12 清 AIGC 残留 + snapshot.restore 审计 + 根校验跨端同规则）、STORE_PERSISTENCE_ROADMAP.md（v1.5.8 已落地 + SQLite 载体注记）。第十一版（2026-10-13）：份数实测不变；各份是否已补 1.6.x 内容**本轮未逐份复核**（工作区存在对 DESIGN / SECURITY / ROADMAP 的未提交改动，属其他回写轮遗留，本报告不代为宣称已对齐）；另根仓库 docs/ 实测新增 2 份 1.6.x 方案稿（视觉更新方案_v1.6.0 / 任务页视觉改进整合方案_v1.6.1）
├── logs/         ← 运行时日志（已入 .gitignore）
├── server/       ← Go 后端（internal/ 12 包 + store/model 子包，第十一版实测 110 个 .go = 58 实现 17,197 行 + 52 测试 9,183 行（第十版记载 107 个 / 49 测试）；根包仅剩 main.go 274 行；store 已迁 SQLite：sqlite*.go + P3-5 backup/migrate/restore + fvcc.db 运行时数据库）
├── temp/         ← 2026-09-21 已清理 18 项（打包 stage fpk_stage* 15 目录 + fvcc-win-debug.exe + vet_err.txt，释放约 560.3MB）；第十版记载"仅剩 logs/（9KB）"；第十一版目录级核对可见 fpk_stage/ + logs/ + verify/（体积未测、已被 .gitignore 忽略，属本地构建件回潮观察，见 §2.7）
└── ui-src/       ← 前端源码（第十一版实测 33 个 TS 文件 11,474 行（上一记载 11,257 行）；src/lib/ 4 件套 + ui.ts listSkeleton 泛化组件（1.6.4 批次 C）；pages/editor/ 14 模块）
```

---

## 2. 分维度评价（含具体证据）

### 2.1 代码组织与分层 —— 2/10（已分层且持续细化）

**现状（P2-1 已落地并继续演进，2026-10-13 第十一版实测）**：`server/internal/` 12 包（api / edl / media / node / pathutil / protocol / remote / scheduler / security / store / version / ws，v1.5.8 增补 `pathutil`；第十一版按目录逐项核对为 12 包，与第十版记载一致）+ store/model 子包共 **110 个 .go 文件（58 实现 + 52 测试）**，根包仅剩 `main.go`（274 行，入口与装配、优雅退出）。1.6.x 期间无新增包、无再拆分（第十一版按包目录核对）：结构变化仅 `internal/version/version_test.go` 加固（1.6.5）与 `version.go` 注释修正（实现行数 +6，17,191→17,197）。行为零回归（实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md`，阶段 A/B/C 全部完成、8 个 shim 兼容文件已内联删除）。

**1.4.8/1.4.9 新增拆分（2026-09-20 晚实测）**：
- `internal/media/` 新增 `stream_media.go`、`roots.go`——预览流媒体与授权根从 api/ 下沉至 media 域；
- `internal/api/` 新增 `aliases.go`、`config.go`、`hashutil.go`、`security_roots.go`、`trash_handlers.go`（回收站 handler 从 store/trash.go 拆出）；
- `internal/edl/` 扩展为 4 文件（新增 `edl_support.go`、`export.go`、`audit_hook.go`）；
- `internal/store/trash.go` 收窄为纯存储（78 行），handler 侧迁至 api/trash_handlers.go；
- 新增测试 `render_progress_test.go`、`model_convert_test.go`。

**积极面**：分层后职责边界清晰（api 13 实现文件 / edl 4 / media 7 / remote 2 / scheduler 1 / security 7 / store 4 / ws 2 / protocol 3 / node 1 / version 1——**1.4.x 实现文件口径的历史快照，第十一版未按"实现/测试分列"重测各包，故不覆盖原值**）；handler 按领域分文件；`RenderDispatcher` / `MediaProber` / `ProxyProber` 接口注入保持；1.4.8/1.4.9 的继续拆分证明分层模式可承载后续演进。

### 2.2 命名一致性 —— 5/10（轻度混乱，无新增）

**证据 1：JSON 字段大小写风格混杂（未变）。** 协议层既有 camelCase（`authKey`、`customFfmpeg`、`videoCache`），又有 snake_case（`server/internal/remote` 包 `progressPush` 结构体：`task_id`、`out_time_ms`、`total_ms`、`seg_total`）。API_CONTRACT.md 已定"协议字段一律 camelCase（FVCS 对等 WS 报文允许 snake_case 双风格兼容）"，存量代码未迁移。

**证据 2：注释任务编号两套语义（已登记）。** `// P2-1` 在 `handler_metrics_test.go` 指 Prometheus 指标端点，与本报告 P2-1（后端分层）语义不同；已登记于 docs/TASK_REFERENCE.md §2.1（编号漂移登记）。

**证据 3：新旧错误契约并存（过渡期）。** `ui-src/src/api.ts` 声明统一错误契约 { ok:false, code, msg, detail? }，request() 保留 `body.msg || body.error` 兜底——API_CONTRACT.md 明确"旧格式过渡期兜底，新代码只读 msg"。

**证据 4（已落地，P2-7）**：测试文件命名已统一为 `handlers_<domain>_test.go` 风格（git mv 保留历史）。

**第十一版复核（2026-10-13）**：无新增命名混乱证据——1.6.4 / 1.6.5 未引入新的字段命名风格（`models.go` 的 `speed,omitempty` 属既有 camelCase 协议字段，WS 报文 `TaskUpdateFullMsg` 同风格），新增前端 URL 开关 `?fx=stripes|breathe` 沿用既有小写 query 参数写法；原四处证据（协议双风格 / 编号两套语义 / 新旧错误契约 / 测试命名）状态未变，**维持 5/10**。

### 2.3 重复代码 —— 3/10（两大重复源已消灭，剩余中等）

- ✅ **updateProfile 59 块手写映射 → 反射白名单**（internal/protocol/applyjson.go，P1-1 落地）；实测 `if v, ok := updates[` = 0。
- ✅ **SMB 路径校验复制 3 处 → 统一 helper**（internal/security/smb_validate.go，P1-2 落地；5 处调用点单行化）。
- ✅ **前端跨页面样板**（P2-2 收口）：crudActions / scrollPos / useListPage / formBuilder 4 件套接入 6 页，CRUD/滚动/订阅/表单样板残留 0。
- ⚠️ **仍在**：VideoInfo 30+ 字段逐字段复制出现 2 处（`doScanDirectory()` cache→VideoInfo 赋值块与 `UpsertVideoCache` 调用参数块），字段新增时两处必改。

**第十一版增量（2026-10-13，1.6.4 任务页批次）**：`ui.ts` 新增 **`listSkeleton` 泛化组件**（骨架屏共享化，替代页内各写一份），`render` / `updateOverview` 增加 **id/顺序与计数签名守卫**（消除每 tick 无条件重排/重绘的重复路径），WS `applyTaskUpdate` 的 `speed` 合并进单一更新点而非另起分支。⚠️ **但收敛尚未完成**：`listSkeleton` 目前仅 tasks 页切换，**其余页面骨架迁移未做**（登记 §4.4）；VideoInfo 逐字段复制 2 处仍在（未变）。证据为"改善 + 残留同量"，**维持 3/10**（本报告混乱度越低越好；未据此下调，因残留项数量与口径未变）。

### 2.4 依赖管理 —— 5/10（前端打包侧已瘦身，后端间接依赖未变）

- **后端**：go.mod 直接依赖 gin + websocket + **modernc.org/sqlite v1.14.0**（P0-2 迁移引入，纯 Go 无 CGO）；indirect 链含与业务无关的 `go.mongodb.org/mongo-driver/v2`、`quic-go/qpack`、`bytedance/sonic` 等（gin 生态连带）与 sqlite 驱动的纯 Go 转译依赖（modernc.org/libc / cc/v3 等，属合理功能依赖），已执行 `go mod tidy` 复核零变化、无进一步精简空间。
- **前端**：`app/ui/assets` 保持瘦身（10 个 latin 字体 woff/woff2 + index css/js + lightweight-charts 异步 chunk）；`node_modules` 本体不进入构建产物（本轮实测 temp/ 下出现 node_modules 全量拷贝，属清理问题而非打包问题，见 §2.7）。
- **积极面**：package-lock.json 与 go.sum 存在；scripts/check-versions.ps1（版本一致性校验）与 check-coverage.ps1（覆盖率门禁）保持。

**第十一版补充（2026-10-13）**：v1.6.0 同批的 FVCS 1.2.6→**1.3.0** 把 Fyne 托盘 + 设置窗口纳入构建（`build.ps1 -Target FVCS-UI`）并新增 `FVCS/pkg/fynetheme`——**FVCS 侧依赖面扩大**（GUI 框架），但不进入 FVCC 打包产物；FVCC 侧 `go.mod` / `package.json` 依赖本轮**未重跑 `go mod tidy` 复核**（第十一版为纯文档回写，未运行构建），沿用第六版"零变化、无精简空间"结论。**维持 5/10**。

### 2.5 配置与硬编码 —— 2/10（版本单一来源推进至 1.6.5：漂移事件已根治为"漂移即红"）

- ✅ **凭据 AES-GCM 加密落盘**（internal/security/crypto.go，P0-1）：主密钥 0600 落盘 `<dataDir>/secret.key`，`enc:v1:` 前缀 + 旧明文兼容迁移 + API 脱敏；原 models.go 过时注释已清理无残留。
- ✅ **manifest 元数据正常**（P1-4）：UTF-8 无乱码，version=1.6.5（2026-10-13 实测；第十一版另核验交付物 `FVCC/fvcc.fpk` 重打后解包，包内 manifest `version = 1.6.5`，`app/fvcc` 二进制内 `1.6.x` 版本串仅出现 `1.6.5` 一次，`1.6.4`/`1.6.0` 命中数为 0）。
- ✅ **版本单一来源**（P1-4）：version.go go:embed VERSION，version_test.go 自动校验三处一致（2026-10-13 实测 manifest / VERSION / package.json 均为 1.6.5；真机核验：用户确认 NAS「设置」页显示 1.6.5）。
- 🔴→✅ **v1.6.4 真实漂移事件 + v1.6.5 根治（第十一版登记，本维度最重要的证据变化）**：v1.6.4 出包只 bump 了 manifest + package.json，**漏改 go:embed 的 `internal/version/VERSION`（仍 1.6.0）**，导致已推送包的 `/api/info` 与「设置」页显示 1.6.0。根因不是"忘了改"而是**校验长期失效**：旧 version_test.go 把相对路径写成 `../../manifest`（从 `internal/version` 上溯 2 层实为不存在的 `FVCC/server/manifest`，正确是 4 层到 `FVCC/`），读不到即 `t.Skipf`，于是 Windows 本机 `go test ./...` 长期"**全绿但零校验**"，漂移只在 Linux CI 暴露。v1.6.5 处置：①新增 `findRepoRoot`（`runtime.Caller` 测试源文件目录 + `os.Getwd()` 双起点向上搜索，判定标志为目录内同时存在 `FVCC/manifest` 与 `FVCC/ui-src/package.json`；不用 `.git`，因 tarball/CI 检出未必带）；②定位失败 / 读取失败 / 字段缺失 / 版本不一致**四类一律 `t.Fatal`，彻底移除 `t.Skipf`**（漂移即红）；③新增 `TestAppVerIsNotPinnedLiteral`——扫本包非测试 `.go` 文件，出现 `"x.y.z"` 硬编码版本字面量即判红（同类历史缺陷见 FVCS `hello.go` 硬编码 `1.2.0`）；④`scripts/check-versions.ps1` 去除硬编码 `D:\Fnos.VideoConversion`，改由 `$PSScriptRoot` 推导仓库根、支持 `-RepoRoot` 覆盖、定位失败判红（主机环境事实：本机 PowerShell 5.1 变量名区分大小写，`$BaseDir` 有值而 `$BASE_DIR` 为空，故参数命名避开该歧义）；⑤`version.go` 注释修正——原"同步修改 `../manifest`"的相对路径本身就是错的（正是它误导了上一轮只改两处），已按仓库根写清三处真源与层数。发版路线取舍：**发新补丁号 1.6.5 而不强推重指 `v1.6.4` tag**，硬约束是 fnOS 仅接受严格更高版本的覆盖安装，同版本号重打根本装不进 NAS。
- ✅ **文档版本表已全面同步（2026-10-12 第十版收口）**：根 README.md 已对齐 v1.5.12 / FVCS v1.2.6（4dcdb26）；FVCC/README.md 版本表 → 1.5.12 + 版本线摘要；根 BUILD.md 版本同步表 → 1.5.12；docs 内外滞后清零（P3-4 剩余项关闭）。**第十一版（2026-10-13）**：本混乱报告与 `docs/项目分析与改进方向.md` 两份长台账对齐至 1.6.5；其余文档（根 README / BUILD、FVCC/README、FVCC/docs 7 份、项目现状与开发步骤）的 1.6.x 版本表**本轮未实测**，登记为开放项见 §4.4。
- ⚠️ **仍在**：默认值散落硬编码（`SchedulerIntervalSec=1`、`ChunkSizeMB=4`、`HistoryLimit=1000`、scheduler.go `chunkSize: 4*1024*1024` 与设置项语义重复、main.go 开发模式 `127.0.0.1:8088`）。

**第十一版评分说明**：本维度证据**一增一减**——减项是 1.6.4 确曾发生真源漂移且被静默放过（暴露"全绿但零校验"这一长期隐患）；增项是 v1.6.5 把校验从"Windows 静默 SKIP"根治为"漂移即红 + 反硬编码字面量守卫 + 主机无关脚本"，**消灭了一类会长期潜伏的一致性缺陷**。量纲已处低位（2/10 为该维度历史最优，无更低空间），且残留硬编码默认值清单未变，故**维持 2/10**，改善计入 §3 综合。

### 2.6 文档与注释 —— 2/10（docs 布局收口，但 AIGC 残留出现新点）

**已兑现**：
- **规格文档入库 + 速查表**（P1-3）：WebVideoEditor_Design/ 01-10 + FVCC/README.md 引用速查表，注释悬空引用问题根除；
- **docs 布局收口（本轮改善）**：PRODUCT.md / DESIGN.md 已从 ui-src/ 迁入 `FVCC/docs/`，原"文档位置不统一（两处文档根）"问题解决；当前布局 = 根仓库 docs/ 7 份台账 + FVCC/docs/ 7 份技术文档（2026-09-21 补写 SECURITY.md 与 STORE_PERSISTENCE_ROADMAP.md）；
- 根 README.md、BUILD.md、API_CONTRACT.md（统一错误契约 + code 映射表）保持；
- 注释密度与质量保持（每个 handler 有职责注释，新文件均带"Px-x 对应混乱报告条目"落地注释）。

**残留混乱点（2026-09-21 更新）**：
1. ✅ **AIGC frontmatter 新残留已清除（P2-6 恢复清零，2026-10-12 保持）**：`docs/FVCC_项目现状与开发步骤.md` 等 3 份文档的 AIGC base64 块已于 2026-09-21 清理（P3-4）；本轮补清 `FVCC/docs/SECURITY.md` frontmatter 与"内容由AI生成"尾注、`docs/FVCC_项目现状与开发步骤.md` 尾部同款声明残留，全库 AIGC 关键词命中仅剩台账登记语句本身；
2. ✅ 根 README.md 已对齐 **v1.5.12 / FVCS v1.2.6**（2026-10-12，提交 4dcdb26 补登 1.4.9 以来能力）；根 BUILD.md 版本同步表已更新至 1.5.12（P3-4 清零）；
3. **新文档与混乱报告存在内容重叠**：`docs/FVCC_项目现状与开发步骤.md` 与混乱报告 §1 概览/目录树大量重复，建议明确两文档定位（台账 vs 现状指引）避免双轨漂移。

**第十一版（2026-10-13）状态**：① 版本线文档新增**易错点**并已在本报告 §2.10 固化——`1.6.1` / `1.6.2` / `1.6.3` 只是批次开发过程中的本地中间 bump，**从未进入 git 历史、未出包、未打 tag**（git 核验 manifest 版本序列为 `1.6.0 → 1.6.4 → 1.6.5`），方案文档 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md` 文件名里的 "v1.6.1" 是**方案稿编号**而非交付版本号，故"1.6.1~1.6.4 版本线"的写法一律视为错误表述；② 本轮为**叙述性长台账对齐轮（第二十八轮）**，仅本混乱报告 + `docs/项目分析与改进方向.md` 两份对齐 1.6.5，**其余文档的 1.6.x 对齐情况本轮未实测**（§4.4 登记，不作为已落地表述）；③ AIGC 残留、docs 布局、注释密度三项证据均未变，**维持 2/10**。

### 2.7 遗留/废弃代码 —— 8/10（temp 与根目录产物已清理，遗留少量无害占位）

**本轮（2026-09-21）已清理**：`temp/` 打包 stage 与调试残留 **18 项**已移入回收站，释放约 **560.3MB**：
- 打包 stage 残留：`fpk_stage/` + `fpk_stage_145/146/147/149/` 等 **15 个目录**（1.4.9 新增 fpk_stage_149，无 148——与版本包跳号无关，系构建目录命名）；
- 调试二进制：`fvcc-win-debug.exe`（34.19MB）；
- 0 字节空文件：`server/vet_err.txt`；
- 误构建嵌套目录：`FVCC/FVCC/`（含 app/fvcc，22.16MB）；
- `temp/` 现仅剩 `logs/`（9KB）保留。

**实测不存在（旧文档误记，已修正）**：`chrome-profile-9223/9224`、`cdp-fine-test.mjs` / `cdp-real-test.mjs`、`repro-data/`、`temp/secret.key`、`temp/locks.json`、`temp/node_modules`、`build*/commit*/fmt_out.txt` / `c2~c6.txt` 等均未发现。

**根目录（2026-09-21 已归档）**：本轮 6 个 fpk（`FVCC_v1.4.9~v1.5.3_fnos_x86.fpk` + `fvcc.fpk` 副本）已归档至根仓库 `archive/fpk/`，该目录现完整收纳 **v1.1.0~v1.5.3**；根目录已无 fpk / exe 物理堆积。实测修正：历史 fpk 非 10 个（v1.4.0~v1.4.8 早已归档）；根目录无 `fvcc.exe` / `fvcs-service.exe` / `task.db`（task.db 仅 archive/ 下 12KB 备份，为 FVCS 侧运行产物）。

**保留（按禁删原则）**：`credentials.db` / `master.key` / `secret.key` / `locks.json` / `tasks.json` / `history_tasks.json` 等运行时数据保留原位。

**已保持**：server/ 与 ui-src/ 根下 0 字节文件 = 0（2026-09-21 实测 server/ 根曾存 `build_err.txt` 0 字节空文件，登记 §4 P3-9；**2026-10-12 第二十六轮已删除，归零**）；logs 内 0 字节残留已入 .gitignore；FVCS/ 侧 build_check.txt / _cgo_full.txt 已移入根 archive/（`_build_check.txt`、`fvcs-service.exe.bak_preupgrade`），FVCS 根已无 0 字节文件。

**第十一版（2026-10-13）复核**：
- 根 `archive/` 的项数与体积**本轮未重新扫描**，沿用第六版口径（archive/fpk 完整 v1.1.0~v1.5.3）；
- `FVCC/temp/` 做了一次**目录级**核对（未测体积、未逐项清点）：现可见 `fpk_stage/`、`logs/`、`verify/` 三项，与第十版记载"仅剩 logs/（9KB）"相比有**本地构建件回潮迹象**；因 `FVCC/.gitignore:12` 已忽略 `temp/`，其内容不进入仓库、**不构成仓库债**，且本轮未见第五版登记过的敏感残留（node_modules 全量 / chrome-profile / secret.key），故**不据此改分**，仅作观察记录；如需处置，按 P3-2 既有口径另行清理轮执行；
- 直接核到的新增小项：`FVCC/.commandcode/`（工具产物目录）**既未被 git 跟踪、也未进 `.gitignore`**（`git check-ignore` 无命中、`git status` 显示 `?? FVCC/.commandcode/`），属仓库治理待裁决项，登记 §4.4；
- FVCC/ 根另见 v1.5.4~v1.5.7 / v1.5.11 五个 fpk 与 `fvcs-service.exe`、`fvcc.fpk` 本地件，均在 `.gitignore` 覆盖范围内（`*.exe` / `*.fpk`），与 §1 "1.5.4~1.6.5 构建产物为 gitignore 本地件"口径一致，**不构成仓库债**；此处澄清：第八版"根目录已无 fpk/exe"的表述适用范围是**仓库根**与当时归档动作，非 FVCC/ 子目录的 gitignore 本地件；
- 结论：除 `.commandcode` 一项新增待裁决、temp 目录级观察外，无仓库债层面回潮证据，**维持 8/10**。

### 2.8 测试情况 —— 2/10（优秀且持续增强；规模推进至 52 个测试文件，覆盖率本轮未重跑）

- **规模**：第十一版实测 **52 个 `_test.go` / 9,183 行**（演进链：第七版 43 个 / 6,853 行（2026-09-21 实测）→ 第十版 49 个 → 第二十六轮 52 个，+logger / smbshare / media 三份 helper 测试）；历史条目保留：SQLite 迁移配套 `sqlite_crypto_test.go` / `sqlite_import_test.go` / `sqlite_test.go`（覆盖加密轮转、存量导入、读回主路径），另含 P2-1 分层期 `render_progress_test.go`、`model_convert_test.go`、P3-5 备份/迁移/回滚配套、#8 `render_poll` 等；**1.6.5 新增版本守卫用例**（`internal/version/version_test.go`：`findRepoRoot` 双起点定位 + 四类失败一律 `t.Fatal` + `TestAppVerIsNotPinnedLiteral` 反硬编码字面量扫描）。
- **基建**：接口注入（RenderDispatcher/MediaProber/ProxyProber）专为单测替身设计；main.go 与单测共用构造点。
- **门禁**：CI 已建立（P2-3，.github/workflows/fvcc-ci.yml：go vet + test -race + 覆盖率 ≥60% + 前端 check:design/tsc/build）；`scripts/check-coverage.ps1` 强制，口径修复（测试目标 `'.'`→`'./...'`，第十七轮）后实测基线 56.7%→59.0%→**60.7%**，门槛 55%→**60%**（CI `-Race -Threshold 60`）；**第十一版口径：覆盖率本轮未重跑，引用值统一标注"第二十六轮实测 60.7%"**。
- **1.6.4 门禁侧事件（第十一版登记）**：`check-design` R3 规则曾误报 CSS 属性 `box-shadow`（规则本意是查内联样式），已按前缀排除 `-` / `:`；任务页选中态 inset 阴影按方案 §2.3 裁决为合法。属规则精度修正，非新增债。
- **第十一版本轮验证（2026-10-13，实跑）**：`go vet ./...` 通过；`go test ./internal/version -count=1` 与全量 `go test ./...` 绿；`npm run build`（`check:design` + `tsc -b` + `vite build`）绿；`check-versions.ps1` 绿（含从任意 CWD 与 `-RepoRoot` 判红两端验证）。
- 测试随 P2-1 迁移至 internal 各包，平铺问题已消除。

### 2.9 安全与健壮性 —— 2/10（五硬伤保持清零，新增敏感残留观察）

**保持（首版 5 项硬伤 → 0 项未决）**：
1. ✅ 凭据 AES-GCM 加密（internal/security/crypto.go + API 脱敏）；
2. ✅ 删除回收站化（internal/store/trash.go 纯存储 + api/trash_handlers.go，移入 `<授权根>/_trash`，同名时间戳后缀）；
3. ✅ updateProfile 白名单遗漏修复（applyjson.go 反射白名单）；
4. ✅ 破坏性操作审计闭环（internal/security/audit.go + 8 处调用点，清日志/清缓存/删除视频/服务器/方案/任务/历史/清空回收站）；
5. ✅ git 版本控制（可回滚、可取证）。

**既有防线保持**：PathValidator 四层路径校验、NoRoute 兜底、requireAdmin 门禁、网关身份透传（X-Trim-* + 写操作 admin）、securityHeaders、崩溃恢复（非终态任务重置 QUEUE）、限流（ratelimit / ws_limit / stream 单文件 4 路 / 全局 64 路）。

**观察项（1.4.8/1.4.9 期间登记，2026-09-21 关闭）**：
- ✅ `temp/secret.key`：2026-09-21 复核实测不存在（temp 清理时未见该文件），观察项关闭；与 §2.7 同源登记为 P3-2 已处置。

**第十一版（2026-10-13）**：五硬伤清零状态保持，既有防线（PathValidator / requireAdmin / 网关身份透传 / securityHeaders / 崩溃恢复 / 限流）无变动证据也未重测；新增健壮性收益属**一致性防线**而非运行时安全，已在 §2.5 记账——版本一致性校验由"Windows 静默 SKIP（全绿但零校验）"改为"漂移即红"，并加硬编码字面量守卫，堵住的是"发布包元数据与真源长期不一致却无人发现"这一类缺陷（1.6.4 已实际发生一次）。另：1.6.4 批次 B 的"视觉改进不得增加 WS tick 重绘"硬约束与签名守卫，属前端热路径健壮性（详见新增 §2.10）。**维持 2/10**（历史最优档，量纲无更低空间）。

### 2.10 前端信息层级与热路径健壮性（第十一版新增小节，2026-10-13）

> 本节是第十一版为 1.6.4 任务页三批次新增的登记位。**不进入 §3 评分表的九维度序列**（避免改动首版~第七版历史列），其证据已计入 §3 综合判定；本节自身不单独打分。

**v1.6.4（tag b007a3d，含 fe06e52 代码，7 文件 +197/−44；方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`，GLM5.3F / Step5P / Gemini-3.8F 三方外部模型评审整合）三批次**：

- **批次 A（信息层级 + 状态反馈）**：`errorMsg` 独立成行并带 `title`（错误呈现不再挤在副标题里）；拆除 `sub` 上 `truncate` 与 `flex-wrap` 的冲突；首屏骨架接入 `listSkeleton`；批量三键零选中禁用 + 刷新 pending；`.task-selected` 选中即时高亮；活动卡左侧 `signal` 静态指示线；`getPhaseStyle` 补全状态文案且 ERROR 走红条；操作区去掉 `w-[200px]` 硬宽；`serverName` 中性标签去撞色；hover ring 与 servers 页对齐。
- **批次 B（热路径性能）**：进度条 `width` → `transform: scaleX(--p)` 走合成层；`render` 增加 id/顺序签名重排守卫；`updateOverview` 增加计数签名守卫；`scrollPos` 接线（主页与抽屉独立 key 不互串）。硬约束：**视觉改进不得增加 WS tick 重绘**。
- **批次 C（速率 + 实验项）**：WS `applyTaskUpdate` 合并 `speed`（链路核实为 `models.go` `speed,omitempty` + `TaskUpdateFullMsg`）后转正，进度文案实时带倍速后缀；`#/tasks?fx=stripes` / `?fx=breathe` URL 开关（**默认关**，待真机裁决转正或弃用）；`ui.ts` 新增 `listSkeleton` 泛化组件。

**两处由用户真机实测抓出的缺陷（第十一版如实登记，属"改进引入的回归"证据）**：① 空任务库下骨架常驻"出不来"——初版退出条件写成 `tasks.length>0`，改为**首次成功 render 即置 `loaded`**，空列表才正常落到 `emptyState`；② `check-design` R3 误报 CSS 属性 `box-shadow`（见 §2.8）。

**评价**：1.6.4 的收敛方向正确（错误可见性、选中即时反馈、每 tick DOM churn 走合成层 + 签名守卫），且带真机验证闭环；但仍有三项刻意未做/待裁决（Filter Chips 状态过滤、`?fx=` 实验动画转正、其余页面骨架迁移 listSkeleton），已登记 §4.4。本节证据与 §2.5 的 v1.6.5 校验根治共同构成本轮综合评分 2.4→2.3 的依据。

---

## 3. 量化混乱度评分（第七版成表 2026-09-21；末列为第十一版 2026-10-13 现值）

| 维度 | 首版 | 二版 | 三版 | 四版 | 五版 | **六版** | **七版** | **十一版** | 一句话结论（变化） |
|---|---|---|---|---|---|---|---|---|---|
| 代码组织与分层 | 7 | 6 | 2 | 2 | 2 | **2** | **2** | **2** | 分层稳定（第十一版实测 internal 12 包 / 110 个 .go；八~十版记载 12 包 / 107 个 .go 全绿）；1.6.x 无新增包、无再拆分 |
| 命名一致性 | 6 | 6 | 5 | 5 | 5 | **5** | **5** | **5** | 无新增（1.6.4/1.6.5 未引入新命名风格）；编号歧义已登记（TASK_REFERENCE §2.1，P0-2 双语义补录） |
| 重复代码 | 7 | 5 | 3 | 3 | 3 | **3** | **3** | **3** | 1.6.4 新增 listSkeleton 泛化组件与签名守卫，但仅 tasks 页切换、其余页面骨架未迁移；VideoInfo 逐字段复制 2 处仍在 |
| 依赖管理 | 5 | 5 | 5 | 5 | 5 | **5** | **5** | **5** | modernc.org/sqlite 直接依赖保持；FVCS 1.3.0 引入 Fyne 托盘/设置窗口属对端依赖面；FVCC 依赖本轮未重跑 tidy 复核 |
| 配置与硬编码 | 7 | 4 | 3 | 2 | 2 | 2 | **2** | **2** | 版本单一来源推进至 **1.6.5**；1.6.4 曾真源漂移（VERSION 仍 1.6.0）+ 校验长期静默 SKIP，1.6.5 根治为漂移即红；量纲已在低位 |
| 文档与注释 | 5 | 3 | 2 | 2 | 2 | 2 | **2** | **2** | docs 布局收口；AIGC 保持清零；1.6.x 版本线含易错点（1.6.1~1.6.3 未进 git）已固化；其余文档 1.6.x 对齐本轮未实测 |
| 遗留/废弃代码 | 8 | 6 | 6 | 6 | 5 | **8** | **8** | **8** | 历史：temp 18 项清理（约 560.3MB）+ fpk 归档 v1.1.0~v1.5.3；第十一版新增 `FVCC/.commandcode/` 未跟踪未忽略待裁决，temp/archive 本轮未重扫 |
| 测试情况 | 3 | 2 | 2 | 2 | 2 | **2** | **2** | **2** | *_test.go 43→49→**52**（9,183 行）；覆盖率本轮未重跑，沿用第二十六轮实测 60.7%（门禁 60%）；1.6.5 补版本守卫用例 |
| 安全与健壮性 | 5 | 2 | 2 | 2 | 2 | **2** | **2** | **2** | 五硬伤清零保持；新增一致性健壮性（漂移即红 + 反字面量守卫）与 WS tick 重绘硬约束 |
| **综合** | **6.5** | **4.3** | **2.8** | **2.4** | **2.8** | **2.4** | **2.4（轻度混乱）** | **2.3（轻度混乱）** | 九维度分值全部维持原分（多数已在历史最优档，量纲无更低空间）；综合 2.4→**2.3** 的依据是 §2.5 的 v1.6.5 校验根治（消灭一类长期潜伏的一致性缺陷）+ §2.10 的 1.6.4 信息层级/热路径收敛 + 剩余 P3 债务收敛为两项 |

等级判定：**B / 轻度混乱**。工程卫生基本面保持（P0/P1/P2 全部落地、1.5.3 三处版本一致、docs 布局收口）；第五版登记的**物理清理回潮**与**文档同步滞后**已在前两轮（2026-09-21）全部处置。**第七版新增落地**：P0-2 SQLite 迁移（三个提交 a72b8ff / 9e122d5 / 5dd373e，modernc.org/sqlite v1.14.0 + 12 张实体表 + 存量 JSON 导入 + 读回主路径）、P1-1 事件驱动调度（eab7688，第十六轮漏登补记）、P3-1 提交收尾（tag v1.5.3）；store.go:19 TODO(P1) 定时备份/快照回滚延续（方案稿 STORE_PERSISTENCE_ROADMAP.md 已出）。当前剩余债务收敛为工程项（P3-5 store 备份/回滚代码落地 / P3-6 ffprobe 依赖 / P3-7 覆盖率上调）与文档校准（第十八轮进行中，工作区 5 文件未提交，P3-1 收尾待办有效）。（2026-10-11 第九版校正：P3-5 已随 v1.5.8 落地关闭，store.go TODO(P1) 已移除；1.5.5~1.5.7 已提交 68ebdec + tag v1.5.7。）**第十版（2026-10-12）**：步骤 8 e2e A 档五项缺陷 + #8 渲染四层全链路修复收口并真机验证通过（v1.5.9~1.5.12，已推送 fork/baseline-snapshot）；文档全面对齐轮完成，P3-4 清零；当前工作区未提交项仅剩本轮 docs 对齐；评分维持轻度混乱**第十一版（2026-10-13）**：版本基线 1.5.12 → **1.6.5**（配套 FVCS v1.3.0），交付物重打并解包核验、真机「设置」页显示 1.6.5；v1.6.0 视觉打磨 + v1.6.4 任务页三批次（信息层级/错误呈现/选中反馈 + 每 tick DOM churn 走合成层与签名守卫）+ v1.6.5 版本真源漂移修复与校验加固（静默 SKIP → 漂移即红 + 反硬编码字面量）；剩余 P3 债务收敛为 **P3-6（实际已闭）/ P3-8** 两项。九维度分值**全部维持原分**（多数维度已在历史最优档，量纲无更低空间；残留清单未变者不涨不降），综合 **2.4 → 2.3**，等级仍为 **B / 轻度混乱**。未纳入本轮事实的项（其余文档 1.6.x 对齐、temp 重扫、覆盖率重跑、渲染 Failed/Cancelled 分支、1.6.x 视觉线四项遗留）见 §2.6 / §2.7 / §2.8 / §4.4

---

## 4. 修改建议（P0/P1/P2 全部落地，压缩为状态汇总；P3 为本轮新增遗留债务）

### 4.0 历史整改状态汇总（P0/P1/P2 全部落地或收口）

| # | 建议 | 状态 | 落地证据（简） |
|---|---|---|---|
| P0-1 | 凭据加密落库 | ✅ 已落地 | internal/security/crypto.go（AES-GCM + 0600 + enc:v1: + 兼容迁移 + API 脱敏）；过时注释已清 |
| P0-2 | 清理工程残留 + .gitignore | ✅ 已落地 | .gitignore 已建；fpk_build_test2 / fpk_inspect / 0 字节残留已清；temp 18 项清理（释放约 560.3MB）与 fpk 归档（archive/fpk/ 完整 v1.1.0~v1.5.3）已于 2026-09-21 完成 |
| P0-3 | git 建立版本控制 | ✅ 已落地（剩余见 P3-1） | 上层仓库统一管理，baseline-snapshot 已推送 GitHub；分层前 tag v1.4.3 |
| P1-1 | 消灭 updateProfile 59 块手写映射 | ✅ 已落地 | internal/protocol/applyjson.go 反射白名单，实测 `if v, ok := updates[` = 0；另含事件驱动调度（提交 eab7688，见项目分析 §5 P1-1） |
| P1-2 | 抽取 SMB 校验 helper | ✅ 已落地 | internal/security/smb_validate.go，5 处调用点统一 |
| P1-3 | 文档落地消除悬空引用 | ✅ 已落地 | WebVideoEditor_Design/ 01-10 + 速查表 + 根 README/BUILD/API_CONTRACT |
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地（文档表滞后见 P3-4） | version.go go:embed + version_test.go 三处校验 + check-versions.ps1；manifest 1.5.12 正常（第十版记载）→ **1.6.5 正常且校验加固**（第十一版：v1.6.4 曾漏改 VERSION 引发真源漂移，v1.6.5 已改为一律 `t.Fatal`、移除 `t.Skipf`，并新增 `TestAppVerIsNotPinnedLiteral` 与 `$PSScriptRoot` 化的 check-versions.ps1，见 §2.5） |
| P1-5 | 依赖瘦身 | 🟡 部分落地 | app/ui 字体 latin 子集瘦身；go.mod tidy 复核零变化、无进一步精简空间 |
| P2-1 | 后端分层 internal | ✅ 已落地（2026-09-19） | 11 包 + store/model 子包，根包仅剩 main.go；阶段 A/B/C 完成、shim 已删；实施记录见 FVCC/docs/P2-1_后端分层_阶段A实施记录.md |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | src/lib/ 4 件套接入 6 页，样板残留 0；细则见 FVCC/docs/P2-2_细化细则.md。**延伸未闭环**：1.6.4 批次 C 在 ui.ts 新增 `listSkeleton` 泛化组件，目前仅 tasks 页接入，其余页面骨架迁移未完成（§4.4） |
| P2-3 | 建立 CI 流水线 | ✅ 已落地 | .github/workflows/fvcc-ci.yml + check-coverage.ps1（门槛 55%→**60%**，2026-10-12 P3-7 上调；**第二十六轮实测 60.7%**，第十一版未重跑覆盖率） |
| P2-4 | 统一 API 契约与命名 | ✅ 已收口 | API_CONTRACT.md + apierr.go + api.ts 适配兜底；`{error}` 裸格式 grep 0；remote snake_case 为 FVCS 对等协议段已登记边界 |
| P2-5 | 删除操作回收站化 | ✅ 已落地 | store/trash.go（纯存储）+ api/trash_handlers.go；`_trash` 保留相对路径 + 时间戳后缀 |
| P2-6 | 清理 AIGC 残留与任务编号注释 | ✅ 已收口（2026-09-21 恢复清零） | 存量 5+3 份文档已清；新文档 docs/FVCC_项目现状与开发步骤.md 的 AIGC frontmatter 已补清（2026-09-21） |
| P2-7 | 测试文件命名规范化 | ✅ 已落地 | git mv 为 handlers_<domain>_test.go 风格 |
| P2-8 | 破坏性操作审计闭环 | ✅ 已落地 | internal/security/audit.go + 8 处调用点 + 单测 |

### 4.1 / 4.2 落地前快照（已删除）

> P2-1 后端分层与 P2-2 前端组件化的**落地前快照、分阶段计划、目标架构、迁移映射、风险表**已随落地完成删除，不再保留于本报告；实施记录与验收依据分别见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` 与 `FVCC/docs/P2-2_细化细则.md`（Checklist 全勾）。保留的开放决策已收口：P2-2 §4.2.3 决策门（组合器方案去留 / Svelte 5 试点）随 **P1-2 前端收敛定案（方案 A 维持零框架，2026-10-11，见 IMPROVEMENT_LOG §十三）**关闭为远期绑定项——仅当"时间线双栏/多轨 `{videoTracks,audioTracks}`"立项时一并评审。

### 4.3 当前遗留债务（P3 组，本轮新增，按优先级）

> **第十一版（2026-10-13）收敛结论**：P3 组九项中 P3-1 / P3-2 / P3-3 / P3-4 / P3-5 / P3-7 / P3-9 均已关闭，**实际未决仅剩 P3-6 与 P3-8 两项**——其中 **P3-6 经 NAS `/info` 实测 `ffprobe:true` 已"实际已闭"**（仅剩台账正式勾选的收尾动作，本轮不做该勾选之外的处置），P3-8 为远期决策门（多轨工程立项时一并评审）。1.6.x 视觉线遗留与治理小项**不占用 P 编号**（避免与 TASK_REFERENCE 编号族冲突），单点登记于新增 §4.4。

| # | 建议 | 优先级 | 说明 / 证据 |
|---|---|---|---|
| P3-1 | **提交 git 未跟踪改动并打 tag** | 🔴 高 | ✅ 已解决（2026-09-21 第十七轮）：1.5.3 全部提交并打 tag（含 SQLite 迁移三提交）；第十八轮文档校准已随 85f4c7a 收口；**（2026-10-12 第二十六轮收口）1.5.4~1.5.12 各批次均已提交推送 fork/baseline-snapshot，v1.5.12 已打注解 tag 并推送（含 2026-10-12 文档全面对齐轮 ad77266）**；提交纪律延续。**（第十一版 2026-10-13）**1.6.x 各批次亦已提交推送 fork/baseline-snapshot 并打注解 tag（v1.6.0=b1a0d93 / v1.6.4=b007a3d / v1.6.5=5166cbe，链见 §1 版本控制行）；本轮为文档回写，未执行任何 git 变更 |
| P3-2 | **temp/ 清理回潮（549.7MB）** | 🔴 高 | ✅ 已处置（2026-09-21）：temp 下 15 个打包 stage + fvcc-win-debug.exe 等 18 项移入回收站释放约 560.3MB；实测不存在 node_modules 全量拷贝、chrome-profile-9223/9224、repro-data、cdp-*-test.mjs、temp/secret.key、temp/locks.json；temp/ 仅剩 logs/（9KB） |
| P3-3 | **根目录产物物理清理** | 🟡 中 | ✅ 已处置（2026-09-21）：6 个 fpk（v1.4.9~v1.5.3 + fvcc.fpk 副本）归档 archive/fpk/（完整 v1.1.0~v1.5.3）；误构建嵌套目录 FVCC/FVCC/（22.16MB）已清；task.db 实测不存在（仅 archive/ 下 12KB 备份，为 FVCS 侧产物）；credentials.db / master.key / secret.key / locks.json 按禁删原则保留原位 |
| P3-4 | **文档同步（第十版滚动至 1.5.12；1.6.x 对齐转入 §4.4）** | 🟡 中 | ✅ 已执行（2026-09-21）：FVCC/README.md 版本表 → 1.5.3；docs 三份 AIGC frontmatter 已清理（P2-6 恢复清零）；项目现状与治理台账定位已明确（现状指引 vs 治理台账）；根 BUILD.md 版本同步表已更新 1.5.3。（**2026-10-12 第十版收口，剩余项清零**）：根 README.md 对齐 v1.5.12/FVCS v1.2.6（4dcdb26）；FVCC/README.md 版本表+版本线摘要+目录树+设计规格链接修复；FVCC/docs/ 7 份全对齐（API_CONTRACT helper 真源与接口段登记、SECURITY 清 AIGC 残留+审计/根校验补登、P2-1 提交哈希回填、P2-2 决策门定案、DESIGN 复核头、ROADMAP SQLite 载体注记）；根 BUILD.md → 1.5.12；根 docs/ 台账同步回写。**（第十一版 2026-10-13）**本轮为**叙述性长台账对齐轮**——本混乱报告 + `docs/项目分析与改进方向.md` 两份对齐至 1.6.5 基线；**其余文档（根 README / BUILD、FVCC/README、FVCC/docs 7 份、项目现状与开发步骤）的 1.6.x 对齐本轮未实测**，改登记为开放项（§4.4），不再沿用"docs 内外滞后清零"的表述覆盖 1.6.x |
| P3-5 | **store 持久化健壮性** | 🟡 中 | ✅ 已关闭（2026-10-11，v1.5.8）：阶段 A~D 落地——定时 30min×12 备份 + 写前备份（efef4bd）、data_version 版本化迁移链（16f1043，与 SQLite schema_version 正交）、快照整包回滚 + POST /api/store/restore（59e4004，验签后换入/zip-slip 白名单/prerestore 兜底）、收口移除 store.go 头部 TODO(P1)；仅后端 API，前端入口待排期。详见 IMPROVEMENT_LOG §十二 |
| P3-6 | **ffprobe 运行时环境依赖评估** | 🟡 中 | ✅ **实际已闭**（第十一版 2026-10-13 核验：NAS `/info` 返回 `ffprobe:true`，目标环境探测能力可用，无生产缺探测风险）；原表述：来自项目现状文档，确认 ffprobe 在 fnOS 目标环境的安装/路径依赖 |
| P3-7 | **覆盖率门槛上调** | 🟢 低 | ✅ 已关闭（2026-10-12 第二十六轮）：补 `logger/logger_api_test.go`、`smbshare/smbshare_parse_test.go`、`media/transcode_failure_test.go` 后实测 **60.7%**，check-coverage.ps1 默认阈值与 CI 门禁同步上调 55%→**60%** |
| P3-8 | **远期决策门** | 🟢 低 | 🟡 收窄（2026-10-11）：P1-2 前端收敛（freecut 双轨）已定案关闭——方案 A 维持零框架、借设计不借底座（IMPROVEMENT_LOG §十三）；P2-2 Svelte 5 试点改绑多轨立项评审。本项仅剩 VideoInfo 逐字段复制 2 处（§2.3）随下次模型重构合并 |
| P3-9 | **server/ 根 build_err.txt 待清** | 🟢 低 | ✅ 已清理（2026-10-12 第二十六轮）：server/ 根 0 字节 `build_err.txt` 已删除，未被 git 跟踪、无仓库影响；server/ 根 0 字节文件归零 |

### 4.4 开放项与未验证分支（第十一版 2026-10-13 新增；均为**未完成 / 待裁决**，不得当作已交付）

| 项 | 类别 | 当前状态 | 说明 / 阻塞点 |
|---|---|---|---|
| 概览 **Filter Chips 状态过滤** | 1.6.x 视觉线遗留 | ⬜ 未完成（待产品拍板） | 1.6.4 三批次**刻意未做**项，需产品决策后再排期 |
| `?fx=stripes` / `?fx=breathe` **实验动画** | 1.6.x 视觉线遗留 | ⬜ 待裁决（默认关） | URL 开关已落地但默认关闭，需真机并发场景裁决**转正或弃用** |
| **其余页面骨架迁移 `listSkeleton()`** | 1.6.x 视觉线遗留 | ⬜ 未完成 | `listSkeleton` 泛化组件已在 ui.ts 交付，目前**仅 tasks 页切换**，其余页仍走各自首屏 |
| **store 备份 / 恢复的前端入口** | P3-5 尾部 | ⬜ 未完成 | P3-5 仅交付后端 API（`/api/store/*`、`POST /api/store/restore`），**前端入口未做** |
| e2e **B 档 #9**：渲染/EDL **跨多授权根混合素材**时间线 | e2e B 档 | ⬜ 未完成（需用户决策或第二台机器） | 当前明确拒绝 `E_EDL_INVALID`；v1.5.11 的"有界单根"已吃掉单根子集，跨根需 payload 改造 + 真机验证。附带：`proxyByFile` / `GET /proxy` 仍按**裸相对文件名为键**，跨根同名碰撞同属 #9 |
| e2e **B 档 #7**：`judgeVisibility` **忽略分辨率** | e2e B 档 | ⬜ 未完成（需用户给阈值） | 属产品阈值决策，未给值前不动 |
| **多轨工程立项** `{videoTracks, audioTracks}` + EDL 转场 | 结构性立项 | ⬜ 未立项 | P1-2 freecut 结论把框架试点（含 P2-2 Svelte 5 决策门）**绑定**在此项目立项时一并评审 |
| 渲染 **Failed→ERROR / Cancelled→CANCELLED** 终态分支 | 未验证分支 | ⬜ 未验证 | 无自然复现路径；按约定"遇真实失败顺带验证"，第十版登记的即为此口径，第十一版状态不变 |
| `FVCC/.commandcode/`（工具产物） | 仓库治理小项 | ⬜ 待裁决 | 第十一版实测：既未被 git 跟踪、也未进 `.gitignore`（`git status` 显示 `??`）；处置方式（忽略 / 删除 / 入库）待定 |
| 根 README / BUILD、FVCC/README、FVCC/docs 7 份、项目现状文档的 **1.6.x 版本表对齐** | 文档同步 | ⬜ 本轮未实测 | 第十一版为叙述性长台账对齐轮，仅覆盖本混乱报告 + 项目分析两份；其余文档是否已同步 1.6.0~1.6.5 未核查，不作"已清零"表述 |

---

## 5. 结论（第七版，2026-09-21）

FVCC 从首版评价（2026-09-18，1.4.0）到第七版复核（2026-09-21，1.5.3）：**P0/P1/P2 全部落地**，混乱度综合评分 **6.5 → 4.3 → 2.8 → 2.4 → 2.8 → 2.4 → 2.4**（轻度混乱）。架构与质量保障无退化；第六版已清零物理清理与文档滞后债务，**第七版核心变化是 SQLite 迁移（P0-2）与事件驱动调度（P1-1）两项结构性改进落地**，评分持平于轻度混乱区间：

**保持的改善（高置信证据）**：
1. **安全硬伤清零并闭环**：凭据加密 / 删除回收站化 / 反射白名单 / 审计闭环（8 处调用点）/ git 版本控制——首版五硬伤全部落地且配套单测；
2. **架构演进持续**：P2-1 后端分层（第十版实测 12 包 / 107 个 .go 全绿）+ P2-2 前端组件化（src/lib/ 4 件套，样板残留 0）+ **P0-2 SQLite 迁移落地**（modernc.org/sqlite v1.14.0，store 扩至 8 文件）+ **P1-1 事件驱动调度落地**（eab7688）+ 步骤 3 代码清理（防注入死分支 / 空占位 / 过时用例移除）+ 第十九轮 Windows 盘符根路径边界修复（5 处前缀判定统一）；
3. **质量保障闭环**：CI 流水线、版本单一来源推进至 1.5.12（三处一致 + version_test 自动校验 + check-versions.ps1）；
4. **仓库治理落地**：temp 18 项清理释放约 560.3MB、6 个 fpk 归档 archive/fpk/ 完整 v1.1.0~v1.5.3、运行时数据按禁删原则保留原位。

**本轮关闭的债务（第五版 P3 组）**：
1. P3-2 / P3-3（物理清理）✅：temp 回潮与根目录版本包堆积已处置，实测修正多项不存在项；
2. P3-4（文档同步）✅：README 1.5.3、AIGC frontmatter 清零、docs 7 份台账状态一致（根 README.md v1.4.9 滞后为当时 docs/ 外剩余 1 处——**2026-10-12 第十版已对齐 v1.5.12，清零**）；
3. P3-1（git 提交）✅：1.5.3 全部提交并打 tag（含 SQLite 迁移三提交与文档同步）；当前工作区为 1.5.3 基线 + 第十八轮文档校准改动（5 文件）未提交，提交纪律延续（2026-10-12 第十版校准：上述滞留已随 68ebdec / 800eb4c~f74ec54 / a20cc6b / 4dcdb26 分批提交并推送 fork/baseline-snapshot，当前工作区未提交项仅剩本轮文档全面对齐）。

**当前剩余债务**收敛为工程项（P3-6 ffprobe 依赖 / P3-8 远期决策门）。P3-1（tag v1.5.12）、P3-5 已于 2026-10-11 v1.5.8、P3-4 已于 2026-10-12 第十版、**P3-7（覆盖率门槛 60%）与 P3-9（build_err.txt 已清）已于 2026-10-12 第二十六轮关闭**。后续工作重心保持"清理纪律 + 文档实时同步 + 提交纪律"三件事。

**第十一版追加（2026-10-13，基线 v1.6.5 / FVCS v1.3.0）**：从第十版（1.5.12，2.4/10）到第十一版（1.6.5，**2.3/10**），九维度分值**全部维持**，综合下调 0.1 的依据只有两条硬证据——① **v1.6.5 把版本一致性校验从"Windows 静默 SKIP（全绿但零校验）"根治为"漂移即红"**（四类失败一律 `t.Fatal`、移除 `t.Skipf`、新增反硬编码版本字面量用例、check-versions.ps1 去仓库绝对路径），这消灭的是"发布包元数据与真源长期不一致却无人发现"这一类**会长期潜伏**的一致性缺陷（1.6.4 已实际发生一次：VERSION 仍 1.6.0，`/api/info` 与「设置」页显示错版本）；② **v1.6.4 任务页三批次**收敛了信息层级 / 错误呈现 / 加载与选中反馈，并压掉每 WS tick 的 DOM churn（进度条走合成层 + render/updateOverview 签名守卫，硬约束"视觉改进不得增加 WS tick 重绘"）。规模与状态实测：internal **12 包**（目录逐项核对）+ store/model 子包、**110 个 .go**（58 实现 17,197 行 + 52 测试 9,183 行）、TS **33 文件 11,474 行**；三真源一致 1.6.5 且 fpk 解包核验、真机「设置」页 1.6.5；1.6.0 / 1.6.4 / 1.6.5 均已提交推送 `fork/baseline-snapshot` 并打注解 tag（b1a0d93 / b007a3d / 5166cbe）。**明确不涨分的事项**：覆盖率本轮未重跑（沿用第二十六轮实测 60.7%）、FVCC 依赖未重跑 tidy 复核、temp 与 archive 未重扫、其余文档的 1.6.x 对齐未实测；**仍未完成项**（Filter Chips、`?fx=` 动画裁决、其余页面骨架迁移、store 前端入口、e2e B 档 #7/#9、多轨立项、渲染 Failed/Cancelled 分支验证、`.commandcode` 处置）全部保留在 §4.4，状态为未完成/待裁决。剩余 P3 债务仅 **P3-6（实际已闭）/ P3-8** 两项，故本轮重心转为"1.6.x 视觉线遗留裁决 + 非长台账文档的版本对齐核查"。

> 维护记录：首版 2026-09-18 上午（1.4.0，6.5/10）→ 第二版 2026-09-18（1.4.2，4.3/10）→ 第三版 2026-09-19（1.4.4，2.8/10）→ 第四版 2026-09-20（1.4.7，2.4/10）→ 第五版 2026-09-20 晚（1.4.9，2.8/10）→ 第六版 2026-09-21（1.5.3，2.4/10）→ 第七版 2026-09-21（1.5.3，2.4/10）→ 第八版 2026-09-22（1.5.7，2.4/10）→ 第九版 2026-10-11（1.5.8，2.4/10）→ 第十版 2026-10-12（1.5.12，2.4/10）→ **第十一版 2026-10-13（1.6.5，2.3/10）**。第八版依据第十九轮：1.5.4 编辑器变速播放 J/K/L + 按钮图标化（080754c 已提交）、1.5.5~1.5.7 Windows 盘符根路径边界修复 + 剪辑页多根兜底 + roots 回落 + 素材递归扫描 + 单测（工作区未提交）；server .go 96→97（+media/roots_test.go）；三处版本一致 1.5.7。**第九版（2026-10-11）**：P3-5 store 备份/迁移/快照回滚落地（v1.5.8，阶段 A~D：efef4bd / 16f1043 / 59e4004 / 收口提交），store.go TODO(P1) 移除；剩余债务收敛为 P3-6 / P3-7 / P3-8 / P3-9 与 P3-1；coverage 56.7%→59.0%（门禁 55%）。**第十版（2026-10-12）**：步骤 8 e2e A 档修复 + #8 渲染四层全链路收口（v1.5.9~1.5.12，真机验证通过，推送 fork/baseline-snapshot）；文档全面对齐轮——根 README/BUILD、FVCC/README、FVCC/docs 7 份、根 docs 台账统一对齐 v1.5.12，P3-4 清零；server .go 97→107（58 实现 + 49 测试，internal 12 包）；TS 11,257 行。**第二十六轮回写（2026-10-12，非改版次）**：v1.5.12 打注解 tag 并推送（P3-1 收尾）；P3-7 关闭——新增 logger/smbshare/media 三份测试，实测 59.0%→60.7%，check-coverage.ps1 默认阈值与 CI 门禁 55%→60%；P3-9 关闭——server/ 根 0 字节 build_err.txt 已删；测试文件 49→52。剩余 P3 债务仅 P3-6 / P3-8。**第十一版（2026-10-13，第二十八轮长台账对齐）**：版本基线 1.5.12→**1.6.5**（配套 FVCS v1.3.0）；登记 1.6.x 版本线三批——v1.6.0（tag b1a0d93，含 1e622f0 批次①-⑤ / 22035c6 FVCS GUI 复活；视觉打磨 + FVCS 1.2.6→1.3.0）、v1.6.4（tag b007a3d / fe06e52，7 文件 +197/−44；任务页批次 A 层级与状态反馈、B 热路径性能、C 速率与实验项；方案稿文件名带 v1.6.1 属**稿号非版本号**）、v1.6.5（tag 5166cbe / ceeb1b2；VERSION 第三真源漂移修复 + 校验加固，四类失败一律判红、移除 `t.Skipf`、新增反硬编码字面量用例、check-versions.ps1 `$PSScriptRoot` 化）；**易错点已固化**：1.6.1/1.6.2/1.6.3 从未进 git 历史、未出包、未打 tag（manifest 实测序列 1.6.0→1.6.4→1.6.5），"1.6.1~1.6.4 版本线"写法作废；规模实测 internal 12 包（目录逐项核对，与第十版一致）/ 110 个 .go（58 实现 17,197 行 + 52 测试 9,183 行，第十版记载为 107 个 / 49 测试）/ TS 33 文件 11,474 行（第十版记载 11,257 行，增量为 1.6.4 任务页批次）；**覆盖率本轮未重跑**，引用值标注"第二十六轮实测 60.7%"；本轮实跑 go vet / `go test ./internal/version -count=1` / 全量 go test / npm run build / check-versions.ps1 全绿，fpk 重打并解包核验、真机「设置」页 1.6.5（用户 2026-10-13 确认）。**评分：九维度全部维持原分，综合 2.4→2.3**，依据仅取两条硬证据（v1.6.5 校验"漂移即红"根治一类长期潜伏的一致性缺陷；v1.6.4 任务页信息层级/错误呈现/选中反馈 + 每 tick DOM churn 走合成层与签名守卫），§2.5 因已在 2/10 低位不再体现分差；§2.3 / §2.4 / §2.7 因残留未变或本轮未重测而维持原分；P3 债务收敛为 P3-6（实际已闭，NAS `/info` `ffprobe:true`）/ P3-8 两项，1.6.x 视觉线遗留与治理小项另记 §4.4（Filter Chips、`?fx=` 动画裁决、其余页面骨架迁移 listSkeleton、store 前端入口、e2e B 档 #7/#9、多轨立项、渲染 Failed/Cancelled 分支、`.commandcode` 处置——**均标注未完成/待裁决**）；本轮为文档回写，未执行 git 变更与构建。
