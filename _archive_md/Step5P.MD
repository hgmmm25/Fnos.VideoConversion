# FVCC 主页（任务页）视觉改进评审报告

> 评估基于实际代码（v1.6.0 视觉改版后）：`FVCC/ui-src/src/pages/tasks.ts`（582 行）、`main.ts`、`style.css`、`lib/useListPage.ts`、`crudActions.ts`、`scrollPos.ts`、`tailwind.config.js`、`theme.ts`、`types.ts`、`ui.ts`、`store.ts`，及兄弟页 `history.ts` / `servers.ts`、`docs/FVCC_视觉更新方案_v1.6.0.md`、`scripts/check-design.mjs`。

**核心问题**：在现有实现框架不变的前提下（不引入 React/Vue 等框架、不换构建链），主页还能往哪些方向变得更好看？每个方向的性能代价和回归风险是什么？

---

## 一、现状体检（5 条，按严重度）

### 1. 副信息行截断，会"吃掉"错误信息 —— 功能性视觉缺陷

`tasks.ts:388-401`：`sub` 容器带 `truncate`（overflow:hidden），而错误信息 `errorMsg` 被最后 append 进去（`tasks.ts:401`）。副行同时要放 `→ outputName`、`[serverName]`、`方案: xxx`、extras、创建时间，窄容器下最后入队的错误文案直接被裁掉——而 ERROR 态最重要的恰恰是它。另 `fileName`（`tasks.ts:387`）与副行都无 `title` 属性，截断后信息不可恢复。

```
tasks.ts:388-401
const sub = el('div', { class: 'text-xs text-ink-muted truncate flex items-center gap-2 flex-wrap' }, [...])
const errorMsg = el('span', { class: 'text-danger' }, [])
...
sub.appendChild(errorMsg)   // ← 最后一个入队，最容易被裁掉
```

### 2. 卡片单行 6 元素并列，信息层级扁平

`tasks.ts:304` 卡片是 `flex flex-wrap items-center gap-3`，`tasks.ts:464` 一次性 append：拖拽柄 + checkbox + badge + info + 进度列（`w-full sm:w-40`，`tasks.ts:405`）+ 操作列（固定 `w-[200px]`，`tasks.ts:427`）。主文件名、状态、进度、操作在同一视觉行、同一权重；宽屏下 info 被 160px+200px 两个定宽列挤压，窄屏下 200px 操作列对单按钮行（如 QUEUE 只有"取消"）是纯浪费。

### 3. 选中态无任何卡片级反馈

`tasks.ts:377-380`：勾选只改 `selected` Set 和 checkbox 自身样式，卡片外观零变化。批量暂停/继续/取消时用户根本无法在列表中确认"选了哪几个"。

```
tasks.ts:377-380
cb.onchange = () => {
  if (cb.checked) selected.add(t.id)
  else selected.delete(t.id)
  // ← 卡片本身没有任何视觉变化
}
```

### 4. 首屏与刷新无加载/进行态

`tasks.ts:234-243`：`useListPage.mount()` 是异步 `loadTasks` 完成后才 render，之前 `listWrap` 完全空白；刷新按钮（`tasks.ts:73-79`）点击后无 pending 态；批量三按钮（`tasks.ts:93-103`）在 `selected.size === 0` 时仍可点、只弹 toast。项目里 `skeleton()` / `skeletonRows()` 已存在于 `ui.ts:90-101`，scanner（`scanner.ts:1164`）、settings（`settings.ts:24`）都在用，唯独 tasks 页没用。

### 5. 状态呈现细节不一致 + 每次 notify 的全量 DOM churn

`getPhaseStyle`（`tasks.ts:274-300`）的 default 分支把 ERROR 也画成 `bg-neutral-soft` 灰色进度条，与红色 badge（`types.ts:279`）语义相反；`Task.speed` / `outTimeMs`（`types.ts:56-57`）后端已下发，列表未使用，进行中任务缺少速率感知。

另：每次 `store.notify`（WS 每个活跃任务约 1 次/秒，`store.ts:152-171`）都会触发 `render()`：

- `tasks.ts:224-230` 对**每一张卡**执行一次 `appendChild` 重排（即使顺序没变）；
- `updateOverview` 整体 `replaceChildren`（`tasks.ts:150-154`）重建三段 DOM。

属可零成本消掉的 churn，长列表 + 滚动时最明显。

---

## 二、改进方向清单（按性价比排序）

### 方向 1：错误信息独立成行 + 截断可恢复（修体检 1）

**做法**：把 `errorMsg` 从 `sub` 摘出，`info.append(name, errLine, sub)` 顺序放入（`tasks.ts:395-401` 区域），`errLine` 加 `truncate` + `title={t.errorMsg}`；`fileName`（`tasks.ts:387`）加 `title={t.fileName}`。`updateCardDynamic` 已有 `errorMsg` 分支（`tasks.ts:266-271`），同步改 `display` 即可，两处保持一致（现有代码模式就是双写，照抄）。

```ts
const errLine = el('div', { class: 'text-xs text-danger truncate', title: t.errorMsg }, [t.errorMsg])
info.append(name, errLine, sub)
```

- **性能代价**：零新增成本；反而让副行少一个 inline span。首屏/滚动/WS 刷新均无感。
- **回归风险**：暗色无新色（`text-danger` 已是 token）；窄屏多一个 block 行、卡片增高约 16px，与 `flex-wrap` 兼容；不涉及其他页。

### 方向 2：选中卡片高亮（修体检 3）

**做法**：`cb.onchange`（`tasks.ts:377`）里 `card.classList.toggle('task-selected', cb.checked)`；`buildCard` 初始化时同样按 `selected.has(t.id)` 加类；CSS 加在 `style.css` components 层：

```css
.task-selected {
  box-shadow: inset 0 0 0 1px rgb(var(--c-primary) / 0.55), inset 3px 0 0 rgb(var(--c-primary));
}
```

- **性能代价**：仅勾选瞬间一次 repaint；不随 WS 刷新触发（不进 `updateCardDynamic`）。
- **回归风险**：颜色走 `--c-primary`，暗色自动适配；不与拖拽落点的 `border-t-drop/border-drop-line`（`tasks.ts:335-360`）冲突；`render(true)` 全量重建（`tasks.ts:90`）时 cb.checked 已同步设类，需核对一遍。

### 方向 3：加载骨架 + 刷新 pending + 批量按钮禁用（修体检 4）

**做法**：`renderTasks` 内加 `let loaded = false`，首次 `refresh()` 成功后置 true；`tasks.length === 0 && !loaded` 时渲染 `skeletonRows(5)`（卡成任务卡高度：`ui.ts:95-101` 已支持传 `h`）；`refresh.onclick` 期间 `refresh.disabled = true`，finally 恢复；`render()` 里统一 `batchPause.disabled = batchResume.disabled = batchCancel.disabled = selected.size === 0`，`selectAllBtn.disabled = tasks.length === 0`。`.btn` 已有 disabled 态样式（`style.css:177`）。

```ts
if (tasks.length === 0 && !loaded) {
  listWrap.replaceChildren(el('div', { class: 'space-y-2' }, skeletonRows(5, '3.5rem')))
  return
}
```

- **性能代价**：骨架只在首屏存在一次，shimmer 动画（`style.css:438-445`）数据到达即移除；按钮禁用只在 render() 里 set 属性。
- **回归风险**：暗色 skeleton 已有适配（`style.css:446-448`）；`loaded` 标志跨 `render()` 闭包保持，注意与空态 `emptyState`（`tasks.ts:172-179`）互斥；此模式可原样泛化到 servers/history（见批次 C）。

### 方向 4：进度条 transform 化 + ERROR 语义色 + 速率展示（修体检 5）

**做法一（性能核心）**：`fill` 从 width 动画改为 `transform: scaleX()`——这是 WS 每秒推送 `task_update`（`store.ts:152-171`）→ `updateCardDynamic`（`tasks.ts:248-272`）路径上每次都在写的属性：

```css
.task-fill {
  width: 100%;
  transform-origin: left center;
  transform: scaleX(var(--p, 0));
  transition: transform 0.2s var(--ease-out-strong), background-color 0.15s;
}
```

TS 侧 `entry.fill.style.setProperty('--p', String(displayProgress / 100))` 替换 `entry.fill.style.width`（`tasks.ts:256`）；删掉 `transition-all`（`tasks.ts:420`）。父级 `bar` 已有 `overflow-hidden rounded-full`（`tasks.ts:411-413`）负责裁切，视觉不变。宽度动画每次都触发 layout，transform 走合成器，长列表 + 多活跃任务时收益最大。

**做法二**：`getPhaseStyle` default 前增加 `case 'ERROR': phaseColor = 'bg-danger'`（`tasks.ts:294-297`），与红色 badge 对齐。

**做法三（可选）**：`progText` 文案在 TRANSCODING 且 `t.speed` 非空时追加 ` · ${t.speed}`（`types.ts:57`），`progressLabel` 在 `taskView.ts:37-44`，加参数即可。

- **性能代价**：首屏无感；滚动无感；WS tick 时每活跃卡从一次 layout+repaint 降为一次 compositor transform，这是全清单里唯一直接改善"轮询刷新"体感的项。ERROR 色和 speed 是纯文本/类名替换，零成本。
- **回归风险**：scaleX 方案需回归一次 0%/100% 两端渲染（百分比与 aria 值仍走 `String(displayProgress)`，`tasks.ts:263`，不受影响）；`speed` 字符串由后端决定，展示前需 trim，空串跳过；`bg-danger` 是现有 token，暗色已有定义。

### 方向 5（低优先）：概览条与徽标 chip 化 + churn 守卫

**做法**：`updateOverview` 右侧 `labels.join(' · ')` 裸文本（`tasks.ts:153`）改为与外壳状态条一致的 `status-chip` 样式（`style.css:289-291`）+ 小色点；或给 badge（`tasks.ts:383`）加 6px 状态点 span（`currentColor`），增强扫描性。可顺手给重排循环加守卫：记住上次 order 签名，`tasks.ts:224-230` 仅在顺序真变化时重排；`updateOverview` 仅在 counts 变化时 `replaceChildren`。

- **性能代价**：重排守卫直接砍掉每次 notify 的全卡 DOM churn（长列表 + 滚动时最明显）；chip 化是一次性 DOM。
- **回归风险**：`status-chip` 是外壳通用类，tasks 页复用不串味；徽标加点会影响 `history.ts:131`、`taskDrawer` 等所有 badge 使用处——建议只做 tasks 页的点，或拧成 `.badge-dot` 新类局部用。

---

## 三、明确不推荐清单

| 手段 | 原因 |
|---|---|
| toolbar/overview 加 `backdrop-filter` 毛玻璃 | 低端 NAS 浏览器整区重绘，滚动每帧都在罚；现状 style.css 零 blur 是优点 |
| 进度条条纹流动动画 `.progress-animate`（`style.css:413-426`，目前无人用）上列表 | 持续动画 × N 行，且与 WS 1Hz 进度叠加视觉抖动；违背硬约束 3 |
| 卡片大投影/多层 box-shadow | 违背 v1.6.0 明度分层路线（`style.css:199-202` 注释即 P1-2）；滚动持续重绘阴影 |
| hover `transform: scale` 卡片动效 | 长列表每卡挂合成层；`hover:bg-surface-hover` 已足够（`servers.ts:161` 就是范例） |
| 虚拟滚动 / 引入 list 依赖 | 违反无框架约束；cardMap 增量渲染（`tasks.ts:196-222`）+ 重排守卫已覆盖，tasks 量级（NAS 家庭/小工作室）通常 <100 |
| CDN 图标字体/icon 库 | NAS 内网友好性归零；`svgIcon`（`ui.ts:56-68`）加路径即可 |
| sticky 卡片头/粘性进度 | `content` 容器（`main.ts:216`）内 sticky 在滚动时反复重排，收益低 |
| 骨架 shimmer 常驻 | shimmer 是加载态专用（`style.css:433-448`），别改成常驻氛围动画 |

---

## 四、实施批次划分

### 批次 A（建议合成一个 v1.6.1「任务页打磨」，半天可完成、一次验收）

方向 1 + 方向 2 + 方向 3 + badge 状态点。全部是局部 DOM/CSS 改动、零新依赖、零新色（除 `bg-danger` 既有 token），明暗双主题只需各截一次图对比。

### 批次 B（可与 A 同版本，也可紧随）

方向 4 三件套（transform / ERROR 色 / speed）+ 方向 5 的重排守卫与 `updateOverview` 守卫。transform 是唯一动到"每 tick 热路径"的改动，单独一批便于用 WS 真实推送回归滚动流畅度。

### 批次 C（可选后续，跨页泛化）

骨架模式提取为 `ui.ts` 的 `listSkeleton()`，servers/history 复用；概览 chip 化；`formatTime` 相对时间。

### 验收基线（沿用 v1.6.0 门禁，勿绕开）

- `npm run build`（含 `check:design:diff` + `tsc`）+ `node scripts/check-design.mjs --all`。
- 新增 CSS/类名不得触发 R1（hex 硬编码）/R2（Tailwind 原色板）/R3（默认态阴影）/R4（<12px 字）/R5（max-height 动画）——上面所有片段均为 token 或 transform/opacity 动画，合规。
- 功能回归重点：
  - WS tick 下滚动位置不跳（tasks 页未用 `scrollPos`，靠卡片身份保持，勿改全量重建的触发条件）；
  - 暗色下选中/错误行对比度 ≥4.5:1；
  - 320px 窄屏卡片换行后的点击热区（`btn-sm` 移动端 ≥44px 规则见 `style.css:187-193`，勿破）。
