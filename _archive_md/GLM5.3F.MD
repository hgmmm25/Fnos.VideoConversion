# FVCC 主页（任务页）视觉改进方向评审

> 评估主体：`ui-src/src/pages/tasks.ts`（主页/任务页）
> 前提：不引入 React/Vue 等框架、不换构建链，保持 v1.6.0「方向 A：冷灰」基调与 CSS 变量 token 体系。

---

## 一、现状体检（最明显的 5 个短板）

1. **次级信息行过载、无层级** — `ui-src/src/pages/tasks.ts:388-400`：`sub` 行塞了 6 类碎片（`→ 输出文件`、`[服务器]`、`方案:`、阶段、耗时、时间），全部同为 `text-xs text-ink-muted`，靠色相小差异（signal/success span）区分。视觉上是"一串等权重的灰字"，扫读时无法快速定位"这是哪个文件的什么任务"。

2. **错误信息与普通元数据同待遇** — `ui-src/src/pages/tasks.ts:399-402`：`errorMsg` 拼在 `sub` 行末尾（`| xxx`），而 `sub` 整体是 `truncate`（tasks.ts:388），出错任务的**最关键信息恰恰是被截断的那个**。状态徽标 `bg-danger-soft`（types.ts:279）虽已变红，但错误文本本体不可读。

3. **进行中任务的视觉权重不足** — `tasks.ts:405`：进度区仅 `sm:w-40`（160px），`h-1.5` 细条 + 右对齐小字。在 10+ 任务的队列里，"正在跑的任务"和"排队的任务"在卡片轮廓上几乎无差别；且 `getPhaseStyle`（tasks.ts:283-307）对 `PAUSED/CANCELLED/ERROR/QUEUE` 返回空 `phaseLabel`，进度文本只剩裸百分号。

4. **首屏加载态空白** — `tasks.ts:229-241`：`useListPage` 首次 `refresh` 完成前，`listWrap`（tasks.ts:108）完全空白，无任何加载暗示。骨架屏组件 `.skeleton` 已存在（style.css:433-447）但本页没用。

5. **工具栏常驻噪音** — `tasks.ts:70-105`：刷新/全选/暂停/继续/取消 5 个按钮无条件常驻，批量三键在零选中时与普通按钮同等视觉权重；且底部 `stats` 文案（tasks.ts:102）与概览条的计数摘要（tasks.ts:116-157）信息重复。

另注一个**功能债**：`scrollPos.ts` 存在但 `tasks.ts` 全文未调用 `saveScrollPos/restoreScrollPos`，页面切换后滚动位置靠 `render()` 的 diff 更新（tasks.ts:198-246 不重建 `listWrap`）间接保住。任何"整表重建"式的视觉改动都会击穿这一点。

---

## 二、改进方向清单（按性价比排序）

### 方向 1：卡片信息层级重组 + 错误信息独立成行 ⭐ 性价比最高

**做法**：`buildCard` 内（tasks.ts:386-404）把 `sub` 拆两级——文件名行保持 `text-sm font-medium`，次级元数据收敛为 `text-xs text-ink-subtle` 并用 `·` 分隔减少边框噪音；`errorMsg` 从 `sub` 移出，作为卡片内独立行（在 `info` 末尾）：

```ts
// buildCard 内，替换现 errorMsg 挂载方式
const errRow = el('div', {
  class: 'mt-1.5 px-2 py-1 rounded bg-danger-soft/50 text-xs text-danger-soft-text break-all',
  style: 'display:none',
}, [])
// 有错误时 errRow.style.display = ''，updateCardDynamic 同步
```

- **性能代价**：零。纯静态类名，无动画、无额外层；`updateCardDynamic`（tasks.ts:248）只多维护一个节点的 textContent，轮询刷新增量不变。
- **回归风险**：仅 tasks 页；暗色下 `--c-danger-soft` 已是深红底，`/50` 透明度两端可读。注意 `w-[200px]` 的 actions 列（tasks.ts:427）在小屏 `flex-wrap` 时会换行，重组时把 actions 改为 `w-auto sm:w-[200px]` 顺便修掉小屏右侧空块。

### 方向 2：首次加载骨架屏（复用已有 `.skeleton`）⭐

**做法**：`useListPage` 首次 `refresh` 未返回前，向 `listWrap` 渲染 3–5 张骨架卡；数据到位后一次性替换（仅首屏一次，不进轮询路径）：

```ts
// tasks.ts render() 之前：首次加载占位
if (store.tasks.length === 0 && !loaded) {
  listWrap.replaceChildren(...[1,2,3].map(() =>
    el('div', { class: 'card p-3 flex items-center gap-3' }, [
      el('div', { class: 'skeleton w-4 h-4 rounded' }),
      el('div', { class: 'skeleton h-4 flex-1 rounded' }),
      el('div', { class: 'skeleton h-1.5 w-40 rounded' }),
    ])))
}
```

- **性能代价**：一次性；shimmer 动画（style.css:438-445）只动 `transform`，走合成层，且加载完成即销毁。
- **回归风险**：低。注意骨架卡不能进 `cardMap`、不能参与后续 diff（用一次性 `replaceChildren` 即可）；暗色 shimmer 强度已有独立定义（style.css:446-448）。

### 方向 3：进行中任务的静态视觉锚点（先做静态，动画慎选）

**做法 A（推荐，零动画）**：给活动态（UPLOADING/TRANSCODING/DOWNLOADING）卡片加左侧 2px 信号色竖条 + 卡片 ring 微增：

```ts
// buildCard 内
const active = ['UPLOADING','TRANSCODING','DOWNLOADING'].includes(t.status)
card.classList.add(active ? 'ring-1 ring-signal/30 border-l-2 border-l-signal' : '')
```

**做法 B（可选实验）**：对进行中卡片的 `fill` 启用已有的 `.progress-animate` 条纹（style.css:413-427）。**代价要算清楚**：这是 `background-position` 动画，逐帧 repaint 不走合成层，N 张进行中卡片 = N 个持续 repaint 区，低性能 NAS 客户端上多任务并发时滚动会掉帧。若要用，建议限定每屏最多 3 张、并先实测。默认推 A。

- **性能代价**：A 为零；B 见上。
- **回归风险**：A 极低（纯 token 类，双主题自动生效）；B 有暗色下条纹对比度问题（白 0.15 叠加在 signal 上，暗色 surface 底需调 alpha），且与"不得增加重绘频率"约束紧贴——列为实验项而非默认。

### 方向 4：工具栏状态衰减

**做法**：批量三键在 `selected.size === 0` 时 `disabled`（样式已由 style.css `.btn` 的 `disabled:opacity-50` 覆盖），`render()` 里已统计 `selected.size`（tasks.ts:158），顺手同步即可；`stats` 与概览条二选一，建议保留概览条、`stats` 移入其行尾。

- **性能代价**：零（disabled 状态只在选择变化时切换，与轮询无关）。
- **回归风险**：极低；注意 `selectAllBtn.className` 每次渲染整体重写（tasks.ts:162-166）的既有模式，保持一致即可。

### 方向 5：卡片 hover 层次统一 + 密度微调

**做法**：服务器页已确立 `hover:ring-2 hover:ring-primary/30`（servers.ts serverCard），任务卡片同步加上；列表 `space-y-2`（tasks.ts:189）→ `space-y-2.5`，`listWrap` 的 `p-4`（tasks.ts:108）→ `px-4 py-5`。这让主页与兄弟页呼吸感一致。

- **性能代价**：ring 只在 hover 时重绘单卡，可忽略。
- **回归风险**：低；`space-y` 调整会影响拖拽落点线的视觉对位，需回归拖拽排序一次。

### 方向 6（泛化项）：`getPhaseStyle` 补全状态标签

**做法**：补 `QUEUE:'排队'`、`PAUSED:'已暂停'`、`COOLDOWN:'冷却'` 等 `phaseLabel`，让 `progressLabel` 在所有状态下都有语义（此函数同时服务主页与任务抽屉，一处改两处受益）。

- **性能代价/风险**：零/极低，纯文案。

---

## 三、明确不推荐清单

| 手段 | 不推荐原因 |
|---|---|
| `backdrop-filter`/大面积 blur（毛玻璃导航、卡片） | GPU 合成开销大，滚动时逐帧采样，低性能 NAS 浏览器上最典型的掉帧源；也破坏"明度差分层"的 v1.6 决策（P1-2） |
| 持续运行的 `animate-pulse-soft` 状态徽标 | 每张活动卡片一个 2s 无限 opacity 动画，N 卡 N 路；`.animate-pulse-soft`（style.css:405-407）已存在但只能用于单点（如 WS 断线提示） |
| 大阴影/多层 box-shadow 卡片 | 与 P1-2"卡片以明度分层、阴影只留浮动层"的方向直接冲突，加了等于回退 |
| FLIP/重排序动画 | `render()` 每次轮询后按序重挂 DOM（tasks.ts:224-229），加 FLIP 需要测量+transform，轮询高频时收益极小、打断滚动位置的回归风险高 |
| 引入任何前端依赖（动画库、组件库） | 硬约束；且现有自绘 SVG/散点图（servers.ts miniGauge、history.ts 趋势图）已证明零依赖可达成效果 |
| 空态大插画图片 | NAS 离线场景，图片资源进包代价 > 收益；现有 `svgIcon(40)` + 文案 + 动作按钮（ui.ts:371-387）已够 |
| 桌面端卡片改双栏 grid | 任务卡片是横向宽条（信息+进度+按钮），双栏会挤压 actions 区，小屏回归风险高、收益存疑 |

---

## 四、建议实施批次

- **批次 1（一个小版本见效）**：方向 2（骨架屏）+ 方向 4（工具栏衰减）+ 方向 5（hover/密度）+ 方向 6（状态标签补全）。全是低风险增量改动，不触碰 `updateCardDynamic` 热路径。
- **批次 2**：方向 1（信息层级重组 + 错误行）。改动集中在 `buildCard/updateCardDynamic` 两个函数，需重点回归：轮询增量更新、暗色错误行、小屏 `flex-wrap`、拖拽排序。
- **批次 3（实验性）**：方向 3B（条纹进度动画，限定张数实测后决定）；同时考虑把 `saveScrollPos/restoreScrollPos` 补接到 `listWrap` 上，作为本批次的顺手修——这是现有实现里唯一已封装但未接线的滚动位置机制。

---

**整体判断**：v1.6.0 的骨架（token 体系、明度分层、diff 渲染）是健康的，剩余短板全在**信息层级和加载/状态反馈**层，用纯类名 + 已有组件即可收敛，不需要任何框架或依赖。
