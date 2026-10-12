import { store } from '../store'
import { api } from '../api'
import { el, toast, formatTime, emptyState, svgIcon, showBatchResult, listSkeleton } from '../ui'
import { type Task, type TaskStatus, STATUS_LABEL, STATUS_CLASS } from '../types'
import { crudActions } from '../lib/crudActions'
import { useListPage } from '../lib/useListPage'
import { saveScrollPos, restoreScrollPos } from '../lib/scrollPos'
// C-09：渲染/代理任务的阶段文案、进度文案、成品回看统一由 taskView 派生（06 §2.2 / §6）
import {
  isProxyTask,
  isRenderTask,
  stageText,
  progressLabel,
  elapsedText,
  outputPreviewPath,
  previewOutput,
  copyOutputPath,
} from './editor/taskView'

const selected = new Set<string>()

// v1.6.2-C2：批次 C 实验动画 URL 开关（默认全关，硬约束 3 不破）——
// 形如 #/tasks?fx=stripes,breathe：条纹进度动画 / 活动徽标呼吸，限真实并发实测用
const FX_PARAMS = (() => {
  const m = location.hash.match(/\?fx=([\w,]+)/)
  return new Set(m ? m[1].split(',') : [])
})()
const FX_STRIPES = FX_PARAMS.has('stripes')
const FX_BREATHE = FX_PARAMS.has('breathe')

function isActivityStatus(s: TaskStatus): boolean {
  return s === 'UPLOADING' || s === 'TRANSCODING' || s === 'DOWNLOADING'
}

/** 倍速后缀文案：原样展示 speed（trim），空串/空白跳过（列表接口与 WS 均可能下发） */
function speedText(t: Task): string {
  const s = (t.speed ?? '').trim()
  return s ? ` · ${s}` : ''
}

let dragSrcId: string | null = null

interface CardEntry {
  status: TaskStatus
  card: HTMLElement
  cb: HTMLInputElement
  badge: HTMLElement
  progText: HTMLElement
  progLabel: HTMLElement
  speedLabel: HTMLElement
  fill: HTMLElement
  bar: HTMLElement
  errLine: HTMLElement
  dragHandle: HTMLElement
}

// B-阶段（7.3 tasks 行）：队列状态可视化 —— 状态分组与对应 token 色。
// 分组与 tasks.ts getPhaseStyle / types.ts STATUS_CLASS 的语义一致：
// 进行中=signal、待执行=neutral-soft、完成=success、错误=danger、暂停/取消=neutral、冷却=warning。
type QueueGroupKey = 'active' | 'waiting' | 'completed' | 'error' | 'paused' | 'cooldown'
const QUEUE_GROUPS: { key: QueueGroupKey; label: string; colorClass: string; match: (s: TaskStatus) => boolean }[] = [
  {
    key: 'active', label: '进行中', colorClass: 'bg-signal',
    match: (s) => s === 'UPLOADING' || s === 'TRANSCODING' || s === 'DOWNLOADING',
  },
  {
    key: 'waiting', label: '待执行', colorClass: 'bg-neutral-soft',
    match: (s) => s === 'QUEUE' || s === 'WAITING_TRANS' || s === 'WAITING_DOWN',
  },
  {
    key: 'completed', label: '完成', colorClass: 'bg-success',
    match: (s) => s === 'COMPLETED',
  },
  {
    key: 'error', label: '错误', colorClass: 'bg-danger',
    match: (s) => s === 'ERROR',
  },
  {
    key: 'paused', label: '暂停/取消', colorClass: 'bg-neutral',
    match: (s) => s === 'PAUSED' || s === 'CANCELLED',
  },
  {
    key: 'cooldown', label: '冷却', colorClass: 'bg-warning',
    match: (s) => s === 'COOLDOWN',
  },
]

export function renderTasks(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full' })

  // 工具栏
  const toolbar = el('div', {
    class: 'flex flex-wrap items-center gap-2 px-3 sm:px-4 py-2 border-b border-line bg-surface',
  })
  const refresh = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  refresh.append(svgIcon('refresh', 14) as unknown as Node, el('span', {}, ['刷新']))
  // 2026-09-19 修复：此前「刷新」按钮创建后从未绑定点击事件，点击无任何反应。
  // 点击后重新拉取任务列表，store 层已做数据去重：有变化才 notify 并触发渲染，无变化零开销。
  refresh.onclick = () => {
    refresh.disabled = true
    void store.loadTasks().finally(() => {
      refresh.disabled = false
    })
  }

  const selectAllBtn = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [el('span', {}, ['全选'])])
  selectAllBtn.onclick = (e) => {
    e.stopPropagation()
    const isAllSelected = store.tasks.length > 0 && store.tasks.every(t => selected.has(t.id))
    if (isAllSelected) {
      selected.clear()
    } else {
      store.tasks.forEach(t => selected.add(t.id))
    }
    render(true)
  }

  const batchPause = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  batchPause.append(svgIcon('pause', 14) as unknown as Node, el('span', {}, ['暂停']))
  const batchResume = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  batchResume.append(svgIcon('play', 14) as unknown as Node, el('span', {}, ['继续']))
  const batchCancel = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  batchCancel.append(svgIcon('x', 14) as unknown as Node, el('span', {}, ['取消']))
  batchPause.onclick = () => batchOp('pause')
  batchResume.onclick = () => batchOp('resume')
  batchCancel.onclick = () => batchOp('cancel')
  const stats = el('span', { class: 'ml-auto shrink-0 pl-2 text-xs text-ink-muted tabular-nums' }, [])
  // v1.6.1-A3：工具栏状态衰减 —— 批量三键零选中禁用、全选空列表禁用；
  // 勾选不触发整页 render，需由 syncToolbar 就地同步（render 内同样调用）。
  const syncToolbar = () => {
    const hasSelection = selected.size > 0
    batchPause.disabled = !hasSelection
    batchResume.disabled = !hasSelection
    batchCancel.disabled = !hasSelection
    selectAllBtn.disabled = store.tasks.length === 0
    stats.textContent = `共 ${store.tasks.length} 个任务，已选 ${selected.size} 个`
  }
  toolbar.append(selectAllBtn, refresh, batchPause, batchResume, batchCancel, stats)
  // 初始即零选中，批量键先置灰（首次 render 前也保持正确状态）
  syncToolbar()

  // B-阶段（7.3 tasks 行）：队列状态可视化 —— 状态占比堆叠条 + 计数摘要（自绘 CSS，零依赖）
  // V1.6-④ V5 修复：overview 容器补 flex（此前缺省导致占比条独占一行，视觉上像一条伪小节分隔线）
  const overview = el('div', { class: 'flex items-center gap-3 px-3 sm:px-4 py-2 border-b border-line bg-surface-alt hidden' })
  const listWrap = el('div', { class: 'flex-1 overflow-auto p-4' })
  wrap.append(toolbar, overview, listWrap)
  container.appendChild(wrap)

  // 卡片缓存：taskID -> 卡片元素及动态引用
  const cardMap = new Map<string, CardEntry>()
  // v1.6.1-A2：首屏骨架标志——首次 render（loadTasks 成功后）置真；仅首次拉取在途时显示骨架。
  // v1.6.3 修复：空列表不再依赖 tasks.length 判定，否则空数据库骨架常驻"出不来"
  let loaded = false
  // v1.6.2-B2：上次渲染的 id/顺序签名，未变化则跳过全量 appendChild 重排
  let lastOrderKey = ''
  // v1.6.2-B3：上次概览分组计数签名，未变化则跳过 replaceChildren
  let lastOverviewKey = ''
  // v1.6.2-B4：滚动位置按实例上下文保存/恢复（scrollPos 接线）——
  // 主页与任务抽屉（role=dialog 内惰性挂载）各用独立 key，避免两处列表互串
  const SCROLL_KEY = container.closest('[role="dialog"]') ? 'task-drawer' : 'tasks-list'

  // B-阶段（7.3 tasks 行）：队列状态概览 —— 状态占比堆叠条 + 计数摘要
  const updateOverview = () => {
    const tasks = store.tasks
    if (tasks.length === 0) {
      // v1.6.2-B3：空态签名固定 '0'，重复空态不再动概览 DOM
      if (lastOverviewKey === '0') return
      lastOverviewKey = '0'
      overview.classList.add('hidden')
      overview.replaceChildren()
      return
    }
    overview.classList.remove('hidden')

    const counts = new Map<QueueGroupKey, number>(QUEUE_GROUPS.map((g) => [g.key, 0]))
    for (const t of tasks) {
      const g = QUEUE_GROUPS.find((g) => g.match(t.status))
      if (g) counts.set(g.key, (counts.get(g.key) ?? 0) + 1)
    }
    const total = tasks.length

    // v1.6.2-B3：分组计数与总量均未变化时不重建概览 DOM
    const overviewKey = `${total}|${QUEUE_GROUPS.map((g) => counts.get(g.key)).join(',')}`
    if (overviewKey === lastOverviewKey) return
    lastOverviewKey = overviewKey

    const bar = el('div', {
      class: 'flex flex-1 min-w-[80px] max-w-[420px] h-2 rounded-full overflow-hidden bg-surface ring-1 ring-line-subtle',
      role: 'img',
    })
    const labels: string[] = []
    for (const g of QUEUE_GROUPS) {
      const n = counts.get(g.key) ?? 0
      if (n === 0) continue
      const seg = el('div', {
        class: `${g.colorClass} h-full transition-all`,
        style: `width:${(n / total) * 100}%`,
        title: `${g.label} ${n}`,
      })
      bar.appendChild(seg)
      labels.push(`${g.label} ${n}`)
    }
    bar.setAttribute('aria-label', labels.join('，'))

    overview.replaceChildren(
      el('span', { class: 'text-xs text-ink-muted shrink-0' }, ['队列']),
      bar,
      el('span', { class: 'text-xs text-ink-muted font-mono tabular-nums shrink-0' }, [labels.join(' · ')]),
    )
  }

  // full=true 时强制全量重建（用于全选/删除等结构性变化）
  const render = (full = false) => {
    const tasks = store.tasks
    // v1.6.2-B4：render 会重建/重排 listWrap 内容，先存后复，滚动位置不跳
    saveScrollPos(listWrap, SCROLL_KEY)
    // v1.6.3 修复：render 仅在 loadTasks 成功后到达，首次 render 即结束骨架态——
    // 空列表也要落到 emptyState（此前仅 tasks>0 才置真，空库骨架常驻出不来）
    loaded = true
    syncToolbar()
    updateOverview()
    const isAllSelected = tasks.length > 0 && tasks.every(t => selected.has(t.id))
    selectAllBtn.className = isAllSelected
      ? 'btn btn-sm flex items-center gap-1.5 chip-active'
      : 'btn btn-sm flex items-center gap-1.5'

    if (tasks.length === 0) {
      lastOrderKey = ''
      listWrap.innerHTML = ''
      cardMap.clear()
      if (!loaded) {
        // v1.6.1-A2：首屏数据未达先铺骨架（仅 render 到达前的窗口期），与 emptyState 互斥
        listWrap.appendChild(listSkeleton(5, '3.5rem'))
        restoreScrollPos(listWrap, SCROLL_KEY)
        return
      }
      // P2-1：空状态承载"下一步动作"；V1.6-④ V5：在剩余空间内垂直居中，不再贴顶
      listWrap.appendChild(
        el('div', { class: 'flex h-full min-h-[300px] w-full items-center justify-center' }, [
          emptyState('暂无任务，请到「视频文件」页创建转码任务', 'list', {
            label: '去视频文件页创建',
            onClick: () => {
              location.hash = '#/scanner'
            },
          }),
        ])
      )
      restoreScrollPos(listWrap, SCROLL_KEY)
      return
    }

    // 确保列表容器存在
    let list = listWrap.querySelector('[data-task-list]') as HTMLElement | null
    if (!list) {
      listWrap.innerHTML = ''
      cardMap.clear()
      list = el('div', { class: 'space-y-2.5', 'data-task-list': '' })
      listWrap.appendChild(list)
    }

    const currentIds = new Set(tasks.map(t => t.id))

    // 移除已不存在的任务卡片
    for (const [id, entry] of cardMap) {
      if (!currentIds.has(id) || full) {
        entry.card.remove()
        cardMap.delete(id)
      }
    }

    // 新建或更新卡片
    for (const t of tasks) {
      const existing = cardMap.get(t.id)
      if (existing && !full && existing.status === t.status) {
        // 状态未变：只更新进度和选中态，不重建按钮
        updateCardDynamic(existing, t)
      } else {
        // 状态变化或新任务：重建卡片
        const entry = buildCard(t, syncToolbar)
        if (existing) {
          existing.card.replaceWith(entry.card)
        } else {
          // V1.6-⑤：新任务卡首次入场淡入；动画结束即摘类，防止后续排序重挂时重放闪烁
          entry.card.classList.add('animate-fade-in-up')
          entry.card.addEventListener('animationend', () => entry.card.classList.remove('animate-fade-in-up'), { once: true })
          list.appendChild(entry.card)
        }
        cardMap.set(t.id, entry)
      }
    }

    // 按新顺序重排 DOM（拖拽排序后需要）
    // v1.6.2-B2：ids/顺序签名未变且卡数一致时跳过重排循环——WS tick 渲染从
    // N 次 appendChild（全量 detach/attach 重排）收敛为 0 次
    const orderKey = tasks.map(t => t.id).join('|')
    if (orderKey !== lastOrderKey || list.children.length !== tasks.length) {
      for (const t of tasks) {
        const entry = cardMap.get(t.id)
        if (entry) {
          list.appendChild(entry.card)
        }
      }
      lastOrderKey = orderKey
    }
    restoreScrollPos(listWrap, SCROLL_KEY)
  }

  // C-09：返回退订句柄（任务抽屉复用本列表，关闭抽屉不销毁，仅真正卸载时才退订，避免重复订阅）；P2-2：改走 useListPage 统一生命周期
  const page = useListPage({
    load: async () => {
      await store.loadTasks()
      return store.tasks
    },
    render: () => render(),
    subscribe: (cb) => store.subscribe(cb),
    errorLabel: '任务列表',
  })
  page.mount()
  return page.dispose
}

// 更新卡片中动态变化的部分（进度、选中态、错误信息），不触碰按钮
function updateCardDynamic(entry: CardEntry, t: Task) {
  entry.cb.checked = selected.has(t.id)

  // 更新进度文本和进度条
  const displayProgress = t.status === 'COMPLETED' ? 100 : Math.max(0, Math.min(100, t.progress))
  const { phaseColor, phaseLabel } = getPhaseStyle(t.status)
  // 渲染/代理任务优先展示阶段与段序号（如「分段渲染 2/4 · 63%」，06 §2.2）
  entry.progText.textContent = progressLabel(t, displayProgress, phaseLabel)
  // v1.6.2-B1：只写 --p（transform:scaleX 由 CSS 引用），不触碰 width、不重挂 transition
  entry.fill.style.setProperty('--p', String(displayProgress / 100))
  // v1.6.2-C1：倍速后缀独立 span，随 WS 合并的 speed 实时更新
  entry.progLabel.textContent = progressLabel(t, displayProgress, phaseLabel)
  entry.speedLabel.textContent = speedText(t)

  // v1.6.2：进度条颜色只在状态真正变化时重挂（同状态 tick 零 class 写入）
  if (entry.status !== t.status) {
    const stripes = isActivityStatus(t.status) && FX_STRIPES ? ' progress-animate' : ''
    entry.fill.className = `h-full ${phaseColor} origin-left transition-transform${stripes}`
    entry.status = t.status
  }

  // P2-2：进度条同步无障碍值
  entry.bar.setAttribute('aria-valuenow', String(displayProgress))

  // 更新错误信息（v1.6.1-A1：独立成行，不再挂在 sub 行尾被 truncate 裁掉）
  if (t.errorMsg) {
    entry.errLine.textContent = t.errorMsg
    entry.errLine.title = t.errorMsg
    entry.errLine.style.display = ''
  } else {
    entry.errLine.style.display = 'none'
  }
}

function getPhaseStyle(status: TaskStatus) {
  let phaseColor = 'bg-primary'
  let phaseLabel = ''
  switch (status) {
    case 'UPLOADING':
      phaseColor = 'bg-signal'
      phaseLabel = '上传'
      break
    case 'TRANSCODING':
      phaseColor = 'bg-signal'
      phaseLabel = '转码'
      break
    case 'DOWNLOADING':
      phaseColor = 'bg-signal'
      phaseLabel = '下载'
      break
    case 'COMPLETED':
      phaseColor = 'bg-success'
      phaseLabel = '完成'
      break
    // v1.6.1-A6：补全此前返回空标签/灰条的状态，进度文案不再是裸百分号，
    // ERROR 进度条与红色徽标语义对齐（一处改，主页与任务抽屉同受益）
    case 'QUEUE':
      phaseColor = 'bg-neutral-soft'
      phaseLabel = '排队'
      break
    case 'WAITING_TRANS':
      phaseColor = 'bg-neutral-soft'
      phaseLabel = '待转码'
      break
    case 'WAITING_DOWN':
      phaseColor = 'bg-neutral-soft'
      phaseLabel = '待下载'
      break
    case 'PAUSED':
      phaseColor = 'bg-neutral'
      phaseLabel = '已暂停'
      break
    case 'COOLDOWN':
      phaseColor = 'bg-warning'
      phaseLabel = '冷却'
      break
    case 'CANCELLED':
      phaseColor = 'bg-neutral-soft'
      phaseLabel = '已取消'
      break
    case 'ERROR':
      phaseColor = 'bg-danger'
      phaseLabel = '异常'
      break
    default:
      phaseColor = 'bg-neutral-soft'
      phaseLabel = ''
      break
  }
  return { phaseColor, phaseLabel }
}

// 构建完整卡片（含按钮），返回卡片元素和动态引用
// onSelectionChange：勾选变化时由页面层同步工具栏禁用态/计数（v1.6.1-A3/A4）
function buildCard(t: Task, onSelectionChange?: () => void): CardEntry {
  // v1.6.1-A5/A9：活动卡（上传/转码/下载）左侧静态信号指示线作视觉锚点，
  // hover 环对齐 servers 页既有模式；选中高亮不随 WS 更新（不进 updateCardDynamic）
  const isActive = isActivityStatus(t.status)
  let cardClass = 'card p-3 flex flex-wrap items-center gap-3 transition hover:ring-2 hover:ring-primary/30'
  if (isActive) cardClass += ' border-l-[3px] border-l-signal'
  if (selected.has(t.id)) cardClass += ' task-selected'
  const card = el('div', { class: cardClass, draggable: 'false', 'data-task-id': t.id })

  const dragHandle = el('div', {
    class: 'cursor-grab active:cursor-grabbing text-ink-muted hover:text-ink select-none shrink-0',
    title: '拖动排序',
  })
  dragHandle.append(svgIcon('grip', 16) as unknown as Node)
  dragHandle.addEventListener('mousedown', () => {
    card.draggable = true
  })
  dragHandle.addEventListener('mouseup', () => {
    card.draggable = false
  })

  card.addEventListener('dragstart', (e) => {
    if (!card.draggable) {
      e.preventDefault()
      return
    }
    dragSrcId = t.id
    card.classList.add('opacity-50')
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', t.id)
    }
  })

  card.addEventListener('dragend', () => {
    card.draggable = false
    dragSrcId = null
    card.classList.remove('opacity-50')
    document.querySelectorAll('[data-task-id]').forEach((el) => {
      el.classList.remove('border-t-drop', 'border-drop-line', 'border-b-drop')
    })
  })

  card.addEventListener('dragover', (e) => {
    e.preventDefault()
    if (!dragSrcId || dragSrcId === t.id) return
    if (e.dataTransfer) {
      e.dataTransfer.dropEffect = 'move'
    }
    const rect = card.getBoundingClientRect()
    const midY = rect.top + rect.height / 2
    const isAbove = e.clientY < midY
    card.classList.remove('border-t-drop', 'border-b-drop', 'border-t-0', 'border-b-0')
    if (isAbove) {
      card.classList.add('border-t-drop', 'border-drop-line')
      card.classList.remove('border-b-drop')
    } else {
      card.classList.add('border-b-drop', 'border-drop-line')
      card.classList.remove('border-t-drop')
    }
  })

  card.addEventListener('dragleave', () => {
    card.classList.remove('border-t-drop', 'border-b-drop', 'border-drop-line')
  })

  card.addEventListener('drop', (e) => {
    e.preventDefault()
    card.classList.remove('border-t-drop', 'border-b-drop', 'border-drop-line')
    if (!dragSrcId || dragSrcId === t.id) return

    const rect = card.getBoundingClientRect()
    const midY = rect.top + rect.height / 2
    const insertBefore = e.clientY < midY
    handleReorder(dragSrcId, t.id, insertBefore)
  })

  // 选择框
  const cb = el('input', { type: 'checkbox', class: 'checkbox' }) as HTMLInputElement
  cb.checked = selected.has(t.id)
  cb.onchange = () => {
    if (cb.checked) selected.add(t.id)
    else selected.delete(t.id)
    // v1.6.1-A4：选中态即时反馈（不重渲染、不增加 WS tick 重绘）
    card.classList.toggle('task-selected', cb.checked)
    onSelectionChange?.()
  }

  // 状态徽标（v1.6.2-C2：?fx=breathe 实验开关下活动徽标呼吸，默认关）
  const breathe = FX_BREATHE && isActivityStatus(t.status) ? ' animate-pulse-soft' : ''
  const badge = el('span', { class: `badge ${STATUS_CLASS[t.status]}${breathe}` }, [STATUS_LABEL[t.status]])

  // 主信息
  const info = el('div', { class: 'flex-1 min-w-0' })
  const name = el('div', { class: 'font-medium truncate', title: t.fileName }, [t.fileName])
  // v1.6.1-A1：拆除 truncate(nowrap)+flex-wrap 的自相矛盾，整行换行、子项 shrink-0
  const sub = el('div', { class: 'text-xs text-ink-muted flex items-center gap-2 flex-wrap' }, [
    el('span', { class: 'shrink-0' }, [`→ ${t.outputName}`]),
    // v1.6.1-A8：serverName 改中性低饱和标签，不再与进行中进度条撞 signal 色
    el('span', { class: 'shrink-0 bg-surface-alt px-1.5 py-0.5 rounded text-xs text-ink-muted' }, [t.serverName || '未知服务器']),
    el('span', { class: 'shrink-0' }, [`方案: ${t.profileName || '未知方案'}`]),
    ...taskExtra(t),
    el('span', { class: 'shrink-0' }, [formatTime(t.createdAt)]),
  ])
  // v1.6.1-A1：错误信息独立成行（ERROR 态最关键信息不再被裁切），含 title 兜底
  const errLine = el('div', { class: 'text-xs text-danger truncate mt-1' }, [])
  if (t.errorMsg) {
    errLine.textContent = t.errorMsg
    errLine.title = t.errorMsg
  } else {
    errLine.style.display = 'none'
  }
  info.append(name, sub, errLine)

  // 进度条
  const progWrap = el('div', { class: 'w-full sm:w-40 shrink-0' })
  const displayProgress = t.status === 'COMPLETED' ? 100 : Math.max(0, Math.min(100, t.progress))
  const { phaseColor, phaseLabel } = getPhaseStyle(t.status)
  // v1.6.2-C1：倍速后缀（speed 原样 trim，空串/缺省跳过；默认态由后端 omitempty 不下发）
  const progLabelSpan = el('span', {}, [progressLabel(t, displayProgress, phaseLabel)])
  const speedSpan = el('span', { class: 'text-ink-subtle' }, [speedText(t)])
  const progText = el('div', { class: 'text-xs text-ink-muted mb-0.5 text-right font-mono tabular-nums' }, [
    progLabelSpan, speedSpan,
  ])
  const bar = el('div', {
    class: 'h-1.5 bg-neutral-soft rounded-full overflow-hidden',
    role: 'progressbar',
    'aria-valuemin': '0',
    'aria-valuemax': '100',
    'aria-valuenow': String(displayProgress),
    'aria-label': `${t.fileName} 进度`,
  })
  // v1.6.2-B1：进度走 transform:scaleX(--p)（合成层），WS 推送更新 --p 不再触发 layout+repaint
  const fill = el('div', {
    class: `h-full ${phaseColor} origin-left transition-transform${isActivityStatus(t.status) && FX_STRIPES ? ' progress-animate' : ''}`,
    style: `transform:scaleX(var(--p, ${displayProgress / 100}))`,
  })
  fill.style.setProperty('--p', String(displayProgress / 100))
  bar.appendChild(fill)
  progWrap.append(progText, bar)

  // 操作按钮（v1.6.1-A7：去 w-[200px] 硬宽，终态单按钮不再留大片虚空；卡片 flex-wrap 兜底窄屏换行）
  const actions = el('div', { class: 'flex items-center justify-end gap-1 shrink-0 ml-auto' })
  const showResume = t.status === 'PAUSED'
  const showPause = !showResume && !isTerminal(t.status)

  if (showResume) {
    const resume = el('button', { class: 'btn btn-sm' }, ['继续'])
    resume.onclick = () => doResume(t.id)
    actions.appendChild(resume)
  }
  if (showPause) {
    const pause = el('button', { class: 'btn btn-sm' }, ['暂停'])
    pause.onclick = () => doPause(t.id)
    actions.appendChild(pause)
  }
  // C-09：渲染/代理任务的成品回看与路径复制（01 §4.5「成品回看 / 下载」）
  if ((isRenderTask(t) || isProxyTask(t)) && outputPreviewPath(t)) {
    const view = el('button', { class: 'btn btn-sm', title: '新窗口预览成品（/stream 票据）' }, ['预览'])
    view.onclick = () => void previewOutput(t)
    const copy = el('button', { class: 'btn btn-sm', title: '复制产物路径' }, ['路径'])
    copy.onclick = () => void copyOutputPath(t)
    actions.append(view, copy)
  }
  if (t.status === 'ERROR') {
    const retry = el('button', { class: 'btn btn-sm btn-primary' }, ['重试'])
    retry.onclick = () => doRetry(t.id)
    actions.appendChild(retry)
  }
  if (!isTerminal(t.status)) {
    const cancel = el('button', { class: 'btn btn-sm' }, ['取消'])
    cancel.onclick = () => doCancel(t.id)
    actions.appendChild(cancel)
  } else {
    const del = el('button', { class: 'btn btn-sm btn-danger-ghost' }, ['删除'])
    del.onclick = () => doDelete(t.id)
    actions.appendChild(del)
  }

  card.append(dragHandle, cb, badge, info, progWrap, actions)

  return { status: t.status, card, cb, badge, progText, progLabel: progLabelSpan, speedLabel: speedSpan, fill, bar, errLine, dragHandle }
}

/** 渲染/代理任务的补充信息（06 §2.2）：阶段、同源快速导出、耗时 */
function taskExtra(t: Task): HTMLElement[] {
  if (!isRenderTask(t) && !isProxyTask(t)) return []
  const out: HTMLElement[] = []
  const stage = stageText(t)
  if (stage) out.push(el('span', { class: 'text-signal' }, [stage]))
  if (t.fastCopyAllowed) out.push(el('span', { class: 'text-success' }, ['同源快速导出']))
  const spent = elapsedText(t)
  if (spent) out.push(el('span', {}, [spent]))
  return out
}

function isTerminal(s: TaskStatus): boolean {
  return s === 'COMPLETED' || s === 'CANCELLED'
}

async function handleReorder(srcId: string, targetId: string, insertBefore: boolean) {
  const tasks = store.tasks
  const srcIdx = tasks.findIndex(t => t.id === srcId)
  const targetIdx = tasks.findIndex(t => t.id === targetId)
  if (srcIdx < 0 || targetIdx < 0) return

  const newOrder = [...tasks]
  const [srcTask] = newOrder.splice(srcIdx, 1)
  const newTargetIdx = newOrder.findIndex(t => t.id === targetId)
  const insertIdx = insertBefore ? newTargetIdx : newTargetIdx + 1
  newOrder.splice(insertIdx, 0, srcTask)

  store.tasks = newOrder
  store.notify()

  try {
    await api.reorderTasks(newOrder.map(t => t.id))
  } catch (e) {
    toast('排序失败: ' + (e as Error).message, 'error')
    store.loadTasks()
  }
}

async function doPause(id: string) {
  try {
    await api.pauseTask(id)
    toast('已暂停', 'success')
  } catch (e) {
    toast((e as Error).message, 'error')
  }
}
async function doResume(id: string) {
  try {
    await api.resumeTask(id)
    toast('已继续', 'success')
  } catch (e) {
    toast((e as Error).message, 'error')
  }
}
async function doRetry(id: string) {
  try {
    await api.retryTask(id)
    toast('已重试', 'success')
  } catch (e) {
    toast((e as Error).message, 'error')
  }
}
// P2-2：取消/删除任务走公共 CRUD 四件套（确认弹窗 → api → toast → reload）
const taskCancelActions = crudActions<string>({
  confirmTitle: () => '确定取消该任务？',
  danger: false,
  confirmFor: () => true,
  apiCall: (_action, id) => api.cancelTask(id),
  reload: () => store.loadTasks(),
  successMsg: () => '已取消',
})
const taskDeleteActions = crudActions<string>({
  confirmTitle: () => '确定删除该任务记录？',
  danger: false,
  apiCall: async (_action, id) => {
    await api.deleteTask(id)
    selected.delete(id)
  },
  reload: () => store.loadTasks(),
  successMsg: () => '已删除',
})

async function doCancel(id: string) {
  void taskCancelActions.update(id)
}
async function doDelete(id: string) {
  void taskDeleteActions.remove(id)
}

async function batchOp(op: 'pause' | 'resume' | 'cancel') {
  if (selected.size === 0) {
    toast('请先选择任务')
    return
  }
  const fn = op === 'pause' ? api.pauseTask : op === 'resume' ? api.resumeTask : api.cancelTask
  const opLabel = op === 'pause' ? '暂停' : op === 'resume' ? '继续' : '取消'
  // P2-1：批量操作提供逐项结果（成功 N、失败 M 及原因）
  let ok = 0
  const failures: { name: string; reason: string }[] = []
  const taskList = store.tasks
  for (const id of [...selected]) {
    try {
      await fn(id)
      ok++
    } catch (e) {
      const t = taskList.find((x) => x.id === id)
      failures.push({ name: t?.fileName ?? id, reason: (e as Error).message })
    }
  }
  showBatchResult(opLabel, ok, failures)
  selected.clear()
  store.loadTasks()
}
