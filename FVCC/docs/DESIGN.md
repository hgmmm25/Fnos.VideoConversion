---
name: FVCC Web UI
description: NAS 视频转码调度端 Web UI。零框架（el() 手工 DOM）+ Vite + TypeScript + Tailwind。设计目标范式参照 freecut（The Quiet Instrument）：器材感、克制、数据可读优先。
colors:
  # 真源：src/style.css 的 :root 与 [data-theme="dark"] 两套 RGB 通道变量（tailwind.config.js 映射为 token 类）
  # 引用方式：rgb(var(--c-x) / <alpha-value>)；禁止在页面中直接书写 hex / rgba / 原色板类
  page: "rgb(var(--c-page))"
  surface: "rgb(var(--c-surface))"
  surface-hover: "rgb(var(--c-surface-hover))"
  surface-alt: "rgb(var(--c-surface-alt))  # 内嵌面（明度介于 surface 与 page）"
  line: "rgb(var(--c-line))"
  line-subtle: "rgb(var(--c-line-subtle))"
  ink: "rgb(var(--c-ink))"
  ink-muted: "rgb(var(--c-ink-muted))   # 正文/次要文本（浅色面对比度 ≥4.5:1）"
  ink-subtle: "rgb(var(--c-ink-subtle)) # 仅用于图标/分隔等非文本元素，禁止用于正文与 hint"
  primary: "rgb(var(--c-primary))       # 品牌与主操作"
  primary-hover: "rgb(var(--c-primary-hover))"
  signal: "rgb(var(--c-signal))         # 单一信号色：仅表达"进行中/活动态"
  success: "rgb(var(--c-success))"
  danger: "rgb(var(--c-danger))"
  warning: "rgb(var(--c-warning))"
  neutral: "rgb(var(--c-neutral))"
  preview-bg: "rgb(var(--c-preview-bg)) # 编辑器预览区恒定深底（亮/暗主题保持一致）"
  drop-line: "rgb(var(--c-drop-line))   # 拖拽落点指示线"
  clip-video/audio/image/text/marker: "rgb(var(--c-clip-*)) # 时间线数据色，承载语义不可挪用"
  ansi-*: "rgb(var(--ansi-*))           # 日志 ANSI 语义色（门禁登记豁免的仅此一组）"
typography:
  # 字阶（当前为系统栈 + 数据等宽；P1-3 可选的 IBM Plex 引入不阻塞本契约）
  title: "font-semibold text-base"      # 面板/区块标题 600
  card-title: "font-semibold text-sm"   # 卡片标题 600
  body: "text-sm"                       # 正文 400
  label: "text-xs font-medium"          # 标签 500
  mono: "font-mono tabular-nums"        # 时间码/帧号/FPS/分辨率/文件大小/进度百分比/日志一律等宽 + tabular-nums
radius:
  sm: "rounded"      # 4px
  md: "rounded-md"   # 6px
  lg: "rounded-lg"   # 8px（卡片）
  full: "rounded-full" # 胶囊（仅 badge）
spacing:
  xs: "4px"  sm: "8px"  md: "12px"  lg: "16px"  xl: "24px"
motion:
  ease-out-strong: "var(--ease-out-strong, cubic-bezier(0.23,1,0.32,1))"
  ease-in-out-strong: "var(--ease-in-out-strong, cubic-bezier(0.23,1,0.32,1))"
  rule: "动画只动 transform / opacity；禁止 max-height 过渡；全部尊重 prefers-reduced-motion"
---

# FVCC Web UI — Design Contract

> 复核：2026-10-14（对齐 **FVCC v1.6.7 / FVCS v1.3.1**，台账见仓库根 `docs/IMPROVEMENT_LOG.md` §二十三 / §二十四）——本轮为 sqlite 驱动与 Node 运行时的依赖/工具链批次，**零 ui-src 视觉与令牌改动**（构建产物内容哈希逐项不变），故本文全部条款、令牌真源位置与 `check:design` 门禁口径原样成立，热路径与状态反馈惯例无新增约束。需注意的后续项：`tailwindcss` 3.4.19→4.3.3 专项落地时将直接改写本文的令牌映射机制（v4 为 CSS-first，`tailwind.config.js` 的 token 映射与 `@apply` 语义均变），届时必须同步回写本文，不可"先升库再补文档"。

> 复核：2026-10-13（对齐 **FVCC v1.6.5 / FVCS v1.3.0**，台账见仓库根 `docs/IMPROVEMENT_LOG.md` §二十 / 第二十八轮）——本轮 1.6.0（方向 A 令牌层重排 / 控件自绘 / 外壳 / 逐页修复 / 微交互+WCAG）与 1.6.4（任务页批次 A+B+C）都在本契约约束内落地，`npm run build` 链路仍先跑 `check:design` 再 `tsc -b` + `vite build`（2026-10-13 全绿）；**令牌真源 `src/style.css` 与门禁命令 `check:design` / `check:design:diff`（`ui-src/scripts/check-design.mjs`）的位置与口径维持成立，但 frontmatter 的逐条取值与 R1-R10 的逐条文案本次未逐项复跑**，沿用上一轮结论。新形成的前端惯例（`listSkeleton` 组件、进度条合成层写法、签名守卫、骨架退出条件）补记于 R7 与「热路径与状态反馈惯例」节。
>
> 上一轮复核（保留为历史快照）：2026-10-12（对齐 FVCC v1.6.0）——令牌真源 `src/style.css`、门禁命令 `check:design` / `check:design:diff`（`ui-src/scripts/check-design.mjs`）与 Named Rules 均与代码一致；P1-2 决策门定案（2026-10-11）后本契约维持零框架前端不变。

## 北极星

**"调度台，不是仪表盘。"** FVCC 是 NAS 上的转码调度端：用户要的是"一眼看到队列与节点在干什么、哪里出了问题、怎么补一刀"，不是消费级糖果界面，也不是炫技 SaaS 数据看板。素材信息、进度与日志是屏幕上最需要可读的东西。

## Named Rules

### R1 · The Token Rule（令牌真源）
一切颜色、圆角、缓动只允许来自 `src/style.css` 的 `--c-*` / `--ease-*` 变量及其 tailwind 映射类。**禁止**在 `src/` 内书写 hex、`rgba()` 或任意 Tailwind 原色板（`slate/blue/red/...`）。唯一豁免：日志 ANSI 语义色（`--ansi-*` 变量声明），且必须集中在 `style.css` 中登记。

### R2 · The Single Signal Rule（单一信号色）
`signal` 只表达"进行中 / 活动态"（进度、队列活跃、进行中的渲染）。主操作归 `primary`，语义反馈归 `success/danger/warning`。信号色出现面积必须克制——Squint Test（截图缩半）后仍能一眼指出"现在在跑什么"。

### R3 · The Value-Hierarchy Rule（明度分层，不靠边框与阴影）
层次用 `page → surface → surface-alt` 明度阶梯表达；默认态**无阴影**，仅弹层/抽屉保留浮动阴影（`shadow-lg/xl`）。同一屏不出现"卡片套卡片"。

### R4 · The Mono-For-Data Rule（数据一律等宽）
时间码、帧号、FPS、分辨率、文件大小、进度百分比、日志，一律 `font-mono` + `tabular-nums`，保证列内数字纵向对齐不跳动。

### R5 · The Flat-By-Default Rule（静止面扁平）
静止元素不加渐变、不加投影、不加彩色描边。危险级别用标题红字 + 危险按钮表达，不用 2px 彩色边框。

### R6 · The Dialog Semantics Rule（自建浮层无障碍）
所有自建 overlay（确认弹窗、日志查看器、命令面板、目录浏览器、创建任务弹窗、渲染弹窗、任务抽屉）必须具备：`role="dialog"` + `aria-modal` + `aria-labelledby`、Esc 关闭、Tab 焦点陷阱、关闭后焦点返回触发元素。

### R7 · The Skeleton-First Rule（状态完备）
- 加载态：骨架屏（`.skeleton` / `skeletonRows`），替代"加载中..."纯文本与 spinner；
- 空态：必须承载下一步动作按钮（如"暂无任务 → 去视频文件页创建"）；
- 批量操作：逐项结果（成功 N、失败 M 及原因），不只报总数；
- 低风险即时动作（选中、排序、静音切换）：乐观更新 + 失败回滚。
- **骨架写法（2026-10-13 补记，v1.6.4 批次 A/C 形成）**：
  - 长列表首屏骨架一律用 `ui.ts` 的泛化组件 `listSkeleton(n, h)`（v1.6.4 新增），不再各页自铺 `.skeleton` 块——**目前仅 tasks 页已切换**，其余页面尚未迁移，迁移时沿用下列退出条件；
  - 骨架退出条件**必须**挂在"首次成功 render 即置 `loaded`"这一事件上，**禁止**写成 `items.length > 0`：v1.6.4 首版即因 `tasks.length>0` 导致空库时骨架常驻出不来，由用户真机实测抓出后改正，空列表应正常落到 `emptyState`；
  - 骨架与空态互斥：只在首次拉取在途的窗口期铺骨架，此后刷新不再回落骨架。

### R8 · The Contrast Rule（对比度）
正文与占位符 ≥ 4.5:1，大号/粗体 ≥ 3:1。`ink-muted` 用于正文与 hint；`ink-subtle` 只用于图标/分隔等非文本。进度条、badge 必须补 `role="progressbar"` / `aria-valuenow` 与文本等价，不单靠颜色表达状态。

### R9 · The Touch Rule（触控）
移动端（`md` 以下）所有可点元素 ≥ 44×44px（`btn-sm` 通过 `min-height` 强制）。键盘 Tab 走查所有按钮焦点可见（`focus-visible` ring 2px）。

### R10 · The Incremental Gate Rule（增量门禁）
`npm run check:design`（全量）/ `check:design:diff`（增量，默认 git diff）执行 `scripts/check-design.mjs`：
- 禁止硬编码 hex（ANSI 语义色登记豁免）；
- 禁止 Tailwind 原色板；
- 禁止默认态阴影（`shadow-sm/md`；`shadow-lg/xl` 仅限弹层）；
- 禁止 <12px 文字；
- 禁止 `max-height` 过渡。
门禁以增量文件为范围，存量告警不计入本次提交阻塞；新代码违反即失败。

**门禁口径补记（2026-10-13，v1.6.4 批次过程修复）**：阴影检查（对应 R3）此前会把 CSS 属性 `box-shadow` 当内联样式误报——规则本意只查内联 `style`，已按前缀排除 `-` / `:`；**选中态的 inset 阴影**（`.task-selected`）按方案稿 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md` §2.3 裁决为**合法**，属活动态反馈而非 R3/R5 禁止的默认态浮动阴影。

## 热路径与状态反馈惯例（2026-10-13 补记，v1.6.4 批次 A+B/C 形成）

这些写法在 1.6.4 任务页改造中固化为前端惯例，后续列表/队列类页面改动同等适用：

- **硬约束：视觉改进不得增加 WS tick 重绘**（方案稿立规，批次 B 落地）。任何以"好看"为名的改动若让 1s 级 WS 推送路径多做一次 layout / repaint，视为不通过。
- **进度条走合成层**：只写自定义属性 `--p`，由 CSS 以 `transform: scaleX(var(--p))` 引用；**禁止**动画 `width`、禁止每次更新重挂 `transition`（批次 B1）。
- **签名守卫（signature guard）**：渲染前比对上一次的签名，未变化即跳过 DOM 重建——
  - `render`：id / 顺序签名，未变且卡数一致时跳过重排循环（批次 B2）；
  - `updateOverview`：分组计数 + 总量签名，未变时不 `replaceChildren`；空态签名固定 `'0'`，重复空态不再动概览 DOM（批次 B3）。
- **滚动位置按实例上下文保存/恢复**：`lib/scrollPos` 的 `saveScrollPos` / `restoreScrollPos`，主页与抽屉使用**独立 key 不互串**；DOM 重建前先存、重建后恢复，WS tick 不跳滚动（批次 B4）。
- **状态反馈的层级优先**：错误信息独立成行并带 `title`（不与 `truncate` 行尾互相抵消）；`serverName` 等中性标签走低饱和标签、不与进度条撞 `signal` 色；活动卡以左侧 `signal` 静态指示线作锚点；零选中的批量按钮直接禁用 + 刷新期 pending；选中态用 `.task-selected` 即时高亮，**不走重渲染**（批次 A）。
- **倍速等实时数据走 WS 合并**：`store.ts` 的 `applyTaskUpdate` 合并 `speed`（后端 `models.go` `speed,omitempty` + `TaskUpdateFullMsg`），文案后缀单独 `span` 更新，不因一个字段重建整卡。
- **实验动画一律挂在 URL 开关后**：`#/tasks?fx=stripes` / `?fx=breathe` **默认全关**，仅供真实并发实测裁决；未经真机裁决不得进入默认路径（也不得写进本契约作为规范）。

## 反例清单（不要做）

- ❌ 消费级剪辑器糖果风：高饱和渐变按钮、圆角胶囊满屏、emoji 图标
- ❌ 炫技 SaaS 仪表盘：渐变头图、glassmorphism、大数字指标卡、彩色卡片海洋
- ❌ 拥挤的老式 NLE 灰工具栏：无层次的信息堆砌
- ❌ 把状态画成颜色而不给文本等价（红/绿盲区不可用）
- ❌ 用 `border-t` 在设置页铺满分段（改用背景差 `surface-alt`）
- ❌ 用 `max-height` 做展开收起（300px 截断、布局属性动画）
- ❌ 在页面里写 `#000` / `rgba(0,0,0,0.03)` / `bg-slate-*` 等硬编码
- ❌ 默认态阴影、卡片套卡片

