# FVCC 项目混乱度评价报告（细化版）

> 项目：FVCC（飞牛视频转换 NAS 调度端 Web UI）@ D:\Fnos.VideoConversion\FVCC
> 基线评价日期：2026-09-18（首版）
> **复查更新日期：2026-10-14（第十四版）**
> 评价视角：Vibe Coding（AI 辅助编码）模式下的工程卫生与可维护性
> 评价方式：目录树扫描 + 关键源码阅读 + 配置与文档核查 + git log/status 交叉核对 + 与根 README / BUILD / docs 台账交叉核对
> **2026-10-14 压缩说明**：各版次逐版叙述压缩为下方版本史表（完整叙述以 git 历史为准）；九维度评分表、P 项状态表、§4.4 开放项保持现行口径不变。**后续轮次按惯例在版本史表追加一行、在 §2/§3/§4 更新现状即可。**

**当前基线（第十四版，2026-10-14）**：FVCC **v1.6.9** / FVCS v1.3.1（三真源一致，`check-versions.ps1` 绿）。后端 internal **12 包** + store/model 子包 / **110 个 .go**（58 实现 17,197 行 + 52 测试 9,183 行，本轮零后端改动）；前端 TS **33 文件 11,622 行**（本轮实测；上版 11,474，+148 行全部来自 store 备份/恢复面板与类型）；覆盖率沿用第二十六轮实测 **60.7%**（门禁 60%，本轮未重跑）。**综合评分 2.3/10（B / 轻度混乱）**，九维度分值自第十一版起维持。

**版本史表（首版~第十四版）**：

| 版次 | 日期 | 基线 | 核心事项 | 综合评分 |
|---|---|---|---|---|
| 首版 | 2026-09-18 | 1.4.0 | 基线评价，登记五硬伤 | 6.5 |
| 二版 | 2026-09-18 | 1.4.2 | 快速整改轮 | 4.3 |
| 三版 | 2026-09-19 | 1.4.4 | P2-1/P2-2 分层与组件化推进 | 2.8 |
| 四版 | 2026-09-20 | 1.4.7 | 分层继续细化 | 2.4 |
| 五版 | 2026-09-20 | 1.4.9 | 后端细化拆分（stream_media/roots/aliases/config/trash_handlers）；登记 temp 膨胀至 549.7MB 清理回潮 | 2.8 |
| 六版 | 2026-09-21 | 1.5.3 | 步骤 3 代码清理 + 步骤 6 仓库治理（temp 18 项清理释放约 560.3MB、6 个 fpk 归档 archive/fpk/） | 2.4 |
| 七版 | 2026-09-21 | 1.5.3 | **P0-2 SQLite 迁移落地**（a72b8ff/9e122d5/5dd373e，modernc.org/sqlite 纯 Go 驱动 + 12 张实体表 + 存量导入）+ **P1-1 事件驱动调度落地**（eab7688） | 2.4 |
| 八版 | 2026-09-22 | 1.5.7 | 1.5.4 已提交（080754c 变速播放 J/K/L）；1.5.5~1.5.7 Windows 盘符根双分隔符边界修复（4 处前缀判定）+ 多根兜底 + roots 回落 + 素材递归扫描（后经 68ebdec + tag v1.5.7 入库） | 2.4 |
| 九版 | 2026-10-11 | 1.5.8 | **P3-5 关闭**：store 备份/迁移/快照回滚四阶段（efef4bd/16f1043/59e4004 + 收口）；coverage 56.7%→59.0% | 2.4 |
| 十版 | 2026-10-12 | 1.5.12 | 步骤 8 e2e A 档五项缺陷 + **#8 渲染四层全链路修复**收口（v1.5.9~1.5.12，真机验证 Success 收口）；文档全面对齐轮（P3-4 清零）；107 个 .go / TS 11,257 行 | 2.4 |
| 十一版 | 2026-10-13 | 1.6.5 | 1.6.x 版本线登记：v1.6.0 视觉打磨（tag b1a0d93，含 FVCS 1.3.0 GUI 复活）、v1.6.4 任务页三批次（tag b007a3d）、v1.6.5 VERSION 第三真源漂移修复 + 校验加固（tag 5166cbe，静默 SKIP→漂移即红）；110 个 .go / TS 11,474 行；**综合 2.4→2.3**；易错点固化：1.6.1~1.6.3 从未进 git（方案稿号≠版本号） | 2.3 |
| 十二版 | 2026-10-14 | 1.6.7 | 依赖与工具链专项：v1.6.6 低风险刷新 + FVCS 1.3.1；v1.6.7 sqlite **v1.14.0→v1.60.1** 跨 46 minor（老库新驱动真数据验证通过）；第三十三轮 Node 25.9.0（已 EOL）→26.11.1；规模/覆盖率亲跑复测无漂移；评分维持 2.3 | 2.3 |
| 十三版 | 2026-10-14 | 1.6.8 | **Tailwind CSS 4.3.3 大版本专项收口**（依赖刷新主题唯一余项）：五道门禁落地，`npm audit` 7 条告警清零、`npm outdated` 归零；`tailwind.config.js` 删除（token 映射并入 style.css `@theme inline`）；门禁脚本 stripComments 行号错位缺陷修复 + 新增 TW4-token-form 反回潮规则；388 类 token 逐项比对 0 缺失、gzip 10.00→9.78 kB；fpk 11,013,500 B 解包核验；tag v1.6.8（批次主体 e1ed35c）；产品裁决：接受 v4 移除 button cursor:pointer 默认；**NAS 真机视觉对照用户实测中**；评分维持 2.3 | 2.3 |
| 十四版 | 2026-10-14 | 1.6.9 | **低风险收尾批次（第三十七轮）**：① **P3-5 尾部还清——store 备份/恢复前端入口**（设置页「数据备份与恢复」面板接上 v1.5.8 就交付却无人接的 `/api/store/*` 三端点；交付前用 dev 模式真实后端 + 浏览器把"空态→手动备份→整包恢复→prerestore 兜底→23 份列表展开"全链路走通，顺手补出 `prerestore` 等 5 个 kind 的中文标签缺口）；② **P3-6 台账正式勾选关闭**；③ `FVCC/.commandcode/` 按裁决入 `.gitignore`（AI 工具产物，不入库不删除）。纯前端 + 治理，后端零改动、覆盖率未重跑；**新登记一处缺口：手动备份永久保留但无删除入口（后端亦无 DELETE 端点）**。评分维持 2.3 | 2.3 |

---

## 1. 项目概览

| 项 | 内容 |
|---|---|
| 定位 | fnOS NAS 上的视频转码调度端：素材扫描 → 转码方案 → 渲染节点集群 → 任务队列 → EDL 剪辑/代理预览全链路 |
| 后端 | Go 1.27.1，gin v1.12 + gorilla/websocket，**已分层**：`server/internal` 12 包（api/edl/media/node/pathutil/protocol/remote/scheduler/security/store/version/ws）+ store/model 子包；110 个 .go（58 实现 17,197 行 + 52 测试 9,183 行，2026-10-13 实测）；根包仅剩 main.go 274 行；store 已迁 SQLite（sqlite.go/sqlite_import.go/sqlite_load.go/sqlite_persist.go + P3-5 backup/migrate/restore） |
| 前端 | Vite + TypeScript + TailwindCSS v4.3.3（1.6.8 起 CSS-first），零框架（`el()` 手工 DOM；P1-2 决策门 2026-10-11 定案维持）；公共样板收敛至 src/lib/ 4 件套 + ui.ts listSkeleton；TS 33 文件 11,474 行（2026-10-13 实测） |
| 版本 | **1.6.8**（server/internal/version/VERSION、ui-src/package.json、manifest 三处一致；version_test.go 已加固为"漂移即红"；配套 FVCS v1.3.1）；历史版本包归档 `archive/fpk/`（v1.1.0~v1.5.3）；1.5.4 以后构建产物为 gitignore 本地件 |
| 版本控制 | **git 已启用**：上层仓库 `D:\Fnos.VideoConversion` 统一管理，分支 baseline-snapshot，已推送 GitHub hgmmm25/Fnos.VideoConversion；推送目标 fork；1.5.3~1.6.8 各批次均提交推送并打注解 tag（v1.6.0=b1a0d93 / v1.6.4=b007a3d / v1.6.5=5166cbe / v1.6.6=1dcb774 / v1.6.7=4154d31 / v1.6.8=e1ed35c 后记笔）；工作区状态随各轮收口保持干净 |
| 文档 | **双 docs 布局已收口**：根仓库 docs/ 台账 7 份 + 1.6.x 方案稿 2 份（`FVCC_视觉更新方案_v1.6.0.md`、`FVCC_任务页视觉改进整合方案_v1.6.1.md`——文件名 v1.6.1 为方案稿编号）+ `FVCC/docs/` 7 份（API_CONTRACT/DESIGN/PRODUCT/P2-1/P2-2/SECURITY/STORE_PERSISTENCE_ROADMAP）；WebVideoEditor_Design/ 01-10 规格文档仍入库 |

目录结构（根目录，2026-10-13 实测口径）：

```
FVCC/
├── .gitignore                            ← 已建立（*.exe/*.fpk/node_modules/temp/logs/0字节残留；a20cc6b 补 FVCS 凭据/密钥/运行库忽略）
├── README.md                             ← 版本表已对齐（随各轮文档回写推进）
├── archive/fpk/                          ← 历史版本包完整归档 v1.1.0~v1.5.3
├── credentials.db / master.key / secret.key / locks.json / tasks.json / history_tasks.json / config.enc.json  ← 运行时数据（按禁删原则保留原位）
├── ICON.PNG / ICON_256.png / manifest    ← fnOS 应用清单（UTF-8 正常，version=1.6.8）
├── app/          ← fnOS 应用打包目录（字体 latin 子集 + lightweight-charts 异步 chunk）
├── cmd/          ← fnOS 生命周期回调脚本（install/config/uninstall/upgrade）
├── config/       ← privilege / resource 权限声明
├── docs/         ← 7 份技术文档（见上表）
├── logs/         ← 运行时日志（已入 .gitignore）
├── server/       ← Go 后端（internal/ 12 包 + store/model 子包；根包仅剩 main.go 274 行）
├── temp/         ← 本地构建件（fpk_stage/logs/verify，已 .gitignore，属回潮观察项见 §2.7）
└── ui-src/       ← 前端源码（33 个 TS 文件 11,474 行；src/lib/ 4 件套；pages/editor/ 14 模块）
```

---

## 2. 分维度评价（含具体证据）

> 各小节"现状"为第十一版（2026-10-13）实测口径；1.6.6~1.6.8 为纯依赖/工具链轮次，九维度证据无变化（第十二/十三版核验）。

### 2.1 代码组织与分层 —— 2/10（已分层且持续细化）

`server/internal/` 12 包 + store/model 子包共 110 个 .go，根包仅剩 main.go（274 行，入口与装配、优雅退出）。1.6.x 期间无新增包、无再拆分；结构变化仅 internal/version 的校验加固（1.6.5）。分层历史：1.4.8/1.4.9 曾新增 stream_media/roots（media 域）、aliases/config/hashutil/security_roots/trash_handlers（api 域）、edl 扩展为 4 文件、store/trash.go 收窄为纯存储（handler 侧迁 api）；v1.5.8 增 internal/pathutil。积极面：handler 按领域分文件；`RenderDispatcher`/`MediaProber`/`ProxyProber` 接口注入保持；分层模式可承载后续演进（实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md`）。

### 2.2 命名一致性 —— 5/10（轻度混乱，无新增）

- **证据 1**：JSON 字段大小写风格混杂——协议层既有 camelCase（`authKey`、`customFfmpeg`），又有 snake_case（remote 包 `progressPush`：`task_id` 等）；API_CONTRACT.md 已定"协议字段一律 camelCase（FVCS 对等 WS 报文允许 snake_case 双风格兼容）"，存量未迁移。
- **证据 2**：注释任务编号两套语义——`// P2-1` 在 handler_metrics_test.go 指 Prometheus 指标端点，与本报告 P2-1（后端分层）不同；已登记 TASK_REFERENCE §2.1。
- **证据 3**：新旧错误契约并存（过渡期）——api.ts 保留 `body.msg || body.error` 兜底；新代码只读 msg。
- **证据 4（已落地 P2-7）**：测试文件命名统一为 `handlers_<domain>_test.go` 风格。
- 第十一版复核：1.6.4/1.6.5 未引入新命名风格，**维持 5/10**。

### 2.3 重复代码 —— 3/10（两大重复源已消灭，剩余中等）

- ✅ updateProfile 59 块手写映射 → 反射白名单（internal/protocol/applyjson.go，P1-1）。
- ✅ SMB 路径校验复制 3 处 → 统一 helper（internal/security/smb_validate.go，P1-2）。
- ✅ 前端跨页面样板（P2-2）：crudActions/scrollPos/useListPage/formBuilder 4 件套接入 6 页。
- ✅ 1.6.4 增量：ui.ts 新增 listSkeleton 泛化组件 + render/updateOverview 签名守卫 + WS speed 单一更新点。
- ⚠️ **仍在**：VideoInfo 30+ 字段逐字段复制 2 处（`doScanDirectory()` 与 `UpsertVideoCache` 调用参数块）；listSkeleton 仅 tasks 页切换，其余页面骨架迁移未做（§4.4）。**维持 3/10**。

### 2.4 依赖管理 —— 5/10（前端打包侧已瘦身）

- 后端：go.mod 直接依赖 gin + websocket + modernc.org/sqlite（**v1.60.1**，第三十二轮自 v1.14.0 升级，纯 Go 无 CGO）；`go mod tidy` 复核零变化。
- 前端：app/ui/assets 保持瘦身（latin 字体子集 + 异步 chunk）；**Tailwind 4.3.3 专项（v1.6.8）后 `npm outdated` 归零、`npm audit` 清零**，依赖刷新主题无余项。
- 积极面：package-lock.json 与 go.sum 存在；check-versions.ps1 / check-coverage.ps1 门禁保持。
- FVCS 1.3.0 引入 Fyne 托盘/设置窗口（对端依赖面扩大，不进入 FVCC 打包产物）。**维持 5/10**。

### 2.5 配置与硬编码 —— 2/10（版本单一来源：漂移事件已根治为"漂移即红"）

- ✅ 凭据 AES-GCM 加密落盘（internal/security/crypto.go，P0-1）：主密钥 0600 落盘 `<dataDir>/secret.key`，`enc:v1:` 前缀 + 旧明文兼容迁移 + API 脱敏。
- ✅ 版本单一来源（P1-4）：version.go go:embed VERSION + version_test.go 三处自动校验 + check-versions.ps1；真机核验「设置」页版本显示正确（1.6.5 用户确认；1.6.8 随真机视觉对照待确认）。
- 🔴→✅ **v1.6.4 真实漂移事件 + v1.6.5 根治（本维度最重要证据）**：v1.6.4 出包漏改 go:embed 的 VERSION（仍 1.6.0），根因是旧 version_test.go 相对路径写错（`../../manifest` 实为不存在路径）读不到即 `t.Skipf`，Windows 本机"全绿但零校验"。v1.6.5 处置：①`findRepoRoot` 双起点向上搜索（判定标志 = 目录内同时存在 `FVCC/manifest` 与 `FVCC/ui-src/package.json`，不用 `.git`）；②四类失败一律 `t.Fatal`、移除 `t.Skipf`；③新增 `TestAppVerIsNotPinnedLiteral` 反硬编码字面量扫描；④check-versions.ps1 改 `$PSScriptRoot` 推导 + `-RepoRoot` 覆盖（本机 PS 5.1 变量名区分大小写）；⑤version.go 注释修正。发版取舍：发新补丁号 1.6.5 而非重指 tag（fnOS 仅接受严格更高版本覆盖安装）。
- ⚠️ **仍在**：默认值散落硬编码（`SchedulerIntervalSec=1`、`ChunkSizeMB=4`、`HistoryLimit=1000`、scheduler.go `chunkSize: 4*1024*1024` 与设置项语义重复、main.go 开发模式 `127.0.0.1:8088`）。**维持 2/10**（量纲已在低位）。

### 2.6 文档与注释 —— 2/10（docs 布局收口，AIGC 残留清零）

- ✅ 规格文档入库 + 速查表（P1-3）；PRODUCT.md/DESIGN.md 已迁入 FVCC/docs/（原"两处文档根"解决）。
- ✅ AIGC frontmatter 残留已清（P2-6，2026-09-21 恢复清零，2026-10-12 保持）；全库 AIGC 关键词命中仅剩台账登记语句本身。
- ✅ 根 README / BUILD / API_CONTRACT 版本与契约口径随各轮回写对齐。
- 已固化的易错点：`1.6.1/1.6.2/1.6.3` 仅为批次开发期本地中间 bump，**从未进 git/未出包/未打 tag**；"1.6.1~1.6.4 版本线"写法作废（TASK_REFERENCE §3.8）。
- 残留观察：`docs/FVCC_项目现状与开发步骤.md` 与混乱报告 §1 存在内容重叠（台账 vs 现状指引定位），避免双轨漂移。**维持 2/10**。

### 2.7 遗留/废弃代码 —— 8/10（temp 与根目录产物已清理）

- 历史已清理：temp 打包 stage 与调试残留 18 项（释放约 560.3MB）、根目录 6 个 fpk 归档 archive/fpk/、误构建嵌套目录 FVCC/FVCC/（22.16MB）；server/ 与 ui-src/ 根 0 字节文件归零（build_err.txt 已于第二十六轮删除，P3-9）。
- 保留（禁删原则）：credentials.db/master.key/secret.key/locks.json 等运行时数据原位。
- 观察项：`FVCC/temp/` 现见 fpk_stage/logs/verify（本地构建件回潮，已 .gitignore 不构成仓库债）；`FVCC/.commandcode/`（AI 工具产物）**已随 v1.6.9 按用户裁决入 .gitignore**（不入库、不删除，§4.4 已移出）。**维持 8/10**。

### 2.8 测试情况 —— 2/10（优秀且持续增强）

- 规模：52 个 `_test.go` / 9,183 行（演进链 43→49→52）；1.6.5 新增版本守卫用例（findRepoRoot 定位 + 四类失败 t.Fatal + 反字面量扫描）。
- 基建：接口注入（RenderDispatcher/MediaProber/ProxyProber）专为单测替身设计；main.go 与单测共用构造点。
- 门禁：CI（fvcc-ci.yml：vet + test -race + 覆盖率 ≥60% + 前端 check:design/tsc/build）；check-coverage.ps1 实测基线 56.7%→59.0%→**60.7%**，门槛 55%→**60%**（第二十六轮；本轮未重跑）。
- 1.6.4 门禁侧事件：check-design R3 误报 `box-shadow` 已按前缀排除修复；任务页选中态 inset 阴影按方案 §2.3 裁决合法。

### 2.9 安全与健壮性 —— 2/10（五硬伤保持清零）

- 首版 5 项硬伤 → 0 项未决：凭据加密、删除回收站化（`_trash` + 时间戳后缀）、updateProfile 白名单、破坏性操作审计闭环（8 处调用点）、git 版本控制。
- 既有防线保持：PathValidator 四层路径校验、NoRoute 兜底、requireAdmin 门禁、网关身份透传（X-Trim-* + 写操作 admin）、securityHeaders、崩溃恢复（非终态任务重置 QUEUE）、限流（ratelimit/ws_limit/stream 单文件 4 路/全局 64 路）。
- 新增健壮性收益（1.6.5 记账）：版本一致性校验"静默 SKIP → 漂移即红"属一致性防线；1.6.4"视觉改进不得增加 WS tick 重绘"硬约束属前端热路径健壮性（见 §2.10）。**维持 2/10**。

### 2.10 前端信息层级与热路径健壮性（第十一版新增，不进入九维度评分序列）

v1.6.4（tag b007a3d，含 fe06e52，7 文件 +197/−44；方案稿三方外部模型评审整合）三批次：

- **批次 A（信息层级 + 状态反馈）**：errorMsg 独立成行带 title；拆除 sub 上 truncate 与 flex-wrap 冲突；首屏骨架接入 listSkeleton；批量三键零选中禁用 + 刷新 pending；选中即时高亮；活动卡 signal 静态指示线；getPhaseStyle 补全状态文案且 ERROR 走红条；去 `w-[200px]` 硬宽；serverName 中性标签。
- **批次 B（热路径性能）**：进度条 `width` → `transform: scaleX(--p)` 走合成层；render 增加 id/顺序签名守卫；updateOverview 增加计数签名守卫；scrollPos 独立 key。硬约束：**视觉改进不得增加 WS tick 重绘**。
- **批次 C（速率 + 实验项）**：WS 合并 `speed`（链路核实 `models.go speed,omitempty` + `TaskUpdateFullMsg`）；`?fx=stripes/breathe` URL 开关（默认关，待真机裁决）；ui.ts 新增 listSkeleton。

**用户真机实测抓出的两处缺陷**（改进引入的回归，如实登记）：① 空任务库骨架常驻——初版退出条件 `tasks.length>0` 改为"首次成功 render 即置 loaded"；② check-design R3 误报 box-shadow（见 §2.8）。三项刻意未做/待裁决（Filter Chips、fx 动画、其余页骨架迁移）登记 §4.4。

---

## 3. 量化混乱度评分（第七版成表 2026-09-21；末列为当前值）

| 维度 | 首版 | 二版 | 三版 | 四版 | 五版 | 六版 | 七版 | 当前 | 一句话结论（变化） |
|---|---|---|---|---|---|---|---|---|---|
| 代码组织与分层 | 7 | 6 | 2 | 2 | 2 | 2 | 2 | **2** | 分层稳定（internal 12 包 / 110 个 .go）；1.6.x 无新增包、无再拆分 |
| 命名一致性 | 6 | 6 | 5 | 5 | 5 | 5 | 5 | **5** | 无新增；编号歧义已登记（TASK_REFERENCE §2.1） |
| 重复代码 | 7 | 5 | 3 | 3 | 3 | 3 | 3 | **3** | 1.6.4 新增 listSkeleton 与签名守卫，但仅 tasks 页切换；VideoInfo 复制 2 处仍在 |
| 依赖管理 | 5 | 5 | 5 | 5 | 5 | 5 | 5 | **5** | sqlite 升级至 v1.60.1、Tailwind 4 专项收口、npm outdated/audit 双清零（1.6.7/1.6.8） |
| 配置与硬编码 | 7 | 4 | 3 | 2 | 2 | 2 | 2 | **2** | 版本单一来源 + "漂移即红"根治；量纲已在低位 |
| 文档与注释 | 5 | 3 | 2 | 2 | 2 | 2 | 2 | **2** | docs 布局收口；AIGC 清零；1.6.x 版本线易错点已固化 |
| 遗留/废弃代码 | 8 | 6 | 6 | 6 | 5 | 8 | 8 | **8** | 历史清理完成；`.commandcode/` 已随 v1.6.9 入 .gitignore（AI 工具产物，不入库）、temp 构建件回潮观察 |
| 测试情况 | 3 | 2 | 2 | 2 | 2 | 2 | 2 | **2** | 52 个测试文件；覆盖率 60.7%（门禁 60%）；1.6.5 补版本守卫用例 |
| 安全与健壮性 | 5 | 2 | 2 | 2 | 2 | 2 | 2 | **2** | 五硬伤清零保持；新增一致性健壮性（漂移即红）+ WS tick 重绘硬约束 |
| **综合** | **6.5** | **4.3** | **2.8** | **2.4** | **2.8** | **2.4** | **2.4** | **2.3（轻度混乱）** | 九维度分值全部维持（多数已在历史最优档）；综合 2.4→2.3 依据 = §2.5 v1.6.5 校验根治 + §2.10 任务页收敛；1.6.6~1.6.8 纯依赖轮次评分不变 |

等级判定：**B / 轻度混乱**。P0/P1/P2 全部落地；第七版核心变化是 SQLite 迁移（P0-2）与事件驱动调度（P1-1）；第十版 e2e A 档 + #8 渲染全链路收口；第十一版 1.6.x 视觉线登记（综合降至 2.3）；第十二/十三版为依赖与工具链专项（sqlite v1.60.1 / Node 26 / Tailwind 4），评分维持；**第十四版（v1.6.9）P3-6 台账正式勾选关闭 + P3-5 前端入口还清 + `.commandcode/` 收编，评分维持**。P3 组自此**仅剩 P3-8（远期决策门，随多轨立项评审）**；1.6.x 视觉线遗留与治理小项登记于 §4.4。

---

## 4. 修改建议（P0/P1/P2 全部落地，压缩为状态汇总；P3 为遗留债务）

### 4.0 历史整改状态汇总（P0/P1/P2 全部落地或收口）

| # | 建议 | 状态 | 落地证据（简） |
|---|---|---|---|
| P0-1 | 凭据加密落库 | ✅ 已落地 | internal/security/crypto.go（AES-GCM + 0600 + enc:v1: + 兼容迁移 + API 脱敏） |
| P0-2 | 清理工程残留 + .gitignore | ✅ 已落地 | .gitignore 已建；temp 18 项清理（约 560.3MB）与 fpk 归档（archive/fpk/ v1.1.0~v1.5.3） |
| P0-3 | git 建立版本控制 | ✅ 已落地 | 上层仓库统一管理，baseline-snapshot 已推送 GitHub |
| P1-1 | 消灭 updateProfile 59 块手写映射 | ✅ 已落地 | internal/protocol/applyjson.go 反射白名单；另含事件驱动调度（eab7688） |
| P1-2 | 抽取 SMB 校验 helper | ✅ 已落地 | internal/security/smb_validate.go，5 处调用点统一 |
| P1-3 | 文档落地消除悬空引用 | ✅ 已落地 | WebVideoEditor_Design/ 01-10 + 速查表 + 根 README/BUILD/API_CONTRACT |
| P1-4 | manifest 修复 + 版本单一来源 | ✅ 已落地 | version.go go:embed + version_test.go 三处校验（1.6.5 加固为漂移即红）+ check-versions.ps1 |
| P1-5 | 依赖瘦身 | 🟡 部分落地 | app/ui 字体 latin 子集瘦身；go.mod tidy 复核零变化 |
| P2-1 | 后端分层 internal | ✅ 已落地（2026-09-19） | 12 包 + store/model 子包，根包仅剩 main.go；实施记录见 FVCC/docs/P2-1_后端分层_阶段A实施记录.md |
| P2-2 | 前端组件化 | ✅ 已收口（2026-09-19） | src/lib/ 4 件套接入 6 页；细则见 FVCC/docs/P2-2_细化细则.md。延伸未闭环：listSkeleton 仅 tasks 页（§4.4） |
| P2-3 | 建立 CI 流水线 | ✅ 已落地 | fvcc-ci.yml + check-coverage.ps1（门槛 60%，实测 60.7%） |
| P2-4 | 统一 API 契约与命名 | ✅ 已收口 | API_CONTRACT.md + apierr.go + api.ts 适配兜底 |
| P2-5 | 删除操作回收站化 | ✅ 已落地 | store/trash.go（纯存储）+ api/trash_handlers.go |
| P2-6 | 清理 AIGC 残留与任务编号注释 | ✅ 已收口 | 存量文档已清；2026-09-21 恢复清零 |
| P2-7 | 测试文件命名规范化 | ✅ 已落地 | git mv 为 handlers_<domain>_test.go 风格 |
| P2-8 | 破坏性操作审计闭环 | ✅ 已落地 | internal/security/audit.go + 8 处调用点 + 单测 |

### 4.1 / 4.2 落地前快照（已删除）

> P2-1/P2-2 的落地前快照、分阶段计划、迁移映射、风险表已随落地完成删除；实施记录见 `FVCC/docs/P2-1_后端分层_阶段A实施记录.md` 与 `FVCC/docs/P2-2_细化细则.md`。保留的开放决策：P2-2 §4.2.3 决策门（组合器方案去留 / Svelte 5 试点）随 P1-2 前端收敛定案（方案 A 维持零框架，2026-10-11）关闭为远期绑定项——仅当多轨 `{videoTracks,audioTracks}` 立项时一并评审。

### 4.3 当前遗留债务（P3 组，按优先级）

> **收敛结论（第十一版）**：P3 组九项中 P3-1/2/3/4/5/7/9 均已关闭，**实际未决仅剩 P3-6 与 P3-8**——其中 P3-6 经 NAS `/info` 实测 `ffprobe:true` 已"实际已闭"（仅剩台账正式勾选收尾），P3-8 为远期决策门。1.6.x 视觉线遗留与治理小项不占用 P 编号，单点登记于 §4.4。
>
> **收敛结论（第十四版回写，v1.6.9 / 第三十七轮）**：P3-6 已正式勾选关闭、P3-5 的前端入口已还清，**P3 组九项自此仅剩 P3-8 一项**（远期决策门，随多轨立项评审触发）。§4.4 开放项由十项收敛为九项（还清 2、新增 1）。

| # | 建议 | 优先级 | 说明 / 证据 |
|---|---|---|---|
| P3-1 | 提交 git 未跟踪改动并打 tag | 🔴 高 | ✅ 已解决：1.5.3~1.6.8 各批次均已提交推送 fork/baseline-snapshot 并打注解 tag（v1.5.12=ad77266 / v1.6.0=b1a0d93 / v1.6.4=b007a3d / v1.6.5=5166cbe / v1.6.6=1dcb774 / v1.6.7=4154d31 / v1.6.8=批次主体 e1ed35c + 后记笔）；提交纪律延续 |
| P3-2 | temp/ 清理回潮（549.7MB） | 🔴 高 | ✅ 已处置（2026-09-21）：18 项移入回收站释放约 560.3MB；敏感残留（node_modules 全量/chrome-profile/secret.key）实测不存在 |
| P3-3 | 根目录产物物理清理 | 🟡 中 | ✅ 已处置（2026-09-21）：6 个 fpk 归档；嵌套目录 FVCC/FVCC/ 已清；运行时数据按禁删原则保留 |
| P3-4 | 文档同步 | 🟡 中 | ✅ 已执行并滚动收口：第十版对齐 v1.5.12 清零；后续 1.6.x 对齐随各轮文档回写（246e905 统一至 v1.6.7，第十三版随 v1.6.8 回写） |
| P3-5 | store 持久化健壮性 | 🟡 中 | ✅ 已关闭（2026-10-11，v1.5.8）：定时备份/写前备份（efef4bd）、data_version 迁移链（16f1043）、快照整包回滚 + POST /api/store/restore（59e4004）；**前端入口已随 v1.6.9 补齐**（设置页「数据备份与恢复」面板，本版第三十七轮交付）；残余缺口另登记于 §4.4（手动备份删除入口） |
| P3-6 | ffprobe 运行时环境依赖评估 | 🟡 中 | ✅ **已关闭**（2026-10-11 真机 `/info` `ffprobe:true`，第十一版核验实际已闭；**第十四版（v1.6.9 第三十七轮）台账正式勾选**——fnOS mediasrv 提供、FVCC 无 stub 降级被触发、启动自检 `main.go` 打印 `ffprobe=%v`。可选的 probe-vs-ffprobe 差异核对不再占 P 编号） |
| P3-7 | 覆盖率门槛上调 | 🟢 低 | ✅ 已关闭（2026-10-12 第二十六轮）：补 3 份测试后实测 60.7%，门禁 55%→60% |
| P3-8 | 远期决策门 | 🟢 低 | 🟡 收窄：P1-2 前端收敛已定案关闭（维持零框架）；Svelte 5 试点改绑多轨立项评审；仅剩 VideoInfo 复制 2 处随下次模型重构合并 |
| P3-9 | server/ 根 build_err.txt 待清 | 🟢 低 | ✅ 已清理（2026-10-12 第二十六轮）：0 字节文件已删，server/ 根归零 |

### 4.4 开放项与未验证分支（均为**未完成 / 待裁决**，不得当作已交付）

| 项 | 类别 | 当前状态 | 说明 / 阻塞点 |
|---|---|---|---|
| 概览 Filter Chips 状态过滤 | 1.6.x 视觉线遗留 | ⬜ 未完成（待产品拍板） | 1.6.4 三批次刻意未做项 |
| `?fx=stripes/breathe` 实验动画 | 1.6.x 视觉线遗留 | ⬜ 待裁决（默认关） | 需真机并发场景裁决转正或弃用 |
| 其余页面骨架迁移 listSkeleton | 1.6.x 视觉线遗留 | ⬜ 未完成 | 目前仅 tasks 页切换 |
| 手动备份删除入口 | v1.6.9 新登记 | ⬜ 待裁决 | `rotateBackupPool` 明确跳过 manual（永久保留），后端仅有 backup/backups/restore 三端点、**无 `DELETE /api/store/backups/:id`**——设置页面板里的手动备份只增不减，磁盘回收目前只能进 shell 手删。加删除端点=新破坏性 API + 审计 + 单测，本轮按"不扩范围"未做 |
| e2e B 档 #9：跨多授权根混合素材时间线 | e2e B 档 | ⬜ 未完成（需用户决策或第二台机器） | 当前明确拒绝 `E_EDL_INVALID`；附带 proxyByFile 裸相对文件名键跨根同名碰撞同属 #9 |
| e2e B 档 #7：judgeVisibility 忽略分辨率 | e2e B 档 | ⬜ 未完成（需用户给阈值） | 产品阈值决策 |
| 多轨工程立项 `{videoTracks,audioTracks}` + EDL 转场 | 结构性立项 | ⬜ 未立项 | P1-2/P2-2 框架决策绑定此项立项时一并评审 |
| 渲染 Failed→ERROR / Cancelled→CANCELLED 终态分支 | 未验证分支 | ⬜ 未验证 | 无自然复现路径；按约定"遇真实失败顺带验证" |
| Tailwind 4 迁移后的真机视觉对照 | 未验证分支 | ⬜ 待用户实测（v1.6.8） | 构建产物保真已核验（388 token 0 缺失/gzip 9.78kB），渲染观感需真机：低透明度色差、button 无 cursor:pointer 可点感、hover 门控触屏表现、forced-colors 焦点可见性；用户 NAS 实测中 |

> **本版（第十四版）移出 §4.4 的两项**：`FVCC/.commandcode/` 处置（已入 .gitignore）、store 备份/恢复前端入口（v1.6.9 已交付，见 §4.3 P3-5）；同时**新登记**「手动备份删除入口」（§4.3 P3-5 残余缺口）。

---

## 5. 结论（当前口径，2026-10-14）

FVCC 从首版评价（2026-09-18，1.4.0，6.5/10）到第十四版复核（2026-10-14，1.6.9）：**综合评分 6.5 → 4.3 → 2.8 → 2.4 → 2.8 → 2.4 → 2.4 → 2.3（轻度混乱）**，等级 B。

**保持的改善（高置信证据）**：
1. **安全硬伤清零并闭环**：凭据加密 / 删除回收站化 / 反射白名单 / 审计闭环（8 处调用点）/ git 版本控制；
2. **架构演进持续**：P2-1 后端分层（12 包 / 110 个 .go）+ P2-2 前端组件化 + P0-2 SQLite 迁移 + P1-1 事件驱动调度 + P3-5 备份/迁移/回滚链；
3. **质量保障闭环**：CI 流水线、覆盖率门禁 60%（实测 60.7%）、版本单一来源推进至 1.6.8（1.6.5 起校验"漂移即红"）；
4. **仓库治理落地**：temp 清理（约 560.3MB）、fpk 归档、0 字节残留归零、依赖刷新主题无余项（1.6.8：npm audit/outdated 双清零）。

**综合 2.4→2.3 的唯一依据（第十一版）**：① v1.6.5 把版本一致性校验从"Windows 静默 SKIP（全绿但零校验）"根治为"漂移即红"——消灭一类长期潜伏的一致性缺陷（1.6.4 已实际发生一次）；② v1.6.4 任务页三批次收敛信息层级/错误呈现/选中反馈并压掉每 WS tick 的 DOM churn。1.6.6~1.6.8 为纯依赖/工具链轮次，评分维持 2.3（正向增量：sqlite 跨 46 minor、Node 26、Tailwind 4，未单列新维度）。

**明确不涨分/未纳入本轮的事实**：覆盖率本轮未重跑（沿用第二十六轮 60.7%）、temp/archive 未重扫、NAS 真机视觉对照进行中。

**剩余债务**：P3 组**仅 P3-8 一项**（远期决策门，随多轨立项评审触发；P3-1~P3-7/P3-9 全部关闭，P3-6 于第十四版正式勾选）；§4.4 九项开放项全部标注未完成/待裁决。后续工作重心保持"清理纪律 + 文档实时同步 + 提交纪律"三件事。
