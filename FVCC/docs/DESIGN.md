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

## 反例清单（不要做）

- ❌ 消费级剪辑器糖果风：高饱和渐变按钮、圆角胶囊满屏、emoji 图标
- ❌ 炫技 SaaS 仪表盘：渐变头图、glassmorphism、大数字指标卡、彩色卡片海洋
- ❌ 拥挤的老式 NLE 灰工具栏：无层次的信息堆砌
- ❌ 把状态画成颜色而不给文本等价（红/绿盲区不可用）
- ❌ 用 `border-t` 在设置页铺满分段（改用背景差 `surface-alt`）
- ❌ 用 `max-height` 做展开收起（300px 截断、布局属性动画）
- ❌ 在页面里写 `#000` / `rgba(0,0,0,0.03)` / `bg-slate-*` 等硬编码
- ❌ 默认态阴影、卡片套卡片

