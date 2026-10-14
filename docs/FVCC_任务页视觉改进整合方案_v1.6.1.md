# FVCC 主页（任务页）视觉改进整合方案

> 版本定位：v1.6.1 / v1.6.2（v1.6.0「方向 A：冷灰」基调的延续打磨）
> 评估主体：`FVCC/ui-src/src/pages/tasks.ts`（582 行）
> 编写日期：2026-10-12
> 结论已逐条对照实际代码核实（行号以当前 baseline-snapshot 分支为准）

---

## 一、背景与素材来源

v1.6.0 视觉收口后，为确定主页下一步改进方向，委托 3 个外部模型（CLI，同仓库读码）各出一份独立评审。三份原始报告已归档：

- `_archive_md/GLM5.3F.MD` — 约束把关最稳，性能判断最严谨（区分合成层/repaint），独有发现 `scrollPos.ts` 未接线
- `_archive_md/Step5P.MD` — 唯一抓到热路径性能问题（width 动画、每 tick 全量 DOM churn），验收基线最完整
- `_archive_md/Gemini-3.8F.MD` — 视觉诊断最细（`truncate`+`flex-wrap` 类冲突、serverName 撞色），但夹带两个功能提案

三方「不推荐清单」高度收敛，且均正确引用 `style.css:199` P1-2 明度分层决策与 `ui-src/scripts/check-design.mjs` R1–R5 门禁。本方案为三方的整合 + 代码核实裁决。

## 二、代码核实结果

### 2.1 三方共识、核实为真（必做项）

| 缺陷 | 证据位置 |
|---|---|
| 错误信息挂在 `sub` 行尾被 `truncate` 裁掉（ERROR 态最关键信息不可读） | `tasks.ts:388`（sub 带 truncate）、`tasks.ts:401`（errorMsg 最后 append） |
| `sub` 同时声明 `truncate`（含 nowrap）与 `flex-wrap`，CSS 层面自相矛盾 | `tasks.ts:388` |
| 首屏无骨架：`skeleton/skeletonRows` 已存在且 scanner/settings/editor 在用，唯独 tasks 页未接 | `ui.ts:90-101`；`tasks.ts:234-243` |
| 批量三键零选中时不禁用，点击仅弹 toast | `tasks.ts:93-103`、`tasks.ts:559-562` |
| 活动任务缺视觉锚点，与排队任务卡片轮廓几乎无差别 | `tasks.ts:405`（进度区仅 `sm:w-40`） |
| `getPhaseStyle` 对 QUEUE/PAUSED/CANCELLED/ERROR/COOLDOWN 返回空标签，ERROR 进度条画成灰色（与红色徽标语义冲突） | `tasks.ts:294-297` |
| 操作区硬编码 `w-[200px]`，终态单按钮时右侧大片虚空 | `tasks.ts:427` |

### 2.2 独有高价值发现（核实为真）

- **进度条 width 动画走 layout**（Step5P）：`updateCardDynamic` 每次 WS 推送写 `fill.style.width` + `transition-all`，触发 layout+repaint。`tasks.ts:256`、`tasks.ts:420`
- **每次 notify 全量 DOM churn**（Step5P/Gemini）：`store.ts:152-166` 每条 `task_update` 都 `notify()` → `render()` 对全部卡片跑一遍 `appendChild` 重排（`tasks.ts:225-230`）+ `updateOverview` 整体 `replaceChildren`（`tasks.ts:150`）
- **选中态卡片零反馈**（Step5P）：`cb.onchange` 只改 Set，卡面外观不变（`tasks.ts:377-380`），批量操作无法确认选择范围
- **`scrollPos.ts` 已封装未接线**（GLM）：全仓 grep 无引用；滚动位置目前靠 render 不重建 `listWrap` 间接保住
- **serverName 用 `text-signal` 与进行中进度条撞色**（Gemini）：`tasks.ts:390`

### 2.3 需打折 / 修正的论断

| 原论断 | 出处 | 核实结论 |
|---|---|---|
| 「隐藏终态任务拖拽把手」 | Gemini | **有功能回归**：终态任务当前可拖拽排序且整表顺序持久化（`handleReorder` → `api.reorderTasks`）。至多弱化为 hover 显形，不可移除 |
| 「概览条升级可点击 Filter Chips」 | Gemini | 属**新功能**（状态过滤），涉及过滤态下 WS 推送同步语义，超出视觉改进范畴 → 转产品决策（§五批次 C） |
| 「进度文本追加 speed 速率」 | Step5P | `store.ts` `applyTaskUpdate` 只合并 status/progress/errorMsg，**不含 `speed`**（`types.ts:57`）；WS 路径速率不实时。实现前需确认列表接口是否下发，展示前 trim、空串跳过 |
| 「活动徽标加 `animate-pulse-soft`」 | Gemini | 违反硬约束 3（禁持续动画）。虽走合成层且 `prefers-reduced-motion` 有兜底（`style.css:451`），N 张活动卡 = N 个常驻合成层 → 降为实验项（§五批次 C） |
| 「`.task-selected` 用 inset box-shadow」 | Step5P | 不触发 R3（仅选中态，非默认态），可用 |

## 三、冲突裁决

- **活动任务强调方式**（GLM 静态竖条 vs Gemini 竖条+呼吸）：采 GLM——静态左侧 2–3px 信号色指示线进批次 1；呼吸动画实测后再定。
- **条纹进度动画 `.progress-animate`**：三方一致不推荐默认启用（`background-position` 逐帧 repaint 不走合成层），仅保留实测选项。
- **hover 效果**：采 servers 页既有 `hover:ring-2 hover:ring-primary/30` 统一模式（GLM），不引入 transform scale（三份一致反对）。

## 四、硬约束（沿用 v1.6.0，不变更）

1. 保持无框架 `renderXxx + cleanup` 手写渲染与 hash 路由，不引依赖。
2. 保持冷灰基调与 `--c-*` token 体系，所有新样式必须走 token。
3. 不新增：`backdrop-filter`/blur、常驻无限动画、默认态阴影、CDN 图标库。
4. 暗色模式与亮色同步验证；320px 窄屏 `btn-sm` ≥44px 热区规则（`style.css:187-193`）不破。
5. 视觉改进不得增加 WS tick 重绘频率、不得打断滚动位置保持。

## 五、实施批次

### 批次 A → v1.6.1「层级修复 + 状态反馈」（纯类名/局部 DOM，不动热路径）

1. **errorMsg 独立成行**：从 `sub` 摘出为 `info` 内独立块（含 `title`），`buildCard` 与 `updateCardDynamic` 两处同步；顺带 `name` 补 `title`，拆除 `sub` 的 `truncate`+`flex-wrap` 冲突（改 `flex-wrap` + 子项各自 truncate，或去 truncate 保留 nowrap，验收时明暗两态各截图比对）。
2. **首屏骨架**：`loaded` 标志 + `skeletonRows(5)`（复用 `ui.ts` 现成组件），仅首屏一次，不进轮询路径，与 `emptyState` 互斥。
3. **工具栏状态衰减**：批量三键 `selected.size===0` 禁用（render 内统一 set，`.btn` disabled 态样式已具备）；刷新按钮 pending 禁用；`selectAllBtn` 空列表禁用。
4. **选中卡片高亮**：`.task-selected`（inset ring + 左侧 3px primary 内阴影），`cb.onchange` 与 `buildCard` 初始化两处按 `selected` Set 挂类；不进 `updateCardDynamic`（不随 WS 触发）。
5. **活动卡静态指示线**：UPLOADING/TRANSCODING/DOWNLOADING 卡片 `border-l-[3px] border-l-signal`。
6. **`getPhaseStyle` 补全**：`QUEUE:'排队'`、`PAUSED:'已暂停'`、`COOLDOWN:'冷却'`、`CANCELLED:'已取消'` 文案；`ERROR → bg-danger`（一处改，主页与任务抽屉同受益）。
7. **操作区弹性化**：`w-[200px]` → `ml-auto shrink-0`；小屏 `flex-wrap` 回归一次。
8. **serverName 去撞色**：`text-signal` → 中性低饱和标签（`bg-surface-alt px-1.5 py-0.5 rounded text-xs text-ink-muted`）。
9. **密度与 hover 统一**：列表 `space-y-2` → `space-y-2.5`；卡片补 `hover:ring-2 hover:ring-primary/30` 对齐 servers 页。

### 批次 B → v1.6.2「热路径性能」（单独成批，便于 WS 真实推送回归）

1. **进度条 transform 化**：`fill` 改 `transform: scaleX(var(--p))` + `transform-origin:left`，TS 侧 `setProperty('--p', …)` 替换 `style.width`，删 `transition-all`；回归 0%/100% 两端与 aria 值。
2. **重排守卫**：`render()` 记住上次 order 签名，仅在 ids/顺序变化时执行 `appendChild` 重排循环。
3. **`updateOverview` 守卫**：counts 未变化时不 `replaceChildren`。
4. **scrollPos 接线**：将 `saveScrollPos/restoreScrollPos` 补接到 `listWrap`（本页现存的未接线封装）。

### 批次 C → 实验 / 产品决策（不进默认排期）

- 条纹进度动画（`.progress-animate`）与活动徽标呼吸（`animate-pulse-soft`）：限每屏 ≤3 张、真实多任务并发实测后裁决。
- 概览条 Filter Chips 状态过滤：**功能提案，待产品拍板**。
- 速率展示（`t.speed`）：待确认 WS/接口下发链路后并入批次 B。
- 跨页泛化：`listSkeleton()` 提取、`formatTime` 相对时间。

## 六、验收基线（沿用 v1.6.0 门禁）

- `npm run build`（含 `check:design:diff` + `tsc`）+ `npm run check:design`（R1 hex / R2 原色板 / R3 默认态阴影 / R4 <12px / R5 max-height 动画 全绿）。
- 明暗双主题主页截图逐批对比；暗色选中/错误行对比度 ≥4.5:1。
- 功能回归重点：
  - WS tick 下滚动位置不跳（批次 B 前勿改全量重建触发条件）；
  - 拖拽排序落点线（`border-t-drop/border-drop-line`）与新样式不冲突；
  - 320px 窄屏卡片换行后按钮热区 ≥44px；
  - ERROR 卡错误行完整可读、QUEUE/PAUSED 进度文案非裸百分号。

## 七、整体判断

v1.6.0 骨架（token 体系、明度分层、cardMap 增量渲染）健康，三份独立评审一致确认剩余短板集中在**信息层级、错误呈现、加载/选中反馈**与**每 tick DOM churn**，全部可在现有框架内以纯类名 + 现成组件收敛。批次 A 半天量级、零热路径风险；批次 B 是全清单唯一直接改善轮询体感的改动，收益最实。
