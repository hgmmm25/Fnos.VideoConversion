// C-02：剪辑页三段布局（01 §4.1）
// 顶栏 | 中部：素材库(320px 可折叠) + 预览器(自适应) | 底部：工具条(紧贴时间线) + 时间线(160px 可拖拽 96~320px)
// 尺寸比例持久化到 localStorage，键名 wve.layout.v1；P1-5：支持布局预设一键切换
// （默认 / 紧凑时间线 / 大预览，切换即持久化；手动拖拽后降级为自定义）
import { el, svgIcon } from '../../ui'

export interface EditorLayout {
  root: HTMLElement
  /** 顶栏左侧（项目名之后）的扩展位 */
  topLeft: HTMLElement
  /** 顶栏右侧（渲染导出之前）的扩展位 */
  topRight: HTMLElement
  assets: HTMLElement
  preview: HTMLElement
  timeline: HTMLElement
  toolbar: HTMLElement
  setSaveState(text: string, kind?: 'ok' | 'warn' | 'busy'): void
  setNodeStatus(text: string, kind?: 'ok' | 'bad' | 'muted'): void
  setAssetsCollapsed(collapsed: boolean): void
  destroy(): void
}

export interface LayoutOptions {
  projectName: string
  projectId: string
  onBack: () => void
  onOpenTasks: () => void
  onRender: () => void
  onAssetsToggle?: (collapsed: boolean) => void
}

interface LayoutPersist {
  assetsWidth: number
  assetsCollapsed: boolean
  timelineHeight: number
  /** P1-5：工作区布局预设（default / compact / preview / custom） */
  presetId: LayoutPresetId
}

/** P1-5：工作区布局预设（一键切换并持久化；拖拽尺寸后自动降级为 custom） */
type LayoutPresetId = 'default' | 'compact' | 'preview' | 'custom'
interface LayoutPreset {
  label: string
  assetsWidth: number
  timelineHeight: number
}
const LAYOUT_PRESETS: Record<Exclude<LayoutPresetId, 'custom'>, LayoutPreset> = {
  default: { label: '默认布局', assetsWidth: 320, timelineHeight: 160 },
  compact: { label: '紧凑时间线', assetsWidth: 280, timelineHeight: 108 },
  preview: { label: '大预览', assetsWidth: 240, timelineHeight: 96 },
}

const KEY = 'wve.layout.v1'
const DEFAULTS: LayoutPersist = { assetsWidth: 320, assetsCollapsed: false, timelineHeight: 160, presetId: 'default' }
const ASSETS_MIN = 240
const ASSETS_MAX = 480
const TL_MIN = 96
const TL_MAX = 320

function clamp(n: number, lo: number, hi: number): number {
  return Math.max(lo, Math.min(hi, n))
}

function loadLayout(): LayoutPersist {
  try {
    const raw = localStorage.getItem(KEY)
    if (!raw) return { ...DEFAULTS }
    const v = JSON.parse(raw) as Partial<LayoutPersist>
    const presetId: LayoutPresetId =
      v.presetId === 'default' || v.presetId === 'compact' || v.presetId === 'preview' || v.presetId === 'custom'
        ? v.presetId
        : 'custom'
    return {
      assetsWidth: clamp(Number(v.assetsWidth) || DEFAULTS.assetsWidth, ASSETS_MIN, ASSETS_MAX),
      assetsCollapsed: !!v.assetsCollapsed,
      timelineHeight: clamp(
        Number(v.timelineHeight) || DEFAULTS.timelineHeight,
        TL_MIN,
        TL_MAX
      ),
      presetId,
    }
  } catch {
    return { ...DEFAULTS }
  }
}

function saveLayout(v: LayoutPersist) {
  try {
    localStorage.setItem(KEY, JSON.stringify(v))
  } catch {
    /* 隐私模式或配额不足时静默降级 */
  }
}

export function buildEditorLayout(opts: LayoutOptions): EditorLayout {
  const persist = loadLayout()

  // ===== 顶栏 =====
  const backBtn = el('button', { class: 'btn btn-sm flex items-center', title: '返回' })
  backBtn.append(svgIcon('back', 16) as unknown as Node)
  backBtn.onclick = () => opts.onBack()

  const nameEl = el('div', { class: 'font-semibold text-sm truncate max-w-[40vw]' }, [
    opts.projectName || '未命名项目',
  ])
  const idEl = el('span', { class: 'text-xs text-ink-muted hidden sm:inline' }, [opts.projectId])

  const saveStateEl = el('span', { class: 'text-xs text-ink-muted px-1' }, ['—'])

  const topLeft = el('div', { class: 'flex items-center gap-1' })
  const topRight = el('div', { class: 'flex items-center gap-2' })

  // P1-5：工作区布局预设切换（默认 / 紧凑时间线 / 大预览；切换即持久化到 wve.layout.v1）
  const presetSel = el('select', {
    class: 'input text-xs h-7 w-28 shrink-0 hidden sm:block',
    title: '工作区布局预设（素材/预览/时间线比例）',
  }) as HTMLSelectElement
  presetSel.append(
    el('option', { value: 'default' }, ['默认布局']),
    el('option', { value: 'compact' }, ['紧凑时间线']),
    el('option', { value: 'preview' }, ['大预览']),
    el('option', { value: 'custom' }, ['自定义'])
  )
  presetSel.value = persist.presetId

  const nodeEl = el('span', { class: 'text-xs text-ink-muted hidden md:inline' }, ['节点 —'])

  const assetsBtn = el('button', { class: 'btn btn-sm flex items-center gap-1', title: '素材库开关' })
  assetsBtn.append(svgIcon('folder', 16) as unknown as Node, el('span', { class: 'hidden lg:inline' }, ['素材库']))
  assetsBtn.onclick = () => setAssetsCollapsed(!persist.assetsCollapsed)

  const tasksBtn = el('button', { class: 'btn btn-sm flex items-center gap-1', title: '任务中心' })
  tasksBtn.append(svgIcon('list', 16) as unknown as Node, el('span', { class: 'hidden lg:inline' }, ['任务中心']))
  tasksBtn.onclick = () => opts.onOpenTasks()

  const renderBtn = el('button', { class: 'btn btn-sm btn-primary flex items-center gap-1' }, [
    '渲染导出',
  ])
  renderBtn.onclick = () => opts.onRender()

  const top = el('div', {
    class: 'flex items-center gap-2 px-3 h-11 bg-surface border-b border-line shrink-0',
  })
  top.append(backBtn, nameEl, idEl, saveStateEl, topLeft, el('div', { class: 'flex-1' }), presetSel, topRight, nodeEl, assetsBtn, tasksBtn, renderBtn)

  // ===== 中部：素材库 + 预览器 =====
  const assets = el('div', {
    class: 'shrink-0 border-r border-line bg-surface overflow-auto',
  })
  const assetsResizer = el('div', {
    class: 'w-1 shrink-0 cursor-col-resize hover:bg-primary',
    title: '拖拽调整素材库宽度',
  })
  const preview = el('div', { class: 'flex-1 min-w-0 flex flex-col overflow-hidden' })
  // P0-1：预览区底色收敛为主题令牌（--c-preview-bg，亮/暗主题下恒定为深色）
  preview.style.backgroundColor = 'rgb(var(--c-preview-bg))'

  const mid = el('div', { class: 'flex-1 flex min-h-0' })
  mid.append(assets, assetsResizer, preview)

  // ===== 底部：工具条 + 时间线（P1-5：工具条紧贴时间线——去掉中间分隔线，以背景明度差分组） =====
  const tlHandle = el('div', { class: 'h-1 shrink-0 cursor-row-resize hover:bg-primary', title: '拖拽调整时间线高度' })
  const toolbar = el('div', {
    class: 'h-9 shrink-0 flex items-center gap-2 px-3',
  })
  // P1-5：时间线用内嵌面（surface-alt）承载，与上层工具栏（surface）以明度差形成视觉分组
  const timeline = el('div', { class: 'overflow-x-auto overflow-y-hidden relative bg-surface-alt' })
  const bottom = el('div', { class: 'shrink-0 flex flex-col border-t border-line bg-surface' })
  bottom.append(tlHandle, toolbar, timeline)

  const root = el('div', { class: 'h-full flex flex-col min-h-0' })
  root.append(top, mid, bottom)

  // ===== 尺寸应用 =====
  function applyAssets() {
    if (persist.assetsCollapsed) {
      assets.style.width = '0px'
      assets.style.borderRightWidth = '0px'
      assets.style.display = 'none'
      assetsResizer.style.display = 'none'
    } else {
      assets.style.display = ''
      assets.style.borderRightWidth = ''
      assets.style.width = persist.assetsWidth + 'px'
      assetsResizer.style.display = ''
    }
    assetsBtn.classList.toggle('bg-primary', !persist.assetsCollapsed)
    assetsBtn.classList.toggle('text-white', !persist.assetsCollapsed)
  }
  function applyTimeline() {
    timeline.style.height = persist.timelineHeight + 'px'
    bottom.style.height = persist.timelineHeight + 4 + 36 + 'px'
  }
  applyAssets()
  applyTimeline()

  // P1-5：一键套用布局预设并持久化（不改变素材库折叠态）
  function applyPreset(id: Exclude<LayoutPresetId, 'custom'>) {
    const p = LAYOUT_PRESETS[id]
    persist.assetsWidth = p.assetsWidth
    persist.timelineHeight = p.timelineHeight
    persist.presetId = id
    presetSel.value = id
    applyAssets()
    applyTimeline()
    saveLayout(persist)
  }
  // P1-5：用户手动拖拽调整尺寸 → 预设降级为「自定义」
  function markCustom() {
    if (persist.presetId === 'custom') return
    persist.presetId = 'custom'
    presetSel.value = 'custom'
  }
  presetSel.onchange = () => {
    const v = presetSel.value
    if (v === 'default' || v === 'compact' || v === 'preview') applyPreset(v)
    else presetSel.value = persist.presetId // 选中「自定义」无操作，回显当前预设
  }

  function setAssetsCollapsed(collapsed: boolean) {
    persist.assetsCollapsed = collapsed
    saveLayout(persist)
    applyAssets()
    opts.onAssetsToggle?.(collapsed)
  }

  // ===== 拖拽 =====
  /**
   * 修复⑥：拖拽位移重复累加 + 无节流
   * - 原实现把「距起点的累计位移」叠加到「已被上一次 move 改写过的持久值」上，位移被平方放大；
   *   现改为 pointerdown 时取一次基准快照（begin），每次 move 都用 base + 累计位移重新计算。
   * - pointermove 用 requestAnimationFrame 节流，避免高频重排。
   */
  function bindDrag<T>(
    handle: HTMLElement,
    begin: () => T,
    onMove: (dx: number, dy: number, base: T) => void
  ) {
    handle.addEventListener('pointerdown', (e) => {
      e.preventDefault()
      const start = { x: e.clientX, y: e.clientY }
      const base = begin()
      let raf = 0
      let last: { dx: number; dy: number } | null = null
      handle.setPointerCapture(e.pointerId)
      document.body.style.userSelect = 'none'
      const flush = () => {
        raf = 0
        if (last) onMove(last.dx, last.dy, base)
      }
      const move = (ev: PointerEvent) => {
        last = { dx: ev.clientX - start.x, dy: ev.clientY - start.y }
        if (!raf) raf = requestAnimationFrame(flush)
      }
      const up = () => {
        if (raf) cancelAnimationFrame(raf)
        raf = 0
        flush()
        document.body.style.userSelect = ''
        handle.removeEventListener('pointermove', move)
        handle.removeEventListener('pointerup', up)
        handle.removeEventListener('pointercancel', up)
        saveLayout(persist)
      }
      handle.addEventListener('pointermove', move)
      handle.addEventListener('pointerup', up)
      handle.addEventListener('pointercancel', up)
    })
  }

  bindDrag(
    assetsResizer,
    () => persist.assetsWidth,
    (dx, _dy, base) => {
      markCustom()
      persist.assetsWidth = clamp(base + dx, ASSETS_MIN, ASSETS_MAX)
      applyAssets()
    }
  )
  bindDrag(
    tlHandle,
    () => persist.timelineHeight,
    (_dx, dy, base) => {
      markCustom()
      persist.timelineHeight = clamp(base - dy, TL_MIN, TL_MAX)
      applyTimeline()
    }
  )

  // ===== 顶栏状态位 =====
  function setSaveState(text: string, kind: 'ok' | 'warn' | 'busy' = 'ok') {
    saveStateEl.textContent = text
    saveStateEl.className =
      'text-xs px-1 ' +
      (kind === 'warn' ? 'text-danger' : kind === 'busy' ? 'text-ink-muted' : 'text-success')
  }
  function setNodeStatus(text: string, kind: 'ok' | 'bad' | 'muted' = 'muted') {
    nodeEl.textContent = text
    nodeEl.className =
      'text-xs hidden md:inline ' +
      (kind === 'ok' ? 'text-success' : kind === 'bad' ? 'text-danger' : 'text-ink-muted')
  }

  return {
    root,
    topLeft,
    topRight,
    assets,
    preview,
    timeline,
    toolbar,
    setSaveState,
    setNodeStatus,
    setAssetsCollapsed,
    destroy() {
      root.remove()
    },
  }
}
