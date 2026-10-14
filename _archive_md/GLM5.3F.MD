# FVCC 主页（任务页）视觉改进方向评审 — GLM5.3F 摘要

> 原始评审稿（约 9.4KB）已于 2026-10-14 压缩为本摘要；完整内容曾整合进 `docs/FVCC_任务页视觉改进整合方案_v1.6.1.md`（随 v1.6.4 批次 A/B/C 交付）。评审主体：`ui-src/src/pages/tasks.ts`；前提：零框架、保持 v1.6.0「方向 A：冷灰」token 体系。

## 核心判断

v1.6.0 的骨架（token 体系、明度分层、diff 渲染）是健康的，剩余短板全在**信息层级和加载/状态反馈**层，用纯类名 + 已有组件即可收敛，不需要任何框架或依赖。

## 体检（5 短板）

1. 次级信息行过载无层级（`sub` 行 6 类碎片等权重灰字）
2. 错误信息与普通元数据同待遇（`errorMsg` 拼在 `sub` 末尾且被 `truncate` 截断）
3. 进行中任务视觉权重不足（`sm:w-40` 细进度条，与排队卡轮廓无差别；`getPhaseStyle` 对 PAUSED/CANCELLED/ERROR/QUEUE 返回空 label）
4. 首屏加载态空白（骨架组件存在但本页未用）
5. 工具栏常驻噪音（批量三键零选中时与普通按钮同等权重；`stats` 与概览条计数重复）

附注功能债：`scrollPos.ts` 存在但 tasks 页未接线，滚动位置靠 diff 更新间接保住——任何"整表重建"式视觉改动都会击穿。

## 方向清单（按性价比，共 6 条）

1. ⭐ 卡片信息层级重组 + `errorMsg` 独立成行（性能代价零）
2. ⭐ 首屏骨架屏（复用 `.skeleton`，一次性 `replaceChildren`，不进轮询路径）
3. 活动态静态锚点：**做法 A（推荐，零动画）** 左侧 2px 信号色竖条 + ring 微增；做法 B 条纹动画为实验项（`background-position` 不走合成层，N 卡 N 个持续 repaint，低性能 NAS 会掉帧，默认推 A）
4. 工具栏状态衰减（批量三键 `selected.size===0` 时 disabled；`stats` 并入概览条）
5. 卡片 hover 层次统一 + 密度微调（对齐 servers 页 `hover:ring-2`；注意 `space-y` 调整需回归拖拽落点线）
6. 泛化项：`getPhaseStyle` 补全状态标签（QUEUE/PAUSED/COOLDOWN，一处改两处受益）

## 不推荐清单（7 项）

backdrop-filter 毛玻璃（低性能 NAS 掉帧源）、持续 `animate-pulse-soft` 状态徽标（N 卡 N 路动画，只可用于单点）、大阴影/多层 box-shadow 卡片（回退 P1-2 明度分层）、FLIP/重排序动画（轮询高频收益小、回归风险高）、引入任何前端依赖、空态大插画（NAS 离线场景进包代价 > 收益）、桌面端卡片双栏 grid（挤压 actions 区）。

## 批次建议

- 批次 1（一个小版本见效）：方向 2+4+5+6（全低风险增量，不碰 `updateCardDynamic` 热路径）
- 批次 2：方向 1（集中改 `buildCard/updateCardDynamic`，重点回归轮询增量、暗色错误行、小屏 flex-wrap、拖拽排序）
- 批次 3（实验性）：方向 3B 条纹动画（限定张数实测后决定）+ 顺手把 `saveScrollPos/restoreScrollPos` 补接到 `listWrap`
