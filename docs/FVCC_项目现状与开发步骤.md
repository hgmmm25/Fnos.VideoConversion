# FVCC 项目现状与开发步骤

> 调查日期：2026-09-20
> 调查范围：`D:\Fnos.VideoConversion\FVCC`（视频转码调度端 Web 客户端，fnNAS 应用）
> 数据来源：实际目录扫描 + 源码阅读（server 后端 / ui-src 前端 / docs 文档），非记忆推断。

---

## 1. 现状概述

FVCC（Fnos Video Conversion Client）是 fnNAS 生态的视频转码调度端 Web 客户端：浏览本地素材、把转码/代理/渲染任务下发给 FVCS 渲染端、通过 WebSocket 实时跟踪进度。服务端 Go（gin），前端 Vite + TypeScript 零框架原生 DOM，产物以 fpk 形式打包供 fnOS 安装。

当前版本 **v1.5.12**（**2026-10-12 时点口径，2026-10-13 已被 v1.6.5 取代——见本段末「2026-10-13 更新·当前版本基线」**；manifest / ui-src/package.json / server/internal/version/VERSION 三处一致，2026-10-12 实测；1.5.3 已打 tag，SQLite 迁移三个提交已入库；1.5.4 已提交 080754c；1.5.5~1.5.7 Windows 路径边界修复 + 剪辑页素材递归扫描**已提交 68ebdec 并打 tag v1.5.7**（校正 2026-09-22 台账"工作区未提交"的过时记载，后续 9468ad5 .gitignore / 6b2f399 pathutil 收口）；1.5.8 为 P3-5 store 备份/迁移/回滚落地，**已推送 fork/baseline-snapshot 并打 tag v1.5.8**——见 §8 步骤 4 与 IMPROVEMENT_LOG §十二；**1.5.9 为步骤 8 e2e A 档缺陷修复起点（代理回填 #6 / 代理带根 #5 / 渲染未映射根早失败 #8 / scanner 删除按钮 #10 / FVCS 重连自动 online #11）；1.5.10 为复验后返工——#6/#5/#10/#11 真机通过，#8 原闸门4b 判为误修，改为渲染取根跨端对齐代理（FVCS `srcBase=mountPath` + FVCC 回退闸门4b，需重部署渲染机），并因 fnOS 仅接受更高版本覆盖安装而递增版本；1.5.11 为 #8 再复验后的提交侧收口——渲染仍报 `E_ASSET_MISSING` 定位到 `precheckAssets` 一律回落 `videoRoot`、丢弃片段所属授权根，改为「有界单根」：EDLClip 透传 `sourceRoot`，`resolveRenderRoots` 按片段所属授权根取根（同代理机制），跨根明确拒绝、旧无根片段回落 videoRoot，不动 checksum/WireClip；同轮追加第三层 FVCS `makeSrcResolver` 对齐代理 `BuildSMBPath`，修渲染落地 `sourceRoot 为空` 误报并重建 `fvcs-service.exe`（见 IMPROVEMENT_LOG §十五）；**1.5.12 为 #8 出片后再复验——FVCS 渲染已成功出片（`_exports` 内 MP4 可播放），但 FVCC 任务永远卡在「转码中 / 收尾·100%」。定位：RENDER_EDL 无终态收口（WS 只回推进度、不发终态事件，`ApplyRenderProgress` 不改 Status；调度分支仅 GEN_PROXY 有 `proxyWf.Poll`，RENDER_EDL 直接 return）。修复：FVCC-only 新增 `pollRenderTerminal` 按窗口 `QueryTaskDetail` 轮询终态（镜像 `proxyWf.Poll`）+ `completeRenderTask`（Success→COMPLETED+延迟归档 / Failed→ERROR 带节点码 / Cancelled→CANCELLED），渲染产物已落共享导出目录故无需代理式本地校验（见 IMPROVEMENT_LOG §十六）；至此 #8 全链路 = 取根三层 + 完成收口，端到端等价代理。**2026-10-12 真机复验通过（Success 收口 + 进度平滑推进），#8 全链路已提交并推送 `fork/baseline-snapshot`（800eb4c / de20d03 / d63d68e / f74ec54，另 a20cc6b .gitignore / 4dcdb26 根 README 对齐），未打新 tag（v1.5.12 即当前收口版本）；Failed/Cancelled 终态分支无自然复现，按约定遇真实失败顺带验证**）。

【2026-10-13 更新·当前版本基线】以上为 v1.5.12 历史记载（保留作当时状态快照）。**当前版本 FVCC v1.6.5 / FVCS v1.3.0**——三真源一致（`FVCC/manifest`、`FVCC/ui-src/package.json`、`FVCC/server/internal/version/VERSION` 2026-10-13 实测均为 `1.6.5`；FVCS `pkg/version/VERSION` = 1.3.0）。1.6.x 版本线（git 核验，manifest 版本序列为 `1.6.0 → 1.6.4 → 1.6.5`）：**1.6.0**（tag `v1.6.0` → `b1a0d93`，含 `1e622f0` 批次①-⑤ / `22035c6` FVCS GUI 复活）视觉打磨版——方向 A「精修冷灰」令牌层重排 / 控件层自绘（`selectField`、`.checkbox`、按钮变体）/ 外壳导航分组·搜索胶囊·状态栏 / 逐页修复 V1 趋势图·V5-V8 / 微交互 + WCAG 对比度（V9）验收；同批 FVCS 1.2.6→1.3.0（Fyne 托盘 + 设置窗口纳入构建 `build.ps1 -Target FVCS-UI`，新增 `FVCS/pkg/fynetheme`）。**1.6.4**（tag `v1.6.4` → `b007a3d`，含 `fe06e52` 代码）任务页视觉改进三批次 A+B+C，方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`（GLM5.3F / Step5P / Gemini-3.8F 三方外部模型评审整合）。**1.6.5**（tag `v1.6.5` → `5166cbe`，含 `ceeb1b2` 代码）VERSION 第三真源漂移修复 + 版本一致性校验加固。交付物 `FVCC/fvcc.fpk` 已重打并解包核验，真机 2026-10-13 确认 NAS「设置」页显示 **1.6.5**。**易错点**：`1.6.1` / `1.6.2` / `1.6.3` 只是批次开发过程中的本地中间 bump，**从未进入 git 历史、未出包、未打 tag**；方案文档文件名里的 "v1.6.1" 是**方案稿编号**、不是交付版本号——不可写成"1.6.1~1.6.4 版本线"，应写"**1.6.4**（方案稿 v1.6.1 的落地批次）"。
【2026-10-14 更新·当前版本基线】以上 1.6.5 记载保留为当时快照。当前基线 **FVCC v1.6.7 / FVCS v1.3.1**（三真源 2026-10-14 实测一致，`scripts/check-versions.ps1` 绿）。1.6.x 后半段为**依赖与工具链专项**：v1.6.6（tag `v1.6.6` → `1dcb774`，第三十轮）低风险刷新 + 配套 FVCS 1.3.1；v1.6.7（第三十二轮）`modernc.org/sqlite` **v1.14.0 → v1.60.1** 跨 46 个 minor 专项——升级前审干 store 层对驱动的接触面（`sql.Open` 无 DSN 参数、三条 PRAGMA 走 `db.Exec`、无驱动私有 API、SQL 为纯 SQLite 子集），升级后用老驱动真实产出的 fvcc.db 由新驱动经实际 store 路径 Open→migrate→写回→二次 Load 验证（WAL 保持、`schema_version` 不误重置、`AUTOINCREMENT` 续号、立即落盘可见），`go test ./...` 14 包全绿、依赖面净删 10 项、`go.sum` 净减 109 行，二进制 +565 KB / fpk +2.4%；第三十三轮 Node 运行时由**已 EOL 的奇数 Current 线 v25.9.0** 升到 **v26.11.1**（系统级 MSI，SHA256 与官方核对，另存 v25.9.0 回滚包），四处口径一起对齐：本机运行时、`engines.node >=26.0.0`（新增）、CI `setup-node` 22→26、`@types/node` ^22→^26；`npm ci` + `npm run build` 绿且 **asset 内容哈希逐项不变**，故该轮不再 bump、不重打包。规模与覆盖率**本轮亲跑复测（非沿用）**：后端 internal **12 包** / **110 个 .go**（58 实现 17,197 行 + 52 测试 9,183 行）、前端 TS **33 文件 11,474 行**、覆盖率 **60.7%**（`check-coverage.ps1 -Threshold 60` PASS）——与第二十九轮逐项一致，无漂移。**依赖刷新主题余项只剩 `tailwindcss` 3.4.19 → 4.3.3 专项**（CSS-first 重写 + `tailwind.config.js` 令牌迁移，同时也是 `npm audit` 现存 7 条告警的唯一修复路径——7 条全部源自 tailwindcss 3 的传递链 braces/chokidar/micromatch/postcss-selector-parser，Node 26 升级零新增告警）。台账轮次：`docs/IMPROVEMENT_LOG.md` §二十三（第三十二轮）/ §二十四（第三十三轮）。**提交状态**：v1.6.7 批次已按三段入库 `fork/baseline-snapshot`（`923a8b3` 工具链脚本 / `3bb1772` 依赖与版本 bump / `246e905` 文档对齐），**注解 tag `v1.6.7` 已打并推送**（tag 对象 `fd5b04e` → commit `4154d31`）。
【2026-10-14 二次更新·当前版本基线】以上 1.6.7 记载保留为当时快照。当前基线 **FVCC v1.6.8**（FVCS 保持 v1.3.1，本轮零渲染端改动）——三真源一致（`FVCC/manifest`、`FVCC/ui-src/package.json`、`FVCC/server/internal/version/VERSION` 均为 `1.6.8`）。**v1.6.8 即上条所述唯一余项的收口批次：`tailwindcss` 3.4.19 → 4.3.3 大版本专项**，走五道门禁——① 依赖与 PostCSS 接线（`@tailwindcss/postcss` 独立包，删 `autoprefixer`，前缀改由 v4 内建 Lightning CSS）；② `tailwind.config.js` **整文件删除**、token 映射迁入 `src/style.css` 的 `@theme inline`，暗色改 `@custom-variant dark`，`--c-*` 38 个令牌（明暗两套共 76 条定义）由裸 RGB 通道改为完整 `rgb()` 颜色（v4 不替换 `<alpha-value>`，`/NN` 透明度改编译期 `color-mix(in oklab, …)`）；③ 改名与语义漂移逐项对齐（`shadow-sm`→`shadow-xs`、`shadow`→`shadow-sm`、`outline-none`→`outline-hidden` 三处焦点环），扫描面用 `source(none)` + 显式 `@source` 锁定（v4 自动探测会把 `scripts/*.mjs` 注释里的类名编进产物），`check-design.mjs` 同步 R3 口径并新增 `TW4-token-form` 规则拦截 v3 遗留写法，顺带修复 `stripComments` 折叠多行块注释导致的行号错位；④ 构建与迁移保真校验（388 个类 token 与 v3 产物逐项比对，v3 有 / v4 无 = 0；22 个 `/NN` 淡色类全部存在、`<alpha-value>` 泄漏 0；CSS 原始 42,782→49,956 B、**gzip 10.00→9.78 kB**）；⑤ 文档回写（`FVCC/docs/DESIGN.md` 令牌映射机制 + BUILD.md 依赖基线 + 本份台账）与版本 bump、fpk 重打（`FVCC/fvcc.fpk` 11,013,500 B，解包核验 manifest 1.6.8 / `checksum` == `md5(app.tgz)` / banner `tailwindcss v4.3.3`）。`npm audit` **7 条告警全清零**，`npm outdated` **归零**——**依赖刷新主题至此完整收口，无余项**。一处产品裁决随批落地：接受 v4 移除 `button { cursor: pointer }` 的新默认（94 处按钮不再举手型），另两处 v4 新默认（`hover:` 受 `@media (hover:hover)` 门控、淡色走 oklab 插值）一并接受。**未验证面**：NAS 真机视觉对照（淡色、悬停/焦点、触屏、forced-colors）由用户实测中；渲染 Failed/Cancelled 终态分支仍无自然复现。台账轮次：`docs/IMPROVEMENT_LOG.md` 第三十四轮。


治理进展：P0-2 SQLite 迁移已落地（2026-09-21，提交 a72b8ff / 9e122d5 / 5dd373e）；P1 组（P1-1 ~ P1-5）全部落地或部分落地（P1-1 事件驱动调度已落地，提交 eab7688）；P2 组（P2-1 ~ P2-8）全部收口（后端 internal 分层、前端组件化、CI 流水线、API 契约统一、回收站化、AIGC 清理、测试命名规范化、破坏性操作审计）。**2026-10-12 文档全面对齐轮**：FVCC/README.md、FVCC/docs/ 7 份、根 BUILD.md/README.md 与根 docs/ 台账统一对齐 v1.5.12（helper 真源路径、SQLite 主载体口径、/api/store/* 接口段登记、SECURITY.md AIGC 残留清除、P2-2 决策门定案回写、README 设计规格链接修复）。设计文档引用速查见 `FVCC/README.md`，治理台账见 `docs/FVCC_混乱度评价报告_细化版.md`。**2026-10-13 叙述性长台账对齐轮**（IMPROVEMENT_LOG §二十 第二十八轮的文档对齐部分）：本份 `docs/FVCC_项目现状与开发步骤.md` 回写至 **FVCC v1.6.5 / FVCS v1.3.0** 基线（1.6.0 视觉打磨版 + FVCS GUI 复活、1.6.4 任务页视觉改进批次 A+B+C、1.6.5 VERSION 三真源漂移修复 + 版本一致性校验加固），数据来源为 2026-10-13 代码规模实测与 git 提交/tag 核验；本轮仅改本份长台账，其余文档口径见 §6.3。

**总体判断**：项目主干功能完整、工程质量较高（分层清晰、测试齐全、契约规范、SQLite 持久化落地），处于"功能已闭环、局部待打磨"阶段。真实待办集中在：ffprobe 生产环境依赖的进一步评估（P3-6）与远期决策门（P3-8）；覆盖率门槛上调已随 P3-7 关闭（2026-10-12 第二十六轮，实测 60.7%、门禁上调至 60%），server/build_err.txt 清理残留已随 P3-9 关闭（2026-10-12 删除）；store 定时备份/快照回滚已随 v1.5.8 落地关闭（P3-5，`/api/store/*` 仅后端 API，前端入口有意缓做），ffprobe 运行时依赖真机验证已满足（NAS `/info` ffprobe:true，2026-10-11）。**2026-10-13 更新**：1.6.x 视觉线遗留项为本批刻意未做的开放项——概览 **Filter Chips 状态过滤**（待产品拍板）、`?fx=stripes|breathe` **实验动画转正/弃用**（待真机并发裁决）、其余页面骨架迁移到 `listSkeleton()`（目前仅 tasks 页切换）、store 备份/恢复的**前端入口**（P3-5 只交付后端 API）；e2e B 档 **#7**（`judgeVisibility` 忽略分辨率，需用户给阈值）/**#9**（渲染/EDL 跨多授权根混合素材时间线，当前明确拒绝 `E_EDL_INVALID`，需跨端 payload 改造 + 真机验证）仍为**未完成/待裁决**。P3 债务仅剩 P3-6（ffprobe 生产依赖，**实际已闭**——NAS `/info` `ffprobe:true`）与 P3-8（远期决策门），P3-7 / P3-9 已于第二十六轮关闭。

### 1.1 代码规模（实测）

| 维度 | 文件数 | 行数 | 说明 |
|---|---|---|---|
| Go 后端（非测试） | 58 | 17,197 | `server/` 根包 + `internal/` 12 包 + `logger/` + `smbshare/`（2026-10-13 实测，**2026-10-14 在新 SQLite 驱动 + Node 26 下复测逐项一致**；v1.5.8 增 `internal/pathutil`，历史 2026-09-21 快照为 53 文件 / 14,218 行、2026-10-12 复测 17,191 行——+6 来自 1.6.5 的 `version.go` 注释） |
| Go 测试 | 52 | 9,183 | 含 e2e / 并发 / SQLite 迁移（sqlite_*_test.go 3 份）/ P3-5 备份-迁移-回滚 / #8 render_poll 回归（2026-10-13 实测；历史 2026-09-21 快照为 43 文件 / 6,853 行；49→52 为第二十六轮 P3-7 补 logger / smbshare / media 三份） |
| TypeScript 前端 | 33 | 11,474 | `ui-src/src/` 全部页面与逻辑（2026-10-13 实测；历史 2026-09-21 快照 10,315 行、2026-10-12 复测 11,257 行——增量来自 1.6.4 任务页批次） |
| **合计** | **143** | **37,854** | 不含 node_modules、构建产物、fpk 包（Go 合计 110 个 `.go`） |

### 1.2 构建产物与运行形态

- 根目录：构建产物与历史 fpk 包已于 2026-09-21 归档至根仓库 `archive/fpk/`（完整收纳 v1.1.0~v1.5.3）；`manifest`（应用清单，version=1.6.8，2026-10-14 实测）。实测修正：根目录**无** `fvcc.exe` / `fvcs-service.exe` / `fvcc.fpk` 物理堆积。
- 运行时数据（2026-09-21 复核，按禁删原则保留原位）：`config.enc.json`（加密配置）、`master.key` / `secret.key`（凭据密钥）、`credentials.db`（凭据库 SQLite，含 -shm/-wal）、`tasks.json` / `history_tasks.json`（任务存储）、`locks.json`（锁表）。实测修正：根目录**无** `task.db`（仅 `archive/` 下存 12KB 备份）。
- `config/`：fnOS 应用配置目录（privilege / resource）。
- `logs/`：运行时日志（按日期 + app_*.log）。
- `app/`：前端构建产物（vite 输出 + 静态资源），由 fpk 打包；`app/ui/assets` 为 hash 资源目录（2026-09-21 实测 13 个文件；1.6.x 多次重建后本轮未复核该计数，以最新构建的 index.html 引用为准核对）。
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
│   │   ├── store/             # 持久化（P0-2 已迁 SQLite 主载体 + JSON 回退 + 内存索引 + Flush 批量落盘）：store.go、store_edl.go、store_proxy.go、trash.go、sqlite.go、sqlite_import.go、sqlite_load.go、sqlite_persist.go、model/models.go 32K
│   │   ├── version/           # 版本信息（go:embed VERSION 单一来源）
│   │   └── ws/                # WebSocket Hub 推送与限流（ws.go、ws_limit.go）
│   ├── logger/                # 日志
│   ├── smbshare/              # SMB 共享能力
│   ├── temp/                  # 调试重定向残留（2026-09-21 已清空，当前为空目录）
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
│   └── package.json / vite.config.ts / postcss.config.js / tsconfig.json   # v1.6.8 起无 tailwind.config.js（Tailwind 4 CSS-first，token 映射在 src/style.css 的 @theme inline）
├── cmd/                       # fnOS 应用生命周期脚本（install / upgrade / uninstall / config，各含 init/callback）
├── config/                    # fnOS 应用配置（privilege / resource）
├── app/                       # 前端构建产物（git 忽略，vite outDir）
├── docs/                      # 项目内文档（7 份）
│   ├── API_CONTRACT.md        # 前后端 API 契约（错误码、路由、载荷）
│   ├── DESIGN.md              # 前端设计契约（Named Rules + check:design 门禁）
│   ├── PRODUCT.md             # 产品定位与反参照
│   ├── P2-1_后端分层_阶段A实施记录.md
│   ├── P2-2_细化细则.md
│   ├── SECURITY.md            # 安全设计落地文档（2026-09-21 补写，消除 main.go/models.go 悬空引用，见 §6.3）
│   └── STORE_PERSISTENCE_ROADMAP.md  # 持久化方案评审稿（SQLite 迁移已落地，剩余定时备份/快照回滚方案见 §8 步骤 4）
├── logs/                      # 运行时日志（git 忽略）
├── temp/                      # 打包 stage 目录（2026-09-21 已清理 15 个 fpk_stage* 与 fvcc-win-debug.exe，现仅剩 logs/，git 忽略）
├── manifest                   # fnOS 应用清单（UTF-8 无 BOM，version=1.6.8）
├── config.enc.json            # 加密配置（凭据相关）
├── credentials.db / -shm / -wal   # 凭据库（SQLite）
├── master.key / secret.key    # 凭据密钥
├── locks.json / tasks.json / history_tasks.json   # 运行时数据（task.db 无——为 FVCS 侧运行产物，仅 archive/ 下 12KB 备份）
├── fvcc.fpk / FVCC_v1.4.0~1.4.8_fnos_x86.fpk 已归档   # 构建产物已移入根仓库 archive/fpk/（现完整收纳 v1.1.0~v1.5.3；根目录无 exe / fpk 堆积）
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
| 存储 | SQLite 主载体（P0-2 已落地：modernc.org/sqlite v1.14.0 纯 Go 驱动，运行时 `fvcc.db`，12 张实体表 + schema v1 迁移链）+ JSON 文件回退（settings.json / tasks.json / history_tasks.json / servers.json / profiles.json / projects.json 等）+ `credentials.db` 凭据库（FVCS 侧运行产物）+ `_trash` 回收站目录 |
| 网络形态 | 生产：fnOS 网关 Unix Socket；开发：TCP 127.0.0.1:8088，前缀 `/app/fvcc` |

### 3.2 前端（ui-src/）

| 项 | 版本/内容 |
|---|---|
| 构建 | Vite 8.3（`base='/app/fvcc/'`，`outDir='../app/ui'`，`emptyOutDir: true`） |
| 语言 | TypeScript 7.0 |
| 样式 | TailwindCSS 4.3（CSS-first，`@tailwindcss/postcss`，无 `tailwind.config.js`）；设计令牌真源 `src/style.css`（`--c-*` 为完整 `rgb()` 颜色，`@theme inline` 注册 token 类，`/NN` 透明度编译期 `color-mix`） |
| UI 范式 | 零框架，`el()` 手工 DOM（`src/ui.ts`）；hash 路由（`#/editor/:id`） |
| 图表 | lightweight-charts（指标/图表） |
| 字体 | @fontsource/ibm-plex-mono / ibm-plex-sans（latin 子集，已瘦身） |
| 实时 | WebSocket（`/app/fvcc/ws`）+ SSE（`/video/scan-stream`） |
| 质量门禁 | `npm run check:design` / `check:design:diff`（scripts/check-design.mjs）、`check-versions.ps1` |

### 3.3 构建/CI

| 项 | 内容 |
|---|---|
| 全量构建 | `D:\Fnos.VideoConversion\build.ps1 -Target FVCC`（前端 + 后端 + fnpack 打 fpk） |
| CI | `.github/workflows/fvcc-ci.yml`（paths 限定 FVCC/**）+ `scripts/check-coverage.ps1`（覆盖率门槛 ≥60%，2026-10-12 P3-7 上调；演进：口径修复后 56.7%→59.0%→补测后 60.7%，为**第二十六轮实测值**——2026-10-13 本轮未重跑覆盖率） |
| 版本单一来源 | `server/internal/version/VERSION`，构建时 `check-versions.ps1` 校验三处一致（1.6.5 加固：定位失败 / 读取失败 / 字段缺失 / 版本不一致一律判红，移除 `t.Skipf`；新增 `TestAppVerIsNotPinnedLiteral` 扫硬编码版本字面量；脚本去硬编码仓库路径改由 `$PSScriptRoot` 推导、支持 `-RepoRoot` 覆盖） |

---

## 4. 核心模块说明

### 4.1 后端核心模块

| 模块 | 位置 | 职责 |
|---|---|---|
| **api** | `server/internal/api/` | HTTP 全部入口：handlers（settings/video/trash/servers/profiles/tasks/history/projects/render/edl）、router.go 路由挂载、stream.go 票据流、trash_handlers.go 回收站、ratelimit.go 限流、security_roots.go、aliases.go（P2-1 内联 shim 转发符号）、config.go 装配入口（NewHandlers / NewRouter） |
| **store** | `server/internal/store/` | 持久化域（P0-2 已迁 SQLite 主载体 + JSON 回退 + 内存索引 + 脏标记 Flush 批量落盘）：store.go（核心 + 定时落盘）、sqlite.go / sqlite_import.go / sqlite_load.go / sqlite_persist.go（SQLite 迁移四件套）、store_edl.go（EDL 项目）、store_proxy.go（代理任务）、trash.go（回收站，删除移入 `_trash` 同名时间戳后缀）；**P3-5（1.5.8）新增**：backup.go（定时/写前/迁移前/回滚前/手动备份 + manifest 验签 + 保留轮转）、migrate.go（data_version 锚点 + 版本化迁移链）、restore.go（快照整包回滚）；`model/models.go` 数据模型域（32K，含 Task/Server/Profile/Settings/Project/EDLClip 等 40+ 类型）；store.go 头部 `TODO(P1)` 已随 P3-5 落地移除 |
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

> 2026-09-21 复核：以下代码级 TODO 经全量关键词扫描确认；步骤 3 代码清理已关闭 3 项，其余仍存在；store.go:19 TODO(P1) 方案稿已出（见 §8 步骤 4）。
> 2026-10-11 更新：store.go:19 TODO(P1) 已随 P3-5 阶段 A~D 落地关闭（v1.5.8，见 §8 步骤 4）。
> 2026-10-13 更新（叙述性长台账对齐轮）：**P3 债务仅剩 P3-6（ffprobe 生产依赖，实际已闭——NAS `/info` `ffprobe:true`）与 P3-8（远期决策门）**，P3-7 / P3-9 已于第二十六轮关闭。**1.6.x 视觉线遗留（本批刻意未做，登记为未完成/待裁决）**：概览 Filter Chips 状态过滤（待产品拍板）、`?fx=stripes|breathe` 实验动画转正/弃用（待真机并发裁决）、其余页面骨架迁移到 `listSkeleton()`（目前仅 tasks 页切换，`ui.ts` 已新增该泛化组件）、store 备份/恢复的前端入口（P3-5 只交付后端 API）。**e2e B 档**：#9 渲染/EDL 跨多授权根混合素材时间线（当前明确拒绝 `E_EDL_INVALID`，v1.5.11 的有界单根已吃掉单根子集，需跨端 payload 改造 + 真机验证；`proxyByFile` / `GET /proxy` 仍按裸相对文件名为键、跨根同名碰撞同属 #9）、#7 `judgeVisibility` 忽略分辨率（需用户给阈值）——均**未完成**。另有结构性立项（多轨 `{videoTracks, audioTracks}` + EDL 转场，P1-2 freecut 框架试点绑于此）、未验证分支（渲染 Failed→ERROR / Cancelled→CANCELLED 无自然复现，遇真实失败顺带验证）、仓库治理小项（`FVCC/.commandcode/` 工具产物未跟踪也未进 `.gitignore`，待裁决）。

### 6.1 代码级真实 TODO（需开发处理）

| 位置 | 内容 | 影响 | 建议 |
|---|---|---|---|
| ~~`server/internal/store/store.go:19`~~ | ~~`TODO(P1)`：定时备份、配置迁移、快照回滚~~ | ✅ 已关闭（2026-10-11，v1.5.8）：P3-5 阶段 A~D 落地——backup.go 定时 30min/写前/迁移前/回滚前备份 + 保留 12 份轮转、migrate.go data_version 迁移链、restore.go 整包回滚 + snapshot.restore 审计；TODO 注释已移除（提交 efef4bd / 16f1043 / 59e4004） | — |
| ~~`server/internal/scheduler/scheduler.go:487 / 667`~~ | ~~`checkUploadProgress` / `checkDownloadProgress` 为空操作占位~~ | ✅ 已清理（2026-09-21）：remote 进度协议 Stage 不含上传/下载阶段内进度，空函数已删除并注释说明进度来源 | — |
| ~~`server/internal/scheduler/scheduler.go:828`~~ | ~~`TODO(B-06)` 注释：RenderDispatcher 未注入时保持 QUEUE~~ | ✅ 已清理（2026-09-21）：`main.go:170` 已注入 `scheduler.SetRenderDispatcher`，防注入死分支与过时注释已删除 | — |
| `server/internal/store/model/models.go:463` | Transition 片段转场字段 P0 为 nil 仅占位固化 JSON 形状 | EDL 转场能力未启用，数据结构已预留 | 与 FVCS 渲染端同步评估转场支持（跨端特性，建议单独立项）；**2026-10-13**：已登记为结构性立项「多轨 `{videoTracks, audioTracks}` + EDL 转场」，P1-2 freecut 框架试点绑在此项目上（见 §6.5 / §7） |
| `ui-src/src/pages/editor/index.ts:24` | `placeholder()` 占位函数 | assets/preview/timeline 真实面板已接入，现仅用于空态兜底与未启用面板 | ✅ 已更新（2026-09-21）：注释改为"空态兜底"，功能保留 |
| `server/internal/media/ffprobe.go:49` | ffprobe 二进制缺失时降级 stub 数据 | 探测结果非真实（功能可用但数据失真） | 运行时环境安装 ffmpeg/ffprobe（见 §8 步骤 1） |

### 6.2 环境依赖缺口

| 项 | 现状 | 动作 |
|---|---|---|
| ffprobe/ffmpeg | 本机构建机已装 ffprobe 9.0.1（2026-09-20 实测），fnOS 运行时待核验；未安装时探测走 stub | 部署/安装脚本补 ffmpeg 依赖；`check-versions.ps1` 或启动自检提示 |
| Go 工具链 | go.mod 要求 go 1.27.1，本机 go 1.27.1 可用（2026-09-20 实测）；fnOS 运行时需 `GOTOOLCHAIN=auto` | 保持 auto；勿依赖固定路径（历史踩坑：台账路径 `D:\fntv-tools\go\bin\go.exe` 已失效、系统 Go std 不完整） |

### 6.3 文档缺口

| 项 | 现状 | 动作 |
|---|---|---|
| `FVCC/docs/SECURITY.md` | ✅ 已补写（2026-09-21，第十七轮步骤 5），消除 `server/main.go:205/207`、`models.go:158` 与 gateway.go 对 docs/SECURITY.md 的悬空引用 | 无需处理（内容覆盖网关鉴权/凭据加密/审计/限流/路径约束） |
| `FVCC/README.md` 版本表 | 已更新至 **1.5.12**（2026-10-12 文档全面对齐轮），与工作区三处版本一致；并新增「近期版本线摘要 1.5.4~1.5.12」小节（该行为当时口径；本份长台账于 2026-10-13 已单独对齐至 v1.6.5，README 是否随轮更新以 README 自身为准） | 无需处理 |
| `FVCC/README.md` 目录树 | 已同步（2026-10-12 复核：补 `internal/pathutil` 与 store 的 SQLite/backup/migrate/restore 口径，docs 清单补全 7 份；修复设计规格 01~10 速查表相对链接 `../../WebVideoEditor_Design/`） | 无需处理 |
| 上层 `docs/IMPROVEMENT_LOG.md` 等 | 与代码现状需定期核对（如 P2 状态表） | 纳入版本发布 checklist |
| 本份长台账 `docs/FVCC_项目现状与开发步骤.md` | 已对齐 **FVCC v1.6.5 / FVCS v1.3.0**（2026-10-13 叙述性长台账对齐轮 = IMPROVEMENT_LOG §二十 第二十八轮的文档对齐部分）：版本基线与 1.6.x 版本线、§1.1 实测规模、待办/开放项收敛已回写；历史叙述（2026-09-20 调查日期、各步骤当时结论）保留原样 | 无需处理 |

### 6.4 清理项（低风险，不阻塞功能）

| 项 | 位置 | 说明 | 处置（2026-09-21） |
|---|---|---|---|
| 调试重定向残留 | `server/temp/` 下 `build*.txt` / `commit*.txt` / `fmt_out.txt` / `c2~c6.txt` 等 | 历史构建/提交重定向残留（含 0 字节与非 0 字节） | ✅ 已清理：`vet_err.txt`（0 字节空文件）已移入回收站；`build*/commit*/fmt_out.txt` / `c2~c6.txt` 等实测不存在；server/ 根曾存 `build_err.txt` 0 字节空文件（P3-9，**2026-10-12 第二十六轮已删除，server/ 根 0 字节文件归零**） |
| CDP 调试缓存 | `temp/chrome-profile-9223/`、`temp/chrome-profile-9224/` | 调试用 Chrome 用户数据 | ✅ 实测不存在，无需清理 |
| 打包 stage 残留 | `temp/fpk_stage/`、`fpk_stage_145/146/147/149/` 等 | 历史打包 stage 目录 | ✅ 已清理：15 个 `fpk_stage*` 目录已移入回收站 |
| 调试脚本/二进制 | `temp/cdp-fine-test.mjs`、`cdp-real-test.mjs`、`fvcc-win-debug.exe`、`temp/repro-data/`、`temp/secret.key`、`temp/locks.json` | 调试期产物 | ✅ 已清理：`fvcc-win-debug.exe`（34.19MB）已移入回收站；`cdp-*.mjs` / `repro-data/` / `secret.key` / `locks.json` 实测不存在 |
| 历史 fpk 包 | FVCC 根目录 `FVCC_v1.4.0_fnos_x86.fpk` ~ `FVCC_v1.4.9_fnos_x86.fpk` | 确认 fnOS 安装机制不再需要历史包后归档/清理 | ✅ 已归档：实测历史包非 10 个（v1.4.0~v1.4.8 早已归档）；本轮 6 个 fpk（v1.4.9~v1.5.3 + fvcc.fpk 副本）归档至根仓库 `archive/fpk/`，现完整收纳 v1.1.0~v1.5.3 |
| 工具产物目录（2026-10-13 新登记） | `FVCC/.commandcode/` | 工具产物，既未跟踪也未进 `.gitignore` | ⏳ 待裁决（清理或加入 `.gitignore`），不影响功能 |
| app/ui/assets 旧 hash | `FVCC/app/ui/assets/`（当前 13 文件） | 以 index.html 引用为准核对；vite 现配置 `emptyOutDir:true`，新构建后核对无多余 hash 资源 | 保持核对（13 文件为 2026-09-21 口径；2026-10-13 本轮未复核文件数，1.6.x 出包后以 index.html 引用为准） |

### 6.5 规划中未触发项（决策门保留）

| 项 | 说明 |
|---|---|
| ~~P1-2 前端收敛（freecut 双轨）~~ | ✅ **已定案关闭（2026-10-11，基线 1.5.8）**：方案 A——维持零框架，借 freecut 设计不借底座；排除"复用 freecut 底座"（本地解码 vs 票据流+NAS 渲染架构冲突）；详见 IMPROVEMENT_LOG §十三 与 `FVCC_UI设计优化方向.md` §7.2 |
| P2-2 决策门 | 组件化收敛已达标（样板残留 0），渐进式框架（Svelte 5 试点）评估**未触发**；随 P1-2 定案，该试点不再独立保留，**改绑"时间线双栏/多轨 `{videoTracks,audioTracks}` 立项"**（本文档 §7 功能 1 剩余子项）时一并评审 |
| CI 覆盖率门槛 | **≥60%**（P3-7 已关闭，2026-10-12：补 logger/smbshare/media 测试后实测 60.7%，check-coverage.ps1 默认值与 CI `-Threshold 60` 同步上调；历史：55% 基线 56.7%→59.0%） |

---

## 7. 剪辑页功能改进可行性方案（实现状态：功能 1 已实现 3/4，功能 2 已完整实现，功能 3 已删除，功能 4/5 新增已实现；2026-10-11 校准：1.5.4 已提交 080754c，1.5.5~1.5.7 Windows 路径边界修复 + 素材递归扫描已提交 68ebdec 并打 tag v1.5.7）

> 适用范围：`ui-src/src/pages/editor/`（剪辑编辑器，14 个 TS 模块）；功能 1/2/4/5 均为前端层改动，无架构性风险。
> 现状标注（2026-09-21 代码核查）：功能 1「视频栏改造」**已实现 3/4**——标题半透明浮层（timeline.ts `buildClipEl` 内 titleBar `bg-black/55` 覆盖缩略图下缘）、删除时长信息（行内 label + 时长已移除，移入片段 tooltip）、随时间滚动指示条（`playBarByClip` + `updatePlayhead` 按播放比例覆盖 + 底部亮线）；**未实现**：视频/音频双栏（仍为单轨 v1，无 `{videoTracks,audioTracks}` 数据模型）。功能 2「ALT+滚轮 / 双指缩放时间轴」**已完整实现**——ALT+滚轮（timeline.ts `onWheel` 校验 `e.altKey`、±20%/格、锚点 ms 回正滚动）、双指捏合（`onPinchDown/Move/End` 两指距离比值驱动、中点锚定、`touch-action:pan-x`）、scale 状态入 editorStore（0.25~8、`setScale`、缩放百分比 chip）。
> 现状标注（2026-09-22 复核，1.5.4~1.5.7）：功能 4「预览变速播放 J/K/L」**已实现（1.5.4，080754c）**——preview.ts `setSpeed(dir)` 驱动 `video.playbackRate`，PR 惯例同向 2x→3x→4x、最大 4x、反向/K 键重置；shortcuts.ts 接入 J/K/L；播放控制按钮图标化（ui.ts ICON_PATHS）。功能 5「素材递归子文件夹扫描」**已实现（1.5.7，已提交 68ebdec）**——assets.ts `recursive=true`，修复 v1.5.6 子文件夹视频不显示；配套 Windows 盘符根路径边界修复（stream/edl/trash/smbshare/handlers 五处）使 `D:\` 授权根下素材可正常预览/回收。
> 排期建议：剩余工作仅功能 1 的**双栏子项**（涉及数据模型 `{videoTracks,audioTracks}` 变更，需先确认后端/渲染端是否依赖单轨结构）；功能 2/4/5 已实现，无需排期。
> 2026-10-13 复核：双栏/多轨子项**仍未实现**（剪辑页仍为单轨 v1，无 `{videoTracks,audioTracks}` 数据模型），并已作为**结构性立项**登记——多轨 `{videoTracks,audioTracks}` + EDL 转场，P1-2 freecut 结论把框架试点绑在此项目上（见 §6.5）；1.6.4 视觉批次仅覆盖 tasks 页，其余页面（含剪辑页）列表骨架仍待迁移到 `listSkeleton()`。

| 功能 | 难度 | 关键点 | 状态 |
|---|---|---|---|
| 1. 视频栏改造 | 低~中 | 标题半透明浮层（纯 CSS）；删除时长信息（注意联动引用）；随时间滚动指示条（监听播放进度 + rAF/节流绘制）；改"视频/音频"双栏（唯一涉及数据模型 `{videoTracks,audioTracks}`，后端/渲染端需兼容） | ⚠️ 已实现 3/4（仅双栏未实现） |
| 2. ALT+滚轮 / 双指缩放时间轴 | 高 | wheel 监听 `e.altKey` 以鼠标为锚点缩放；触屏 pointer 双触点距离比值驱动；共用 scale 状态（建议放 editorStore），`touch-action:none` | ✅ 已完整实现 |
| 4. 预览变速播放 J/K/L | 中 | preview.ts `setSpeed(dir)` 驱动 `video.playbackRate`（同向 2x→3x→4x、最大 4x、反向重置）；shortcuts.ts 接入 J/K/L；控制按钮图标化 | ✅ 已实现（1.5.4，080754c） |
| 5. 素材递归子文件夹扫描 | 低 | assets.ts `recursive=true` 使子文件夹视频汇入列表；依赖 Windows 盘符根路径边界修复（盘符根 `D:\\` 双分隔符）才能命中子路径 | ✅ 已实现（1.5.7，已提交 68ebdec） |

**踩坑预警（基于 v1.2.3 白屏经验）**：指示条回调订阅注意变量声明顺序避免 TDZ 白屏；组件销毁时移除监听防泄漏（复用 P2-2 dispose 模式）。

---

## 8. 下一步开发步骤

> 完成状态总览（2026-09-21 实测，2026-10-11 更新）：**步骤 1 本机已就绪（部署机待核验）**；**步骤 2 部分完成**；**步骤 3 / 5 / 6 已完成（2026-09-21）**；**步骤 4 已完成（2026-10-11，v1.5.8，P3-5 阶段 A~D 落地）**；**步骤 7 已完成（2026-09-21 口径修复；2026-10-12 第二十六轮 P3-7 关闭：实测 60.7%、门禁上调 60%）**；**步骤 8 已执行（2026-10-11，真实 NAS+Windows 走查，①④⑤ pass / ②③ 缺陷），A 档五项缺陷已修复并本地提交（#6/#5/#10/#11 复验通过；#8 返工为渲染根跨端对齐 v1.5.10 → 再复验仍报 `E_ASSET_MISSING`，v1.5.11 提交侧「有界单根」按片段所属授权根取根，同轮追加 FVCS `makeSrcResolver` 落地加固 v1.5.11→出片；1.5.12 补 RENDER_EDL 终态收口 `pollRenderTerminal`（出片后由「收尾·100%」翻 COMPLETED），#8 端到端等价代理，**2026-10-12 真机复验通过并推送 fork/baseline-snapshot**）；B 档 #7/#9 延后——见 IMPROVEMENT_LOG §十四~§十六**。各步骤标题后〔〕内为实测状态标注。**2026-10-13 更新**：步骤 1~8 状态未变（均已收口或按决策门延后）；本轮新增的开放项不在步骤表内，而是 1.6.x 视觉线遗留（Filter Chips 待产品拍板 / `?fx=` 实验动画待真机裁决转正或弃用 / 其余页面骨架迁移 `listSkeleton` 目前仅 tasks 页 / store 备份前端入口未做）与结构性立项（多轨 + EDL 转场），详见 §6 与本文档 §1「总体判断」。

按"环境就绪 → 数据安全 → 代码清理 → 文档同步 → 质量门禁 → 端到端验收"顺序执行，每步含验收标准。

### 步骤 1：运行时环境就绪核验（约 0.5 天）〔✅ 本机已就绪（部署机待核验）〕

1. 在 fnOS 部署机安装 ffmpeg/ffprobe，确认 `which ffprobe` 有输出；本机构建机同验。
2. 验证 Go 工具链：在 `D:\Fnos.VideoConversion\FVCC\server` 执行：
   ```powershell
   $env:GOTOOLCHAIN = "auto"
   go version   # 应解析到 go 1.27.x 而非系统旧版
   ```
3. 前端依赖：`cd D:\Fnos.VideoConversion\FVCC\ui-src && npm ci`。
4. **验收**：`go build ./...`、`go test ./...`、`npm run build` 均通过；`ffprobe -version` 正常。

### 步骤 2：验证 ffprobe 探测真实性（约 0.5 天）〔⚠️ 部分完成：启动自检已存在（main.go:215 `ffprobe=%v`），stub 降级保留，未做运行时实测〕

1. 启动 dev server（`cd server && go run .`），调用 `POST /app/fvcc/api/video/probe` 传入真实视频路径。
2. 对比返回 `VideoInfo`（分辨率/时长/编码）与 `ffprobe` 直接输出是否一致；若返回 stub 数据（如默认 1080p），说明运行时缺 ffprobe。
3. **验收**：探测结果与 ffprobe 一致；缺 ffprobe 时启动日志有明确告警（若无，需在 main.go 装配阶段加启动自检提示）。

### 步骤 3：清理调度器占位与过时注释（约 0.5 天）〔✅ 已完成（2026-09-21）〕

1. ✅ 已执行：`main.go:170` 已注入 `scheduler.SetRenderDispatcher(remote)`，TODO(B-06) 注释与 `s.dispatcher == nil` 防注入死分支已删除。
2. ✅ 已执行：remote 进度协议 Stage 不含上传/下载阶段内进度，`checkUploadProgress` / `checkDownloadProgress` 空占位已删除并注释说明进度来源；`scheduler_edl_test.go` 中 TestB05SideGuards 过时用例一并移除。
3. ✅ 已执行：placeholder 注释已更新为"空态兜底"。
4. **验收（2026-09-21 通过）**：`go vet ./...` + `go test ./...` 全绿（11 包）；`npm run build` 通过；编辑器空态显示正常。

### 步骤 4：实现 store 备份/配置迁移/快照回滚（P1 TODO，约 2~3 天，🔴 涉及数据写入，需先评审）〔✅ 已完成（2026-10-11，v1.5.8）：阶段 A 备份 efef4bd → 阶段 B 迁移链 16f1043 → 阶段 C 整包回滚 59e4004 → 阶段 D 收口；单测覆盖备份验签/轮转/迁移幂等/损坏包拒绝〕

1. **方案评审稿已产出（2026-09-21，`FVCC/docs/STORE_PERSISTENCE_ROADMAP.md`）**，对齐 store.go:19 TODO(P1)，实施拆分 A（备份）→ B（迁移）→ C（回滚）→ D（收口）：
   - 备份：定时（默认 30min）+ 关键写前 + 手动 `POST /api/store/backup`；zip 含 manifest（SHA-256 校验），定时备份保留 12 份；
   - 迁移：`migrate.go` 版本化迁移链（From→To + Apply），幂等、迁移前自动备份、失败回滚；projects `SchemaVer=0→1` 收口为显式迁移；
   - 快照回滚：`POST /api/store/restore` 整包恢复（禁止部分回滚），恢复后运行时态重置 QUEUE（同崩溃恢复语义），新增审计类型 `snapshot.restore`。
2. 代码落地时路由挂载到 `server/internal/api/router.go`；破坏性恢复操作接入 `internal/security/audit.go` 的 `auditDestructive`。✅ 已落地（2026-10-11）：`POST /api/store/backup` / `GET /api/store/backups` / `POST /api/store/restore` 均挂 RequireAdmin；回滚审计新增枚举 `AuditActionSnapshotRestore`，成功走 store 内 `snapshot.restore(ok)` + 失败 `security.alert(error)`，handler 另记带操作者的 `AuditDestructive`。
3. **验收**：模拟损坏 settings.json → 回滚快照恢复；连续 3 次备份仅保留最新 N 份；`go test ./...` 含备份/迁移/回滚单测。✅ 已过（2026-10-11）：篡改 zip 验签拒绝且内存零改动、轮转 12 份保留、迁移链幂等/部分链跳步/未来版本拒启均有单测；全量 `go test ./...` 12 包绿（`-race` 需 Linux CI，本机 cgo 缺失未跑）；coverage 门禁 59.0%（阈值 55%）。**2026-10-13 补充**：P3-5 交付范围为**后端 API**，store 备份/恢复的**前端入口**属 1.6.x 视觉线刻意未做的开放项（见 §6 与 §1 总体判断），步骤 4 本身仍为已完成。

### 步骤 5：文档同步（约 0.5 天）〔✅ 已完成（2026-09-21；2026-10-12 随 v1.5.12 全量再同步：FVCC/README.md + FVCC/docs/ 7 份 + 根 BUILD.md；2026-10-13 本份长台账按 v1.6.5 基线单独对齐）〕

1. ✅ 已执行（2026-09-21 第十七轮）：按 `WebVideoEditor_Design/07-安全校验与凭据管理细则.md` 补写 `FVCC/docs/SECURITY.md`（网关鉴权、requireAdmin、DPAPI 凭据、限流、审计、路径约束），消除 `server/main.go:205/207`、`models.go:158`、gateway.go 注释悬空引用。
2. ✅ 已执行（2026-09-21）：`FVCC/README.md`「版本号单一来源」表 1.4.4 → 1.5.3；目录树补 `ui-src/src/lib/` 与 `FVCC/docs/` 下 P2-1/P2-2 文档。**2026-10-12 再同步**：版本表 → 1.5.12 + FVCS v1.2.6 对照 + 版本线摘要；目录树/文档体系表补全；SECURITY.md 清 AIGC 残留并补 snapshot.restore 审计与根校验跨端同规则；API_CONTRACT.md helper 路径改 internal 分层真源并登记 `/api/store/*` 接口段。README 非 docs/ 台账，治理状态记录统一维护于 `docs/IMPROVEMENT_LOG.md`。**2026-10-13 追加（叙述性长台账对齐轮）**：本份 `docs/FVCC_项目现状与开发步骤.md` 单独回写至 FVCC v1.6.5 / FVCS v1.3.0 基线，数据来源为 2026-10-13 实测与 git 提交/tag 核验（IMPROVEMENT_LOG §二十 第二十八轮的文档对齐部分）。
3. **验收**：README 三处版本与 `manifest` / `package.json` / `VERSION` 一致；`npm run check:design` 不回归。

### 步骤 6：清理低风险残留（约 0.5 天，🟡 删除类操作逐项确认）〔✅ 已完成（2026-09-21）〕

1. ✅ 已执行：`server/temp/` 打包 stage（`fpk_stage*` 共 15 目录）与调试残留 18 项已移入回收站，释放约 560.3MB（含误构建嵌套目录 `FVCC/FVCC/` 22.16MB、`fvcc-win-debug.exe` 34.19MB、`server/vet_err.txt` 0 字节空文件）；实测不存在 `build*/commit*/fmt_out.txt` / `c2~c6.txt` 等。
2. ✅ 已执行：`temp/chrome-profile-9223/9224` CDP 缓存实测不存在。
3. ✅ 已执行：`temp/cdp-*.mjs` / `repro-data/` / `secret.key` / `locks.json` 实测不存在；`temp/` 现仅剩 `logs/`（9KB）保留。
4. ✅ 已核对：`app/ui/assets/` 当前 13 文件，无多余 hash 残留（vite `emptyOutDir:true` 生效）。
5. ✅ 已执行：历史 fpk 实测非 10 个（v1.4.0~v1.4.8 早已归档）；本轮 6 个 fpk（`FVCC_v1.4.9~v1.5.3_fnos_x86.fpk` + `fvcc.fpk` 副本）归档至根仓库 `archive/fpk/`，现完整收纳 v1.1.0~v1.5.3。
6. **验收（2026-09-21 通过）**：删除全走回收站；`npm run build` 后 assets 无多余 hash 残留。

### 步骤 7：质量门禁与 CI 上调（约 1 天）〔✅ 已完成（2026-09-21 第十七轮口径修复：check-coverage.ps1 测试目标 `'.'`→`'./...'`，实测 56.7%；2026-10-12 第二十六轮 P3-7 关闭：补测后实测 60.7%，门禁 55%→60%；2026-10-13 本轮未重跑覆盖率，引用值仍为**第二十六轮实测 60.7%**）〕

1. 后端全量：`cd server && go build ./... && go vet ./... && go test ./...`（单测约 90s+，全绿为准）。
2. 前端门禁：`cd ui-src && npm run check:design && npm run build`。
3. ✅ 已执行（2026-09-21 第十七轮）：修复 `check-coverage.ps1` 口径 bug（测试目标原为 `'.'` 仅主包导致恒 0%，改 `'./...'` 后实测 **56.7%**），55% 门槛 PASS（EXIT=0）；修复 flaky 测试 `TestP1EventDrivenQueueDispatch`（2s 超时等待事件 + 状态轮询断言，覆盖率模式连跑 5 次全绿）；`fvcc-ci.yml` 基线注释更新 56.1% → 56.7%。实测 56.7% < 60%，门槛不上调（P3-7 延续）。
   ✅ **2026-10-13 追加（v1.6.5 校验加固 + 本轮验证）**：`version_test.go` 新增 `findRepoRoot`（以 `runtime.Caller` 测试源文件目录 + `os.Getwd()` 双起点向上搜索，判定标志为目录内同时存在 `FVCC/manifest` 与 `FVCC/ui-src/package.json`，不用 `.git` 因 tarball/CI 检出未必带），定位失败 / 读取失败 / 字段缺失 / 版本不一致四类一律 `t.Fatal`，**彻底移除 `t.Skipf`**；根因是旧实现把路径写成 `../../manifest`（从 `internal/version` 上溯 2 层实为不存在的 `FVCC/server/manifest`，正确是 4 层到 `FVCC/`），读不到即 Skip，导致 Windows 本机 `go test ./...` 长期"全绿但零校验"、漂移只在 Linux CI 暴露。另新增 `TestAppVerIsNotPinnedLiteral`（扫本包非测试 `.go`，出现 `"x.y.z"` 硬编码版本字面量即判红，守住 VERSION 单一真源）。本轮验证：`go vet ./...` 通过、`go test ./internal/version -count=1` 与全量 `go test ./...` 绿、`npm run build`（`check:design` + `tsc -b` + `vite build`）绿、`check-versions.ps1` 绿（含从任意 CWD 与 `-RepoRoot` 判红两端验证）；**覆盖率未重跑**，沿用第二十六轮实测 60.7%。
4. 全量打包回归：`D:\Fnos.VideoConversion\build.ps1 -Target FVCC` 产出新 fpk。
5. **验收**：三项全绿；CI 流水线跑通；fpk 产出成功。

### 步骤 8：端到端验收（约 1 天）〔🟡 已执行（2026-10-11，真实 NAS+Windows）：①转码/④回收站/⑤断联 pass；②代理/③EDL 渲染暴露缺陷；A 档 #5/#6/#10/#11 已修复（复验通过）；#8 渲染全链路收口——取根三层：①FVCS 落地 `srcBase=mountPath`（v1.5.10）②FVCC 提交侧「有界单根」按片段所属授权根取根（v1.5.11，`resolveRenderRoots`）③FVCS `makeSrcResolver` 对齐代理 `BuildSMBPath`（修 `sourceRoot 为空` 误报，重建 `fvcs-service.exe`）；完成收口第四层：④FVCC 补 RENDER_EDL 终态轮询 `pollRenderTerminal`/`completeRenderTask`（v1.5.12，出片后由「收尾·100%」翻 COMPLETED，见 IMPROVEMENT_LOG §十六）；**2026-10-12 真机复验通过（Success 收口 + 进度平滑）并已提交推送 fork/baseline-snapshot**（Failed/Cancelled 分支无自然复现，顺带验证）；B 档 #7 `judgeVisibility` 忽略分辨率（需用户给阈值）/ #9 渲染跨多授权根混合素材时间线（当前明确拒绝 `E_EDL_INVALID`，v1.5.11 的有界单根已吃掉单根子集，需跨端 payload 改造 + 真机验证；`proxyByFile` / `GET /proxy` 按裸相对文件名为键的跨根同名碰撞同属 #9）**仍延后、未完成**（2026-10-13 复核）。走查记录与缺陷台账见 IMPROVEMENT_LOG §十四~§十六〕

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
- 代码规模：Go 110 文件 / 26,380 行（非测试 58 / 17,197 + 测试 52 / 9,183）；TS 33 文件 / 11,474 行（2026-10-13 实测；历史快照 2026-09-21：Go 96 文件 / 21,071 行、TS 33 / 10,315；2026-10-12 复测：Go 107 文件 / 26,032 行（58 / 17,191 + 49 / 8,841）、TS 33 / 11,257）
- 版本核验：`manifest` / `ui-src/package.json` / `server/internal/version/VERSION` 三处一致 = **1.6.5**（2026-10-13 实测；FVCS `pkg/version/VERSION` = 1.3.0）；1.6.x 均已打 tag——`v1.6.0`→`b1a0d93`、`v1.6.4`→`b007a3d`、`v1.6.5`→`5166cbe`，推送链 `1e622f0` → `22035c6` → `b1a0d93` → `6f80233` → `2f5c4e7` → `649d079` → `fe06e52` → `b007a3d` → `ceeb1b2` → `5166cbe`（目标 `fork` = hgmmm25/Fnos.VideoConversion，分支 `baseline-snapshot`；另有 `origin` = zdongno01/... 非推送目标）。历史核验记载（2026-10-12 时点）：三处一致 = 1.5.12；1.5.4 已提交 080754c，1.5.5~1.5.7 已提交 68ebdec 并打 tag v1.5.7；1.5.8 为 P3-5 store 备份/迁移/回滚收口版本并打 tag v1.5.8；1.5.9~1.5.12 为步骤 8 e2e 缺陷修复与 #8 渲染全链路收口，已推送 fork/baseline-snapshot，v1.5.12 已于 2026-10-12 第二十六轮打注解 tag 并推送
- 复核记录：**2026-10-13 · 叙述性长台账对齐轮**（IMPROVEMENT_LOG §二十 第二十八轮的文档对齐部分）——本份 `docs/FVCC_项目现状与开发步骤.md` 由 v1.5.12 基线回写至 FVCC v1.6.5 / FVCS v1.3.0；数据来源为 2026-10-13 代码规模实测 + git 提交/tag 核验（本轮未重跑覆盖率，引用为第二十六轮实测 60.7%）；历史叙述（2026-09-20 调查日期、各步骤当时结论、旧的"工作区未提交"类记载）一律保留原样，仅在其后追加更新说明
- 复核记录：**2026-10-14 · Tailwind 4 专项轮**（IMPROVEMENT_LOG 第三十四轮）——本份回写至 **FVCC v1.6.8 / FVCS v1.3.1**：§1 追加 v1.6.8 基线段（依赖刷新主题完整收口）、§2 目录树删 `tailwind.config.js`、`manifest` version 与「技术栈」样式行改 Tailwind 4 口径。版本核验：`manifest` / `ui-src/package.json` / `server/internal/version/VERSION` 三处一致 = **1.6.8**（2026-10-14 实测，`check-versions.ps1` 绿）；v1.6.8 已按「同主题收口统一入库」提交：代码与文档批次 `e1ed35c`（23 文件 +1,219/−1,418，含 `tailwind.config.js` 删除）+ 台账后记一笔，**注解 tag `v1.6.8` 打在后记笔并推送 `fork/baseline-snapshot`**。零后端改动，故代码规模与覆盖率沿用 2026-10-14 复测口径（Go 110 / TS 33 文件 11,474 行、60.7%）不变
- TODO 扫描：`server/`、`ui-src/src/`、`cmd/` 全量关键词扫描（TODO/FIXME/未完成/占位/stub）；2026-10-13 复核——本轮 1.6.x 遗留项（Filter Chips / `?fx=` 实验动画 / 其余页面骨架 `listSkeleton` / store 备份前端入口）均为**刻意未做或待裁决**，非新增代码级 TODO 扫描发现
- 关键文件：`server/main.go`、`server/internal/api/router.go`、`server/internal/scheduler/scheduler.go`、`server/internal/store/store.go`、`ui-src/src/main.ts`、`ui-src/src/store.ts`、`ui-src/src/api.ts`、`ui-src/src/pages/editor/*.ts`
- 文档：`FVCC/README.md`、`FVCC/docs/*.md`、`D:\Fnos.VideoConversion\docs\*`
