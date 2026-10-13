# FVCC Web UI 设计优化方向

> 方法：按 `frontend-design` skill 的设计准则（排版 / 色彩与对比 / 布局与空间 / 视觉细节 / 动效 / 打磨清单 / AI Slop 检验），以 `D:\Fnos.VideoConversion\freecut\` 为设计目标参照，对当前 NAS 上运行的版本 `D:\Fnos.VideoConversion\FVCC\ui-src\` 提出可执行的优化方向。
>
> 口径说明（按你的补充）：
> - 设计目标参照：`freecut\`（上游参考项目，仅借其设计语言与设计契约范式，不借功能与框架）
> - 优化对象：`FVCC\ui-src\`（当前 NAS 上运行的版本）
> - 已排除：`FVCC_official_unpack\`（备份的源安装包，已迭代，不作为设计依据）
>
> 证据来源：`FVCC\ui-src\src\style.css`、`theme.ts`、`ui.ts`、`main.ts`、`index.html`、`pages\{tasks,settings,scanner,history,servers,profiles}.ts`、`pages\editor\{layout,preset,dialog,taskDrawer}.ts`、`lib\useListPage.ts`、`scripts\check-design.mjs`、`docs\DESIGN.md`；`freecut\DESIGN.md`、`freecut\PRODUCT.md`、`freecut\src\index.css`。（v1.6.8 起 token 映射的载体是 `style.css` 内的 `@theme inline` 块，`tailwind.config.js` 已随 Tailwind 4 迁移删除。）
>
> 状态（2026-09-22 复核，**2026-10-12 对齐 v1.5.12 再复核，2026-10-13 对齐 FVCC v1.6.5 / FVCS v1.3.0 三复核**）：P0 / P1-1~P1-5 / P2-2 无障碍 / P2-3 门禁 与 §7 美化图示化阶段 A-D 均已落地（当前版本 **1.5.12** 三处一致；1.5.3 已打 tag v1.5.3、1.5.4 已提交 080754c、1.5.5~1.5.7 已提交 68ebdec 并打 tag v1.5.7、1.5.8 已打 tag v1.5.8、1.5.9~1.5.12 已提交推送 fork/baseline-snapshot 并已于 2026-10-12 打 tag v1.5.12；UI 侧 1.5.9~1.5.12 仅剪辑页 sourceRoot 透传与代理回填等增量，本文档结论不变）；1.5.x 编辑器交互增强已实现（播放头随播放联动 / 时间轴缩放 / 预览最大化 / 变速播放 J-K-L，见 §1.1）；后端 P0-2 SQLite 迁移与 P1-1 事件驱动调度已落地，与本文档方向无冲突；剩余收尾见 §5.2 / §7.6；**定案（2026-10-11）**：§7.2 框架决策"维持零框架"转正，项目分析清单 P1-2（freecut 双轨）据此关闭。
>
> 2026-10-14 五复核补充（对齐 **FVCC v1.6.8**，IMPROVEMENT_LOG 第三十四轮）：**Tailwind CSS 3.4.19 → 4.3.3 专项落地**，这是本文档 §0 结论 1（令牌两套真源 / darkMode 策略）与 §1.2 设计令牌小节所依赖的"映射载体"的一次更换，**结论本身不变**：
>
> - 令牌真源仍是 `style.css` 的 `--c-*` 一套，只是变量值从裸 RGB 通道（`37 99 235`）改为完整颜色（`rgb(37 99 235)`）——v4 不再替换 `<alpha-value>`，注册色必须是完整颜色，`/NN` 透明度改由编译期 `color-mix(in oklab, X N%, transparent)` 落地。
> - `tailwind.config.js` **已删除**，token → 工具类的映射迁入 `style.css` 的 `@theme inline`；暗色机制由 v3 的 `darkMode: ['selector', '[data-theme="dark"]']` 改为 `@custom-variant dark (&:where([data-theme="dark"], [data-theme="dark"] *))`，`theme.ts` 的切换逻辑与三选项（`auto / light / dark`）零改动。
> - 扫描面由 v3 的 `content` 数组改为 `@import "tailwindcss" source(none)` + 显式 `@source`（v4 自动探测会把 `scripts/*.mjs`、`vite.config.ts` 注释里的类名编进产物）。
> - 已接受的 v4 新默认（2026-10-14 用户裁决）：preflight 不再给 `button / [role=button]` 加 `cursor: pointer`；`hover:` 变体包进 `@media (hover: hover)`；低透明度淡色走 oklab 插值，色感与 v3 的 sRGB 略有差异。焦点环三处 `outline-none` 改为 v4 的 `outline-hidden`（等价 v3 行为，保留 forced-colors 下的透明描边），R6/R9 无障碍结论不变。
> - 命名漂移清扫：`shadow-sm`→`shadow-xs`、`shadow`→`shadow-sm`（R3 门禁口径同步扩到 `shadow-xs/sm/md`）；388 个类 token 与 v3 产物逐项比对，真实丢失 0 项。
> - 依赖面：`autoprefixer` 移除（v4 内建 Lightning CSS），新增 `@tailwindcss/postcss`；tailwind 3 传递链带来的 7 条 `npm audit` 告警（2 moderate / 5 high）全部清零。**依赖刷新主题至此无余项。**
> - 构建链与 §5.2 门禁结论不变：`npm run build` 仍 `check:design` → `tsc -b` → `vite build`；新增 `TW4-token-form` 规则拦截 v3 遗留写法回潮。CSS 产物原始 42,782→49,956 B、gzip 10.00→9.78 kB。
> - **未验证面（待真机）**：低透明度淡色（`bg-primary/5`、`/15`、进度条纹）、悬停与焦点观感、触屏无悬停态、forced-colors 模式。

> 2026-10-14 四复核补充（对齐 **FVCC v1.6.7 / FVCS v1.3.1**，IMPROVEMENT_LOG §二十三 / §二十四）：v1.6.7 批次为 `modernc.org/sqlite` v1.14.0→v1.60.1 驱动专项与 Node.js 运行时 v25.9.0→v26.11.1 工具链批次，**零 ui-src 改动**（`npm run build` 的 asset 内容哈希逐项不变即为证），本文视觉结论维持不变。工具链侧的新增纪律与本设计契约的交集只有一条：`engines.node` 已写入 `ui-src/package.json`（>=26.0.0），`check:design` 门禁脚本 `scripts/check-design.mjs` 在 Node 26 下行为一致，无需调整。余项仅 `tailwindcss` 3.4.19→4.3.3 专项——它落地时**会直接触碰本文的令牌层与 `tailwind.config.js` 映射口径**，属需要重开设计对齐的大工程，已在 §7.7 之外的待办中登记。**（该项已于 v1.6.8 落地，见上条五复核。）**

> 2026-10-13 三复核补充：1.6.x 视觉线已全部落地并打 tag——1.6.0（方向 A「精修冷灰」令牌层 + 控件自绘 + 外壳 + 逐页修复 + 微交互/WCAG，同批 FVCS 1.2.6→1.3.0 Fyne GUI 复活）、1.6.4（任务页视觉改进批次 A+B+C，方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`）、1.6.5（版本三真源漂移修复与一致性校验加固）。本文 §2 差距对照与 §3 方向结论**均维持不变**（1.6.x 是在已收敛的令牌语言上做精修与热路径治理，未推翻任何方向）；设计侧落实明细与新增的未完成项见 §7.7，规模实测见 §1.1。台账轮次：第二十八轮 / `docs/IMPROVEMENT_LOG.md` §二十。

---

## 0. 核心结论（先看这五条）

| # | 结论（原始判断） | 当前状态（2026-09-21 源码复核） |
|---|------|------|
| 1 | 设计令牌两套真源、darkMode 策略不一致 | ✅ 已收敛：body 改 `bg-page text-ink`，预览底改 `--c-preview-bg`，darkMode 改 `['selector','[data-theme="dark"]']`（v1.6.8 起该策略由 `style.css` 的 `@custom-variant dark` 承载，语义不变） |
| 2 | 通用后台面板色彩语言 vs 专业工具语言 | ✅ 已升级：引入 `--c-signal` 单一信号色，语义色收敛为 success/danger/warning |
| 3 | 无障碍与交互状态硬缺陷 | ✅ 已修：.btn 五态 + 2px 焦点环、移动端可点 ≥44px、动画去 max-height 且尊重 reduced-motion |
| 4 | 编辑器页比列表页更专业（机会点） | ✅ 已固化：clip 数据色 token、导航分组、全局状态条、命令面板（Ctrl+K）、工作区布局预设（default/compact/preview） |
| 5 | 缺设计契约 + 机器校验门禁 | ✅ 已落地：`docs/DESIGN.md`（R1-R10）+ `scripts/check-design.mjs`（机器化子集 R1-R5），已挂入 `npm run build` |

---

## 1. 现状盘点（有源码依据）

### 1.1 技术底座与结构

- 版本：ui-src `package.json` / 服务端 `VERSION` / `manifest` 三处一致——2026-09-22 工作区实测 **1.5.7**，**2026-10-12 实测 1.5.12**（1.5.3 已提交打 tag v1.5.3，1.5.4 已提交 080754c，1.5.5~1.5.7 已提交 68ebdec 并打 tag v1.5.7；1.5.8 打 tag v1.5.8；1.5.9~1.5.12 为 e2e A 档修复与 #8 渲染收口批次，已推送 fork/baseline-snapshot、v1.5.12 已于 2026-10-12 打 tag；1.5.0~1.5.3 为编辑器交互增强 + SQLite 迁移，1.5.4~1.5.7 为变速播放 + 按钮图标化 + Windows 路径边界修复，1.5.8 为 P3-5 持久化收口，1.5.9~1.5.12 为渲染/代理链路收口）；**2026-10-13 实测 1.6.5 三处一致**（`FVCC/manifest` / `FVCC/ui-src/package.json` / `FVCC/server/internal/version/VERSION`；渲染节点端当前基线 FVCS **v1.3.0**，随 1.6.0 同批由 1.2.6 升上来；真机验收：用户 2026-10-13 确认 NAS「设置」页显示 1.6.5）。**2026-10-14 复跑：1.6.6 / 1.6.7 / 1.6.8 三处真源均一致**（1.6.8 为 Tailwind 4 专项版本，fpk 已出、NAS 真机视觉核对由用户进行中）。1.6.x 版本线 git 事实：tag `v1.6.0` → `b1a0d93`（含 `1e622f0` 批次①-⑤、`22035c6` FVCS GUI 复活）、tag `v1.6.4` → `b007a3d`（代码 `fe06e52`，任务页视觉改进三批次）、tag `v1.6.5` → `5166cbe`（代码 `ceeb1b2`，版本一致性收口）。**易错点**：manifest 版本序列实测为 `1.6.0 → 1.6.4 → 1.6.5`，`1.6.1/1.6.2/1.6.3` 只是批次开发过程中的本地中间 bump，从未进 git 历史、未出包、未打 tag；方案文档文件名里的 "v1.6.1" 是**方案稿编号**不是交付版本号，应写"**1.6.4**（方案稿 v1.6.1 的落地批次）"。
- Vite + TypeScript + Tailwind，**零框架**。依赖 `@fontsource/ibm-plex-sans` / `@fontsource/ibm-plex-mono`（^5.3.0）、`lightweight-charts`（^5.2.1，异步 chunk 仅 history 页签加载）与 vite（^8.3.0）/ typescript（^7.0.2）/ tailwindcss（^4.3.3，v1.6.8 起；此前 ^3.4.17）工具链；**无 React/Vue/Preact、无路由库、无状态库、无 UI 组件库**。DOM 由 `src/ui.ts` 的 `el(tag, attrs, children)` 手工构造，图标为内联 SVG 路径表（`ICON_PATHS`，统一 `stroke-width: 2`）。
- 规模：`src/` 共 **33 个 .ts 文件、约 10,315 行**（2026-09-21 实测；2026-10-12 复测 33 文件 / **11,257 行**，增量为 1.5.9~1.5.12 剪辑页 sourceRoot 透传与代理回填等；**2026-10-13 复测 33 文件 / 11,474 行**（较上一记载 +217 行，增量来自 1.6.4 任务页批次，含 `ui.ts` 新增的 `listSkeleton` 泛化组件））；7 个页签页 + editor 子模块 14 个文件（timeline 599 / assets 606 / preview 533 / index 556 行）；最大单文件 scanner.ts（1,332 行）、profiles.ts（1,168 行）。相对上一版复核：scanner/profiles 因分页与重构精简，tasks（523 行）因队列可视化增大；新增 `lib\` 公共层（`useListPage` 列表流程 / `formBuilder` / `crudActions` / `scrollPos`）与 editor 的 dialog / editorStore / preset / ruler / shortcuts / taskDrawer / taskView 等拆分文件。editor 子模块行数与最大单文件为 2026-09-21~2026-10-12 期实测，本轮（2026-10-13）只复核了文件数与总行数两项，未逐项重测。
- 构建产物（`vite` `outDir: '../app/ui'`）：主 chunk `index-*.js` **192.6 KB（无代码分割）**、CSS 36.3 KB、字体 192.2 KB、lightweight-charts/standalone 异步 chunk 189.6 KB（仅 history 页签动态加载），约 13 个核心产物文件（2026-09-20 实测）。后端 Go(gin) 托管，网关前缀 `/app/fvcc`。
- 路由：`main.ts` 中的 hash 路由，7 个平级页签——`剪辑(editor) / 任务清单(tasks) / 视频文件(scanner) / 转码服务器(servers) / 转码方案(profiles) / 历史任务(history) / 设置(settings)`；`editor` 支持 `#/editor/:projectId` 子路由与 `renderProjectPicker`。
- 外壳：顶部导航已按 `NAV_GROUPS` 分组（编辑工作区 / 资源 / 运行 / 系统，main.ts:35），移动端（`md` 以下）折叠为下拉菜单；外壳底部已有**全局状态条**（节点 / 队列 / WS，main.ts:233-274）；**全局命令面板 Ctrl+K** 已接入（main.ts:98）。
- 图示化（§7 已落地）：tasks 队列状态堆叠条、history 趋势图（lightweight-charts）、servers 负载圆环（SVG 自绘）、scanner 格式/大小占比条与分页、editor 进度复用。
- 1.5.x 编辑器交互增强（2026-09-22 复核，工作区 1.5.7）：① 播放头随预览播放联动推进（index.ts:479 `preview.onTime → timeline.setTime`，时间线竖线 playhead + 片段覆盖指示条）；② 时间轴 ALT+滚轮 / 双指捏合缩放（editorStore `scale` 0.25~8，timeline.ts 锚点保持 + 缩放百分比 chip，`touch-action:pan-x`）；③ 预览最大化按钮（preview.ts maxBtn，Esc / 再次点击还原，全屏态 root 自滚动）；④ 预览变速播放（1.5.4，preview.ts `setSpeed(dir)` 驱动 `video.playbackRate`，PR 惯例 J 快退 / K 暂停 / L 快进，同向 2x→3x→4x、最大 4x、反向重置；shortcuts.ts 接入 J/K/L）；⑤ 播放控制按钮图标化（1.5.4，ui.ts ICON_PATHS 新增 rewind/play/ff 内联 SVG，文字按钮换图标按钮）；⑥ 素材递归子文件夹扫描（1.5.7，assets.ts `recursive=true`，子文件夹视频汇入列表，修复 v1.5.6 不显示）。

### 1.2 设计令牌

- `style.css`：`:root`（明亮主题）与 `[data-theme="dark"]` 两套变量，命名体系为 `--c-primary / --c-accent / --c-signal / --c-danger / --c-warning / --c-success / --c-download / --c-neutral / --c-surface / --c-line / --c-ink(-muted/-subtle)`，每类色另带 `-soft` 与 `-soft-text` 变体；另有 `--c-preview-bg`（编辑器预览底）、`--c-drop-line` + `--drop-line-width`（拖拽落点指示线）、`--ansi-*`（日志 ANSI 语义色登记豁免，随主题明暗切换）。**v1.6.8 起变量值为完整 `rgb()` 颜色**（此前为裸 RGB 通道 `37 99 235`，配 `rgb(var(--c-x) / <alpha-value>)` 使用）。
- token 映射载体：**v1.6.8 起为 `style.css` 内的 `@theme inline` 块**（`--color-x: var(--c-x)` → 生成 `bg-x / text-x / border-x` 等 token 类）；此前的 `tailwind.config.js` 已删除。透明度用类名 `/NN` 修饰符，v4 编译期落为 `color-mix(in oklab, …)`；手写 CSS 侧用 `var(--c-x)`，半透明用 `color-mix()`。
- `theme.ts`：三选项主题 `auto / light / dark`，`localStorage` 键 `fvcc-theme`，并兼容 fnOS 桌面下发的 `fnos-theme-mode`。
- 组件类（`@layer components`）：`.btn` / `.btn-accent` / `.btn-danger` / `.btn-sm` / `.input` / `.card` / `.badge`。
  - `.card` = `bg-surface rounded-lg`（已去默认阴影）
  - `.badge` = `rounded-full` 胶囊
  - `.input` 聚焦为 `focus:ring-2 focus:ring-primary/40 focus:border-primary`

### 1.3 动效现状

`style.css` 定义 keyframes：`slide-down` / `slide-up` / `fade-in-up` / `fade-in` / `scale-in` / `pulse-soft` / `slide-in-right` / `progress-stripes` / `skeleton-shimmer`。
**P0-3 已完成**：`slide-*` 弃用 `max-height`（解除 300px 截断），改用 `transform + opacity`；引入缓动 token `--ease-out-strong` / `--ease-in-out-strong`；`@media (prefers-reduced-motion: reduce)` 全局降级。

### 1.4 页面级范式（抽样，2026-09-21 复核）

- `tasks.ts`（523 行）：每任务一张 `.card`，内部横向排布 `拖拽手柄 + 复选框 + 状态 badge + 文件名/元信息 + 进度条 + 最多 4 个按钮`；进度色由 `getPhaseStyle()` 映射（上传=`primary`、转码=`signal`、下载/完成=`success`、其余=`neutral-soft`）；**B-阶段队列可视化**：状态占比堆叠条 + 计数摘要（tasks.ts:105，自绘 CSS 零依赖）；进度条 `role="progressbar"` + `aria-valuenow`（tasks.ts:257/407-410）；拖拽落点已收敛为 `--c-drop-line`；空状态调用 `emptyState()`。
- `settings.ts`（361 行）：单张 `.card` 承载全部设置，分段用 `border-t border-line`；**已改骨架屏** `skeletonRows(6, 'h-10')`（settings.ts:24）；日志查看器为自建 overlay + `<pre>`，ANSI 颜色映射为 `--ansi-*` 变量，且已补对话框语义（role=dialog / aria-modal / Esc / 焦点陷阱 / 返回焦点，settings.ts:282）。
- `scanner.ts`（1,332 行）：**D-阶段分页**（PAGE_SIZE=100，scanner.ts:155-175）+ **C-阶段格式/大小占比条**（scanner.ts:757-760，基于扫描全量、不随搜索词变化）；目录浏览 / 重命名 / 删除确认 / 创建任务 4 个自建弹窗均补对话框语义（scanner.ts:330/439/1145/1350）；表格骨架屏 `skeletonRows`（scanner.ts:1123/1155）；批量创建任务用 `showBatchResult` 逐项汇报（scanner.ts:1342）。
- `history.ts`（329 行）：**B-阶段历史趋势图**——`lightweight-charts` 动态 import（history.ts:307-334，耗时柱状 + 成功率/趋势折线，canvas 渲染、色板取自 token）+ 自带分页控件。
- `servers.ts`（349 行）：**C-阶段负载圆环** `miniGauge`（servers.ts:55-60，纯 SVG 自绘零依赖，进行中任务占比环 + 计数）；服务器卡含在线状态 / 负载统计 / 测试按钮。
- `editor\layout.ts`：顶栏 `h-11` + 中部（素材库 320px 可折叠 + 拖拽调宽，区间 240–480）+ 底部（时间线 160px 可调，区间 96–320，上方 `h-9` 工具条）；**P1-5 工作区布局预设** `LAYOUT_PRESETS`（default / compact / preview，layout.ts:47-50），一键切换并持久化，拖拽尺寸后自动降级 `custom`；预览区背景走 `--c-preview-bg`；尺寸持久化于 `localStorage: wve.layout.v1`。
- `editor\preset.ts`：渲染方案（presetKey）服务端权威枚举表的前端展示层（含硬编降级提示），与 `handlers_render.go` 对齐。

### 1.5 freecut 的设计语言（参照系，摘其 DESIGN.md / PRODUCT.md）

- 北极星：**"The Quiet Instrument"**——器材感、克制、素材是屏幕上最亮最饱和的东西。
- 色彩：OKLCH 中性石墨阶梯（`0.12 → 0.95`），**单一暖橙信号色** `oklch(0.68 0.19 45)` 只用于播放/活动态/焦点/播放头；clip 与 marker 色是**数据色**不是装饰色。
- 规则化（Named Rules）：*The One Signal Rule*（信号色稀少才可读）、*The Value-Hierarchy Rule*（层次靠明度阶梯，不靠边框与阴影）、*The Mono-For-Data Rule*（时间码/帧号/FPS/分辨率一律用 IBM Plex Mono）、*The Flat-By-Default Rule*（静止面扁平，抬升先抬明度）、*The Meaning-Bearing Hue Rule*（类型色承载语义，不可挪用）。
- 明确的反面清单：消费级剪辑器糖果风、炫技 SaaS 仪表盘（渐变头图 / glassmorphism / 大数字指标卡）、拥挤的老式 NLE 灰工具栏。
- 无障碍：正文 ≥4.5:1，占位符同标准；**有意只做暗色**，对比度调整发生在暗阶内。

---

## 2. 差距对照（2026-09-21 复核）

| 维度 | freecut（目标） | FVCC 原始差距 | 当前状态 |
|------|------------------|------------------------|----------|
| 色彩空间 | OKLCH，中性带品牌色相偏移 | sRGB 纯 slate，无色相偏移 | ◐ 保留 sRGB 体系（未引入 OKLCH）；中性仍为纯 slate，未做品牌色相偏移 |
| 主色策略 | 单一信号色（暖橙），只表示"活动/进行中" | 蓝主色多角色 + 7 类语义色 | ✅ 已收敛：`--c-signal`（#6366f1）信号色落地，语义色收敛为 success/danger/warning；tasks `getPhaseStyle` 转码用 `bg-signal` |
| 层次表达 | 明度阶梯，无默认阴影 | `.card` = `shadow-sm + border` | ✅ 已去默认阴影：`.card` = `bg-surface rounded-lg`；分档改明度（surface / surface-alt / page） |
| 主题 | 有意 always-dark | light/dark 定位差异 | 有意保留双主题（见 §4），未跟随 always-dark |
| 字体 | IBM Plex Sans + Mono，数据一律 mono | 无字体策略、系统栈 | ✅ 已落地：`@fontsource/ibm-plex-sans(400/500/600)` + `ibm-plex-mono(400/500)` 本地打包；`formatDuration` 统一 `HH:MM:SS`（ui.ts:110-111） |
| 动效 | easing token，只动 transform/opacity | max-height + 无缓动 token | ✅ 已修：`--ease-out-strong`/`--ease-in-out-strong` + transform/opacity + reduced-motion 降级 |
| 控件状态 | 5 态齐备 + 焦点环可见 | `.btn` 无五态细分 | ✅ 已补：.btn hover/active/focus-visible/disabled 四态 + 统一 2px 焦点环 |
| 无障碍 | 明示 AA 与占位符 4.5:1 | `--c-ink-subtle` 2.6:1 | ◐ 大部分已修：对话框语义 / 进度条 aria / 骨架屏已全量落地；hint 对比度仍需复核 |
| 触控/密度 | 控件 36px 基准 | `btn-sm` 约 26px | ✅ 已修：移动端（<768px）可点区域强制 ≥44px |
| 导航信息架构 | 侧边栏 + 面板系统 | 7 个平级 tab、无全局状态 | ✅ 已升级：`NAV_GROUPS` 分组导航 + 外壳底部全局状态条 + 全局命令面板 Ctrl+K |
| 编辑器签名面 | 布局系统 + 数据色 token | 可拖拽但无预设 | ✅ 已固化：clip 数据色 token + 工作区布局预设（default/compact/preview） |
| 设计契约 | DESIGN.md + named rules + design.json | 无设计文档、规则散落 class | ✅ 已落地：`docs/DESIGN.md`（R1-R10）+ `scripts/check-design.mjs`（机器化子集 R1-R5：禁 hex / 禁原色板 / 禁默认阴影 / 禁 <12px / 禁 max-height 过渡），已挂入 `npm run build`（R6-R10 未脚本化，见 §5.2） |

---

## 3. 优化方向（按优先级）

### 3.0 落地状态总览（2026-09-21 源码复核）

| P 项 | 状态 | 源码证据 |
|------|------|---------|
| P0-1 令牌收敛为单一真源 | ✅ | index.html body `bg-page text-ink`；editor 预览底 `--c-preview-bg`（layout.ts）；darkMode 改 `['selector','[data-theme="dark"]']` |
| P0-2 对比度与焦点可见性 | ✅ | style.css `.btn` 五态 + 统一 2px 焦点环 |
| P0-3 修正动画实现 | ✅ | style.css：去 max-height、缓动 token、reduced-motion 降级 |
| P0-4 触控目标 | ✅ | style.css：移动端 `min-height:44px` |
| P1-1 单一信号色 + 中性明度阶梯 | ✅ | style.css:32-34 `--c-signal`；tasks `getPhaseStyle` 转码用 `bg-signal` |
| P1-2 明度阶梯替代边框 + 阴影 | ✅ | `.card` 去 shadow（`bg-surface rounded-lg`） |
| P1-3 字体与数据排版 | ✅ | main.ts fontsource IBM Plex；ui.ts `formatDuration` HH:MM:SS |
| P1-4 信息架构与全局状态 | ✅ | main.ts:35 `NAV_GROUPS` 分组、:98 命令面板（Ctrl+K）、:233-274 全局状态条 |
| P1-5 编辑器作为签名面 | ✅ | clip 数据色 token（style.css:64）+ 工作区布局预设 `LAYOUT_PRESETS`（layout.ts:47-50） |
| P2-1 状态完备 | ◐ | 骨架屏（settings/scanner）+ `showBatchResult` 逐项结果已做；**乐观更新未落地** |
| P2-2 对话框与语义无障碍 | ✅ | scanner 4 弹窗 / settings 日志 / editor dialog / taskDrawer 均含 role=dialog/aria-modal/Esc/焦点陷阱；tasks 进度条 aria-valuenow |
| P2-3 设计契约与门禁 | ✅ | `docs/DESIGN.md`（R1-R10）+ `scripts/check-design.mjs`（R1-R5），`build` 已挂钩 `check:design:diff` |

### 3.1 P0 —— 正确性与合规（已全部完成）

P0-1 令牌收敛 / P0-2 对比度与焦点 / P0-3 修正动画 / P0-4 触控目标 均已落地，证据见 §3.0。无遗留动作。

### 3.2 P1 —— 设计语言升级（已全部完成）

P1-1 单一信号色 / P1-2 明度阶梯 / P1-3 字体与数据排版 / P1-4 信息架构与全局状态 / P1-5 编辑器签名面 均已落地，证据见 §3.0。
其中 P1-5 的"工作区布局预设"已在 `layout.ts` 以 `LAYOUT_PRESETS`（default / compact / preview / custom，拖拽后自动降级 custom）实现。

### 3.3 P2 —— 打磨与规模化（部分完成）

**P2-1 状态完备 —— ◐ 仅剩乐观更新**
- ✅ 已做：列表/表单骨架屏（settings 整卡、scanner 表格与目录树）；空状态承载下一步动作按钮（history/scanner/servers/profiles 均有）；批量操作逐项结果 `showBatchResult`（成功 N、失败 M 及原因）；错误态 `renderPageError` + `useListPage` 公共错误 toast。
- ⬜ 未做：低风险动作（选中、排序、静音切换）的**乐观更新 + 失败回滚**。
- ◐ 2026-10-13 补（1.6.4 批次 A/C）：任务卡状态反馈已补齐（`errorMsg` 独立成行带 `title`、批量三键零选中禁用 + 刷新 pending、`.task-selected` 选中即时高亮、活动卡左侧 `signal` 静态指示线、`getPhaseStyle` 补全状态文案且 ERROR 走红条）；`ui.ts` 新增 **`listSkeleton` 泛化骨架组件**并已接入任务页首屏——**其余页面骨架尚未迁移到 `listSkeleton()`**（目前仅 tasks 页切换，其余页维持各自既有骨架写法），乐观更新仍未落地，本项状态维持 ◐。

**P2-2 对话框与语义无障碍 —— ✅ 已全量落地**
- scanner 重命名 / 删除确认 / 目录浏览器 / 创建任务、settings 日志查看器、editor 渲染弹窗（dialog.ts:58/209）、任务抽屉（taskDrawer.ts:32）均补 `role="dialog"` / `aria-modal` / Esc 关闭 / 焦点陷阱 / 返回焦点（由 `setupDialogAccessibility` 兜底）。
- 进度条补 `role="progressbar"` + `aria-valuenow`（tasks.ts:257/407-410）。
- 新增 `lib\useListPage.ts` 将"订阅 → 拉取 → 渲染 → 错误处理 → refresh"收敛为公共流程。

**P2-3 设计契约与门禁 —— ✅ 已落地，R6-R10 可进一步脚本化**
- `docs/DESIGN.md` 已扩至 **R1-R10**（新增 R6 对话框语义 / R7 骨架屏 / R8 对比度 / R9 触控 / R10 增量门禁）；`scripts/check-design.mjs` 实现机器化子集 **R1-R5**，`npm run build` 已挂钩 `check:design:diff`（增量门禁，仅扫 diff 文件）。
- ⬜ 可选扩展：把 R6-R10 中可机器判定的部分（如 dialog 缺 aria 属性、progressbar 缺 aria-valuenow）脚本化。

---

## 4. 不建议照搬 freecut 的部分

| 项 | 原因 |
|----|------|
| Always-dark 单主题 | freecut 面向长时间调色/示波器场景；FVCC 是 NAS 调度端，会嵌在 fnOS 桌面、且存在移动端访问，需保留亮色可选项（但建议**编辑器页恒定深色**） |
| React + Radix/shadcn 组件体系 | FVCC 是零框架手写 DOM，`el()` + class 是既定约束；借鉴的是**状态完备性与无障碍属性**，不是组件库 |
| WebGPU / WebCodecs 前端渲染相关的预览交互 | FVCC 采用"调度端 + 局域网渲染"架构，预览走 `/stream` 票据，范式不同 |
| 多轨剪切工具（ripple/roll/slip/slide）的界面细节 | 属功能层，非本轮设计范围；如需引入应单独评估与 EDL 契约的对应关系 |

---

## 5. 落地路线

### 5.1 已完成阶段（2026-09-20 收口）

| 阶段 | 范围 | 状态 |
|------|------|------|
| 阶段一（P0） | 令牌收敛、对比度与焦点、动效修正、触控尺寸 | ✅ 完成 |
| 阶段二（P1-1~P1-3） | 色彩语言、层次、字体与数据排版 | ✅ 完成 |
| 阶段三（P1-4~P1-5） | 信息架构、全局状态条、编辑器签名面（含布局预设） | ✅ 完成 |
| 阶段四（P2） | 状态完备、无障碍语义、设计契约与门禁 | ◐ 除乐观更新外完成 |
| §7 阶段 A-D | 视觉美化 + 图示化（tasks/history/servers/scanner/editor） | ✅ 完成（1.4.9） |

### 5.2 剩余收尾（可按需排期）

1. **P2-1 乐观更新**：tasks 选中/排序、servers 静音切换等低风险操作加乐观更新 + 失败回滚（唯一遗留的 P 级项）。
2. **门禁脚本化扩展（可选）**：DESIGN.md 的 R6-R10 目前仅文档级约束，可将对话框 aria 属性、progressbar aria-valuenow、骨架屏等可机器判定的项并入 check-design.mjs。
3. **复核类（§6 / §7.6 未勾项）**：Squint Test 人工复测；hint 对比度 ≥4.5:1 复核（`--c-ink-subtle` 用于非文本或升级 muted）；`tabular-nums` 在列表页全量核对；转码进度 1s 刷新无肉眼掉帧记录；图示化新增控件移动端 ≥44px 复核。
4. **版本同步**：本报告随版本号迭代（2026-10-12 复核为 **1.5.12** 三处一致；历史：2026-09-22 实测 1.5.7 工作区，1.5.3/1.5.7/1.5.8 已打 tag，1.5.9~1.5.12 已提交推送 fork/baseline-snapshot、v1.5.12 已于 2026-10-12 打 tag）；**2026-10-13 复核为 1.6.5 三真源一致**（1.6.0 / 1.6.4 / 1.6.5 均已推送 fork/baseline-snapshot 并打 tag，配套 FVCS v1.3.0；易错点见 §1.1 与 §7.7——方案稿编号 v1.6.1 ≠ 交付版本号 1.6.4）；后端 P2-1 分层已收尾、P0-2 SQLite 迁移与 P1-1 事件驱动调度已落地，与 UI 方向无冲突。
5. **1.6.x 视觉线遗留（2026-10-13 登记，明细见 §7.7）**：概览 **Filter Chips 状态过滤**（待产品拍板）、`?fx=stripes|breathe` **实验动画转正或弃用**（待真机并发裁决）、**其余页面骨架迁移到 `listSkeleton()`**（目前仅 tasks 页）。三项均为 1.6.4 批次刻意未做的开放项，不阻塞现有验收。

---

## 6. 验收清单（可直接作为 review 依据）

> 状态（2026-09-21）：已勾项均有源码证据（见 §3.0 / §7.6）；未勾项为待人工复核或未落地项。
>
> 2026-10-13 复核补记（第二十八轮 / `docs/IMPROVEMENT_LOG.md` §二十）：1.6.0 方向 A 补齐控件自绘状态层（`selectField`、`.checkbox`、按钮变体）与逐页修复 V1/V5-V8，1.6.4 批次 A 补齐任务页选中 / 零选中禁用 / 刷新 pending 等状态反馈；本清单未勾的三项人工复核项（Squint Test、hint 对比度 ≥4.5:1、`tabular-nums` 全量核对）**本轮未逐项复跑**，维持待复核状态。

- [ ] Squint Test 通过：界面模糊后仍能分辨主次与当前活动态（待人工复测）
- [x] 全站无硬编码颜色（除登记的 `--ansi-*` 语义色），无 Tailwind 原色板引用（check-design R1/R2 已接入 build）
- [ ] 正文/占位符对比度 ≥4.5:1，大号粗体 ≥3:1（hint 对比度待复核）
- [x] 所有交互元素具备 default / hover / active / focus-visible / disabled 五态，键盘焦点可见
- [x] 移动端可点区域 ≥44×44px
- [x] 动画只动 transform / opacity，且尊重 `prefers-reduced-motion`
- [ ] 列表页有时间码/数字时使用等宽字体与 `tabular-nums`（字体已打包，tabular-nums 未全量核）
- [x] 列表、表单、详情均有骨架加载态、承载下一步动作的空态、人话错误态（骨架屏与空态按钮已做，逐项仍建议抽查）
- [x] 默认态无阴影；同一屏不出现嵌套卡片（`.card` 已去 shadow）
- [x] 存在 `DESIGN.md` 且关键禁令已脚本化校验（R1-R5 已挂 build；R6-R10 未脚本化）
- [x] 自建浮层（scanner×4 / settings 日志 / editor 渲染弹窗 / 任务抽屉）具备对话框语义与焦点陷阱
- [x] 进度条具备 `role="progressbar"` + `aria-valuenow`
- [x] 批量操作逐项汇报（`showBatchResult`：成功 N、失败 M 及原因）

---

## 7. 美化与图示化实施记录

> 定位：在 §3 的方向与 §5 的路线之上，补充"全站观感美化 + 数据图示化"的可执行规划。核心原则：**零框架优先、图示化按需引入、首屏体积不劣化、分阶段试点后确认再全量**。
>
> **落地状态（2026-09-22 复核，版本 1.5.7；2026-10-12 对齐 v1.5.12 复核维持；2026-10-13 对齐 v1.6.5 复核维持）**：阶段 A-D 已全部完成并收口（git e151d08）——阶段 B（tasks 队列可视化 + history 趋势图）、阶段 C（servers 负载圆环 + scanner 分布条）、阶段 D（scanner 大列表分页 + 性能复核）均已上线。`app/ui/assets` 含 `lightweight-charts.production-*.js` 异步 chunk（189.6 KB，仅 history 页签动态加载）；1.5.x 编辑器交互增强（播放头联动 / 时间轴缩放 / 预览最大化 / 变速播放 J-K-L / 按钮图标化 / 素材递归扫描）保持本报告结论不变；**1.6.x 视觉线（1.6.0 方向 A / 1.6.4 任务页三批次 / 1.6.5 版本收口）不改写本节结论，实施记录与新增未完成项见 §7.7**。

### 7.1 目标与边界（不变）

- 目标：全站观感从"后台面板"升级为"专业工具"（承接 §3 P0-P2 方向），并把转码进度、历史趋势、服务器负载等数据以图示化呈现。
- 边界：不动业务逻辑与 API 契约；**不引入前端框架**（决策见 7.2）；图示化仅覆盖"值得看图"的数据，不为了图表而图表。
- 性能基线（2026-09-20 实测）：主 chunk `index-*.js` 192.6 KB（无代码分割）、lightweight-charts 独立异步 chunk 189.6 KB（仅 history 页签加载）。任何改动不得让首屏基线劣化，图示化一律异步 chunk。

### 7.2 框架决策（结论保持）

- **结论：维持零框架**（原生 TS + Vite + Tailwind + `el()`/`svgIcon()`）。理由：产物轻量（主 chunk 192.6KB）、约 1 万行命令式 DOM 的迁移成本中等偏上、收益集中在长期可维护性；图示化由图表库自管 canvas/SVG，无需框架配合。引入 Preact 的触发器（跨页签状态联动频繁出错 / `el()` 细节 bug 占比上升 / 团队规模扩大）当前均未触发，出现任一再单独评审。
- **定案转正（2026-10-11，基线 1.5.8）**：本结论由"维持现状 + 触发器观察"升级为正式决策，治理台账"前端收敛（freecut 双轨）"项（项目分析清单 P1-2）据此关闭——**借 freecut 的设计不借其底座**；排除"复用 freecut 底座"（本地解码剪辑与 FVCC 票据流预览 + NAS 侧渲染架构根本冲突，29.4 万行体量与 §7.5 首屏硬约束冲突）。轻量框架 editor 区试点不再单列待决，改绑"时间线视频/音频双栏（多轨数据模型 `{videoTracks,audioTracks}`）立项"时一并评审（项目现状文档 §7 功能 1 剩余子项）。详见 `docs/IMPROVEMENT_LOG.md` §十三。

### 7.3 图示化方案（已实施清单）

| 页签 | 数据对象 | 方案 | 载体 | 状态 |
|------|---------|------|------|------|
| tasks | 任务队列、进度分布、状态占比 | 状态占比堆叠条 + 计数摘要 | 自绘 CSS（tasks.ts:105） | ✅ 已上线 |
| history | 历史任务耗时趋势、成功率 | 耗时柱状 + 成功率/趋势折线 | `lightweight-charts`（history.ts:307-334） | ✅ 已上线（动态 import，189.6KB 异步 chunk） |
| servers | 服务器负载、在线状态 | 状态卡 + 进行中占比圆环 | 自绘 SVG `miniGauge`（servers.ts:55） | ✅ 已上线（零依赖） |
| scanner | 文件类型/大小分布 | 格式/大小占比条 + 大列表分页 | CSS/Tailwind + 分页（scanner.ts:155-175/757-760） | ✅ 已上线（PAGE_SIZE=100） |
| editor | 渲染进度、时间轴密度 | 复用 tasks 的进度模式，不叠加图表 | 自绘 | ✅ 已落地 |

选型原则（保持）：只对时间序列/趋势类数据引入图表库（history 趋势），静态占比与状态展示一律 CSS/SVG 自绘；全部图表库走 `import()` 动态加载，不进主 chunk。

### 7.4 分阶段实施记录

| 阶段 | 范围 | 试点页面 | 交付物 |
|------|------|---------|--------|
| A. 视觉美化基础 | 设计令牌收敛、对比度/焦点/动效修正、触控尺寸（承接 §3 P0） | `tasks` | ✅ 已交付（P0 全部落地） |
| B. 图示化 MVP | tasks 队列可视化 + history 趋势图（lightweight-charts 异步 chunk） | `tasks` → `history` | ✅ 已交付（异步 chunk 189.6KB，仅 history 加载） |
| C. 图示化扩展 | servers 状态可视化、scanner 分布条、editor 进度复用 | `servers` | ✅ 已交付（SVG 圆环 + 占比条，零依赖） |
| D. 性能打磨与门禁 | scanner 大列表分页、chunk 拆分审查、DESIGN.md 脚本门禁（承接 §3 P2） | 全站 | ✅ 已交付（scanner PAGE_SIZE=100 + check-design 挂 build） |

节奏约束（沿用既有偏好）：每阶段**先单页试点**，完成并给出验证证据后向 HuGe 汇报改动清单与证据，**等待确认后才进入下一阶段**；严禁跨阶段连续大改。上述 A-D 均按此节奏完成。

### 7.5 性能守则（硬约束，后续改动仍须遵守）

- 图表库一律 canvas 渲染模式，禁止 SVG 承载大节点量图形；
- 图表库全部动态 import，首屏不加载任何图表依赖；
- 列表/队列渲染采用分页或虚拟滚动，禁止整表全量重建；
- 动画只动 `transform` / `opacity`，尊重 `prefers-reduced-motion`；
- 图示化用色只取自现有 token 色板，禁止新增硬编码颜色；
- 每个阶段验收时记录首屏 JS 体积与渲染耗时基线，劣化即打回；
- **视觉改进不得增加 WS tick 重绘**（1.6.4 批次 B 立为硬约束，沿用方案稿口径）：进度条以 `transform: scaleX(var(--p))` 走合成层而非 `width`；`render` 加 id/顺序签名重排守卫、`updateOverview` 加计数签名守卫；`scrollPos` 主页与抽屉用独立 key 不互串。后续任何任务页/列表页视觉改动都须先过这条约束。

### 7.6 阶段验收清单（增量追加到 §6 之上）

- [x] tasks/history 图表 chunk 仅进页签时加载，首屏 JS 体积未劣化（2026-09-20 实测：主 chunk 192.6KB + 异步 chunk 189.6KB；相对阶段前 190.1KB 的增幅来自图示化与分页代码）
- [ ] 转码进度 1s 刷新间隔下无肉眼掉帧（DevTools Performance 记录）
- [x] 图示化元素全部取自 token 色板（servers `miniGauge` 经 `tokenColor` 走 token，scanner 分布条同）
- [ ] 移动端可点区域 ≥44×44px 在新增可视化控件上依然成立（待复核）
- [x] 每个阶段有截图证据 + 构建通过证据 + 体积对比数据（实施时已按此验收）

### 7.7 1.6.x 视觉批次实施记录（2026-10-13 补记，第二十八轮 / `docs/IMPROVEMENT_LOG.md` §二十）

> 定位：延续 §7.4 的分阶段记录格式，登记 §3 方向之上 1.6.x 三个版本的**设计侧落实**与新增的未完成项。版本/tag/git 事实取自 2026-10-13 核验，本轮规模实测见 §1.1；未在本节出现的数字本轮均未实测。

#### 7.7.1 1.6.0 · 方向 A「精修冷灰」（tag `v1.6.0` → `b1a0d93`，含 `1e622f0` 批次①-⑤、`22035c6` FVCS GUI 复活）

| 层面 | 范围 | 状态 |
|------|------|------|
| 令牌层 | 方向 A「精修冷灰」令牌层重排（`style.css` `:root` / `[data-theme="dark"]` 双通道取值，token 命名体系未变，见 §1.2） | ✅ 已上线 |
| 控件层 | 控件自绘：`selectField`、`.checkbox`、按钮变体（替换浏览器原生控件外观，状态层对齐 §2 控件状态行） | ✅ 已上线 |
| 外壳 | 导航分组 / 搜索胶囊 / 状态栏（承接 §3.0 P1-4 的 `NAV_GROUPS` + 全局状态条） | ✅ 已上线 |
| 逐页修复 | V1 趋势图、V5-V8 逐页视觉缺陷 | ✅ 已上线 |
| 微交互 + WCAG | V9 微交互与对比度验收（§6 未勾的三个人工复核项本轮未复跑） | ✅ 批次内验收 / ◐ 人工复核仍待办 |
| 同批 FVCS | 渲染节点端 1.2.6 → **1.3.0**：Fyne 托盘 + 设置窗口纳入构建（`build.ps1 -Target FVCS-UI`），新增 `FVCS/pkg/fynetheme`（色板贴方向 A 蓝调） | ✅ 已上线 |

#### 7.7.2 1.6.4 · 任务页视觉改进三批次（tag `v1.6.4` → `b007a3d`，代码 `fe06e52`，7 文件 +197/−44）

方案来源：`docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`（GLM5.3F / Step5P / Gemini-3.8F 三方外部模型评审整合稿）。**编号易错点（务必按此表述）**：文件名里的 "v1.6.1" 是**方案稿编号**，交付版本号是 **1.6.4**；`1.6.1`/`1.6.2`/`1.6.3` 只是批次开发过程中的本地中间 bump，从未进 git 历史、未出包、未打 tag。任何"1.6.1~1.6.4 版本线"的写法都是错的，应写"**1.6.4**（方案稿 v1.6.1 的落地批次）"。

| 批次 | 范围 | 设计侧要点 | 状态 |
|------|------|-----------|------|
| A（层级修复 + 状态反馈） | 任务卡信息层级与状态可见性 | `errorMsg` 独立成行并带 `title`；拆除 `sub` 上 `truncate` 与 `flex-wrap` 的冲突；首屏骨架接入 `listSkeleton`；批量三键零选中禁用 + 刷新 pending；`.task-selected` 选中即时高亮；活动卡左侧 `signal` 静态指示线；`getPhaseStyle` 补全状态文案且 ERROR 走红条；操作区去掉 `w-[200px]` 硬宽；`serverName` 改中性标签去撞色；hover ring 与 servers 页对齐 | ✅ 已上线 |
| B（热路径性能） | 进度与重排成本 | 进度条 `width` → `transform: scaleX(var(--p))` 走合成层；`render` 加 id/顺序签名重排守卫；`updateOverview` 加计数签名守卫；`scrollPos` 接线（主页与抽屉独立 key 不互串）。**硬约束：视觉改进不得增加 WS tick 重绘**（已并入 §7.5 守则） | ✅ 已上线 |
| C（速率 + 实验项） | 倍速文案与实验动画 | WS `applyTaskUpdate` 合并 `speed`（链路核实为 `models.go` `speed,omitempty` + `TaskUpdateFullMsg`）后转正，进度文案实时带倍速后缀；`#/tasks?fx=stripes` / `?fx=breathe` URL 开关（**默认关**）；`ui.ts` 新增 `listSkeleton` 泛化组件 | ◐ speed 已转正；`?fx` 待真机裁决 |

**两处由用户真机实测抓出的缺陷（对后续 UI 工作有指导价值，按方案稿口径记此）**：

1. 空任务库下首屏骨架常驻、"出不来"——批次 A 首屏骨架的初版退出条件写成 `tasks.length>0`，有数据才收骨架；已改为**首次成功 render 即置 `loaded`**，空列表正常落到 `emptyState`。**教训**：骨架/加载态的退出条件必须挂在"渲染成功"这一事件上，不能挂在"列表非空"这一数据判断上——否则空库是最常见的进入路径。后续把 `listSkeleton()` 铺到其余页面时，沿用"首次成功 render 即 loaded"的写法。
2. `check-design` R3 规则误报 CSS 属性 `box-shadow`（规则本意是查内联样式），已按前缀排除 `-` / `:`；选中态的 inset 阴影按方案稿 §2.3 裁决为**合法**（不属于 R3 禁止的默认态浮动阴影）。

#### 7.7.3 1.6.5（tag `v1.6.5` → `5166cbe`，代码 `ceeb1b2`）

纯版本收口轮：修复 `internal/version/VERSION` 第三真源漂移 + 版本一致性校验加固。**无设计侧改动，本文 UI 结论不因该版变化**；登记于此仅为避免版本线读起来缺一段。

#### 7.7.4 仍未完成的设计项（2026-10-13 状态，均不阻塞验收）

| 项 | 来源 | 卡在哪 | 状态 |
|----|------|--------|------|
| 概览 Filter Chips 状态过滤 | 1.6.x 视觉线规划（任务页概览区） | **待产品拍板**是否引入状态筛选交互 | ⬜ 本批刻意未做 |
| `?fx=stripes` / `?fx=breathe` 实验动画 | 1.6.4 批次 C 实验开关（默认关） | **待真机并发实测裁决**转正或弃用 | ⬜ 待裁决 |
| 其余页面骨架迁移到 `listSkeleton()` | 1.6.4 批次 C 新增泛化组件 | **目前仅 tasks 页切换**，其余页面维持既有骨架写法 | ⬜ 待迁移 |
| store 备份/恢复的**前端入口** | P3-5 只交付后端 API | 前端入口待排期（与本文档 UI 方向无冲突，登记于 `FVCC/docs/STORE_PERSISTENCE_ROADMAP.md`） | ⬜ 未交付 |
