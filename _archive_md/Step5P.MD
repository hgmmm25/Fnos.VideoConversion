# FVCC 主页（任务页）视觉改进评审报告 — Step5P 摘要

> 原始评审稿（约 11.9KB）已于 2026-10-14 压缩为本摘要；完整内容曾整合进 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`（随 v1.6.4 批次 A/B/C 交付）。评审基于 v1.6.0 改版后实际代码：`tasks.ts`、`style.css`、`lib/useListPage.ts`、兄弟页 history/servers、`check-design.mjs`。

## 体检（5 条，按严重度；本稿对性能 churn 的观察最细）

1. **副信息行截断会"吃掉"错误信息**（功能性缺陷）——`sub` 带 `truncate`，`errorMsg` 最后 append，窄容器下最关键的 ERROR 文案被直接裁掉；`fileName` 与副行都无 `title` 属性，截断后信息不可恢复
2. 卡片单行 6 元素并列、层级扁平——拖拽柄+checkbox+badge+info+进度列（`sm:w-40`）+操作列（`w-[200px]`）同权重；窄屏 200px 操作列对单按钮行是纯浪费
3. **选中态无任何卡片级反馈**——勾选只改 `selected` Set 与 checkbox 样式，批量操作时无法确认"选了哪几个"
4. 首屏与刷新无加载/进行态——`useListPage.mount()` 异步完成前 `listWrap` 空白；刷新按钮无 pending；批量三键零选中仍可点只弹 toast；skeleton 组件已存在但 tasks 页未用（scanner/settings 都在用）
5. 状态呈现不一致 + 每次 notify 全量 DOM churn——`getPhaseStyle` default 分支把 ERROR 画成灰色进度条（与红色 badge 语义相反）；`Task.speed`/`outTimeMs` 后端已下发但列表未用；每次 WS notify（约 1 次/秒）对**每一张卡**执行 `appendChild` 重排 + `updateOverview` 整体 `replaceChildren`——可零成本消掉的 churn

## 方向清单（5 条；方向 4 的 transform 化即 1.6.4 批次 B 的核心）

1. **错误信息独立成行 + 截断可恢复**：`errLine` 加 `truncate` + `title={t.errorMsg}`，`fileName` 加 `title`；`updateCardDynamic` 已有 errorMsg 分支，双写保持一致（照抄现有模式）
2. **选中卡片高亮**：`card.classList.toggle('task-selected', cb.checked)`，CSS 用 `inset 0 0 0 1px rgb(var(--c-primary)/0.55), inset 3px 0 0`；仅勾选瞬间一次 repaint，不随 WS 刷新触发
3. **加载骨架 + 刷新 pending + 批量按钮禁用**：`loaded` 标志首次 refresh 成功后置 true；`skeletonRows(5, '3.5rem')`；`refresh.disabled` 期间置位；批量三键 `selected.size===0` 禁用、全选键 `tasks.length===0` 禁用
4. **进度条 transform 化 + ERROR 语义色 + 速率展示**（性能核心）：`fill` 从 `style.width` 改为 `transform: scaleX(var(--p))`（`transform-origin: left center`）——WS 每秒推送路径上唯一直接改善"轮询刷新"体感的项，宽度动画每次都触发 layout，transform 走合成器；`getPhaseStyle` 增加 `case 'ERROR': bg-danger`；TRANSCODING 且 `t.speed` 非空时进度文案追加 `· ${t.speed}`
5. **（低优先）概览条 chip 化 + churn 守卫**：`updateOverview` 裸文本 join 改 `status-chip` 样式 + 小色点；重排循环加 order 签名守卫（仅顺序真变化时重排）、`updateOverview` 仅 counts 变化时 `replaceChildren`

## 不推荐清单（8 项）

毛玻璃、进度条条纹动画上列表（与 WS 1Hz 叠加视觉抖动）、大投影/多层 box-shadow、hover `transform: scale` 卡片动效、虚拟滚动/引入 list 依赖（tasks 量级 <100，cardMap 增量渲染已覆盖）、CDN 图标字体、sticky 卡片头、骨架 shimmer 常驻化。

## 批次与验收（本稿最完整）

- 批次 A（半天完成、一次验收）：方向 1+2+3 + badge 状态点——全部局部 DOM/CSS、零新依赖、零新色（除既有 token `bg-danger`）
- 批次 B：方向 4 三件套 + 方向 5 守卫——transform 是唯一动到"每 tick 热路径"的改动，单独一批便于用 WS 真实推送回归滚动流畅度
- 批次 C（可选，跨页泛化）：骨架模式提取为 `ui.ts` 的 `listSkeleton()`（servers/history 复用）、概览 chip 化、`formatTime` 相对时间
- 验收基线：`npm run build`（含 check:design:diff + tsc）+ `check-design.mjs --all`，新增类不得触发 R1~R5；回归重点——WS tick 下滚动位置不跳（勿改全量重建触发条件）、暗色选中/错误行对比度 ≥4.5:1、320px 窄屏点击热区 ≥44px
