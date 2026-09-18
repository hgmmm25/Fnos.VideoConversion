import { store } from '../store'
import { api } from '../api'
import { el, toast, confirmDialog, formatTime, emptyState, svgIcon, showBatchResult } from '../ui'
import { type Task, type TaskStatus, STATUS_LABEL, STATUS_CLASS } from '../types'
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

let dragSrcId: string | null = null

interface CardEntry {
  status: TaskStatus
  card: HTMLElement
  cb: HTMLInputElement
  badge: HTMLElement
  progText: HTMLElement
  fill: HTMLElement
  bar: HTMLElement
  errorMsg: HTMLElement
  dragHandle: HTMLElement
}

export function renderTasks(container: HTMLElement) {
  const wrap = el('div', { class: 'flex flex-col h-full' })

  // 工具栏
  const toolbar = el('div', {
    class: 'flex flex-wrap items-center gap-2 px-3 sm:px-4 py-2 border-b border-line bg-surface',
  })
  const refresh = el('button', { class: 'btn btn-sm flex items-center gap-1.5' }, [])
  refresh.append(svgIcon('refresh', 14) as unknown as Node, el('span', {}, ['刷新']))

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
  const stats = el('span', { class: 'ml-auto text-xs text-ink-muted' }, [])
  toolbar.append(selectAllBtn, refresh, batchPause, batchResume, batchCancel, stats)

  const listWrap = el('div', { class: 'flex-1 overflow-auto p-4' })
  wrap.append(toolbar, listWrap)
  container.appendChild(wrap)

  // 卡片缓存：taskID -> 卡片元素及动态引用
  const cardMap = new Map<string, CardEntry>()

  // full=true 时强制全量重建（用于全选/删除等结构性变化）
  const render = (full = false) => {
    const tasks = store.tasks
    stats.textContent = `共 ${tasks.length} 个任务，已选 ${selected.size} 个`
    const isAllSelected = tasks.length > 0 && tasks.every(t => selected.has(t.id))
    selectAllBtn.className = isAllSelected
      ? 'btn btn-sm flex items-center gap-1.5 bg-primary text-white'
      : 'btn btn-sm flex items-center gap-1.5'

    if (tasks.length === 0) {
      listWrap.innerHTML = ''
      cardMap.clear()
      // P2-1：空状态承载"下一步动作"
      listWrap.appendChild(
        emptyState('暂无任务，请到「视频文件」页创建转码任务', 'list', {
          label: '去视频文件页创建',
          onClick: () => {
            location.hash = '#/scanner'
          },
        })
      )
      return
    }

    // 确保列表容器存在
    let list = listWrap.querySelector('[data-task-list]') as HTMLElement | null
    if (!list) {
      listWrap.innerHTML = ''
      cardMap.clear()
      list = el('div', { class: 'space-y-2', 'data-task-list': '' })
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
        const entry = buildCard(t)
        if (existing) {
          existing.card.replaceWith(entry.card)
        } else {
          list.appendChild(entry.card)
        }
        cardMap.set(t.id, entry)
      }
    }

    // 按新顺序重排 DOM（拖拽排序后需要）
    for (const t of tasks) {
      const entry = cardMap.get(t.id)
      if (entry) {
        list.appendChild(entry.card)
      }
    }
  }

  // C-09：返回退订句柄（任务抽屉复用本列表，关闭抽屉不销毁，仅真正卸载时才退订，避免重复订阅）
  const unsubscribe = store.subscribe(render)
  render()
  return unsubscribe
}

// 更新卡片中动态变化的部分（进度、选中态、错误信息），不触碰按钮
function updateCardDynamic(entry: CardEntry, t: Task) {
  entry.cb.checked = selected.has(t.id)

  // 更新进度文本和进度条
  const displayProgress = t.status === 'COMPLETED' ? 100 : Math.max(0, Math.min(100, t.progress))
  const { phaseLabel } = getPhaseStyle(t.status)
  // 渲染/代理任务优先展示阶段与段序号（如「分段渲染 2/4 · 63%」，06 §2.2）
  entry.progText.textContent = progressLabel(t, displayProgress, phaseLabel)
  entry.fill.style.width = `${displayProgress}%`

  // 更新进度条颜色
  const { phaseColor } = getPhaseStyle(t.status)
  entry.fill.className = `h-full ${phaseColor} transition-all`

  // P2-2：进度条同步无障碍值
  entry.bar.setAttribute('aria-valuenow', String(displayProgress))

  // 更新错误信息
  if (t.errorMsg) {
    entry.errorMsg.textContent = `| ${t.errorMsg}`
    entry.errorMsg.style.display = ''
  } else {
    entry.errorMsg.style.display = 'none'
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
    default:
      phaseColor = 'bg-neutral-soft'
      phaseLabel = ''
      break
  }
  return { phaseColor, phaseLabel }
}

// 构建完整卡片（含按钮），返回卡片元素和动态引用
function buildCard(t: Task): CardEntry {
  const card = el('div', { class: 'card p-3 flex flex-wrap items-center gap-3', draggable: 'false', 'data-task-id': t.id })

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
  const cb = el('input', { type: 'checkbox', class: 'rounded' }) as HTMLInputElement
  cb.checked = selected.has(t.id)
  cb.onchange = () => {
    if (cb.checked) selected.add(t.id)
    else selected.delete(t.id)
  }

  // 状态徽标
  const badge = el('span', { class: `badge ${STATUS_CLASS[t.status]}` }, [STATUS_LABEL[t.status]])

  // 主信息
  const info = el('div', { class: 'flex-1 min-w-0' })
  const name = el('div', { class: 'font-medium truncate' }, [t.fileName])
  const sub = el('div', { class: 'text-xs text-ink-muted truncate flex items-center gap-2 flex-wrap' }, [
    el('span', {}, [`→ ${t.outputName}`]),
    el('span', { class: 'text-signal' }, [`[${t.serverName || '未知服务器'}]`]),
    el('span', {}, [`方案: ${t.profileName || '未知方案'}`]),
    ...taskExtra(t),
    el('span', {}, [formatTime(t.createdAt)]),
  ])
  const errorMsg = el('span', { class: 'text-danger' }, [])
  if (t.errorMsg) {
    errorMsg.textContent = `| ${t.errorMsg}`
  } else {
    errorMsg.style.display = 'none'
  }
  sub.appendChild(errorMsg)
  info.append(name, sub)

  // 进度条
  const progWrap = el('div', { class: 'w-full sm:w-40 shrink-0' })
  const displayProgress = t.status === 'COMPLETED' ? 100 : Math.max(0, Math.min(100, t.progress))
  const { phaseColor, phaseLabel } = getPhaseStyle(t.status)
  const progText = el('div', { class: 'text-xs text-ink-muted mb-0.5 text-right font-mono tabular-nums' }, [
    progressLabel(t, displayProgress, phaseLabel),
  ])
  const bar = el('div', {
    class: 'h-1.5 bg-neutral-soft rounded-full overflow-hidden',
    role: 'progressbar',
    'aria-valuemin': '0',
    'aria-valuemax': '100',
    'aria-valuenow': String(displayProgress),
    'aria-label': `${t.fileName} 进度`,
  })
  const fill = el('div', {
    class: `h-full ${phaseColor} transition-all`,
    style: `width:${displayProgress}%`,
  })
  bar.appendChild(fill)
  progWrap.append(progText, bar)

  // 操作按钮
  const actions = el('div', { class: 'flex items-center justify-end gap-1 shrink-0 ml-auto w-[200px]' })
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
    const del = el('button', { class: 'btn btn-sm btn-danger' }, ['删除'])
    del.onclick = () => doDelete(t.id)
    actions.appendChild(del)
  }

  card.append(dragHandle, cb, badge, info, progWrap, actions)

  return { status: t.status, card, cb, badge, progText, fill, bar, errorMsg, dragHandle }
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
async function doCancel(id: string) {
  if (!(await confirmDialog('确定取消该任务？'))) return
  try {
    await api.cancelTask(id)
    toast('已取消', 'success')
  } catch (e) {
    toast((e as Error).message, 'error')
  }
}
async function doDelete(id: string) {
  if (!(await confirmDialog('确定删除该任务记录？'))) return
  try {
    await api.deleteTask(id)
    selected.delete(id)
    store.loadTasks()
    toast('已删除', 'success')
  } catch (e) {
    toast((e as Error).message, 'error')
  }
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
