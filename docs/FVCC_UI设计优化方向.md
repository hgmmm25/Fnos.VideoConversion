---
AIGC:
    Label: "1"
    ContentProducer: 001191440300708461136T1XGW3
    ProduceID: 839b5d1d4fff15220193598838e6072d_71b5175cb26a11f19c7a525400de85a5
    ReservedCode1: nlYJ8q441BYZAF3YW/ZELScss0WFacFfsFV3P2Q6gS+yvc8xy9ATtlQspmIfvxcwbmybsGdT8RUaVlMYnTIytGwVQHhDAw8dI3rdXskLpA7Umq+84onwYjsAIRXJCA/jdiMUgxmUYZLjpZyZLc2gwMNLlZpLHzCqGAWQUMUzYd7vYVtXOA7Nj46TLuk=
    ContentPropagator: 001191440300708461136T1XGW3
    PropagateID: 839b5d1d4fff15220193598838e6072d_71b5175cb26a11f19c7a525400de85a5
    ReservedCode2: nlYJ8q441BYZAF3YW/ZELScss0WFacFfsFV3P2Q6gS+yvc8xy9ATtlQspmIfvxcwbmybsGdT8RUaVlMYnTIytGwVQHhDAw8dI3rdXskLpA7Umq+84onwYjsAIRXJCA/jdiMUgxmUYZLjpZyZLc2gwMNLlZpLHzCqGAWQUMUzYd7vYVtXOA7Nj46TLuk=
---



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

| # | 结论 | 性质 |
|---|------|------|
| 1 | 设计令牌存在**两套真源**：`style.css` 的 `--c-*` 与 `index.html` body 的 `slate-*` 硬编码、页面内的 `#000` / `rgba(0,0,0,0.03)` / `border-t-2` 并存；`tailwind.config.js` 里 `darkMode: ['class','[data-theme="dark"]']` 与实际生效的 `data-theme` 属性策略不完全一致 | 正确性缺陷，P0 |
| 2 | 色彩语言是**通用后台面板范式**（`#3b82f6` 蓝主色 + slate 中性 + 7 类语义色 + 胶囊 badge），与剪辑工具"单一信号色 + 中性明度阶梯"的专业语言相反 | 设计定位，P1 |
| 3 | **无障碍与交互状态有硬缺陷**：`--c-ink-subtle`(#94a3b8) 在浅色面上约 2.6:1、按钮无 focus-visible、`btn-sm` 触控高约 26px、动画使用 `max-height`（布局属性且 300px 截断） | 合规与手感，P0 |
| 4 | **编辑器页比列表页更"专业"**：预览区纯黑、素材库可折叠、时间线可拖拽且尺寸持久化——说明专业感的基础认知已有，缺的是把它固化为设计契约 | 机会点 |
| 5 | 真正缺的不是"更好看"，而是 freecut 那种**写成规则的设计契约 + 可机器校验的门禁**（DESIGN.md / named rules / design.json） | 工程方法，P1 |

---

## 1. 现状盘点（有源码依据）

### 1.1 技术底座与结构

- Vite + TypeScript + Tailwind，**零框架**，DOM 由 `src/ui.ts` 的 `el(tag, attrs, children)` 手工构造，图标为内联 SVG 路径表（`ICON_PATHS`，统一 `stroke-width: 2`）。
- 路由：`main.ts` 中的 hash 路由，7 个平级页签——
  `剪辑(editor) / 任务清单(tasks) / 视频文件(scanner) / 转码服务器(servers) / 转码方案(profiles) / 历史任务(history) / 设置(settings)`；
  `editor` 支持 `#/editor/:projectId` 子路由与 `renderProjectPicker`。
- 外壳：顶部 `h-12` 水平 tab 导航，移动端（`md` 以下）折叠为下拉菜单；`renderShell` 中原本的"顶部状态条"已被移除。
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

`style.css` 中定义 6 个 keyframes：`slide-down` / `slide-up` / `fade-in-up` / `fade-in` / `scale-in` / `pulse-soft` / `slide-in-right` / `progress-stripes`。
其中 `slide-down`、`slide-up` 通过 `max-height: 0 → 300px` 实现展开收起，动画属性为**布局属性**，且 **300px 是硬上限**（超出内容会被裁剪）；缓动全部为 `ease-out` / `ease-in` / `linear`，无自定义缓动 token。

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

| 维度 | freecut（目标） | FVCC `ui-src`（现状） | 主要证据 | 差距 |
|------|------------------|------------------------|----------|------|
| 色彩空间 | OKLCH，中性带品牌色相偏移 | sRGB 十六进制/RGB 通道，中性为纯 slate，无色相偏移 | `style.css` `--c-page:#f8fafc`、`--c-line:#e2e8f0` | 大 |
| 主色策略 | 单一信号色（暖橙），只表示"活动/进行中" | 蓝主色既作品牌、又作按钮、又作进度、又作选中态；另有 7 类语义色与 `-soft` 底色 | `style.css` `--c-primary`、`tailwind.config.js`、`tasks.ts` `getPhaseStyle` | 大 |
| 层次表达 | 明度阶梯（0.12/0.14/0.16/0.18），无默认阴影 | `.card` = `shadow-sm + border`；`confirmDialog` 用 2px 彩色描边强化危险态 | `style.css` `.card`、`ui.ts` `confirmDialog` | 大 |
| 主题 | 有意 always-dark | light 默认 + dark 可切 + 跟随 fnOS 桌面 | `theme.ts`、`index.html`、`editor/layout.ts` 预览区 `#000` | 定位差异 |
| 字体 | IBM Plex Sans + Plex Mono，数据一律 mono | 无字体策略，默认系统栈；时间码/大小/速率均为默认字体 | `index.html` 无字体引入；`ui.ts` `formatDuration` 输出 `h/m` 格式 | 大 |
| 动效 | easing token（`ease-out-strong` 等），只动 transform/opacity | 4 组 `ease-out/in` 常量；`slide-*` 用 `max-height` 且 300px 截断 | `style.css` keyframes | 中 |
| 控件状态 | 5 态齐备 + 焦点环可见 | `.btn` 无 hover/focus-visible/active 细分（仅 `hover:bg-neutral-soft`）；仅 `.input` 有 ring | `style.css` `@layer components` | 大 |
| 无障碍 | 明示 AA 与占位符 4.5:1 | `--c-ink-subtle:#94a3b8` 大量用于 12px 提示文字，浅色面上约 2.6:1 | `style.css`、`settings.ts` hint、`tasks.ts` 副信息 | 大 |
| 触控/密度 | 控件 36px 基准，图标按钮 36×36 | `btn-sm` = `px-2 py-1 text-xs`，高约 26px；移动端菜单项 `py-2` | `style.css` `.btn-sm`、`main.ts` mobileMenu | 中 |
| 导航信息架构 | 侧边栏 + 面板系统 + 路由树（TanStack Router，含 `/projects/:id`） | 7 个平级顶栏 tab；原"顶部状态条"被移除，全局在线/队列状态无承载 | `main.ts` `PAGES` | 中 |
| 设计契约 | `DESIGN.md` 带 front-matter 令牌 + named rules + `PRODUCT.md` 反例清单 + `.impeccable/design.json` | 无设计文档；规则只存在于各页面的 class 字符串里 | 目录对比 | 大 |

---

## 3. 优化方向（按优先级）

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

| 阶段 | 范围 | 影响面 | 风险 |
|------|------|--------|------|
| 阶段一（P0） | 令牌收敛、对比度与焦点、动效修正、触控尺寸 | `style.css`、`tailwind.config.js`、`index.html`、`ui.ts`、`settings.ts`、`tasks.ts`。**不动业务逻辑**，可整批回归 | 低：改色可能暴露此前"靠硬编码救回来"的角落 |
| 阶段二（P1-1~P1-3） | 色彩语言、层次、字体与数据排版 | 全局 CSS + token，页面 class 批量替换 | 中：需逐页截图核对；建议先用一个页面（`tasks`）试点，确认后再全量 |
| 阶段三（P1-4~P1-5） | 信息架构、全局状态条、编辑器签名面 | `main.ts` 外壳重构 + `editor/*` | 中高：外壳改动会波及移动端与 fnOS 内嵌场景，需保留 hash 路由兼容 |
| 阶段四（P2） | 状态完备、无障碍语义、设计契约与门禁 | 全站 + 构建脚本 | 低到中：门禁首次接入会产生一批既有告警，建议以增量文件为范围 |

---

## 6. 验收清单（可直接作为 review 依据）

- [ ] Squint Test 通过：界面模糊后仍能分辨主次与当前活动态
- [ ] 全站无硬编码颜色（除经登记的 ANSI 语义色），无 Tailwind 原色板引用
- [ ] 正文/占位符对比度 ≥4.5:1，大号粗体 ≥3:1
- [ ] 所有交互元素具备 default / hover / active / focus-visible / disabled 五态，键盘焦点可见
- [ ] 移动端可点区域 ≥44×44px
- [ ] 动画只动 transform / opacity，且尊重 `prefers-reduced-motion`
- [ ] 列表页有时间码/数字时使用等宽字体与 `tabular-nums`
- [ ] 列表、表单、详情均有骨架加载态、承载下一步动作的空态、人话错误态
- [ ] 默认态无阴影；同一屏不出现嵌套卡片
- [ ] 存在 `DESIGN.md` 且关键禁令已脚本化校验
*（内容由AI生成，仅供参考）*
*（内容由AI生成，仅供参考）*
