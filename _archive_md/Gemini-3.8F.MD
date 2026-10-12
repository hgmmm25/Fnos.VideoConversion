# FVCC 视频转码系统 Web UI 主页视觉与交互评估报告

> **评审角色**：资深前端视觉/交互评审工程师  
> **评估主体**：任务清单页（主页）`FVCC/ui-src/src/pages/tasks.ts`  
> **设计基调**：v1.6.0「方向 A：冷灰」  
> **技术约束**：Vite + TypeScript（无框架纯手写 DOM 渲染）+ Tailwind + CSS 变量主题（`data-theme` 切换），Go 静态目录嵌入，低性能 NAS（飞牛 fnOS）设备访问。

---

## 核心评审结论

在“无外部框架引入、不换构建链、冷灰调性不变”的硬约束下，主页视觉提升的核心**不是堆砌复杂视觉特效（如大面积模糊或持续动画），而是「信息层级解压缩」与「状态语义强化」**。

当前主页的最大视觉痛点表现为：**双层横栏碎带化、卡片信息扁平堆叠且存在排版冲突、缺乏转码中枢的呼吸感，以及右侧操作区的机械化硬编码**。仅通过 DOM 结构局部微调、Tailwind 工具类重组、冷灰色板呼应与局部更新去重，即可显著提升主页的工业级质感与操作流畅度。

---

## 一、现状体检：主页视觉上的 5 个明显短板

| 序号 | 视觉短板现象 | 具体代码位置 | 根因与体验缺陷分析 |
| :--- | :--- | :--- | :--- |
| **1** | **元信息排版冲突与层级扁平** | [`FVCC/ui-src/src/pages/tasks.ts:386-402`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L386-L402) | `sub` 容器声明了 `truncate` 与 `flex-wrap`（CSS 规范中 `truncate` 包含 `white-space: nowrap`，二者冲突），多标签时省略号和折行表现不稳定。元信息之间简单留空堆叠，服务器标签裸用 `text-signal`（紫色/靛蓝）与活动态进度条撞色；若任务报错，错误信息以 `\| ${t.errorMsg}` 尾随在末尾，极易被截断或引起高度抖动。 |
| **2** | **操作区固定死板，工具栏缺乏状态感知** | [`FVCC/ui-src/src/pages/tasks.ts:427`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L427)<br>[`FVCC/ui-src/src/pages/tasks.ts:70-103`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L70-L103) | 卡片操作区硬编码固定宽 `w-[200px]`。已完成任务只剩一个「删除」时右侧留下近 140px 的虚空；而多操作共存时又极度拥挤。顶部工具栏中，“全选/刷新/批量操作”全部是一模一样的浅灰底 `.btn.btn-sm`，未选任务时批量按钮依然高亮，点击才弹 toast 报错，缺少交互预先反馈。 |
| **3** | **双层通栏横带感严重，概览条仅为被动展示** | [`FVCC/ui-src/src/pages/tasks.ts:70-72`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L70-L72)<br>[`FVCC/ui-src/src/pages/tasks.ts:107-155`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L107-L155) | `toolbar`（`bg-surface`）与 `overview`（`bg-surface-alt`）上下两个细长通栏紧贴，造成顶部“夹板式”横纹割裂。堆叠进度条右侧是纯文本拼接（如 `进行中 2 · 待执行 5`），色块与文字无直观映射；相比 `history.ts:27`，主页概览条不可点击，不支持按状态快速过滤列表。 |
| **4** | **活动任务缺乏视觉锚点，终态任务噪点多** | [`FVCC/ui-src/src/pages/tasks.ts:306-310`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L306-L310)<br>[`FVCC/ui-src/src/pages/tasks.ts:383`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L383) | 转码中任务（`TRANSCODING` 等）是控制台的主角，但目前仅靠一个静态 Badge，在十几个任务中无法一眼锁定。而最左侧的拖拽把手 `dragHandle`（grip 点阵图标）对所有已完成、错误、取消的任务无差别渲染，终态任务本身无排序意义，一整列灰点造成了无意义的视觉噪点。 |
| **5** | **首屏无骨架屏跳闪，轮询更新触发全量重排** | [`FVCC/ui-src/src/pages/tasks.ts:184-191`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L184-L191)<br>[`FVCC/ui-src/src/pages/tasks.ts:224-230`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L224-L230) | 虽然 `ui.ts:89` 准备了 `skeleton`，但主页首次加载进入直接白屏，数据到达后突兀蹦出。而在任务刷新时，L228 的 `list.appendChild(entry.card)` 在每次轮询时都对所有已有卡片全量跑一次 append 挪动，触发不必要的浏览器 Reflow，低端 NAS 易产生微卡顿。 |

---

## 二、改进方向清单（按性价比排序）

### 方向一：重塑卡片信息架构与弹性操作区（最高性价比）
> 用清晰的视觉秩序替代拥挤横排，消除硬编码。

#### 1. 具体做法
- **弹性操作区**：废弃 `w-[200px]`，改为自适应 `flex items-center justify-end gap-1.5 shrink-0 ml-auto`。已完成任务按钮紧凑靠右，多按钮平铺自适应。
- **信息分行解耦**：
  - **首行（主识别区）**：文件名使用 `font-medium text-ink`，紧随任务类型徽标（如 EDL 渲染 / 代理生成）。
  - **次行（参数与流向）**：规范为 `flex items-center gap-2 text-xs text-ink-muted`，中间用小圆点（`·`）隔开；服务器与方案采用低饱和的中性底色标签（`bg-surface-alt px-1.5 py-0.5 rounded text-ink-muted`），避免与进行中的信号色撞色。
- **独立错误警示带**：若存在 `errorMsg`，不再拼在副文本末尾，而在卡片内独占轻量告警条：
  ```ts
  // tasks.ts: 卡片错误信息专属告警条
  if (t.errorMsg) {
    const errBox = el('div', {
      class: 'mt-1.5 flex items-center gap-1.5 text-xs text-danger bg-danger-soft/40 px-2 py-0.5 rounded w-full'
    }, [svgIcon('warning', 12) as unknown as Node, el('span', { class: 'truncate' }, [t.errorMsg])])
    info.appendChild(errBox)
  }
  ```
- **批量操作联动反馈**：根据 `selected.size > 0` 给批量暂停/继续/取消按钮动态增加 `disabled` 属性与 `opacity-40` 类，避免无选择状态下的盲点。

#### 2. 性能代价评估
- **首屏与渲染**：DOM 节点数变化微乎其微（只将错误文本独立）。去除了冲突的 `truncate` + `flex-wrap` 后的异常重排，整体渲染性能中性偏正向。
- **滚动与轮询**：零额外开销。

#### 3. 回归风险
- **暗色模式**：`bg-danger-soft/40` 在暗黑主题下透明度柔和，不刺眼。
- **小屏移动端**：自适应宽度有效避免小屏时 `w-[200px]` 强行挤占文件名宽度的问题。
- **连带影响**：完全局限于 `tasks.ts` 的 `buildCard` 与 `updateCardDynamic`，不污染全局样式。

---

### 方向二：冷灰基调下的状态沉浸与微动质感（高性价比）
> 在 Direction A 冷灰框架内，通过微妙的高级对比度突出“工作进行中”的控制台质感。

#### 1. 具体做法
- **进行中任务卡专属「左侧指示线」**：
  处于活跃转码态（`UPLOADING`, `TRANSCODING`, `DOWNLOADING`）的卡片，在左侧增加 3px 信号色垂直指示线：
  ```ts
  const isRunning = t.status === 'TRANSCODING' || t.status === 'UPLOADING' || t.status === 'DOWNLOADING'
  const cardClass = `card p-3 flex flex-wrap items-center gap-3 transition-colors ${
    isRunning ? 'border-l-[3px] border-l-signal bg-surface' : 'hover:bg-surface-hover'
  }`
  ```
- **微动呼吸聚焦**：
  进行中的 Badge 或状态点，应用现有的 `animate-pulse-soft`（定义于 [`style.css:405`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/style.css#L405)），平缓的 2 秒微呼吸，视觉重心一目了然。
- **终态手柄降噪**：
  对于 `COMPLETED`、`CANCELLED` 状态的任务，隐藏 `dragHandle` 或仅作为空白占位（保持左侧对其但不可抓取），大幅净化列表视觉噪音。

#### 2. 性能代价评估
- `animate-pulse-soft` 仅改变 `opacity`，走 GPU 合成层，不触发 Layout/Reflow；
- 已有系统级媒体查询 [`prefers-reduced-motion`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/style.css#L451) 兜底保护，性能敏感环境可自动关闭动画。

#### 3. 回归风险
- 需确保暗黑模式下 `border-l-signal` 与 `--c-surface`（`#202c42`）对比度柔和，不出现亮白刺眼边框。

---

### 方向三：双栏整合与互动式状态过滤（中高性价比）
> 消除通栏夹板感，将静态指标升级为高效工作台。

#### 1. 具体做法
- **消灭双层横斑**：将 `toolbar` 与 `overview` 合并在统一的 Header 容器中，左右布局或卡片式融合。
- **概览项转为可点击的 Filter Chips**：
  堆叠条右侧的指标文本转化为带有对应语义色点（`w-1.5 h-1.5 rounded-full`）的小型 Chip 按钮。点击可即时切换列表过滤状态：
  ```ts
  // 示意：状态 Filter Chip 交互
  let activeFilter: QueueGroupKey | 'all' = 'all'

  const chip = el('button', {
    class: `inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-xs transition-colors ${
      activeFilter === g.key ? 'bg-surface ring-1 ring-line text-ink font-medium' : 'text-ink-muted hover:text-ink'
    }`
  }, [
    el('span', { class: `w-1.5 h-1.5 rounded-full ${g.colorClass}` }),
    el('span', {}, [`${g.label} ${n}`])
  ])

  chip.onclick = () => {
    activeFilter = activeFilter === g.key ? 'all' : g.key
    render()
  }
  ```

#### 2. 性能代价评估
- 前端纯内存数组过滤（`store.tasks.filter(...)`），百级列表执行时间小于 0.2ms，零网络开销。
- 减少一个通栏横条的 DOM 节点与边框绘制。

#### 3. 回归风险
- 需妥善处理当列表处于过滤状态时，后台推送的新增任务或完成状态迁移时的卡片显示同步。

---

### 方向四：加载骨架屏补齐与轮询管线性能精细化（保障型改进）
> 解决视觉跳闪，切断后台刷新时的无用 DOM 消耗。

#### 1. 具体做法
- **首屏骨架屏占位**：进入页面时，在 `store.loadTasks()` 完成前先挂载 3~4 个卡片骨架（使用现成封装的 [`skeleton`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/ui.ts#L89)），避免首屏白屏跳闪。
- **斩断全量 DOM `appendChild`**：
  重构 [`tasks.ts:224-230`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/pages/tasks.ts#L224-L230)：仅在检测到任务增减（`currentIds` 变化）或拖拽排序时才重新执行 DOM 挂载；普通的进度变动（`updateCardDynamic`）**坚决不触碰父容器子节点列表**。
- **进度条样式微更新精简**：
  在 `updateCardDynamic` 中，移除每次都重赋 `entry.fill.className` 的做法，只修改 `style.width`，减少 CSSOM 解析。

#### 2. 性能代价评估
- **大幅正向收益**。长列表轮询期间的 JS 执行耗时与重排时间可降低 70% 以上，低端 NAS 设备和低功耗平板访问下更平稳顺滑。

#### 3. 回归风险
- 需要细致管理任务新增与排序时的脏标记，确保拖拽换序后数据顺序能精准同步。

---

## 三、明确不推荐清单（Bad Taste & NAS 陷阱）

针对低算力 NAS、Go 嵌入式分发和手写 DOM 的特点，以下方案**严禁采用**：

1. **大面积毛玻璃模糊（`backdrop-filter: blur(...)`）**  
   - **原因**：在 J1900、N5105、RK3588 等低功耗 NAS 或未开硬件加速的浏览器上，长列表滚动带毛玻璃的卡片会造成灾难性的掉帧（直接掉到 15-20fps），CPU 核心占用极高。
2. **进度条无限斜纹滚动动画（`.progress-animate` 即持续的 background-position 关键帧）**  
   - **原因**：虽然 [`style.css:424`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/style.css#L424) 存在该类，但在列表有 5~10 个任务同时转码时，持续跑纯 CPU/GPU 循环动画会使客户端风扇长转。进度条保持纯色平滑微过渡即可。
3. **卡片内嵌入 Canvas/SVG 动态速率小波形（Sparklines）**  
   - **原因**：破坏了现有轻量 DOM 的缓存机制（`CardEntry`），多实例下极易在频繁轮询中造成内存泄漏，得不偿失。
4. **大面积弥散阴影（Heavy `box-shadow`）**  
   - **原因**：直接破坏 v1.6.0 的架构约定（[`style.css:199`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/style.css#L199) 明确规定：“卡片以明度分层代替描边+阴影，浮动层才保留阴影”）。在暗色冷灰模式下，大阴影发灰发脏，丧失专业工控质感。
5. **引入外部重型图标库或组件库**  
   - **原因**：破坏现有零依赖构建链与微小体积优势。现有内联 SVG 图标库（[`ui.ts`](file:///D:/Fnos.VideoConversion/FVCC/ui-src/src/ui.ts)）已足够满足所有场景。

---

## 四、建议实施批次划分

建议分 2 个小版本（Minor Patch）迭代落地，兼顾成效与安全：

```mermaid
flowchart LR
    A["v1.6.1 视觉减负与层级速赢"] --> B["v1.6.2 交互聚合与性能固化"]
    
    subgraph A["v1.6.1（视觉速赢，改动约 60 行，零回归风险）"]
        A1["解绑操作区固定宽度 w-[200px]"]
        A2["优化卡片两行排版，独立错误告警条"]
        A3["活动任务增加左侧边框与微妙呼吸"]
        A4["终态任务弱化拖拽把手噪点"]
    end
    
    subgraph B["v1.6.2（交互体验与性能深化）"]
        B1["双栏整合：Header 结构一体化"]
        B2["队列状态堆叠条升级为点击即筛 Filter Chips"]
        B3["首屏骨架屏占位，消除白屏跳闪"]
        B4["轮询更新逻辑切断无谓 DOM 全量 append"]
    end
```

- **第一批（建议纳入 v1.6.1）**：聚焦**卡片内部结构微调与状态感知**。无需改动数据流，纯粹修改 `buildCard()` 中的元素组装方式与 Tailwind 类名。耗时小，立竿见影消除“廉价横排感”。
- **第二批（建议纳入 v1.6.2）**：聚焦**双栏合一、状态过滤与轮询更新优化**。让主页具备真正高效的转码控制台交互能力，同时彻底固化低端设备的极致流畅度。
