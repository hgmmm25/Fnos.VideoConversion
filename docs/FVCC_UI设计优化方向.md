# FVCC Web UI 设计优化方向

> 方法：按 `frontend-design` skill 的设计准则（排版 / 色彩与对比 / 布局与空间 / 视觉细节 / 动效 / 打磨清单 / AI Slop 检验），以 `D:\Fnos.VideoConversion\freecut\` 为设计目标参照，对当前装在 NAS 上的版本 `D:\Fnos.VideoConversion\FVCC\ui-src\` 提出可执行的优化方向。
>
> 口径说明（按你的补充）：
> - 设计目标参照：`freecut\`（上游参考项目，仅借其设计语言与设计契约范式，不借功能与框架）
> - 优化对象：`FVCC\ui-src\`（当前 NAS 上运行的版本）
> - 已排除：`FVCC_official_unpack\`（备份的源安装包，已迭代，不作为设计依据）
>
> 证据来源：`FVCC\ui-src\src\style.css`、`tailwind.config.js`、`theme.ts`、`ui.ts`、`main.ts`、`index.html`、`pages\tasks.ts`、`pages\settings.ts`、`pages\editor\layout.ts`；`freecut\DESIGN.md`、`freecut\PRODUCT.md`、`freecut\src\index.css`。

---

## 0. 核心结论（先看这五条）

| # | 结论（原始判断） | 当前状态（2026-09-20 源码复核） |
|---|------|------|
| 1 | 设计令牌两套真源、darkMode 策略不一致 | ✅ 已收敛：body 改 `bg-page text-ink`，预览底改 `--c-preview-bg`，darkMode 改 `['selector','[data-theme="dark"]']` |
| 2 | 通用后台面板色彩语言 vs 专业工具语言 | ✅ 已升级：引入 `--c-signal` 单一信号色，语义色收敛为 success/danger/warning |
| 3 | 无障碍与交互状态硬缺陷 | ✅ 已修：.btn 五态 + 2px 焦点环、移动端可点 ≥44px、动画去 max-height 且尊重 reduced-motion |
| 4 | 编辑器页比列表页更专业（机会点） | ✅ 已固化：clip 数据色 token、导航分组、全局状态条、命令面板（Ctrl+K） |
| 5 | 缺设计契约 + 机器校验门禁 | ✅ 已落地：`docs/DESIGN.md` + `scripts/check-design.mjs`（R1-R5），已挂入 `npm run build` |

---

## 1. 现状盘点（有源码依据）

### 1.1 技术底座与结构

- Vite + TypeScript + Tailwind，**零框架**。依赖仅 `@fontsource/ibm-plex-sans` / `@fontsource/ibm-plex-mono` 字体（^5.3.0）与 vite（^8.3.0）/ typescript（^7.0.2）/ tailwindcss（^3.4.17）工具链；**无 React/Vue/Preact、无路由库、无状态库、无 UI 组件库**。DOM 由 `src/ui.ts` 的 `el(tag, attrs, children)` 手工构造，图标为内联 SVG 路径表（`ICON_PATHS`，统一 `stroke-width: 2`）。
- 规模：`src/` 共 **33 个 .ts 文件、约 1 万行**；7 个页签页 + editor 子模块 13 个文件（timeline 631 行 / assets 663 行 / preview 580 行）；最大单文件 profiles.ts（1,283 行）、scanner.ts（1,308 行）。
- 构建产物（`vite` `outDir: '../app/ui'`）：JS **单文件 177.2 KB（无代码分割）**、CSS 32.5 KB、字体 192.2 KB、共 16 个文件 / 466.8 KB。后端 Go(gin) 托管，网关前缀 `/app/fvcc`。
- 路由：`main.ts` 中的 hash 路由，7 个平级页签——
  `剪辑(editor) / 任务清单(tasks) / 视频文件(scanner) / 转码服务器(servers) / 转码方案(profiles) / 历史任务(history) / 设置(settings)`；
  `editor` 支持 `#/editor/:projectId` 子路由与 `renderProjectPicker`。
- 外壳：顶部导航已按 `NAV_GROUPS` 分组（编辑工作区 / 资源 / 运行 / 系统，main.ts:34-35），移动端（`md` 以下）折叠为下拉菜单；外壳底部已有**全局状态条**（队列长度、WS 连接等，main.ts:233）；**全局命令面板 Ctrl+K** 已接入（main.ts:98-99）。
- 页面尺寸：`scanner.ts` 47KB、`profiles.ts` 64KB，属单体大页面；`tasks.ts` 15KB 为卡片列表范式。

### 1.2 设计令牌

- `style.css`：`:root`（明亮主题）与 `[data-theme="dark"]` 两套 **RGB 通道变量**，命名体系为 `--c-primary / --c-accent / --c-danger / --c-warning / --c-success / --c-download / --c-neutral / --c-surface / --c-line / --c-ink(-muted/-subtle)`，每类色另带 `-soft` 与 `-soft-text` 变体。
- `tailwind.config.js`：将上述变量映射为 `primary/accent/danger/warning/success/download/neutral/page/surface/line/ink` token（`rgb(var(--c-x) / <alpha-value>)`）。
- `theme.ts`：三选项主题 `auto / light / dark`，`localStorage` 键 `fvcc-theme`，并兼容 fnOS 桌面下发的 `fnos-theme-mode`。
- 组件类（`@layer components`）：`.btn` / `.btn-accent` / `.btn-danger` / `.btn-sm` / `.input` / `.card` / `.badge`。
  - `.card` = `bg-surface rounded-lg shadow-sm border border-line`
  - `.badge` = `rounded-full` 胶囊
  - `.input` 聚焦为 `focus:ring-2 focus:ring-primary/40 focus:border-primary`

### 1.3 动效现状

`style.css` 定义 keyframes：`slide-down` / `slide-up` / `fade-in-up` / `fade-in` / `scale-in` / `pulse-soft` / `slide-in-right` / `progress-stripes` / `skeleton-shimmer`。
**P0-3 已完成**：`slide-*` 弃用 `max-height`（解除 300px 截断），改用 `transform + opacity`；引入缓动 token `--ease-out-strong` / `--ease-in-out-strong`；`@media (prefers-reduced-motion: reduce)` 全局降级（style.css:200、273-278、340-341）。

### 1.4 页面级范式（抽样）

- `tasks.ts`：每个任务 = 一张 `.card`，内部横向排布 `拖拽手柄 + 复选框 + 状态 badge + 文件名/元信息 + 进度条 + 最多 4 个按钮`；进度色由 `getPhaseStyle()` 映射（上传=`primary`、转码=`accent`、下载/完成=`success`、其余=`neutral-soft`）；拖拽落点用 `border-t-2 border-primary` 表达；空状态调用 `emptyState()`。
- `settings.ts`：单张 `.card` 承载全部设置，用 `border-t border-line` 分段（外观 / 调度与传输 / 传输模式 / 系统信息 / 运行日志 / 视频缓存 / 播放），字段以 `field(label, control, hint)` 生成；"加载中..."为纯文本，非骨架屏；日志查看器是自建 overlay + `<pre>`，ANSI 颜色映射为 `#00FF00`、`#0000FF` 等**纯色 hex**（与主题体系无关）。
- `editor\layout.ts`：顶栏 `h-11` + 中部（素材库 320px 可折叠 + 拖拽调宽，区间 240–480）+ 底部（时间线 160px 可调，区间 96–320，上方 `h-9` 工具条）；预览区背景硬编码 `#000`；尺寸持久化于 `localStorage: wve.layout.v1`。

### 1.5 freecut 的设计语言（参照系，摘其 DESIGN.md / PRODUCT.md）

- 北极星：**"The Quiet Instrument"**——器材感、克制、素材是屏幕上最亮最饱和的东西。
- 色彩：OKLCH 中性石墨阶梯（`0.12 → 0.95`），**单一暖橙信号色** `oklch(0.68 0.19 45)` 只用于播放/活动态/焦点/播放头；clip 与 marker 色是**数据色**不是装饰色。
- 规则化（Named Rules）：*The One Signal Rule*（信号色稀少才可读）、*The Value-Hierarchy Rule*（层次靠明度阶梯，不靠边框与阴影）、*The Mono-For-Data Rule*（时间码/帧号/FPS/分辨率一律用 IBM Plex Mono）、*The Flat-By-Default Rule*（静止面扁平，抬升先抬明度）、*The Meaning-Bearing Hue Rule*（类型色承载语义，不可挪用）。
- 明确的反面清单：消费级剪辑器糖果风、炫技 SaaS 仪表盘（渐变头图 / glassmorphism / 大数字指标卡）、拥挤的老式 NLE 灰工具栏。
- 无障碍：正文 ≥4.5:1，占位符同标准；**有意只做暗色**，对比度调整发生在暗阶内。

---

## 2. 差距对照

| 维度 | freecut（目标） | FVCC 原始差距 | 当前状态（2026-09-20 复核） |
|------|------------------|------------------------|----------|
| 色彩空间 | OKLCH，中性带品牌色相偏移 | sRGB 纯 slate，无色相偏移 | ◐ 保留 sRGB 体系（未引入 OKLCH）；中性仍为纯 slate，未做品牌色相偏移 |
| 主色策略 | 单一信号色（暖橙），只表示"活动/进行中" | 蓝主色多角色 + 7 类语义色 | ✅ 已收敛：`--c-signal`（#6366f1）信号色落地，语义色收敛为 success/danger/warning；tasks `getPhaseStyle` 转码用 `bg-signal` |
| 层次表达 | 明度阶梯，无默认阴影 | `.card` = `shadow-sm + border` | ✅ 已去默认阴影：`.card` = `bg-surface rounded-lg`；分档改明度（surface / surface-alt / page） |
| 主题 | 有意 always-dark | light/dark 定位差异 | 有意保留双主题（见 §4），未跟随 always-dark |
| 字体 | IBM Plex Sans + Mono，数据一律 mono | 无字体策略、系统栈 | ✅ 已落地：`@fontsource/ibm-plex-sans(400/500/600)` + `ibm-plex-mono(400/500)` 本地打包（main.ts:4-8）；`formatDuration` 统一 `HH:MM:SS`（ui.ts:108-114），format.ts 另有 `HH:MM:SS.mmm` |
| 动效 | easing token，只动 transform/opacity | max-height + 无缓动 token | ✅ 已修：`--ease-out-strong`/`--ease-in-out-strong` + transform/opacity + reduced-motion 降级 |
| 控件状态 | 5 态齐备 + 焦点环可见 | `.btn` 无五态细分 | ✅ 已补：.btn hover/active/focus-visible/disabled 四态 + 统一 2px 焦点环（style.css:161-167） |
| 无障碍 | 明示 AA 与占位符 4.5:1 | `--c-ink-subtle` 2.6:1 | ◐ 部分修复：焦点/对话框无障碍已做；hint 对比度需再复核 |
| 触控/密度 | 控件 36px 基准 | `btn-sm` 约 26px | ✅ 已修：移动端（<768px）可点区域强制 ≥44px（style.css:176-180） |
| 导航信息架构 | 侧边栏 + 面板系统 | 7 个平级 tab、无全局状态 | ✅ 已升级：`NAV_GROUPS` 分组导航（main.ts:34-35）+ 外壳底部全局状态条（main.ts:233）+ 全局命令面板 Ctrl+K（main.ts:98-99） |
| 设计契约 | DESIGN.md + named rules + design.json | 无设计文档、规则散落 class | ✅ 已落地：`docs/DESIGN.md` + `scripts/check-design.mjs`（R1-R5：禁 hex / 禁原色板 / 禁默认阴影 / 禁 <12px / 禁 max-height 过渡），已挂入 `npm run build` |

---

## 3. 优化方向（按优先级）

### 3.0 落地状态总览（2026-09-20 源码复核）

| P 项 | 状态 | 源码证据 |
|------|------|---------|
| P0-1 令牌收敛为单一真源 | ✅ | index.html body `bg-page text-ink`；editor 预览底 `--c-preview-bg`（layout.ts:157）；darkMode 改 `['selector','[data-theme="dark"]']` |
| P0-2 对比度与焦点可见性 | ✅ | style.css:161-167 `.btn` 五态 + 统一 2px 焦点环 |
| P0-3 修正动画实现 | ✅ | style.css:200/273-278/340-341：去 max-height、缓动 token、reduced-motion 降级 |
| P0-4 触控目标 | ✅ | style.css:176-180：移动端 `min-height:44px` |
| P1-1 单一信号色 + 中性明度阶梯 | ✅ | style.css:32-34 `--c-signal`；tasks.ts `getPhaseStyle` 转码用 `bg-signal` |
| P1-2 明度阶梯替代边框 + 阴影 | ✅ | `.card` 去 shadow（`bg-surface rounded-lg`） |
| P1-3 字体与数据排版 | ✅ | main.ts:4-8 fontsource IBM Plex；ui.ts:108-114 `formatDuration` HH:MM:SS；format.ts |
| P1-4 信息架构与全局状态 | ✅ | main.ts:34-35 `NAV_GROUPS` 分组、:98-99 命令面板（Ctrl+K）、:233 全局状态条 |
| P1-5 编辑器作为签名面 | ◐ | clip 数据色 token 已落地（style.css:64）；工作区布局预设未做 |
| P2-1 状态完备 | ◐ | `skeletonRows` 骨架屏 + `showBatchResult` 已做；乐观更新未落地 |
| P2-2 对话框与语义无障碍 | ◐ | `setupDialogAccessibility` 已做；进度条 aria / 焦点陷阱逐项未全核 |
| P2-3 设计契约与门禁 | ✅ | `docs/DESIGN.md` + `scripts/check-design.mjs`（R1-R5），`build` 已挂钩 `check:design:diff` |

### P0 —— 先修正确性与合规（改动小、风险低、收益立竿见影）

**P0-1 令牌收敛为单一真源**
- 目标：任何一处改色只改一个地方。
- 动作：
  1. `index.html` body 的 `bg-slate-100 text-slate-800 dark:bg-slate-900 dark:text-slate-100` 换成 `bg-page text-ink`；
  2. 页面内硬编码色归位：`editor\layout.ts` 预览底 `#000`、`settings.ts` 日志面板 `rgba(0,0,0,0.03)`、ANSI 映射表 `#00FF00/#0000FF/...`、`tasks.ts` 拖拽落点 `border-t-2 border-primary`（拆成 `--c-drop-line` 与线宽 token）；
  3. `tailwind.config.js` 的 `darkMode` 策略与实际机制对齐（现状是 `['class','[data-theme="dark"]']`，但生效路径是 `document.documentElement` 上的 `data-theme`，页面中 `dark:` 前缀的可用性需统一为一种）。
- 验收：全局 grep `#` 十六进制与 `slate-` / `neutral-soft` 之外的 Tailwind 原色板，`src/` 内命中数降为 0（日志 ANSI 语义色除外，需单独登记）。

**P0-2 对比度与焦点可见性**
- 动作：把 `--c-ink-subtle` 用于**非文本**（图标、分隔）或在浅色面上升级为 `--c-ink-muted`；对所有 hint 文案做 4.5:1 校验；`.btn` 增加 `:hover / :active / :focus-visible / :disabled` 四态，焦点环统一为 2px（当前只有 `.input` 有 ring）。
- 验收：正文与占位符 ≥4.5:1（大号/粗体 ≥3:1）；键盘 Tab 走查所有按钮焦点可见。

**P0-3 修正动画实现**
- 动作：`slide-down/slide-up` 的 `max-height` 改为 `grid-template-rows: 0fr → 1fr`（或 `transform + opacity` 方案），解除 300px 截断；引入缓动 token（如 freecut 的 `--ease-out-strong: cubic-bezier(0.23,1,0.32,1)`、`--ease-in-out-strong`）；全体动画加 `@media (prefers-reduced-motion: reduce)` 降级。
- 验收：长列表展开无内容裁剪；开系统"减少动态效果"后无位移类动画。

**P0-4 触控目标**
- 动作：`btn-sm` 视觉保持紧凑，但通过 `padding` / `min-height` / 伪元素扩张把可点区域提到 ≥44px（移动端 `md` 以下强制），移动端菜单项保持 `py-2.5` 以上。
- 验收：移动视图下所有可点元素 ≥44×44px。

### P1 —— 设计语言升级（这是"从后台面板变成专业工具"的关键）

**P1-1 引入"单一信号色 + 中性明度阶梯"**
- 现状问题：蓝主色同时承担品牌、主按钮、进度、选中态，另有 `accent`(靛) / `download`(青) / `neutral`(灰) 多套语义色，加上 badge 的 `-soft` 底色，形成典型的"多色后台面板"观感。
- 建议：
  1. 保留蓝作**品牌与主操作**，但把"进行中/活动态"统一到单一信号色（可用现有 `--c-accent` 语义化重命名，例如 `--c-signal`），按 *One Signal Rule* 控制出现面积；
  2. 收敛语义色到 3 类必要区分（成功 / 危险 / 中性），把 `download` 与 `accent` 合并或降饱和；
  3. 中性色引入**品牌色相偏移**（中性里加 0.01 量级色度），替代纯 slate，消除"通用灰"。
- 验收：任一页面截图缩半后（Squint Test）仍能一眼看出"当前活动/进行中"是什么。

**P1-2 用明度阶梯替代边框 + 阴影**
- 动作：`.card` 去掉 `shadow-sm`，改用 `surface / surface-alt / page` 三档明度区分；色彩分段用背景差而非 `border-t border-line` 铺满设置页；`confirmDialog` 去掉 2px 彩色描边，改用标题权重 + 危险色按钮表达风险级别。
- 验收：全站默认态阴影引用降为 0（仅弹层保留浮动阴影）；同一屏内"卡片套卡片"出现次数为 0。

**P1-3 字体与数据排版**
- 动作：引入 IBM Plex Sans / IBM Plex Mono（或等价的中英混排方案，避开 Inter/Roboto/Arial），建立 4 级字阶（标题 600 / 面板题 600 / 正文 400 / 标签 500）；
- **数据一律等宽 + `tabular-nums`**：时间码、帧号、FPS、分辨率、文件大小、进度百分比、日志。
- 顺带修正 `ui.ts` 的 `formatDuration`（当前输出 `1h23m` / `12:34`），改用统一时间码风格（至少 `HH:MM:SS`，编辑器场景建议 `HH:MM:SS:FF`）。
- 验收：任意列表中数字纵向对齐不跳动；同一信息块内字体家族不超过 2 种。

**P1-4 信息架构与全局状态**
- 动作：
  1. 顶栏 7 平级 tab 改为**分组**：编辑工作区（剪辑）/ 资源（视频文件、转码方案）/ 运行（任务清单、历史任务、转码服务器）/ 系统（设置），或改为侧边栏 + 编辑器全屏工作区（`editor` 已具备 `renderProjectPicker` 独立入口，天然适合脱离主外壳）；
  2. 恢复被移除的全局状态条：以细状态条/角标承载"渲染节点在线数、队列长度、WS 连接状态"（`layout.ts` 已有 `setNodeStatus`，可上提为全局）；
  3. 增加全局命令入口（跳转页面、项目、按文件名搜索），呼应专业工具"键盘驱动"预期。
- 验收：从任意页到"看到当前队列与节点状态"不超过 1 次操作。

**P1-5 编辑器作为签名面（signature surface）**
- 动作：把时间线的 clip 类型色定义为**数据色 token**（视频/音频/图像/文字/marker），并与列表页"进度/状态色"解耦；工具条与时间线的关系重排（工具条紧贴时间线、预览区四周中性化而非纯黑）；沿用免费获得的专业度：已有的可拖拽尺寸 + 持久化，建议扩展为"工作区布局预设"（素材/预览/时间线多套比例）。
- 验收：编辑器截图不看标题即可辨认是视频工具而非后台管理页。

### P2 —— 打磨与规模化

**P2-1 状态完备**
- 现状：加载态为文本"加载中..."（`settings.ts`）；空状态只有 `emptyState(text, icon)` 一种；错误态有 `renderPageError`；批量操作 toast 只报总数（`batchOp`）。
- 动作：列表/表单加骨架屏（替代 spinner 与文本）；空状态区承载"下一步动作"（例如"暂无任务 → 去视频文件页创建"，当前 tasks 的文案已具备，可加按钮）；批量操作提供逐项结果（成功 N、失败 M 及原因）；低风险动作（选中、排序、静音切换）用乐观更新 + 失败回滚。

**P2-2 对话框与语义无障碍**
- `confirmDialog`（`ui.ts`）与日志查看器（`settings.ts` `openLogViewer`）为自建 overlay：补 `role="dialog"` / `aria-modal` / Esc 关闭 / 焦点陷阱与返回焦点；进度条补 `role="progressbar"` + `aria-valuenow`；badge 状态补文本等价（不单靠颜色）。

**P2-3 设计契约与门禁（freecut 最值得抄的部分）**
- 动作：在 `FVCC\ui-src\` 下落一份 `DESIGN.md`（含 token front-matter + named rules + 反例清单）+ `PRODUCT.md`（用户、定位、反参照），并配一个校验脚本纳入构建：禁止硬编码 hex、禁止引入 Tailwind 原色板、禁止默认态阴影、禁止 `<12px` 文字、禁止 `max-height` 过渡。规则一旦脚本化，就不会随页面迭代而腐化。

---

## 4. 不建议照搬 freecut 的部分

| 项 | 原因 |
|----|------|
| Always-dark 单主题 | freecut 面向长时间调色/示波器场景；FVCC 是 NAS 调度端，会嵌在 fnOS 桌面、且存在移动端访问，需保留亮色可选项（但建议**编辑器页恒定深色**） |
| React + Radix/shadcn 组件体系 | FVCC 是零框架手写 DOM，`el()` + class 是既定约束；借鉴的是**状态完备性与无障碍属性**，不是组件库 |
| WebGPU / WebCodecs 前端渲染相关的预览交互 | FVCC 采用"调度端 + 局域网渲染"架构，预览走 `/stream` 票据，范式不同 |
| 多轨剪切工具（ripple/roll/slip/slide）的界面细节 | 属功能层，非本轮设计范围；如需引入应单独评估与 EDL 契约的对应关系 |

---

## 5. 落地路线建议

> **状态（2026-09-20）**：阶段一（P0）与阶段二（P1-1~P1-3）已完成；阶段三（P1-4~P1-5）中 P1-4 已完成、P1-5 部分（clip 数据色）已完成；阶段四（P2）中门禁已接入 build、骨架屏/批量结果/对话框无障碍已做，剩余为乐观更新与逐项无障碍复核。§7 为新的待启动阶段（美化与图示化）。

| 阶段 | 范围 | 影响面 | 风险 |
|------|------|--------|------|
| 阶段一（P0） | 令牌收敛、对比度与焦点、动效修正、触控尺寸 | `style.css`、`tailwind.config.js`、`index.html`、`ui.ts`、`settings.ts`、`tasks.ts`。**不动业务逻辑**，可整批回归 | 低：改色可能暴露此前"靠硬编码救回来"的角落 |
| 阶段二（P1-1~P1-3） | 色彩语言、层次、字体与数据排版 | 全局 CSS + token，页面 class 批量替换 | 中：需逐页截图核对；建议先用一个页面（`tasks`）试点，确认后再全量 |
| 阶段三（P1-4~P1-5） | 信息架构、全局状态条、编辑器签名面 | `main.ts` 外壳重构 + `editor/*` | 中高：外壳改动会波及移动端与 fnOS 内嵌场景，需保留 hash 路由兼容 |
| 阶段四（P2） | 状态完备、无障碍语义、设计契约与门禁 | 全站 + 构建脚本 | 低到中：门禁首次接入会产生一批既有告警，建议以增量文件为范围 |

---

## 6. 验收清单（可直接作为 review 依据）

> **状态（2026-09-20）**：下方勾选项均有源码证据（见 §3.0）；未勾项为待人工复核或未落地项。

- [ ] Squint Test 通过：界面模糊后仍能分辨主次与当前活动态（待人工复测）
- [x] 全站无硬编码颜色（除经登记的 ANSI 语义色），无 Tailwind 原色板引用（check-design R1/R2 已接入 build）
- [ ] 正文/占位符对比度 ≥4.5:1，大号粗体 ≥3:1（hint 对比度待复核）
- [x] 所有交互元素具备 default / hover / active / focus-visible / disabled 五态，键盘焦点可见（style.css:161-167）
- [x] 移动端可点区域 ≥44×44px（style.css:176-180）
- [x] 动画只动 transform / opacity，且尊重 `prefers-reduced-motion`（style.css:200/340-341）
- [ ] 列表页有时间码/数字时使用等宽字体与 `tabular-nums`（字体已打包，tabular-nums 未全量核）
- [ ] 列表、表单、详情均有骨架加载态、承载下一步动作的空态、人话错误态（骨架屏已做，空/错误态逐项待核）
- [x] 默认态无阴影；同一屏不出现嵌套卡片（`.card` 已去 shadow）
- [x] 存在 `DESIGN.md` 且关键禁令已脚本化校验（`docs/DESIGN.md` + `scripts/check-design.mjs`，build 已挂钩）

---

## 7. 美化与图示化实施规划

> 定位：在 §3 的方向与 §5 的路线之上，补充"全站观感美化 + 数据图示化"的可执行规划。核心原则：**零框架优先、图示化按需引入、首屏体积不劣化、分阶段试点后确认再全量**。

### 7.1 目标与边界

- 目标：全站观感从"后台面板"升级为"专业工具"（承接 §3 P0-P2 方向），并把转码进度、历史趋势、服务器负载等数据以图示化呈现。
- 边界：不动业务逻辑与 API 契约；**不引入前端框架**（决策见 7.2）；图示化仅覆盖"值得看图"的数据，不为了图表而图表。
- 性能基线：当前产物 JS 177.2 KB（单文件、无代码分割）。任何改动不得让首屏基线劣化，图示化一律异步 chunk。

### 7.2 框架决策（结论 + 触发器）

- **结论：维持零框架**（原生 TS + Vite + Tailwind + `el()`/`svgIcon()`）。理由：产物已极轻（177KB），约 1 万行命令式 DOM 的迁移成本中等偏上，而收益集中在长期可维护性，与本阶段"美观 + 图示化"目标不对齐；图示化由图表库自管 canvas/SVG，无需框架配合。
- **引入 Preact 的触发器**（出现任一再评估，且须单独评审）：
  1. 跨页签/跨模块状态联动开始频繁出错，store 订阅模式成为维护瓶颈；
  2. `el()` 手写 DOM 的细节 bug（事件重绑、节点复用）占比明显上升；
  3. 团队规模扩大，需要降低新成员上手成本。
- 若触发：选 **Preact + @preact/signals**（~4KB gzip，signals 与现有 store 单例订阅心智一致），从 `settings` 页试点增量接入，`editor/*` 模块最后迁移；README 决策记录同步更新。

### 7.3 图示化方案（按页签）

| 页签 | 数据对象 | 方案 | 载体 | 体积策略 |
|------|---------|------|------|---------|
| tasks | 任务队列、进度分布、状态占比 | 迷你进度条 + 队列状态可视化 | 自绘 CSS/SVG（扩展 `svgIcon` 体系） | 无新增依赖 |
| history | 历史任务耗时趋势、成功率 | 时间序列折线/柱状 | `lightweight-charts`（~45KB） | **动态 import**，仅进页签时加载 |
| servers | 服务器负载、在线状态、吞吐 | 状态卡 + 迷你仪表盘 | 自绘 SVG | 无新增依赖 |
| scanner | 文件类型/大小分布 | 简单占比条 | CSS/Tailwind | 无新增依赖 |
| editor | 渲染进度、时间轴密度 | 复用 tasks 的进度模式，不叠加图表 | 自绘 | 无新增依赖 |

选型原则：**只对时间序列/趋势类数据引入图表库**（tasks 的历史、history 趋势），静态占比与状态展示一律 CSS/SVG 自绘；全部图表库走 `import()` 动态加载，不进主 chunk。

### 7.4 分阶段实施（每阶段试点 → 双验证 → 汇报 → 等确认）

| 阶段 | 范围 | 试点页面 | 验证证据（交付物） |
|------|------|---------|-------------------|
| A. 视觉美化基础 | 设计令牌收敛、对比度/焦点/动效修正、触控尺寸（承接 §3 P0） | `tasks` | grep 旧写法清零 + `npm run build` 通过 + 试点页截图对比 |
| B. 图示化 MVP | tasks 队列可视化 + history 趋势图（lightweight-charts 异步 chunk） | `tasks` → `history` | 截图对比 + 首屏 JS 体积报告（基线 177KB）+ 构建通过 |
| C. 图示化扩展 | servers 状态可视化、scanner 分布条、editor 进度复用 | `servers` | 截图 + 体积复核 |
| D. 性能打磨与门禁 | 大列表虚拟滚动复核、chunk 拆分审查、DESIGN.md 脚本门禁（承接 §3 P2） | 全站 | Lighthouse/DevTools 体积与渲染耗时对比 + 门禁脚本跑通 |

节奏约束（沿用既有偏好）：每阶段**先单页试点**，完成并给出验证证据后向 HuGe 汇报改动清单与证据，**等待确认后才进入下一阶段**；严禁跨阶段连续大改。

### 7.5 性能守则（硬约束，任何图示化改动不得违反）

- 图表库一律 canvas 渲染模式，禁止 SVG 承载大节点量图形；
- 图表库全部动态 import，首屏不加载任何图表依赖；
- 列表/队列渲染采用分页或虚拟滚动，禁止整表全量重建；
- 动画只动 `transform` / `opacity`，尊重 `prefers-reduced-motion`；
- 图示化用色只取自现有 token 色板，禁止新增硬编码颜色；
- 每个阶段验收时记录首屏 JS 体积与渲染耗时基线，劣化即打回。

### 7.6 阶段验收清单（增量追加到 §6 之上）

- [ ] tasks/history 图表 chunk 仅进页签时加载，首屏 JS 体积 ≤ 177KB（不劣化）
- [ ] 转码进度 1s 刷新间隔下无肉眼掉帧（DevTools Performance 记录）
- [ ] 图示化元素全部取自 token 色板，无新增硬编码色
- [ ] 移动端可点区域 ≥44×44px 在新增可视化控件上依然成立
- [ ] 每个阶段有截图证据 + 构建通过证据 + 体积对比数据
